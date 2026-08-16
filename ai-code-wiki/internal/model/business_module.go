// Package model 定义数据库表对应的 GORM 结构体。
package model

import "time"

// BusinessModule 业务模块表，对应 business_module。
type BusinessModule struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RepoID     int64     `gorm:"column:repo_id;not null;uniqueIndex:idx_module_name" json:"repo_id"`       // 所属仓库id
	ModuleName string    `gorm:"column:module_name;size:64;not null;uniqueIndex:idx_module_name" json:"module_name"` // 业务模块名称
	Desc       string    `gorm:"column:desc;size:512;default:''" json:"desc"`                              // 模块说明
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	UpdateTime time.Time `gorm:"column:update_time;autoUpdateTime" json:"update_time"`
	IsDeleted  int8      `gorm:"column:is_deleted;default:0" json:"is_deleted"` // 逻辑删除标记
}

// TableName 指定表名。
func (BusinessModule) TableName() string {
	return "business_module"
}