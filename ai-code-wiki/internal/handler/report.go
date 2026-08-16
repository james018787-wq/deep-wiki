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

// Basic 基础统计。
//
//	GET /api/v1/report/basic?repo_id=xx
// 未传 repo_id 时统计全部仓库。
func (h *ReportHandler) Basic(c *gin.Context) {
	repoID := common.Str2Int64(c.Query("repo_id"))
	data, err := h.svc.Report.Basic(c.Request.Context(), repoID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, data)
}
