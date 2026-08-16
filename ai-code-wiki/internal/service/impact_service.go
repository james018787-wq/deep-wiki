package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"ai-code-wiki/internal/config"
	"ai-code-wiki/internal/model"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/astgo"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/git"
	"ai-code-wiki/pkg/logger"

	"gorm.io/gorm"
)

// 影响点类型。
const (
	ImpactKindChanged int8 = 1 // 直接修改的函数
	ImpactKindReverse int8 = 2 // 上游调用方（被改动波及，谁调用了我）
	ImpactKindForward int8 = 3 // 下游被调用方（改动牵连，我调用了谁）
)

// 影响分析方向过滤。
const (
	ImpactDirectionBoth       = "both"       // 默认：上游+下游
	ImpactDirectionUpstream   = "upstream"   // 仅上游（谁调用我）
	ImpactDirectionDownstream = "downstream" // 仅下游（我调用谁）
)

// impactMaxQuerySeeds 自然语言定位的变更函数种子数量上限（含关键字兜底）。
const impactMaxQuerySeeds = 8

// impactRAGSeedLimit 向量召回直接作为变更种子的数量上限。
// 影响分析要求"直接修改"集合收敛，取最相关的少量候选，避免跨模块关联函数卷入变更集合。
const impactRAGSeedLimit = 4

// FuncRef 影响分析中的函数引用（模块+函数+文件）。
type FuncRef struct {
	Module  string `json:"module"`  // 业务模块
	Func    string `json:"func"`    // 函数名
	File    string `json:"file"`    // 文件路径（部分场景未知）
	Depth   int    `json:"depth"`   // 传播深度（0=直接修改）
	Kind    int8   `json:"kind"`    // 影响点类型：1直接修改 2上游调用方 3下游被调用方
	Edge    string `json:"edge"`    // 传播边说明，如 "order.CreateOrder -> user.GetUser"
	Summary string `json:"summary"` // 关联文档摘要（Wiki 检索到则返回，否则空）
}

// ImpactAnalyzeReq 迭代影响分析入参：分支 / 显式函数 / 自然语言描述，三选一。
type ImpactAnalyzeReq struct {
	RepoID    int64     `json:"repo_id" binding:"required"` // 所属仓库id
	Branch    string    `json:"branch"`                     // 分支：自动 diff 推导变更函数（对比默认分支）
	Functions []FuncRef `json:"functions"`                  // 显式指定变更函数（与 branch/query 二选一）
	Query     string    `json:"query"`                      // 自然语言变更描述（RAG 定位到变更函数，支持多轮追问）
	SessionID string    `json:"session_id"`                 // 会话id：携带时与上一次影响结果合并（多轮追问）
	Direction string    `json:"direction"`                  // 方向过滤：both/upstream/downstream（默认 both）
	MaxDepth  int       `json:"max_depth"`                  // 传播深度（默认2，<=0 取默认）
	Version   string    `json:"version"`                    // 迭代版本号（发布版本/分支名，写入 code_change_log.version）
}

// ImpactAnalyzeResult 影响分析结果。
type ImpactAnalyzeResult struct {
	RepoID      int64               `json:"repo_id"`
	Changed     []*FuncRef          `json:"changed"`        // 直接修改
	Reverse     []*FuncRef          `json:"reverse_impact"` // 上游调用方（按深度升序）
	Forward     []*FuncRef          `json:"forward_impact"` // 下游被调用方（按深度升序）
	DesignDoc   *ImpactDesignDoc    `json:"design_doc"`     // 迭代开发设计文档初稿（LLM 合成）
	FuncChanges []*ImpactFuncChange `json:"func_changes"`   // 每个被修改函数的个性化变更记录（LLM 合成）
	UsedModel   string              `json:"used_model"`     // 合成设计文档实际使用的模型
	Cost        float64             `json:"cost"`           // 本次 LLM 合成调用估算成本（元）
}

// ImpactDesignDoc 迭代影响分析产出的开发设计文档初稿。
type ImpactDesignDoc struct {
	ChangeSummary  string `json:"change_summary"`  // 本次迭代变更摘要
	BusinessImpact string `json:"business_impact"` // 业务影响范围（含受影响模块与函数）
	Attention      string `json:"attention"`       // 上线注意事项与回归测试建议
}

// ImpactFuncChange 单个被修改函数的个性化变更记录（写入 code_change_log，每篇文档各不相同）。
type ImpactFuncChange struct {
	Module         string `json:"module"`          // 模块
	Func           string `json:"func"`            // 函数名
	ChangeSummary  string `json:"change_summary"`  // 该函数本次改动说明
	BusinessImpact string `json:"business_impact"` // 该函数改动影响范围
	Attention      string `json:"attention"`       // 该函数改动上线注意事项
}

// ImpactService 迭代影响分析：基于函数级调用边做反向/正向 BFS 传播。
// 输入本次迭代的变更函数（分支 diff / 显式指定 / 自然语言 RAG 定位），
// 输出受影响的上游调用链与下游依赖，并由 LLM 合成开发设计文档初稿，沉淀到 code_change_log。
// 支持多轮追问（SessionID 累积变更种子集合后重新传播）。
type ImpactService struct {
	db           *gorm.DB
	docRepo      *repo.CodeFunctionDocRepo
	repoRepo     *repo.CodeRepoRepo
	callEdgeRepo *repo.CallEdgeRepo
	changeLog    *repo.CodeChangeLogRepo
	search       *SearchService // RAG 检索（自然语言变更描述定位函数）
	gitCfg       *config.GitConfig
	llmBaseURL   string        // Python LLM 服务地址（合成设计文档）
	chatTimeout  time.Duration // 合成调用超时（LLM_TIMEOUT，默认 60s）

	mu       sync.Mutex                // 会话存储并发保护
	sessions map[string]*impactSession // 多轮追问会话（session_id -> 上一次影响结果）
}

// impactSession 单次会话的影响结果缓存（含过期时间，懒清理）。
type impactSession struct {
	result   *ImpactAnalyzeResult
	expireAt time.Time
}

// impactSessionTTL 会话缓存有效期。
const impactSessionTTL = time.Hour

// NewImpactService 构建影响分析服务。
func NewImpactService(db *gorm.DB, cfg *config.Config, search *SearchService) *ImpactService {
	return &ImpactService{
		db:           db,
		docRepo:      newDocRepo(db),
		repoRepo:     repo.NewCodeRepoRepo(db),
		callEdgeRepo: repo.NewCallEdgeRepo(db),
		changeLog:    repo.NewCodeChangeLogRepo(db),
		search:       search,
		gitCfg:       &cfg.Git,
		llmBaseURL:   cfg.LLM.BaseURL,
		chatTimeout:  llmCallTimeout(cfg.LLM.Timeout, defaultLLMTimeoutSec),
		sessions:     make(map[string]*impactSession),
	}
}

// Analyze 执行影响分析：
//  1. 解析变更函数种子：branch diff / 显式 functions / 自然语言 RAG 定位（多轮追问合并会话上下文）；
//  2. 构建调用图，双向 BFS 传播（按 direction 过滤）；
//  3. LLM 合成设计文档初稿，沉淀 code_change_log。
func (s *ImpactService) Analyze(ctx context.Context, req *ImpactAnalyzeReq) (*ImpactAnalyzeResult, error) {
	if req.MaxDepth <= 0 {
		req.MaxDepth = 2
	}
	if strings.TrimSpace(req.Direction) == "" {
		req.Direction = ImpactDirectionBoth
	}
	switch req.Direction {
	case ImpactDirectionBoth, ImpactDirectionUpstream, ImpactDirectionDownstream:
	default:
		return nil, common.NewError(common.CodeBadRequest, "direction 仅支持 both/upstream/downstream")
	}

	// 变更种子来源互斥校验：branch / functions / query 三者只能选一
	mode := 0
	if strings.TrimSpace(req.Branch) != "" {
		mode++
	}
	if len(req.Functions) > 0 {
		mode++
	}
	if strings.TrimSpace(req.Query) != "" {
		mode++
	}
	if mode > 1 {
		return nil, common.NewError(common.CodeBadRequest, "branch、functions、query 只能三选一")
	}

	var seeds []*FuncRef
	var err error
	switch {
	case strings.TrimSpace(req.Branch) != "":
		seeds, err = s.deriveFuncsFromBranch(req.RepoID, strings.TrimSpace(req.Branch))
	case strings.TrimSpace(req.Query) != "":
		seeds, err = s.locateFuncsByQuery(ctx, req.RepoID, strings.TrimSpace(req.Query))
	case len(req.Functions) > 0:
		for i := range req.Functions {
			seeds = append(seeds, &req.Functions[i])
		}
	}
	if err != nil {
		return nil, err
	}
	if len(seeds) == 0 {
		return nil, common.NewError(common.CodeBadRequest, "未定位到任何变更函数，请补充更具体的变更描述")
	}

	// 多轮追问：会话已有种子时合并（并在响应中标注会话变更集合）
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID != "" {
		prev := s.getSession(sessionID)
		if prev != nil {
			seeds = mergeFuncSeeds(prev.Changed, seeds)
		}
	}

	result, err := s.propagate(req.RepoID, seeds, req.MaxDepth, req.Direction)
	if err != nil {
		return nil, err
	}
	if sessionID != "" {
		s.putSession(sessionID, result)
	}

	// LLM 合成开发设计文档初稿 + 沉淀 code_change_log
	if err := s.synthesizeDesignDoc(ctx, req, result); err != nil {
		return nil, err
	}
	return result, nil
}

// getSession 读取会话缓存，过期记录自动清除。
func (s *ImpactService) getSession(id string) *ImpactAnalyzeResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.sessions { // 懒清理过期会话
		if now.After(v.expireAt) {
			delete(s.sessions, k)
		}
	}
	if e, ok := s.sessions[id]; ok && now.Before(e.expireAt) {
		return e.result
	}
	return nil
}

// putSession 写入会话缓存。
func (s *ImpactService) putSession(id string, result *ImpactAnalyzeResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = &impactSession{result: result, expireAt: time.Now().Add(impactSessionTTL)}
}

// mergeFuncSeeds 合并变更函数种子（按 模块.函数 去重，保留首次出现的文件信息）。
func mergeFuncSeeds(prev, next []*FuncRef) []*FuncRef {
	seen := make(map[string]struct{})
	var merged []*FuncRef
	for _, list := range [][]*FuncRef{prev, next} {
		for _, f := range list {
			key := funcKey(f.Module, f.Func)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, f)
		}
	}
	return merged
}

// locateFuncsByQuery 自然语言变更描述定位变更函数：
//  1. 优先向量召回【不做跨模块扩充】的直接相关文档（按仓库隔离）；
//  2. 向量不可用或未召回时，退化为 MySQL 关键字模糊匹配；
//  3. 结果映射为影响传播的变更种子（上限 impactMaxQuerySeeds）。
func (s *ImpactService) locateFuncsByQuery(ctx context.Context, repoID int64, query string) ([]*FuncRef, error) {
	docs, ragErr := s.search.RetrieveTargetDocs(ctx, repoID, query)
	if ragErr == nil && len(docs) > 0 {
		return funcsFromDocs(docs, impactRAGSeedLimit), nil
	}
	if ragErr != nil {
		logger.Warn(ctx, "向量定位变更函数失败，退化关键字匹配: %v", ragErr)
	}

	kwDocs, err := s.docRepo.SearchByKeyword(repoID, query, impactMaxQuerySeeds)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "定位变更函数失败", err)
	}
	return funcsFromDocs(kwDocs, impactMaxQuerySeeds), nil
}

// funcsFromDocs 将文档列表映射为变更函数种子（去重，带上文件与摘要）。
func funcsFromDocs(docs []*model.CodeFunctionDoc, limit int) []*FuncRef {
	var seeds []*FuncRef
	seen := make(map[string]struct{})
	for _, d := range docs {
		if d == nil {
			continue
		}
		key := funcKey(d.ModuleName, d.FuncName)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		seeds = append(seeds, &FuncRef{
			Module:  d.ModuleName,
			Func:    d.FuncName,
			File:    d.FilePath,
			Summary: d.Summary,
		})
		if limit > 0 && len(seeds) >= limit {
			break
		}
	}
	return seeds
}

// deriveFuncsFromBranch 从任务分支 diff 推导本次迭代变更的函数（对比默认分支）。
// 读取 diff 变更的 .go 文件并 AST 解析出函数名，作为影响传播的种子。
func (s *ImpactService) deriveFuncsFromBranch(repoID int64, branch string) ([]*FuncRef, error) {
	repoInfo, err := s.repoRepo.GetByID(repoID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError(common.CodeBadRequest, "仓库不存在，请先注册仓库")
		}
		return nil, common.WrapError(common.CodeInternalError, "查询仓库失败", err)
	}

	cloneDir := strings.TrimRight(s.gitCfg.CloneDir, "/") + "/" + repoInfo.RepoName
	if strings.TrimSpace(repoInfo.RepoURL) == "" {
		return nil, common.NewError(common.CodeInternalError, "仓库克隆地址未配置")
	}
	if err := git.CloneOrPull(repoInfo.RepoURL, branch, cloneDir); err != nil {
		return nil, common.WrapError(common.CodeInternalError, "拉取代码失败", err)
	}

	files, err := git.GetDiffFiles(cloneDir, "origin/"+repoInfo.DefaultBranch, "origin/"+branch)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "获取变更文件失败", err)
	}

	var seeds []*FuncRef
	seen := make(map[string]struct{})
	for _, file := range files {
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		content, err := git.ReadFile(cloneDir, file)
		if err != nil {
			logger.Warn(context.Background(), "读取变更文件失败 %s: %v", file, err)
			continue
		}
		items, err := astgo.ParseGoFile(content)
		if err != nil {
			logger.Warn(context.Background(), "解析变更文件失败 %s: %v", file, err)
			continue
		}
		module := moduleNameFromPath(file)
		for _, it := range items {
			key := module + "." + it.FuncName
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			seeds = append(seeds, &FuncRef{Module: module, Func: it.FuncName, File: file, Kind: ImpactKindChanged})
		}
	}
	return seeds, nil
}

// propagate 构建调用图后做双向 BFS 传播。
// 反向（上游）：从变更函数出发，沿"谁调用了我"传播；
// 正向（下游）：沿"我调用了谁"传播。最多传播 maxDepth 层。
// direction 控制输出范围：both 返回双向，upstream/downstream 仅返回对应方向。
func (s *ImpactService) propagate(repoID int64, seeds []*FuncRef, maxDepth int, direction string) (*ImpactAnalyzeResult, error) {
	edges, err := s.callEdgeRepo.ListByRepo(repoID)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询调用边失败", err)
	}
	g := buildCallGraph(edges)

	result := &ImpactAnalyzeResult{RepoID: repoID, Changed: []*FuncRef{}, Reverse: []*FuncRef{}, Forward: []*FuncRef{}}
	seedSeen := make(map[string]struct{})
	for _, sd := range seeds {
		k := funcKey(sd.Module, sd.Func)
		if _, dup := seedSeen[k]; dup {
			continue
		}
		seedSeen[k] = struct{}{}
		result.Changed = append(result.Changed, &FuncRef{
			Module: sd.Module, Func: sd.Func, File: sd.File,
			Depth: 0, Kind: ImpactKindChanged, Summary: sd.Summary,
		})
	}
	if len(result.Changed) == 0 {
		return nil, common.NewError(common.CodeBadRequest, "未解析到任何变更函数")
	}

	seedKeys := make([]string, 0, len(result.Changed))
	for _, f := range result.Changed {
		seedKeys = append(seedKeys, funcKey(f.Module, f.Func))
	}

	if direction == ImpactDirectionBoth || direction == ImpactDirectionUpstream {
		result.Reverse = bfs(g, seedKeys, maxDepth, true)
	}
	if direction == ImpactDirectionBoth || direction == ImpactDirectionDownstream {
		result.Forward = bfs(g, seedKeys, maxDepth, false)
	}
	s.enrichSummaries(repoID, result)
	return result, nil
}

// callGraph 内存调用图：按模块.函数 建正向(out)与反向(in)邻接表。
type callGraph struct {
	out map[string][]*model.FunctionCallEdge // key → 该函数调用了谁
	in  map[string][]*model.FunctionCallEdge // key → 谁调用了该函数
}

func buildCallGraph(edges []*model.FunctionCallEdge) *callGraph {
	g := &callGraph{out: make(map[string][]*model.FunctionCallEdge), in: make(map[string][]*model.FunctionCallEdge)}
	for _, e := range edges {
		outK := funcKey(e.CallerModule, e.CallerFunc)
		inK := funcKey(e.CalleeModule, e.CalleeFunc)
		g.out[outK] = append(g.out[outK], e)
		g.in[inK] = append(g.in[inK], e)
	}
	return g
}

// bfs 单向 BFS 传播。reverse=true 沿"谁调用了我"（上游），false 沿"我调用了谁"（下游）。
func bfs(g *callGraph, seeds []string, maxDepth int, reverse bool) []*FuncRef {
	kind := ImpactKindReverse
	if !reverse {
		kind = ImpactKindForward
	}
	seen := make(map[string]struct{})
	for _, k := range seeds {
		seen[k] = struct{}{}
	}

	var result []*FuncRef
	frontier := seeds
	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, k := range frontier {
			adj := g.out[k]
			if reverse {
				adj = g.in[k]
			}
			for _, e := range adj {
				var ref *FuncRef
				if reverse {
					// 上游：受影响的是调用方
					ref = &FuncRef{Module: e.CallerModule, Func: e.CallerFunc, File: e.CallerFile,
						Depth: depth, Kind: kind,
						Edge: e.CallerModule + "." + e.CallerFunc + " 调用了 " + e.CalleeModule + "." + e.CalleeFunc}
				} else {
					// 下游：受影响的是被调用方
					ref = &FuncRef{Module: e.CalleeModule, Func: e.CalleeFunc, File: e.CalleeFile,
						Depth: depth, Kind: kind,
						Edge: e.CallerModule + "." + e.CallerFunc + " 调用了 " + e.CalleeModule + "." + e.CalleeFunc}
				}
				target := funcKey(ref.Module, ref.Func)
				if _, dup := seen[target]; dup {
					continue
				}
				seen[target] = struct{}{}
				next = append(next, target)
				result = append(result, ref)
			}
		}
		frontier = next
	}
	return result
}

// enrichSummaries 为结果中每个函数补齐 Wiki 文档摘要（提升可读性，供影响分析展示）。
func (s *ImpactService) enrichSummaries(repoID int64, result *ImpactAnalyzeResult) {
	docMap := s.docMapForResult(repoID, result)
	if docMap == nil {
		return
	}
	for _, list := range [][]*FuncRef{result.Changed, result.Reverse, result.Forward} {
		for _, f := range list {
			if d, ok := docMap[funcKey(f.Module, f.Func)]; ok {
				f.Summary = d.Summary
			}
		}
	}
}

// docMapForResult 查询结果涉及模块的全部文档，构建 模块.函数 -> 文档 映射。
// 供摘要补齐与 code_change_log 沉淀（doc_id）复用。
func (s *ImpactService) docMapForResult(repoID int64, result *ImpactAnalyzeResult) map[string]*model.CodeFunctionDoc {
	modules := make(map[string]struct{})
	for _, list := range [][]*FuncRef{result.Changed, result.Reverse, result.Forward} {
		for _, f := range list {
			modules[f.Module] = struct{}{}
		}
	}
	if len(modules) == 0 {
		return nil
	}
	modList := make([]string, 0, len(modules))
	for m := range modules {
		modList = append(modList, m)
	}
	docs, err := s.docRepo.ListByModules(repoID, modList, 0)
	if err != nil {
		logger.Warn(context.Background(), "查询影响函数文档失败: %v", err)
		return nil
	}
	docMap := make(map[string]*model.CodeFunctionDoc, len(docs))
	for _, d := range docs {
		key := funcKey(d.ModuleName, d.FuncName)
		if _, exists := docMap[key]; !exists {
			docMap[key] = d
		}
	}
	return docMap
}

// synthesizeDesignDoc 调用 LLM 合成迭代影响分析与开发设计文档初稿，并沉淀到 code_change_log。
func (s *ImpactService) synthesizeDesignDoc(ctx context.Context, req *ImpactAnalyzeReq, result *ImpactAnalyzeResult) error {
	if strings.TrimSpace(s.llmBaseURL) == "" {
		logger.Warn(ctx, "AI 服务未配置，跳过设计文档合成与变更日志沉淀")
		return nil
	}

	system := "你是一名资深架构师。请根据代码知识库 Wiki 业务文档，对本次代码迭代做影响分析，" +
		"输出开发设计文档初稿。必须严格输出 JSON（不要输出任何其他内容），字段如下：" +
		`{"change_summary":"本次迭代变更摘要（改动点、目的）",` +
		`"business_impact":"业务影响范围（列出受影响的模块与函数、调用链风险）",` +
		`"attention":"上线注意事项与回归测试建议"}`

	sched, err := chatLLM(ctx, s.llmBaseURL, s.chatTimeout, system,
		buildImpactUserPrompt(result), "", false, estimateImpactTokens(result))
	if err != nil {
		return common.WrapError(common.CodeUpstreamError, "AI 服务合成设计文档失败，请稍后重试", err)
	}
	doc, err := parseImpactDesignJSON(sched.Answer)
	if err != nil {
		logger.Warn(ctx, "设计文档 JSON 解析失败，跳过沉淀: %v", err)
		// 解析失败不阻断影响分析本身，返回可读文本兜底
		doc = &ImpactDesignDoc{
			ChangeSummary:  truncate(sched.Answer, 2000),
			BusinessImpact: buildImpactTextSummary(result),
			Attention:      "AI 输出未结构化解析，请结合上方影响清单人工核对。",
		}
	}
	result.DesignDoc = doc
	result.UsedModel = sched.UsedModel
	result.Cost = sched.Cost

	// 按被修改函数个性化合成变更记录（每个 doc 的迭代记录互不相同）
	result.FuncChanges = s.synthesizeFuncChanges(ctx, result)

	if err := s.persistChangeLog(req, result); err != nil {
		logger.Warn(ctx, "沉淀迭代变更日志失败: %v", err)
	}
	return nil
}

// synthesizeFuncChanges 为每个被修改函数生成个性化变更记录。
// 单次 LLM 调用输出全部函数的 {module, func, change_summary, business_impact, attention} 数组；
// LLM 失败或解析失败时退化为本地拼接（函数摘要 + 迭代级影响），保证每个 doc 记录互不相同。
func (s *ImpactService) synthesizeFuncChanges(ctx context.Context, result *ImpactAnalyzeResult) []*ImpactFuncChange {
	changed := result.Changed
	if len(changed) == 0 {
		return nil
	}
	if strings.TrimSpace(s.llmBaseURL) == "" {
		return localFuncChanges(result)
	}

	system := "你是一名资深架构师。请为【每个被修改的函数】生成一条针对性、互不相同的变更记录，" +
		"用于写入该函数 Wiki 文档的迭代变更历史。必须严格输出 JSON（不要输出其他内容）：" +
		`{"entries":[{"module":"函数所在模块","func":"函数名",` +
		`"change_summary":"该函数本次改动说明（结合该函数上下文，不要照抄其他函数）",` +
		`"business_impact":"该函数改动影响的业务范围与调用方",` +
		`"attention":"该函数改动上线的注意事项与回归建议"}]}`

	sched, err := chatLLM(ctx, s.llmBaseURL, s.chatTimeout, system,
		buildFuncChangePrompt(result), "", false, 400+len(changed)*250)
	if err != nil {
		logger.Warn(ctx, "函数个性化变更记录合成失败，退化本地拼接: %v", err)
		return localFuncChanges(result)
	}
	entries, err := parseFuncChangesJSON(sched.Answer)
	if err != nil {
		logger.Warn(ctx, "函数个性化变更记录解析失败，退化本地拼接: %v", err)
		return localFuncChanges(result)
	}
	return entries
}

// localFuncChanges LLM 不可用时的本地兜底：按函数上下文拼接个性化记录（避免全部文档相同）。
func localFuncChanges(result *ImpactAnalyzeResult) []*ImpactFuncChange {
	out := make([]*ImpactFuncChange, 0, len(result.Changed))
	for _, f := range result.Changed {
		full := f.Module + "." + f.Func
		// 上游：边"X 调用了 <该函数>"；下游：边"<该函数> 调用了 X"
		var up, down []string
		for _, r := range result.Reverse {
			if strings.HasSuffix(r.Edge, "调用了 "+full) {
				up = append(up, strings.TrimSuffix(r.Edge, " 调用了 "+full))
			}
		}
		for _, r := range result.Forward {
			if strings.HasPrefix(r.Edge, full+" 调用了 ") {
				down = append(down, strings.TrimPrefix(r.Edge, full+" 调用了 "))
			}
		}
		impact := "修改 " + f.Module + "." + f.Func + "。"
		if len(up) > 0 {
			impact += " 上游调用方：" + strings.Join(up, "、") + "。"
		}
		if len(down) > 0 {
			impact += " 下游依赖：" + strings.Join(down, "、") + "。"
		}
		summary := "本轮迭代修改 " + f.Module + "." + f.Func
		if strings.TrimSpace(f.Summary) != "" {
			summary += "：" + f.Summary
		}
		out = append(out, &ImpactFuncChange{
			Module:         f.Module,
			Func:           f.Func,
			ChangeSummary:  summary,
			BusinessImpact: impact,
			Attention:      "建议回归 " + f.Module + "." + f.Func + " 及其调用链。",
		})
	}
	return out
}

// persistChangeLog 为每个直接修改且有 Wiki 文档的函数写入 code_change_log（迭代变更历史）。
// 每条记录使用该函数的个性化变更内容（FuncChanges 命中则用之，否则回退迭代级文本）。
func (s *ImpactService) persistChangeLog(req *ImpactAnalyzeReq, result *ImpactAnalyzeResult) error {
	docMap := s.docMapForResult(req.RepoID, result)
	if docMap == nil || result.DesignDoc == nil {
		return nil
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = strings.TrimSpace(req.Branch)
	}
	if version == "" {
		version = "迭代-" + time.Now().Format("20060102")
	}

	fcMap := make(map[string]*ImpactFuncChange)
	for _, fc := range result.FuncChanges {
		fcMap[funcKey(fc.Module, fc.Func)] = fc
	}

	var logs []*model.CodeChangeLog
	seenDoc := make(map[int64]struct{})
	for _, f := range result.Changed {
		d, ok := docMap[funcKey(f.Module, f.Func)]
		if !ok {
			continue
		}
		if _, dup := seenDoc[d.ID]; dup {
			continue
		}
		seenDoc[d.ID] = struct{}{}
		summary, impact, attention := result.DesignDoc.ChangeSummary, result.DesignDoc.BusinessImpact, result.DesignDoc.Attention
		if fc, ok := fcMap[funcKey(f.Module, f.Func)]; ok {
			if strings.TrimSpace(fc.ChangeSummary) != "" {
				summary = fc.ChangeSummary
			}
			if strings.TrimSpace(fc.BusinessImpact) != "" {
				impact = fc.BusinessImpact
			}
			if strings.TrimSpace(fc.Attention) != "" {
				attention = fc.Attention
			}
		}
		logs = append(logs, &model.CodeChangeLog{
			RepoID:         req.RepoID,
			DocID:          d.ID,
			Version:        version,
			ChangeSummary:  summary,
			BusinessImpact: impact,
			Attention:      attention,
		})
	}
	if len(logs) == 0 {
		return nil
	}
	return s.db.CreateInBatches(logs, 200).Error
}

// impactJSONBlockRe 匹配 ```json ... ``` 代码块。
var impactJSONBlockRe = regexp.MustCompile("```(?:json)?\\s*([\\s\\S]*?)```")

// impactDesignLLM LLM 输出的结构化设计文档 JSON。
type impactDesignLLM struct {
	ChangeSummary  string `json:"change_summary"`
	BusinessImpact string `json:"business_impact"`
	Attention      string `json:"attention"`
}

// parseImpactDesignJSON 容错解析 LLM 输出的设计文档 JSON：
//  1. 去除 ```json``` 代码块包裹；
//  2. 提取首个 { 到最后一个 } 的 JSON 子串；
//  3. 严格按结构体反序列化。
func parseImpactDesignJSON(raw string) (*ImpactDesignDoc, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, fmt.Errorf("LLM输出为空")
	}
	if m := impactJSONBlockRe.FindStringSubmatch(text); len(m) > 1 {
		text = strings.TrimSpace(m[1])
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("LLM输出中未找到JSON对象")
	}
	text = text[start : end+1]

	var out impactDesignLLM
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %w", err)
	}
	return &ImpactDesignDoc{
		ChangeSummary:  out.ChangeSummary,
		BusinessImpact: out.BusinessImpact,
		Attention:      out.Attention,
	}, nil
}

// buildImpactUserPrompt 组装影响分析的用户侧 Prompt：直接修改 + 上游/下游影响点清单。
func buildImpactUserPrompt(result *ImpactAnalyzeResult) string {
	var sb strings.Builder
	sb.WriteString("本次迭代影响分析结果如下（来自代码知识库 Wiki 函数级调用图与业务文档）：\n\n")

	sb.WriteString("【直接修改的函数】\n")
	for _, f := range result.Changed {
		sb.WriteString(fmt.Sprintf("- %s.%s (文件 %s)\n  摘要: %s\n", f.Module, f.Func, f.File, f.Summary))
	}
	if len(result.Reverse) > 0 {
		sb.WriteString("\n【上游调用方（受改动波及的调用链）】\n")
		for _, f := range result.Reverse {
			sb.WriteString(fmt.Sprintf("- [深度%d] %s.%s %s\n  摘要: %s\n", f.Depth, f.Module, f.Func, f.Edge, f.Summary))
		}
	}
	if len(result.Forward) > 0 {
		sb.WriteString("\n【下游被调用函数（改动牵连的依赖）】\n")
		for _, f := range result.Forward {
			sb.WriteString(fmt.Sprintf("- [深度%d] %s.%s %s\n  摘要: %s\n", f.Depth, f.Module, f.Func, f.Edge, f.Summary))
		}
	}
	sb.WriteString("\n请基于以上信息输出 JSON 格式的开发设计文档初稿。")
	return sb.String()
}

// buildImpactTextSummary LLM 输出解析失败时的兜底影响文本（基于真实影响清单生成）。
func buildImpactTextSummary(result *ImpactAnalyzeResult) string {
	var sb strings.Builder
	sb.WriteString("直接修改: ")
	for i, f := range result.Changed {
		if i > 0 {
			sb.WriteString("、")
		}
		sb.WriteString(f.Module + "." + f.Func)
	}
	sb.WriteString("；上游受影响: ")
	for i, f := range result.Reverse {
		if i > 0 {
			sb.WriteString("、")
		}
		sb.WriteString(f.Module + "." + f.Func)
	}
	sb.WriteString("；下游受影响: ")
	for i, f := range result.Forward {
		if i > 0 {
			sb.WriteString("、")
		}
		sb.WriteString(f.Module + "." + f.Func)
	}
	return sb.String()
}

// impactFuncChangeLLM LLM 输出的函数个性化变更记录 JSON。
type impactFuncChangeLLM struct {
	Entries []*ImpactFuncChange `json:"entries"`
}

// parseFuncChangesJSON 容错解析 LLM 输出的函数个性化变更记录 JSON（数组包裹在 {"entries":[...]} 中）。
func parseFuncChangesJSON(raw string) ([]*ImpactFuncChange, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, fmt.Errorf("LLM输出为空")
	}
	if m := impactJSONBlockRe.FindStringSubmatch(text); len(m) > 1 {
		text = strings.TrimSpace(m[1])
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("LLM输出中未找到JSON对象")
	}
	text = text[start : end+1]

	var out impactFuncChangeLLM
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %w", err)
	}
	if len(out.Entries) == 0 {
		return nil, fmt.Errorf("LLM输出中函数记录为空")
	}
	return out.Entries, nil
}

// buildFuncChangePrompt 组装函数个性化变更记录的用户侧 Prompt：逐函数给出文件、摘要与调用链。
func buildFuncChangePrompt(result *ImpactAnalyzeResult) string {
	var sb strings.Builder
	sb.WriteString("请为下列被修改函数逐一生成个性化变更记录：\n")
	for _, f := range result.Changed {
		sb.WriteString(fmt.Sprintf("\n【%s.%s】文件 %s\n  摘要: %s\n",
			f.Module, f.Func, f.File, f.Summary))
		full := f.Module + "." + f.Func
		var up, down []string
		for _, r := range result.Reverse {
			if strings.HasSuffix(r.Edge, "调用了 "+full) {
				up = append(up, strings.TrimSuffix(r.Edge, " 调用了 "+full)+"(深度"+itoa(r.Depth)+")")
			}
		}
		for _, r := range result.Forward {
			if strings.HasPrefix(r.Edge, full+" 调用了 ") {
				down = append(down, strings.TrimPrefix(r.Edge, full+" 调用了 ")+"(深度"+itoa(r.Depth)+")")
			}
		}
		if len(up) > 0 {
			sb.WriteString("  上游调用方: " + strings.Join(up, "、") + "\n")
		}
		if len(down) > 0 {
			sb.WriteString("  下游依赖: " + strings.Join(down, "、") + "\n")
		}
	}
	sb.WriteString("\n输出 entries 数组，每条针对对应函数（change_summary 必须针对该函数，不得与其他函数雷同）。")
	return sb.String()
}

// itoa 整数转字符串（避免额外依赖 strconv 的简洁封装）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// estimateImpactTokens 估算影响清单文本长度，供 LLM 调度器预估 token。
func estimateImpactTokens(result *ImpactAnalyzeResult) int {
	n := len(result.Changed) + len(result.Reverse) + len(result.Forward)
	if n <= 0 {
		n = 1
	}
	return 600 + n*300
}

// funcKey 生成模块.函数 唯一键（跨包限定名与同包函数名统一按模块归组）。
func funcKey(module, fn string) string {
	return module + "." + fn
}
