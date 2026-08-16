package model

import "time"

// CodeRepo 代码仓库注册表。
// 团队可注册多个代码仓库，知识库按 repo_id 隔离（文档/模块/依赖/任务均归属某仓库）。
type CodeRepo struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RepoName      string    `gorm:"column:repo_name;size:64;not null;uniqueIndex:idx_repo_name" json:"repo_name"` // 仓库名称（唯一，如 order-service）
	RepoURL       string    `gorm:"column:repo_url;size:512;not null" json:"repo_url"`                            // git 克隆地址
	DefaultBranch string    `gorm:"column:default_branch;size:64;default:'main'" json:"default_branch"`           // 默认分支（diff 基线）
	Description   string    `gorm:"column:description;size:255;default:''" json:"description"`                    // 仓库说明
	AuthToken     string    `gorm:"column:auth_token;size:512;default:''" json:"-"`                               // 仓库访问令牌（加密存储，出参脱敏）
	HasToken      bool      `gorm:"-" json:"has_token"`                                                           // 是否已配置令牌（非持久化字段）
	Status        int8      `gorm:"column:status;default:1" json:"status"`                                        // 状态：1启用 0停用
	CreateTime    time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	UpdateTime    time.Time `gorm:"column:update_time;autoUpdateTime" json:"update_time"`
	IsDeleted     int8      `gorm:"column:is_deleted;default:0" json:"is_deleted"` // 逻辑删除标记
}

// TableName 指定表名。
func (CodeRepo) TableName() string {
	return "code_repo"
}
