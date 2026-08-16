package handler

import (
	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"

	"github.com/gin-gonic/gin"
)

// RepoHandler 代码仓库注册 HTTP 处理器（多仓库支持）。
type RepoHandler struct {
	svc *service.Service
}

// NewRepoHandler 构建仓库注册处理器。
func NewRepoHandler(svc *service.Service) *RepoHandler {
	return &RepoHandler{svc: svc}
}

// Register 注册代码仓库（幂等）。
//
//	POST /api/v1/repo/register
//	{"repo_name":"testrepo","repo_url":"https://xxx/testrepo.git","default_branch":"main","description":""}
func (h *RepoHandler) Register(c *gin.Context) {
	var req service.RegisterRepoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	info, err := h.svc.Repo.Register(c.Request.Context(), &req)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, info)
}

// List 获取所有启用仓库。
//
//	GET /api/v1/repo/list
func (h *RepoHandler) List(c *gin.Context) {
	list, err := h.svc.Repo.List(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, list)
}

// SetStatus 启用/停用仓库。
//
//	PUT /api/v1/repo/:repo_id/status
//	{"status":1|2}
func (h *RepoHandler) SetStatus(c *gin.Context) {
	repoID := common.Str2Int64(c.Param("repo_id"))
	if repoID <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "repo_id 参数错误")
		return
	}
	var req service.SetStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	if err := h.svc.Repo.SetStatus(c.Request.Context(), repoID, &req); err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, nil)
}