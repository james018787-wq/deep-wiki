package model

import "time"

// CodeSecretFinding 代码安全扫描发现（硬编码密钥/密码），对应 code_secret_finding。
type CodeSecretFinding struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RepoID      int64     `gorm:"column:repo_id;not null;index:idx_repo" json:"repo_id"`        // 所属仓库id
	FilePath    string    `gorm:"column:file_path;size:512;not null" json:"file_path"`          // 文件路径
	Line        int       `gorm:"column:line;default:0" json:"line"`                            // 命中行号（1基）
	SecretType  string    `gorm:"column:secret_type;size:32;not null" json:"secret_type"`       // 类型：aws_key/github_token/password/...
	RiskLevel   string    `gorm:"column:risk_level;size:16;default:'medium'" json:"risk_level"` // high/medium/low
	SecretValue string    `gorm:"column:secret_value;size:256;default:''" json:"secret_value"`  // 命中的敏感值（脱敏存储）
	Snippet     string    `gorm:"column:snippet;type:text" json:"snippet"`                      // 所在行文本（脱敏）
	Recommend   string    `gorm:"column:recommendation;type:text" json:"recommendation"`        // 修复建议
	Status      string    `gorm:"column:status;size:16;default:'open'" json:"status"`           // open/fixed/false_positive
	CreateTime  time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	UpdateTime  time.Time `gorm:"column:update_time;autoUpdateTime" json:"update_time"`
	IsDeleted   int8      `gorm:"column:is_deleted;default:0" json:"is_deleted"`
}

// TableName 指定表名。
func (CodeSecretFinding) TableName() string {
	return "code_secret_finding"
}
