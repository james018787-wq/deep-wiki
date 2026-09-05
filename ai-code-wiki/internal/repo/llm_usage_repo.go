package repo

import (
	"time"

	"ai-code-wiki/internal/model"

	"gorm.io/gorm"
)

// LLMUsageRepo LLM 消耗明细仓库。
type LLMUsageRepo struct {
	*BaseRepo[model.LLMUsage]
}

// NewLLMUsageRepo 构建 LLM 消耗明细仓库。
func NewLLMUsageRepo(db *gorm.DB) *LLMUsageRepo {
	return &LLMUsageRepo{BaseRepo: &BaseRepo[model.LLMUsage]{DB: db}}
}

// UsageRow 聚合结果行（按模型或按日）。
type UsageRow struct {
	GroupKey     string  `json:"group_key"` // 聚合键：模型名或日期(2006-01-02)
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	Calls        int64   `json:"calls"`
	Cost         float64 `json:"cost"`
}

// AggregateByModel 按模型聚合消耗（可选按场景/时间范围过滤）。
func (r *LLMUsageRepo) AggregateByModel(since, until *time.Time, scenario string) ([]*UsageRow, error) {
	q := r.DB.Model(&model.LLMUsage{})
	q = applyUsageFilter(q, since, until, scenario)
	q = q.Select("model_name AS group_key, SUM(input_tokens) AS input_tokens, SUM(output_tokens) AS output_tokens, " +
		"SUM(input_tokens + output_tokens) AS total_tokens, COUNT(*) AS calls, SUM(cost) AS cost").
		Group("model_name").Order("cost DESC")
	return scanUsageRows(q)
}

// AggregateByDay 按日期聚合消耗（可选按模型/时间范围过滤），时间倒序。
func (r *LLMUsageRepo) AggregateByDay(since, until *time.Time, scenario string) ([]*UsageRow, error) {
	q := r.DB.Model(&model.LLMUsage{})
	q = applyUsageFilter(q, since, until, scenario)
	q = q.Select("DATE_FORMAT(create_time, '%Y-%m-%d') AS group_key, SUM(input_tokens) AS input_tokens, " +
		"SUM(output_tokens) AS output_tokens, SUM(input_tokens + output_tokens) AS total_tokens, COUNT(*) AS calls, SUM(cost) AS cost").
		Group("DATE_FORMAT(create_time, '%Y-%m-%d')").Order("group_key DESC")
	return scanUsageRows(q)
}

// AggregateByScenario 按场景聚合消耗（可选按模型/时间范围过滤）。
func (r *LLMUsageRepo) AggregateByScenario(since, until *time.Time, scenario string) ([]*UsageRow, error) {
	q := r.DB.Model(&model.LLMUsage{})
	q = applyUsageFilter(q, since, until, scenario)
	q = q.Select("scenario AS group_key, SUM(input_tokens) AS input_tokens, SUM(output_tokens) AS output_tokens, " +
		"SUM(input_tokens + output_tokens) AS total_tokens, COUNT(*) AS calls, SUM(cost) AS cost").
		Group("scenario").Order("cost DESC")
	return scanUsageRows(q)
}

// TotalSummary 总消耗汇总（可选过滤）。
func (r *LLMUsageRepo) TotalSummary(since, until *time.Time, scenario string) (*UsageRow, error) {
	q := r.DB.Model(&model.LLMUsage{})
	q = applyUsageFilter(q, since, until, scenario)
	var row UsageRow
	err := q.Select("SUM(input_tokens) AS input_tokens, SUM(output_tokens) AS output_tokens, " +
		"SUM(input_tokens + output_tokens) AS total_tokens, COUNT(*) AS calls, SUM(cost) AS cost").
		Scan(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// scanUsageRows 执行聚合查询并扫描结果。
func scanUsageRows(q *gorm.DB) ([]*UsageRow, error) {
	var rows []*UsageRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// applyUsageFilter 组装通用过滤条件。
func applyUsageFilter(q *gorm.DB, since, until *time.Time, scenario string) *gorm.DB {
	if since != nil {
		q = q.Where("create_time >= ?", *since)
	}
	if until != nil {
		q = q.Where("create_time < ?", *until)
	}
	if scenario != "" {
		q = q.Where("scenario = ?", scenario)
	}
	return q
}