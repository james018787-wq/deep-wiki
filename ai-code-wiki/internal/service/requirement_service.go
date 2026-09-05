package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"ai-code-wiki/internal/config"
	"ai-code-wiki/internal/model"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/logger"
)

// RequirementService 新产品需求分析业务逻辑。
type RequirementService struct {
	search      *SearchService // 复用已有 RAG 检索流水线
	llmBaseURL  string         // Python LLM 服务地址
	chatTimeout time.Duration  // 需求分析对话调用超时（LLM_TIMEOUT，默认 60s）
}

// NewRequirementService 构建需求分析服务。
func NewRequirementService(search *SearchService, cfg *config.Config) *RequirementService {
	return &RequirementService{
		search:      search,
		llmBaseURL:  cfg.LLM.BaseURL,
		chatTimeout: llmCallTimeout(cfg.LLM.Timeout, defaultLLMTimeoutSec),
	}
}

// AnalyzeReq 新产品需求分析入参。
type AnalyzeReq struct {
	RepoID           int64  `json:"repo_id" binding:"required"`          // 所属仓库id（按库隔离检索）
	Requirement      string `json:"user_requirement" binding:"required"` // 用户业务需求文本
	ForceModel       string `json:"force_model"`                         // 可选：强制指定模型（不降级/不熔断/不限流）
	ForceHighQuality bool   `json:"force_high_quality"`                  // 可选：仅用高配模型（过滤低价模型）
}

// RelatedFunction 需求涉及的函数文档。
type RelatedFunction struct {
	DocID    int64  `json:"doc_id"`    // 文档id
	FilePath string `json:"file_path"` // 文件路径
	FuncName string `json:"func_name"` // 函数名称
	Summary  string `json:"summary"`   // 一句话业务摘要
}

// AnalyzeResult 需求分析输出结果。
type AnalyzeResult struct {
	RelatedModules   []string          `json:"related_modules"`   // 相关模块
	RelatedFunctions []RelatedFunction `json:"related_functions"` // 相关函数
	Analysis         string            `json:"analysis"`          // 需求分析说明
	RiskPoints       []string          `json:"risk_points"`       // 潜在风险点
	Suggestion       string            `json:"suggestion"`        // 开发建议
	KnowledgeMissing bool              `json:"knowledge_missing"` // 知识库是否缺少对应资料
	UsedModel        string            `json:"used_model"`        // 实际使用的模型
	SwitchCount      int               `json:"switch_count"`      // 实际降级切换次数
	Cost             float64           `json:"cost"`              // 本次调用估算成本（元）
}

// Analyze 需求分析主流程。
//
// 执行流程：
//  1. 接收入参 user_requirement；
//  2. 复用 search_service 检索流水线，根据需求文本检索相关函数文档；
//  3. 把检索出的函数文档作为上下文；
//  4. 构造 Prompt 交给 LLM，要求输出结构化 JSON；
//  5. 返回分析结果（含引用函数来源）。
//
// 约束：
//   - 复用已有检索逻辑，禁止另写一套；
//   - 严格要求 LLM 返回 JSON，解析做好容错；
//   - 未检索到相关文档时仍返回结果，提示知识库缺少对应资料；
//   - LLM 调用带超时控制，上游异常返回友好提示。
func (s *RequirementService) Analyze(ctx context.Context, req *AnalyzeReq) (*AnalyzeResult, error) {
	// 防御：调用方未传 context 时使用 Background
	if ctx == nil {
		ctx = context.Background()
	}

	// step1: 接收入参
	requirement := strings.TrimSpace(req.Requirement)
	if requirement == "" {
		return nil, common.NewError(common.CodeBadRequest, "需求描述不能为空")
	}

	// step2: 复用检索流水线，检索相关函数文档（限定仓库）
	docs, err := s.search.RetrieveRelatedDocs(ctx, req.RepoID, requirement)
	if err != nil {
		return nil, err
	}

	// step3: 把检索出的函数文档作为上下文，构造 Prompt 要求输出结构化 JSON
	if s.llmBaseURL == "" {
		return nil, common.NewError(common.CodeInvalidState, "AI 服务未配置")
	}
	system := "你是一名资深研发需求分析师。请根据用户业务需求与检索到的代码知识库文档，输出开发分析结果。\n" +
		"严格要求：只输出一个 JSON 对象，不要包含 markdown 代码块标记或任何额外解释。\n" +
		`输出字段严格为：{"related_modules":[],"related_functions":[{"doc_id":"","file_path":"","func_name":"","summary":""}],"analysis":"","risk_points":[],"suggestion":""}` + "\n" +
		"related_modules 为涉及的业务模块名数组；related_functions 为引用的函数文档；" +
		"analysis 为需求分析说明（中文）；risk_points 为潜在风险点数组；suggestion 为开发建议。"

	user := buildRequirementUserPrompt(requirement, docs)

	// step4: 经 ai-wiki-llm 多模型调度器调用 LLM（低价优先、失败降级在 Python 侧完成），带超时控制
	estimated := estimateTokens(user)
	sched, err := chatLLM(ctx, s.llmBaseURL, s.chatTimeout, system, user,
		req.ForceModel, req.ForceHighQuality, estimated, UsageScenarioRequirement)
	if err != nil {
		return nil, err
	}
	raw := sched.Answer

	logger.Info(ctx, "[requirement] 分析生成完成 used_model=%s switch_count=%d force_model=%s force_high_quality=%t estimated_context_token=%d input_token=%d output_token=%d cost=%.6f retried_model_list=%v",
		sched.UsedModel, sched.SwitchCount, req.ForceModel, req.ForceHighQuality,
		estimated, sched.TokenInput, sched.TokenOutput, sched.Cost, sched.RetriedModels)

	// 解析 LLM 输出 JSON（容错处理）
	parsed, parseErr := parseAnalyzeJSON(raw)
	if parseErr != nil {
		// 首次解析失败：记录原始输出便于诊断，并用更严格的指令重试一次
		logger.Warn(ctx, "[requirement] 首次 JSON 解析失败，重试一次: %v", parseErr)
		logger.Info(ctx, "[requirement] 首次原始输出(截断): %.1200s", raw)
		retryUser := user + "\n\n【重要】请严格只输出一个合法 JSON 对象，不要包含任何 markdown 代码块、解释或前后缀文本。"
		if sched2, err2 := chatLLM(ctx, s.llmBaseURL, s.chatTimeout, system, retryUser,
			req.ForceModel, req.ForceHighQuality, estimated, UsageScenarioRequirement); err2 == nil {
			if p2, e2 := parseAnalyzeJSON(sched2.Answer); e2 == nil {
				sched = sched2
				raw = sched2.Answer
				parsed = p2
				parseErr = nil
			} else {
				logger.Warn(ctx, "[requirement] 重试后仍解析失败: %v；原始输出(截断): %.1200s", e2, sched2.Answer)
			}
		} else {
			logger.Warn(ctx, "[requirement] 重试调用失败: %v", err2)
		}
	}
	if parseErr != nil {
		// 仍失败：降级为基于真实召回文档的兜底结果，保证接口始终有返回
		fb := fallbackAnalyzeResult(docs)
		fb.UsedModel = sched.UsedModel
		fb.SwitchCount = sched.SwitchCount
		fb.Cost = sched.Cost
		return fb, nil
	}

	// 空值归一：LLM 可能返回 null/缺失数组字段，统一转为空数组，保证 JSON 输出结构稳定
	if parsed.RiskPoints == nil {
		parsed.RiskPoints = []string{}
	}
	if parsed.RelatedModules == nil {
		parsed.RelatedModules = []string{}
	}

	result := &AnalyzeResult{
		RelatedModules:   mergeModules(parsed.RelatedModules, docs),
		RelatedFunctions: buildRelatedFunctions(docs, parsed.RelatedFunctions),
		Analysis:         parsed.Analysis,
		RiskPoints:       parsed.RiskPoints,
		Suggestion:       parsed.Suggestion,
		KnowledgeMissing: len(docs) == 0,
		UsedModel:        sched.UsedModel,
		SwitchCount:      sched.SwitchCount,
		Cost:             sched.Cost,
	}

	// 没有检索到相关文档：提示知识库缺少对应资料
	if result.KnowledgeMissing {
		if strings.TrimSpace(result.Analysis) == "" {
			result.Analysis = "知识库中暂未检索到与当前需求相关的业务文档，缺少对应资料。"
		} else if !strings.Contains(result.Analysis, "知识库") {
			result.Analysis += "\n说明：知识库暂未检索到与当前需求匹配的业务文档，建议补充相关代码后重新分析。"
		}
	}
	return result, nil
}

// buildRequirementUserPrompt 组装用户侧 Prompt：检索到的文档上下文 + 用户需求。
func buildRequirementUserPrompt(requirement string, docs []*model.CodeFunctionDoc) string {
	var sb strings.Builder
	if len(docs) > 0 {
		// 复用上下文字段组装（含截断与长度控制）
		sb.WriteString(buildContextPrompt(docs))
	} else {
		sb.WriteString("（知识库中未检索到相关业务文档，请基于需求文本进行初步分析，并在 analysis 中说明缺少对应资料。）\n")
	}
	sb.WriteString("\n用户业务需求：\n")
	sb.WriteString(requirement)
	return sb.String()
}

// jsonBlockRe 匹配 ```json ... ``` 代码块。
var jsonBlockRe = regexp.MustCompile("```(?:json)?\\s*([\\s\\S]*?)```")

// analyzeLLMResult LLM 输出的结构化分析 JSON。
type analyzeLLMResult struct {
	RelatedModules   []string          `json:"related_modules"`
	RelatedFunctions []RelatedFunction `json:"related_functions"`
	Analysis         string            `json:"analysis"`
	RiskPoints       []string          `json:"risk_points"`
	Suggestion       string            `json:"suggestion"`
}

// parseAnalyzeJSON 容错解析 LLM 输出：
//  1. 直接整体解析；
//  2. 去除 ```json``` 代码块包裹后解析；
//  3. 花括号配平提取候选 JSON 对象（容忍前后多余文本）。
func parseAnalyzeJSON(raw string) (*analyzeLLMResult, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, fmt.Errorf("LLM输出为空")
	}
	// 1. 整体直接解析
	if out, err := tryParseAnalyzeJSON(text); err == nil {
		return out, nil
	}
	// 2. 去除 markdown 代码块包裹
	if m := jsonBlockRe.FindStringSubmatch(text); len(m) > 1 {
		if out, err := tryParseAnalyzeJSON(strings.TrimSpace(m[1])); err == nil {
			return out, nil
		}
	}
	// 3. 花括号配平提取（容忍前后多余文本/代码块）
	for _, seg := range extractBalancedJSON(text) {
		if out, err := tryParseAnalyzeJSON(seg); err == nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("LLM输出未解析出合法JSON对象")
}

// tryParseAnalyzeJSON 严格按结构体解析单个文本片段。
func tryParseAnalyzeJSON(text string) (*analyzeLLMResult, error) {
	var out analyzeLLMResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// extractBalancedJSON 用花括号配平扫描文本，返回所有候选 JSON 对象子串
// （从 { 开始配平到匹配的 }，字符串内的大括号不参与配平）。
func extractBalancedJSON(text string) []string {
	var segs []string
	depth := 0
	start := -1
	inStr := false
	esc := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case inStr:
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
		case c == '{':
			if depth == 0 {
				start = i
			}
			depth++
		case c == '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					segs = append(segs, text[start:i+1])
					start = -1
				}
			}
		}
	}
	return segs
}

// fallbackAnalyzeResult LLM 输出解析失败时的兜底结果，基于真实召回文档生成。
func fallbackAnalyzeResult(docs []*model.CodeFunctionDoc) *AnalyzeResult {
	result := &AnalyzeResult{
		RelatedModules:   modulesFromDocs(docs),
		RelatedFunctions: buildRelatedFunctions(docs, nil),
		RiskPoints:       []string{},
		Suggestion:       "建议补充/更新知识库文档后重新分析。",
		KnowledgeMissing: len(docs) == 0,
	}
	if len(docs) == 0 {
		result.Analysis = "知识库中暂未检索到与当前需求相关的业务文档，缺少对应资料。"
	} else {
		result.Analysis = "知识库检索完成，但 AI 结构化分析输出解析失败，已基于召回文档返回基础信息，请稍后重试。"
	}
	return result
}

// buildRelatedFunctions 生成相关函数列表。
// 有真实召回文档时以真实文档为准（doc_id 权威）；无召回时采用 LLM 输出。
func buildRelatedFunctions(docs []*model.CodeFunctionDoc, llmList []RelatedFunction) []RelatedFunction {
	if len(docs) == 0 {
		if llmList == nil {
			return []RelatedFunction{}
		}
		return llmList
	}
	list := make([]RelatedFunction, 0, len(docs))
	for _, d := range docs {
		if d == nil {
			continue
		}
		list = append(list, RelatedFunction{
			DocID:    d.ID,
			FilePath: d.FilePath,
			FuncName: d.FuncName,
			Summary:  truncate(d.Summary, 100),
		})
	}
	return list
}

// modulesFromDocs 从真实召回文档提取模块列表。
func modulesFromDocs(docs []*model.CodeFunctionDoc) []string {
	set := make(map[string]struct{})
	for _, d := range docs {
		if d != nil && d.ModuleName != "" {
			set[d.ModuleName] = struct{}{}
		}
	}
	return keysOf(set)
}

// mergeModules 合并 LLM 输出的模块与真实召回文档的模块（去重）。
func mergeModules(llmList []string, docs []*model.CodeFunctionDoc) []string {
	set := make(map[string]struct{}, len(llmList)+len(docs))
	for _, m := range llmList {
		if strings.TrimSpace(m) != "" {
			set[m] = struct{}{}
		}
	}
	for _, m := range modulesFromDocs(docs) {
		set[m] = struct{}{}
	}
	return keysOf(set)
}

// keysOf 返回 map 的 key 切片。
func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
