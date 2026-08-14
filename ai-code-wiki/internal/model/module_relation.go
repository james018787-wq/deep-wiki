package model

import "time"

// ModuleRelation 模块依赖知识图谱表，对应 module_relation。
// 关系来源包括 AST 自动识别与人工手动添加，两者取并集展示。
type ModuleRelation struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	SourceModule string    `gorm:"column:source_module;size:64;not null;uniqueIndex:idx_module_relation" json:"source_module"` // 源模块
	TargetModule string    `gorm:"column:target_module;size:64;not null;uniqueIndex:idx_module_relation" json:"target_module"` // 被依赖模块
	RelationType int8      `gorm:"column:relation_type;not null;uniqueIndex:idx_module_relation" json:"relation_type"`        // 关系类型：1同步调用 2异步MQ事件
	Source       int8      `gorm:"column:source;not null;default:1" json:"source"`                                             // 关系来源：1=AST自动识别 2=人工手动添加
	Creator      string    `gorm:"column:creator;size:64;default:''" json:"creator"`                                           // 创建人
	Remark       string    `gorm:"column:remark;size:512;default:''" json:"remark"`                                            // 备注说明
	CreateTime   time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	UpdateTime   time.Time `gorm:"column:update_time;autoUpdateTime" json:"update_time"`
	IsDeleted    int8      `gorm:"column:is_deleted;default:0;uniqueIndex:idx_module_relation" json:"is_deleted"` // 逻辑删除标记
}

// TableName 指定表名。
func (ModuleRelation) TableName() string {
	return "module_relation"
}