package handler

import (
	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"

	"github.com/gin-gonic/gin"
)

// RequirementHandler 新产品需求分析 HTTP 处理器。
type RequirementHandler struct {
	svc *service.Service
}

// NewRequirementHandler 构建需求分析处理器。
func NewRequirementHandler(svc *service.Service) *RequirementHandler {
	return &RequirementHandler{svc: svc}
}

// Analyze 新产品需求分析，生成开发方案。
// 仅做参数校验，业务逻辑调用 RequirementService。
//
//	POST /api/v1/requirement/analyze
func (h *RequirementHandler) Analyze(c *gin.Context) {
	var req service.AnalyzeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	result, err := h.svc.Requirement.Analyze(c.Request.Context(), &req)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, result)
}