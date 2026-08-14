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
	"ai-code-wiki/pkg/llm"
)

// analyzeTimeout 需求分析 LLM 调用超时时间。
const analyzeTimeout = 30 * time.Second

// RequirementService 新产品需求分析业务逻辑。
type RequirementService struct {
	search     *SearchService // 复用已有 RAG 检索流水线
	llmBaseURL string         // Python LLM 服务地址
}

// NewRequirementService 构建需求分析服务。
func NewRequirementService(search *SearchService, cfg *config.Config) *RequirementService {
	return &RequirementService{
		search:     search,
		llmBaseURL: cfg.LLM.BaseURL,
	}
}

// AnalyzeReq 新产品需求分析入参。
type AnalyzeReq struct {
	Requirement string `json:"user_requirement" binding:"required"` // 用户业务需求文本
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
//  - 复用已有检索逻辑，禁止另写一套；
//  - 严格要求 LLM 返回 JSON，解析做好容错；
//  - 未检索到相关文档时仍返回结果，提示知识库缺少对应资料；
//  - LLM 调用带超时控制，上游异常返回友好提示。
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

	// step2: 复用检索流水线，检索相关函数文档
	docs, err := s.search.RetrieveRelatedDocs(ctx, requirement)
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

	// step4: 调用 LLM，带超时控制
	ctx, cancel := context.WithTimeout(ctx, analyzeTimeout)
	defer cancel()
	raw, err := llm.Chat(ctx, s.llmBaseURL, system, user)
	if err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "AI 服务暂时不可用，请稍后重试", err)
	}

	// 解析 LLM 输出 JSON（容错处理）
	parsed, parseErr := parseAnalyzeJSON(raw)
	if parseErr != nil {
		// 解析失败：降级为基于真实召回文档的兜底结果，保证接口始终有返回
		return fallbackAnalyzeResult(docs), nil
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
//  1. 去除 ```json``` 代码块包裹；
//  2. 提取首个 { 到最后一个 } 的 JSON 子串；
//  3. 严格按结构体反序列化。
func parseAnalyzeJSON(raw string) (*analyzeLLMResult, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, fmt.Errorf("LLM输出为空")
	}
	// 去除 markdown 代码块包裹
	if m := jsonBlockRe.FindStringSubmatch(text); len(m) > 1 {
		text = strings.TrimSpace(m[1])
	}
	// 提取 JSON 对象子串，容忍前后多余文本
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("LLM输出中未找到JSON对象")
	}
	text = text[start : end+1]

	var out analyzeLLMResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %w", err)
	}
	return &out, nil
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