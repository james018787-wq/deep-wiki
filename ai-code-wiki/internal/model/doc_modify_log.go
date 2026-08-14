package model

import "time"

// DocModifyLog 文档人工校正日志，对应 doc_modify_log。
// 人工编辑/重置文档时，修改前后的完整文档JSON必须落库，用于审计与追溯。
type DocModifyLog struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	DocID        int64     `gorm:"column:doc_id;not null;index:idx_doc_id" json:"doc_id"` // 关联 code_function_doc 主键
	OperateType  int8      `gorm:"column:operate_type;not null" json:"operate_type"`      // 操作类型：1编辑文档 2重置回AI原始版本
	BeforeContent string   `gorm:"column:before_content;type:text" json:"before_content"` // 修改前完整文档JSON
	AfterContent string   `gorm:"column:after_content;type:text" json:"after_content"`    // 修改后完整文档JSON
	Operator     string    `gorm:"column:operator;size:64;not null" json:"operator"`      // 操作人
	OperateTime  time.Time `gorm:"column:operate_time;autoCreateTime" json:"operate_time"`
	Remark       string    `gorm:"column:remark;size:512;default:''" json:"remark"`
}

// TableName 指定表名。
func (DocModifyLog) TableName() string {
	return "doc_modify_log"
}