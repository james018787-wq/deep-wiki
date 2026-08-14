package service

import (
	"context"
	"errors"
	"time"

	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/common"

	"gorm.io/gorm"
)

// TaskQueryService 代码解析任务查询业务逻辑（只读 task_record 表）。
type TaskQueryService struct {
	taskRepo *repo.TaskRecordRepo
}

// NewTaskQueryService 构建任务查询服务。
func NewTaskQueryService(db *gorm.DB) *TaskQueryService {
	return &TaskQueryService{
		taskRepo: newTaskRepo(db),
	}
}

// TaskStatusResult 任务状态查询结果。
type TaskStatusResult struct {
	TaskID     string     `json:"task_id"`     // 任务唯一标识
	Branch     string     `json:"branch"`      // 代码分支
	Status     int8       `json:"status"`      // 任务状态：0待执行 1执行中 2成功 3失败
	StatusDesc string     `json:"status_desc"` // 任务状态描述
	ErrMsg     string     `json:"err_msg"`     // 错误信息
	CreateTime time.Time  `json:"create_time"` // 创建时间
	FinishTime *time.Time `json:"finish_time"` // 完成时间
}

// GetStatus 查询任务状态。
// 任务不存在时返回业务错误 CodeNotFound。
func (s *TaskQueryService) GetStatus(ctx context.Context, taskID string) (*TaskStatusResult, error) {
	_ = ctx
	record, err := s.taskRepo.GetByTaskID(taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError(common.CodeNotFound, "任务不存在")
		}
		return nil, common.WrapError(common.CodeInternalError, "查询任务失败", err)
	}
	return &TaskStatusResult{
		TaskID:     record.TaskID,
		Branch:     record.Branch,
		Status:     record.Status,
		StatusDesc: taskStatusDesc(record.Status),
		ErrMsg:     record.ErrMsg,
		CreateTime: record.CreateTime,
		FinishTime: record.FinishTime,
	}, nil
}

// List 任务列表，分页查询，按创建时间倒序。
func (s *TaskQueryService) List(ctx context.Context, page, pageSize int) (*common.PageResult, error) {
	_ = ctx
	list, total, err := s.taskRepo.ListByWhere(map[string]any{}, "create_time desc, id desc", page, pageSize)
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "查询任务列表失败", err)
	}
	return &common.PageResult{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// taskStatusDesc 任务状态数字转中文描述。
func taskStatusDesc(status int8) string {
	switch status {
	case common.TaskStatusPending:
		return "待执行"
	case common.TaskStatusRunning:
		return "执行中"
	case common.TaskStatusSuccess:
		return "成功"
	case common.TaskStatusFailed:
		return "失败"
	default:
		return "未知"
	}
}