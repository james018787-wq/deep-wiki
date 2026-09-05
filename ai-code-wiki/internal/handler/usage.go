package handler

import (
	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"

	"github.com/gin-gonic/gin"
)

// UsageHandler LLM 模型配置与消耗统计 HTTP 处理器。
type UsageHandler struct {
	svc *service.Service
}

// NewUsageHandler 构建模型与用量处理器。
func NewUsageHandler(svc *service.Service) *UsageHandler {
	return &UsageHandler{svc: svc}
}

// ListModels 模型池配置（脱敏）。
//
//	GET /api/v1/model/list
func (h *UsageHandler) ListModels(c *gin.Context) {
	info, err := h.svc.Usage.ListModels(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, info)
}

// ListModelStatus 模型运行状态（熔断/限流/降级次数）。
//
//	GET /api/v1/model/status
func (h *UsageHandler) ListModelStatus(c *gin.Context) {
	info, err := h.svc.Usage.ListModelStatus(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, info)
}

// GetUsage 消耗统计。
//
//	GET /api/v1/model/usage?days=7&scenario=&group_by=model&since=&until=
func (h *UsageHandler) GetUsage(c *gin.Context) {
	q := &service.UsageQuery{
		Days:     int(common.Str2Int64(c.DefaultQuery("days", "7"))),
		Since:    c.Query("since"),
		Until:    c.Query("until"),
		Scenario: c.Query("scenario"),
		GroupBy:  c.Query("group_by"),
	}
	data, err := h.svc.Usage.GetUsage(c.Request.Context(), q)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, data)
}