package handler

import (
	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"

	"github.com/gin-gonic/gin"
)

// RelationHandler 模块依赖知识图谱 HTTP 处理器。
type RelationHandler struct {
	svc *service.Service
}

// NewRelationHandler 构建依赖关系处理器。
func NewRelationHandler(svc *service.Service) *RelationHandler {
	return &RelationHandler{svc: svc}
}

// ListRelations 查询模块上下游依赖。
// 仅做参数校验与转换，业务逻辑调用 RelationService。
//
//	GET /api/v1/relation/list?repo_id=xx&module=xxx&direction=out|in
func (h *RelationHandler) ListRelations(c *gin.Context) {
	repoID := common.Str2Int64(c.Query("repo_id"))
	if repoID <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "repo_id 参数错误")
		return
	}
	direction := c.DefaultQuery("direction", "out")
	if direction != "out" && direction != "in" {
		common.Fail(c, 400, common.CodeBadRequest, "direction 仅支持 out/in")
		return
	}
	req := service.ListRelationReq{
		RepoID:    repoID,
		Module:    c.Query("module"),
		Direction: direction,
	}
	if req.Module == "" {
		common.Fail(c, 400, common.CodeBadRequest, "module 参数错误")
		return
	}
	list, err := h.svc.Relation.ListRelations(c.Request.Context(), &req)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, list)
}

// AddRelation 手动新增模块依赖关系。
// 业务逻辑（含 relation_modify_log 操作日志）在 service 层。
//
//	POST /api/v1/relation/add
func (h *RelationHandler) AddRelation(c *gin.Context) {
	var req service.AddRelationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	if err := h.svc.Relation.AddRelation(c.Request.Context(), &req); err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, nil)
}

// DeleteRelation 删除模块依赖。
// 业务逻辑（含 relation_modify_log 操作日志）在 service 层。
//
//	DELETE /api/v1/relation
func (h *RelationHandler) DeleteRelation(c *gin.Context) {
	var req service.DeleteRelationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	if err := h.svc.Relation.DeleteRelation(c.Request.Context(), &req); err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, nil)
}
