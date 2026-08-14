package model

import "time"

// RelationModifyLog 模块依赖关系操作日志，对应 relation_modify_log。
type RelationModifyLog struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	SourceModule string    `gorm:"column:source_module;size:64;not null" json:"source_module"`
	TargetModule string    `gorm:"column:target_module;size:64;not null" json:"target_module"`
	OperateType  int8      `gorm:"column:operate_type;not null" json:"operate_type"` // 操作类型：1新增 2编辑 3删除
	Operator     string    `gorm:"column:operator;size:64;not null" json:"operator"`
	OperateTime  time.Time `gorm:"column:operate_time;autoCreateTime" json:"operate_time"`
	Remark       string    `gorm:"column:remark;size:512;default:''" json:"remark"`
}

// TableName 指定表名。
func (RelationModifyLog) TableName() string {
	return "relation_modify_log"
}