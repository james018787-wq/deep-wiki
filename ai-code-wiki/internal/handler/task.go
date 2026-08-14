package handler

import (
	"strings"

	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"

	"github.com/gin-gonic/gin"
)

// TaskHandler 代码解析任务 HTTP 处理器。
type TaskHandler struct {
	svc *service.Service
}

// NewTaskHandler 构建任务处理器。
func NewTaskHandler(svc *service.Service) *TaskHandler {
	return &TaskHandler{svc: svc}
}

// Trigger 触发代码解析任务（CI 回调）。
//
//	POST /api/v1/task/trigger
func (h *TaskHandler) Trigger(c *gin.Context) {
	var req service.TriggerTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	record, err := h.svc.Task.TriggerTask(c.Request.Context(), &req)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, record)
}

// Status 查询任务状态。
// 仅做参数校验，业务逻辑调用 TaskQueryService。
//
//	GET /api/v1/task/status?task_id=xxx
func (h *TaskHandler) Status(c *gin.Context) {
	taskID := strings.TrimSpace(c.Query("task_id"))
	if taskID == "" {
		common.Fail(c, 400, common.CodeBadRequest, "task_id 参数错误")
		return
	}
	result, err := h.svc.TaskQuery.GetStatus(c.Request.Context(), taskID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, result)
}

// List 任务列表，分页查询，按时间倒序。
// 仅做参数校验，业务逻辑调用 TaskQueryService。
//
//	GET /api/v1/task/list?page=1&page_size=20
func (h *TaskHandler) List(c *gin.Context) {
	page, pageSize := parsePage(c)
	result, err := h.svc.TaskQuery.List(c.Request.Context(), page, pageSize)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, result)
}