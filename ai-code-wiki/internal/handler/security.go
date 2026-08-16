package handler

import (
	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"

	"github.com/gin-gonic/gin"
)

// SecurityHandler 代码安全扫描 HTTP 处理器。
type SecurityHandler struct {
	svc *service.Service
}

// NewSecurityHandler 构建安全扫描处理器。
func NewSecurityHandler(svc *service.Service) *SecurityHandler {
	return &SecurityHandler{svc: svc}
}

// Scan 触发代码安全扫描。
//
//	POST /api/v1/security/scan
//
// body: {"repo_id": 1}
func (h *SecurityHandler) Scan(c *gin.Context) {
	var req struct {
		RepoID int64 `json:"repo_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.RepoID <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "repo_id 参数错误")
		return
	}
	summary, err := h.svc.SecretScan.ScanRepo(c.Request.Context(), req.RepoID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, summary)
}

// List 分页查询安全发现。
//
//	GET /api/v1/security/list?repo_id=1&status=open&risk=high&page=1&page_size=20
func (h *SecurityHandler) List(c *gin.Context) {
	repoID := common.Str2Int64(c.Query("repo_id"))
	if repoID <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "repo_id 参数错误")
		return
	}
	page := common.Str2Int64(c.DefaultQuery("page", "1"))
	pageSize := common.Str2Int64(c.DefaultQuery("page_size", "20"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > common.MaxPageSize {
		pageSize = common.DefaultPageSize
	}
	list, total, err := h.svc.SecretScan.List(c.Request.Context(), repoID,
		c.Query("status"), c.Query("risk"), int(page), int(pageSize))
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, map[string]any{
		"list":      list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// UpdateStatus 更新安全发现状态。
//
//	PUT /api/v1/security/:id/status
//
// body: {"status": "false_positive"}
func (h *SecurityHandler) UpdateStatus(c *gin.Context) {
	id := common.Str2Int64(c.Param("id"))
	if id <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "id 参数错误")
		return
	}
	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	if err := h.svc.SecretScan.UpdateStatus(c.Request.Context(), id, req.Status); err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, nil)
}
