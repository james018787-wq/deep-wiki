// Package service 业务逻辑层。
// SearchService 实现 /api/v1/doc/search 跨模块 RAG 检索。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ai-code-wiki/internal/config"
	"ai-code-wiki/internal/model"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/llm"
	"ai-code-wiki/pkg/vector"

	"gorm.io/gorm"
)

// 检索与上下文长度控制常量。
const (
	TopK          = 10   // 向量初步召回候选数
	ExpandLimit   = 5    // 每个关联模块扩展召回文档上限
	PerDocMaxLen  = 300  // 单文档上下文最大字符数（rune）
	ContextMaxLen = 6000 // 总上下文最大字符数，避免 LLM 超长报错
)

// SearchService 跨模块 RAG 检索服务。
type SearchService struct {
	db           *gorm.DB
	docRepo      *repo.CodeFunctionDocRepo
	relationRepo *repo.ModuleRelationRepo
	llmBaseURL   string              // Python LLM 微服务地址（用于 embedding 与回答生成）
	chatTimeout  time.Duration       // 回答生成 LLM 调用超时（LLM_TIMEOUT，默认 60s）
	vc           vector.VectorClient // 向量存储抽象（业务不感知 chroma/milvus）
}

// NewSearchService 构建检索服务。
func NewSearchService(db *gorm.DB, cfg *config.Config, vc vector.VectorClient) *SearchService {
	return &SearchService{
		db:           db,
		docRepo:      newDocRepo(db),
		relationRepo: repo.NewModuleRelationRepo(db),
		llmBaseURL:   cfg.LLM.BaseURL,
		chatTimeout:  llmCallTimeout(cfg.LLM.Timeout, defaultLLMTimeoutSec),
		vc:           vc,
	}
}

// SearchReq 自然语言查询入参。
type SearchReq struct {
	Query  string `json:"query" binding:"required"` // 自然语言查询
	Module string `json:"module"`                   // 可选的模块过滤（保留兼容）
}

// ReferenceDoc 引用文档来源。
type ReferenceDoc struct {
	DocID      int64  `json:"doc_id"`      // 文档id
	FilePath   string `json:"file_path"`   // 文件路径
	FuncName   string `json:"func_name"`   // 函数名称
	ModuleName string `json:"module_name"` // 所属模块
}

// SearchResult 检索回答结果。
type SearchResult struct {
	Answer        string          `json:"answer"`          // LLM 回答
	ReferenceList []ReferenceDoc  `json:"reference_list"`  // 引用文档来源列表
}

// Search 跨模块 RAG 检索主流程。
//
// 流水线顺序【严格不可调换】：
//  1. 接收用户 query；
//  2. 调用 Python LLM 服务向量接口将 query 转为向量，经向量抽象接口
//    （chroma/milvus 之一）查询得到候选 doc_id 列表；
//  3. 根据 doc_id 从 MySQL 读取 CodeFunctionDoc 候选文档；
//  4. 读取 module_relation 表，合并 AST 自动识别 + 人工新增 的模块依赖关系；
//  5. 根据候选文档所属模块，扩充召回关联模块下文档，实现跨模块召回；
//  6. 简单上下文截断，组装上下文 prompt；
//  7. 调用 LLM，传入用户问题 + 检索到的文档上下文，返回回答及引用来源列表。
func (s *SearchService) Search(ctx context.Context, req *SearchReq) (*SearchResult, error) {
	// step1: 接收用户 query（handler 已做绑定校验，此处再兜底）
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, common.NewError(common.CodeBadRequest, "查询内容不能为空")
	}

	// step2-5: 复用检索流水线（向量召回 + MySQL读候选 + 跨模块扩充）
	recalled, err := s.RetrieveRelatedDocs(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(recalled) == 0 {
		return nil, common.NewError(common.CodeNotFound, "未检索到相关业务文档，请换个问法重试")
	}

	// step6: 上下文截断与组装
	contextPrompt := buildContextPrompt(recalled)

	// step7: 调用 LLM 生成回答
	answer, err := s.askLLM(ctx, query, contextPrompt)
	if err != nil {
		return nil, err
	}

	return &SearchResult{
		Answer:        answer,
		ReferenceList: toReferenceList(recalled),
	}, nil
}

// RetrieveRelatedDocs 复用检索流水线（步骤2-5）：
// query转向量 -> 经向量抽象接口得候选doc_id -> MySQL读取候选文档 -> 跨模块扩充召回。
//
// 供需求分析等场景复用检索逻辑（禁止重复实现一套检索）。
// 无相关文档时返回空切片（不报错），由调用方决定后续处理。
func (s *SearchService) RetrieveRelatedDocs(ctx context.Context, query string) ([]*model.CodeFunctionDoc, error) {
	// step2: query 转向量 -> 查询 chroma 得到候选 doc_id 列表
	candidateIDs, err := s.vectorRecall(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(candidateIDs) == 0 {
		return nil, nil
	}

	// step3: 根据 doc_id 从 MySQL 读取候选文档
	candidates, err := s.loadDocs(ctx, candidateIDs)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	// step4+step5: 读取模块依赖（AST UNION 人工），跨模块扩充召回
	recalled, err := s.expandRecall(ctx, candidates)
	if err != nil {
		return nil, err
	}
	return recalled, nil
}

// vectorRecall 向量初步召回（仅做候选 doc_id 检索，不做跨模块扩充）。
func (s *SearchService) vectorRecall(ctx context.Context, query string) ([]int64, error) {
	if s.llmBaseURL == "" || s.vc == nil {
		return nil, common.NewError(common.CodeInvalidState, "向量检索服务未配置")
	}

	// 调用 Python LLM 服务向量接口，将 query 转为向量
	vec, err := vector.EmbedText(s.llmBaseURL, query)
	if err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "向量服务暂时不可用，请稍后重试", err)
	}

	// 通过向量抽象检索候选 doc_id（底层为 chroma 或 milvus，业务无感知）
	ids, err := s.vc.SearchQuery(vec, TopK)
	if err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "向量检索失败，请稍后重试", err)
	}
	return ids, nil
}

// loadDocs 根据候选 doc_id 从 MySQL 读取候选文档。
func (s *SearchService) loadDocs(ctx context.Context, ids []int64) ([]*model.CodeFunctionDoc, error) {
	_ = ctx
	var docs []*model.CodeFunctionDoc
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		doc, err := s.docRepo.GetByID(id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue // 已删除或不存在，跳过
			}
			return nil, common.WrapError(common.CodeInternalError, "读取文档失败", err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// expandRecall 读取模块依赖并跨模块扩充召回。
//
// 业务规则：
//  - 模块依赖查询 = AST 自动识别(source=1) UNION 人工添加(source=2)，
//    查询时不过滤 source 字段，人工新增依赖不会被丢弃。
//  - 跨模块扩充在 MySQL 层完成（非向量层）。
func (s *SearchService) expandRecall(ctx context.Context, candidates []*model.CodeFunctionDoc) ([]*model.CodeFunctionDoc, error) {
	_ = ctx

	// 候选文档所属模块集合
	moduleSet := make(map[string]struct{}, len(candidates))
	for _, d := range candidates {
		if d == nil || d.ModuleName == "" {
			continue
		}
		moduleSet[d.ModuleName] = struct{}{}
	}
	if len(moduleSet) == 0 {
		return candidates, nil
	}
	modules := make([]string, 0, len(moduleSet))
	for m := range moduleSet {
		modules = append(modules, m)
	}

	// 读取 module_relation（合并 AST + 人工，不过滤 source）
	rels, err := s.relationRepo.ListRelationsByModules(modules)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "读取模块依赖失败", err)
	}

	// 收集关联模块（关系的另一侧）
	related := make(map[string]struct{})
	for _, r := range rels {
		if r == nil {
			continue
		}
		if _, ok := moduleSet[r.SourceModule]; ok {
			related[r.TargetModule] = struct{}{}
		}
		if _, ok := moduleSet[r.TargetModule]; ok {
			related[r.SourceModule] = struct{}{}
		}
	}

	// 过滤掉已召回模块，得到待扩充模块
	toQuery := make([]string, 0, len(related))
	for m := range related {
		if _, ok := moduleSet[m]; !ok {
			toQuery = append(toQuery, m)
		}
	}
	if len(toQuery) == 0 {
		return candidates, nil
	}

	// MySQL 层查询关联模块下文档，扩充召回
	relatedDocs, err := s.docRepo.ListByModules(toQuery, len(toQuery)*ExpandLimit)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "跨模块召回失败", err)
	}
	return append(candidates, relatedDocs...), nil
}

// askLLM 调用 LLM 生成回答（带显式超时，避免上游异常拖垮接口）。
func (s *SearchService) askLLM(ctx context.Context, query, contextPrompt string) (string, error) {
	if s.llmBaseURL == "" {
		return "", common.NewError(common.CodeInvalidState, "AI 服务未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, s.chatTimeout)
	defer cancel()

	system := "你是一名代码知识库问答助手，请根据提供的业务文档上下文，准确、简洁地回答用户问题。" +
		"如果文档信息不足以回答，请如实说明，不要编造。"
	user := contextPrompt + "\n\n用户问题: " + query + "\n请基于以上文档作答。"

	answer, err := llm.Chat(ctx, s.llmBaseURL, system, user)
	if err != nil {
		return "", common.WrapError(common.CodeUpstreamError, "AI 服务暂时不可用，请稍后重试", err)
	}
	return answer, nil
}

// buildContextPrompt 上下文截断与组装。
// 控制单文档与总体长度，避免 LLM 超长报错。
func buildContextPrompt(docs []*model.CodeFunctionDoc) string {
	var sb strings.Builder
	sb.WriteString("以下是代码知识库中检索到的相关业务文档：\n")
	used := 0
	idx := 1
	for _, d := range docs {
		if d == nil {
			continue
		}
		entry := fmt.Sprintf("[%d] 模块:%s 函数:%s 文件:%s\n摘要:%s\n流程:%s\n风险:%s\n",
			idx, d.ModuleName, d.FuncName, d.FilePath,
			truncate(d.Summary, PerDocMaxLen),
			truncate(d.ProcessFlow, PerDocMaxLen),
			truncate(d.RiskPoint, PerDocMaxLen))
		if used+len(entry) > ContextMaxLen {
			break // 超过总长度上限，丢弃剩余文档
		}
		sb.WriteString(entry)
		used += len(entry)
		idx++
	}
	return sb.String()
}

// truncate 按字符（rune）截断字符串，超长追加省略号。
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

// toReferenceList 生成引用文档来源列表。
func toReferenceList(docs []*model.CodeFunctionDoc) []ReferenceDoc {
	list := make([]ReferenceDoc, 0, len(docs))
	for _, d := range docs {
		if d == nil {
			continue
		}
		list = append(list, ReferenceDoc{
			DocID:      d.ID,
			FilePath:   d.FilePath,
			FuncName:   d.FuncName,
			ModuleName: d.ModuleName,
		})
	}
	return list
}