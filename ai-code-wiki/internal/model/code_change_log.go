package model

import "time"

// CodeChangeLog 代码迭代变更历史记录，对应 code_change_log。
type CodeChangeLog struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RepoID        int64     `gorm:"column:repo_id;not null;index:idx_repo_id" json:"repo_id"`     // 所属仓库id
	DocID         int64     `gorm:"column:doc_id;not null;index:idx_doc_id" json:"doc_id"`       // 关联文档id
	Version       string    `gorm:"column:version;size:128;default:''" json:"version"`                   // 发布版本
	ChangeSummary string    `gorm:"column:change_summary;type:text" json:"change_summary"`               // 代码变更摘要
	BusinessImpact string   `gorm:"column:business_impact;type:text" json:"business_impact"`             // 业务影响范围
	Attention     string    `gorm:"column:attention;type:text" json:"attention"`                         // 上线注意事项
	CreateTime    time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
}

// TableName 指定表名。
func (CodeChangeLog) TableName() string {
	return "code_change_log"
}