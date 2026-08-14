package repo

import (
	"ai-code-wiki/internal/model"

	"gorm.io/gorm"
)

// TaskRecordRepo 代码解析任务记录仓库。
type TaskRecordRepo struct {
	*BaseRepo[model.TaskRecord]
}

// NewTaskRecordRepo 构建任务记录仓库。
func NewTaskRecordRepo(db *gorm.DB) *TaskRecordRepo {
	return &TaskRecordRepo{BaseRepo: &BaseRepo[model.TaskRecord]{DB: db}}
}

// GetByTaskID 按任务唯一标识查询。
//
// 注意：task_record 表【没有 is_deleted 列】，不能复用 BaseRepo 的逻辑删除过滤
// （withNotDeleted 会生成 WHERE is_deleted=0，MySQL 报未知列错误）。
func (r *TaskRecordRepo) GetByTaskID(taskID string) (*model.TaskRecord, error) {
	var m model.TaskRecord
	if err := r.DB.Where("task_id = ?", taskID).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// ListByWhere 覆盖 BaseRepo 同名方法：task_record 表无 is_deleted 列，不做逻辑删除过滤。
func (r *TaskRecordRepo) ListByWhere(where map[string]any, order string, page, pageSize int) ([]*model.TaskRecord, int64, error) {
	var list []*model.TaskRecord
	var total int64
	query := r.DB.Model(&model.TaskRecord{}).Where(where)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page > 0 && pageSize > 0 {
		query = query.Offset((page - 1) * pageSize).Limit(pageSize)
	}
	if order != "" {
		query = query.Order(order)
	}
	if err := query.Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// UpdateStatus 更新任务状态与错误信息。
func (r *TaskRecordRepo) UpdateStatus(taskID string, status int8, errMsg string) error {
	return r.DB.Model(&model.TaskRecord{}).
		Where("task_id = ?", taskID).
		Updates(map[string]any{"status": status, "err_msg": errMsg}).Error
}