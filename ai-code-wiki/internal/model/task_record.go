package model

import "time"

// TaskRecord 代码解析任务记录表，对应 task_record。
type TaskRecord struct {
	ID         int64      `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TaskID     string     `gorm:"column:task_id;size:64;not null;uniqueIndex:idx_task_id" json:"task_id"` // 任务唯一标识
	Branch     string     `gorm:"column:branch;size:128;not null" json:"branch"`                          // 代码分支
	Status     int8       `gorm:"column:status;not null" json:"status"`                                   // 任务状态：0待执行 1执行中 2成功 3失败
	RetryCount int        `gorm:"column:retry_count;not null;default:0" json:"retry_count"`               // 失败重试次数（队列消费失败重新投递时自增）
	ErrMsg     string     `gorm:"column:err_msg;type:text" json:"err_msg"`                                // 错误信息
	CreateTime time.Time  `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	FinishTime *time.Time `gorm:"column:finish_time" json:"finish_time"`
}

// TableName 指定表名。
func (TaskRecord) TableName() string {
	return "task_record"
}
