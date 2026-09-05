package model

import "time"

// LLMUsage LLM 调用消耗明细，对应 llm_usage。
// 每次 LLM 调用落一条，用于统计各模型 token/金额消耗。
type LLMUsage struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ModelName   string    `gorm:"column:model_name;size:64;not null;index:idx_model_time" json:"model_name"` // 实际使用模型
	Scenario    string    `gorm:"column:scenario;size:32;not null;index:idx_scenario" json:"scenario"`       // 调用场景：doc/chat/search/impact/design/func_change/rollup/requirement
	InputTokens int       `gorm:"column:input_tokens;not null;default:0" json:"input_tokens"`                // 输入 token 数
	OutputTokens int      `gorm:"column:output_tokens;not null;default:0" json:"output_tokens"`              // 输出 token 数
	Cost        float64   `gorm:"column:cost;not null;default:0" json:"cost"`                                // 本次调用估算成本（元）
	CreateTime  time.Time `gorm:"column:create_time;autoCreateTime;index:idx_model_time" json:"create_time"`
}

// TableName 指定表名。
func (LLMUsage) TableName() string {
	return "llm_usage"
}