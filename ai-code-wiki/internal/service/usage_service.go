package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-code-wiki/internal/model"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/logger"

	"gorm.io/gorm"
)

// UsageScenario LLM 调用场景标签（写入 llm_usage.scenario）。
const (
	UsageScenarioDoc         = "doc"          // 解析任务文档生成（/api/generate/doc）
	UsageScenarioSearch      = "search"       // RAG 问答检索
	UsageScenarioChat        = "chat"         // 多轮智能问答
	UsageScenarioRequirement = "requirement"  // 需求分析
	UsageScenarioImpact      = "impact"       // 迭代影响（变更说明合成）
	UsageScenarioFuncChange  = "func_change"  // 迭代影响（逐函数变更记录）
	UsageScenarioRollup      = "rollup"       // 会话滚动摘要
)

// UsageService LLM 消耗统计与模型配置展示。
// 模型配置来源：Python ai-wiki-llm model_pool.yaml（经 /api/models 转发）。
// 消耗数据来源：chatLLM / 文档生成 每次调用落库 llm_usage。
type UsageService struct {
	db      *gorm.DB
	usage   *repo.LLMUsageRepo
	llmBase string
}

// NewUsageService 构建消耗统计服务。
func NewUsageService(db *gorm.DB, llmBaseURL string) *UsageService {
	return &UsageService{
		db:      db,
		usage:   repo.NewLLMUsageRepo(db),
		llmBase: strings.TrimRight(llmBaseURL, "/"),
	}
}

// Record 记录一次 LLM 调用消耗（best-effort，落库失败仅告警不阻断业务）。
func (s *UsageService) Record(ctx context.Context, modelName, scenario string, inputTokens, outputTokens int, cost float64) {
	if s == nil || s.usage == nil || strings.TrimSpace(modelName) == "" {
		return
	}
	rec := &model.LLMUsage{
		ModelName:    modelName,
		Scenario:     scenario,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Cost:         cost,
	}
	if err := s.usage.Create(rec); err != nil {
		logger.Warn(ctx, "记录 LLM 消耗失败 model=%s scenario=%s err=%v", modelName, scenario, err)
	}
}

// ModelItem 模型配置展示项（脱敏，来自 Python /api/models）。
type ModelItem struct {
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	BaseURL      string  `json:"base_url"`
	InputPrice   float64 `json:"input_price"`
	OutputPrice  float64 `json:"output_price"`
	MaxContext   int     `json:"max_context"`
	RPM          int     `json:"rpm"`
	TPM          int     `json:"tpm"`
	Enable       bool    `json:"enable"`
}

// ModelPoolInfo 模型池展示信息。
type ModelPoolInfo struct {
	Models []*ModelItem            `json:"models"`
	Global map[string]interface{}  `json:"global"`
}

// ModelStatusItem 模型运行状态展示项。
type ModelStatusItem struct {
	Name         string `json:"name"`
	Enable       bool   `json:"enable"`
	KeyReady     bool   `json:"key_ready"`
	CircuitOpen  bool   `json:"circuit_open"`
	CircuitTTL   int64  `json:"circuit_ttl"`
	FailureCount int64  `json:"failure_count"`
	DegradeCount int64  `json:"degrade_count"`
	RPMUsed      int64  `json:"rpm_used"`
	TPMUsed      int64  `json:"tpm_used"`
}

// ModelStatusResult 模型运行状态结果。
type ModelStatusResult struct {
	Models []*ModelStatusItem `json:"models"`
}

// ListModelStatus 转发 Python /api/models/status，返回各模型运行状态（熔断/限流/降级次数）。
func (s *UsageService) ListModelStatus(ctx context.Context) (*ModelStatusResult, error) {
	if s.llmBase == "" {
		return nil, common.NewError(common.CodeInvalidState, "AI 服务未配置")
	}
	url := s.llmBase + "/api/models/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "构建模型状态请求失败", err)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "AI 服务暂时不可用，请稍后重试", err)
	}
	defer resp.Body.Close()
	var apiResp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&apiResp); err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "模型状态响应解析失败", err)
	}
	if apiResp.Code != 0 {
		return nil, common.WrapError(common.CodeUpstreamError, "获取模型状态失败: "+apiResp.Message, nil)
	}
	var result ModelStatusResult
	if err := json.Unmarshal(apiResp.Data, &result); err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "模型状态数据解析失败", err)
	}
	return &result, nil
}

// ListModels 转发 Python /api/models，返回当前模型池配置（脱敏）。
func (s *UsageService) ListModels(ctx context.Context) (*ModelPoolInfo, error) {
	if s.llmBase == "" {
		return nil, common.NewError(common.CodeInvalidState, "AI 服务未配置")
	}
	url := s.llmBase + "/api/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "构建模型列表请求失败", err)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "AI 服务暂时不可用，请稍后重试", err)
	}
	defer resp.Body.Close()
	var apiResp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&apiResp); err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "模型列表响应解析失败", err)
	}
	if apiResp.Code != 0 {
		return nil, common.WrapError(common.CodeUpstreamError, "获取模型列表失败: "+apiResp.Message, nil)
	}
	var info ModelPoolInfo
	if err := json.Unmarshal(apiResp.Data, &info); err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "模型列表数据解析失败", err)
	}
	return &info, nil
}

// UsageQuery 消耗统计查询条件。
type UsageQuery struct {
	Days     int    `json:"days" form:"days"`         // 统计最近 N 天（默认 7），与 since/until 二选一
	Since    string `json:"since" form:"since"`       // 起始日期 2006-01-02
	Until    string `json:"until" form:"until"`       // 截止日期 2006-01-02（不含当天）
	Scenario string `json:"scenario" form:"scenario"` // 场景过滤（doc/chat/search/...），空=全部
	GroupBy  string `json:"group_by" form:"group_by"` // 聚合维度：model/day/scenario（默认 model）
}

// UsageResult 消耗统计结果。
type UsageResult struct {
	Total    *repo.UsageRow   `json:"total"`     // 汇总
	Rows     []*repo.UsageRow `json:"rows"`      // 聚合明细
	GroupBy  string           `json:"group_by"`
	Scenario string           `json:"scenario"`
	Since    string           `json:"since"`
	Until    string           `json:"until"`
}

// GetUsage 聚合查询 LLM 消耗。
func (s *UsageService) GetUsage(ctx context.Context, q *UsageQuery) (*UsageResult, error) {
	since, until, err := parseRange(q.Days, q.Since, q.Until)
	if err != nil {
		return nil, common.NewError(common.CodeBadRequest, err.Error())
	}
	groupBy := q.GroupBy
	if groupBy == "" {
		groupBy = "model"
	}
	var rows []*repo.UsageRow
	switch groupBy {
	case "day":
		rows, err = s.usage.AggregateByDay(since, until, q.Scenario)
	case "scenario":
		rows, err = s.usage.AggregateByScenario(since, until, q.Scenario)
	default:
		groupBy = "model"
		rows, err = s.usage.AggregateByModel(since, until, q.Scenario)
	}
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询 LLM 消耗失败", err)
	}
	total, err := s.usage.TotalSummary(since, until, q.Scenario)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询 LLM 消耗汇总失败", err)
	}
	untilDate := ""
	if until != nil {
		untilDate = until.Format("2006-01-02")
	}
	return &UsageResult{
		Total:    total,
		Rows:     rows,
		GroupBy:  groupBy,
		Scenario: q.Scenario,
		Since:    since.Format("2006-01-02"),
		Until:    untilDate,
	}, nil
}

// parseRange 解析时间范围：优先 since/until，否则 days 回退。
func parseRange(days int, sinceStr, untilStr string) (since, until *time.Time, err error) {
	now := time.Now()
	if sinceStr != "" || untilStr != "" {
		loc := time.Local
		if sinceStr != "" {
			t, e := time.ParseInLocation("2006-01-02", sinceStr, loc)
			if e != nil {
				return nil, nil, fmt.Errorf("since 日期格式错误（应为 2006-01-02）")
			}
			since = &t
		}
		if untilStr != "" {
			t, e := time.ParseInLocation("2006-01-02", untilStr, loc)
			if e != nil {
				return nil, nil, fmt.Errorf("until 日期格式错误（应为 2006-01-02）")
			}
			t = t.AddDate(0, 0, 1)
			until = &t
		}
		return since, until, nil
	}
	if days <= 0 {
		days = 7
	}
	d := now.AddDate(0, 0, -days)
	since = &d
	return since, nil, nil
}