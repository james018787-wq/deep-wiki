package handler

import (
	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"

	"github.com/gin-gonic/gin"
) // ReportHandler 知识库统计接口。
type ReportHandler struct {
	svc *service.Service
}

// NewReportHandler 构建统计处理器。
func NewReportHandler(svc *service.Service) *ReportHandler {
	return &ReportHandler{svc: svc}
}

// Basic 基础统计 GET /api/v1/report/basic。
// 无入参，仅调用统计服务输出知识库基础指标。
func (h *ReportHandler) Basic(c *gin.Context) {
	data, err := h.svc.Report.Basic(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, data)
}
