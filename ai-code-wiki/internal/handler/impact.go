package handler

import (
	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"

	"github.com/gin-gonic/gin"
)

// ImpactHandler 迭代影响分析 HTTP 处理器。
type ImpactHandler struct {
	svc *service.Service
}

// NewImpactHandler 构建影响分析处理器。
func NewImpactHandler(svc *service.Service) *ImpactHandler {
	return &ImpactHandler{svc: svc}
}

// Analyze 迭代影响分析。
//
//	POST /api/v1/impact/analyze
//
// body: {"repo_id":1, "branch":"feature/impact-demo"}  或
//
//	{"repo_id":1, "functions":[{"module":"order","func":"CreateOrder"}], "max_depth":2}
func (h *ImpactHandler) Analyze(c *gin.Context) {
	var req service.ImpactAnalyzeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	if req.RepoID <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "repo_id 参数错误")
		return
	}
	result, err := h.svc.Impact.Analyze(c.Request.Context(), &req)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, result)
}
