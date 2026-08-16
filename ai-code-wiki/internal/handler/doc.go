package handler

import (
	"errors"
	"io"
	"strconv"

	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/logger"

	"github.com/gin-gonic/gin"
)

// handleError 统一错误响应：识别业务错误（*common.AppError）透传错误码，
// 其余按系统内部错误处理，并记录到统一日志（含 request_id 与堆栈）。
func handleError(c *gin.Context, err error) {
	var appErr *common.AppError
	if errors.As(err, &appErr) {
		common.FailWithAppError(c, appErr)
		return
	}
	logger.Error(c.Request.Context(), "系统内部错误: %v", err)
	common.FailWithAppError(c, common.WrapError(common.CodeInternalError, "系统内部错误", err))
}

// DocHandler 业务文档 HTTP 处理器。
type DocHandler struct {
	svc *service.Service
}

// NewDocHandler 构建文档处理器。
func NewDocHandler(svc *service.Service) *DocHandler {
	return &DocHandler{svc: svc}
}

// Search 自然语言查询业务（跨模块检索）。
// 仅做参数校验，业务流水线调用 SearchService。
//
//	POST /api/v1/doc/search
func (h *DocHandler) Search(c *gin.Context) {
	var req service.SearchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	result, err := h.svc.Search.Search(c.Request.Context(), &req)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, result)
}

// ListModules 获取指定仓库的所有业务模块。
//
//	GET /api/v1/doc/module/list?repo_id=xx
func (h *DocHandler) ListModules(c *gin.Context) {
	repoID := common.Str2Int64(c.Query("repo_id"))
	if repoID <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "repo_id 参数错误")
		return
	}
	modules, err := h.svc.Doc.ListModules(c.Request.Context(), repoID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, modules)
}

// List 分页查询函数文档列表，支持按模块筛选（前端文档列表页使用）。
//
//	GET /api/v1/doc/list?repo_id=xx&module=xxx&page=1&page_size=20
func (h *DocHandler) List(c *gin.Context) {
	repoID := common.Str2Int64(c.Query("repo_id"))
	if repoID <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "repo_id 参数错误")
		return
	}
	page, pageSize := parsePage(c)
	result, err := h.svc.Doc.ListDocs(c.Request.Context(), repoID, c.Query("module"), page, pageSize)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, result)
}

// GetDoc 获取文档详情。
//
//	GET /api/v1/doc/:doc_id
func (h *DocHandler) GetDoc(c *gin.Context) {
	docID, ok := parseDocID(c)
	if !ok {
		return
	}
	doc, err := h.svc.Doc.GetDoc(c.Request.Context(), docID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, doc)
}

// GetDocSource 读取文档对应源码文件内容。
//
//	GET /api/v1/doc/:doc_id/source
func (h *DocHandler) GetDocSource(c *gin.Context) {
	docID, ok := parseDocID(c)
	if !ok {
		return
	}
	src, err := h.svc.Doc.GetSource(c.Request.Context(), docID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, src)
}

// GetDocGraph 查询文档对应函数的调用图（上游调用方 + 下游被调用方 + 自身）。
//
//	GET /api/v1/doc/:doc_id/graph
func (h *DocHandler) GetDocGraph(c *gin.Context) {
	docID, ok := parseDocID(c)
	if !ok {
		return
	}
	graph, err := h.svc.Doc.GetDocGraph(docID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, graph)
}

// EditDoc 人工校正业务文档。
//
//	PUT /api/v1/doc/:doc_id/edit
func (h *DocHandler) EditDoc(c *gin.Context) {
	docID, ok := parseDocID(c)
	if !ok {
		return
	}
	var req service.EditDocReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	if err := h.svc.Doc.EditDoc(c.Request.Context(), docID, &req, authOperator(c)); err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, nil)
}

// ResetDoc 文档重置为原始 AI 版本。
//
//	POST /api/v1/doc/:doc_id/reset
func (h *DocHandler) ResetDoc(c *gin.Context) {
	docID, ok := parseDocID(c)
	if !ok {
		return
	}
	var req struct {
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	if err := h.svc.Doc.ResetDoc(c.Request.Context(), docID, authOperator(c)); err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, nil)
}

// authOperator 从鉴权上下文解析当前操作人：用户令牌取 username，
// server-to-server（X-Api-Secret）回退为 api，未知回退 system。
func authOperator(c *gin.Context) string {
	if name := c.GetString("username"); name != "" {
		return name
	}
	if by := c.GetString("auth_by"); by != "" {
		return by
	}
	return "system"
}

// ListModifiedDocs 查询指定仓库所有人工校正文档。
//
//	GET /api/v1/doc/modified/list?repo_id=xx&page=1&page_size=20
func (h *DocHandler) ListModifiedDocs(c *gin.Context) {
	repoID := common.Str2Int64(c.Query("repo_id"))
	if repoID <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "repo_id 参数错误")
		return
	}
	page, pageSize := parsePage(c)
	result, err := h.svc.Doc.ListModifiedDocs(c.Request.Context(), repoID, page, pageSize)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, result)
}

// ListChangeLogs 查看文档迭代变更记录。
//
//	GET /api/v1/doc/changelog?doc_id=xx
func (h *DocHandler) ListChangeLogs(c *gin.Context) {
	docID := common.Str2Int64(c.Query("doc_id"))
	if docID <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "doc_id 参数错误")
		return
	}
	logs, err := h.svc.Doc.ListChangeLogs(c.Request.Context(), docID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, logs)
}

// parseDocID 解析路径参数 :doc_id。
func parseDocID(c *gin.Context) (int64, bool) {
	id := common.Str2Int64(c.Param("doc_id"))
	if id <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "doc_id 参数错误")
		return 0, false
	}
	return id, true
}

// ListDocHistory 查看文档全部修改记录。
//
//	GET /api/v1/doc/:doc_id/history
func (h *DocHandler) ListDocHistory(c *gin.Context) {
	docID, ok := parseDocID(c)
	if !ok {
		return
	}
	list, err := h.svc.Doc.ListDocHistory(c.Request.Context(), docID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, list)
}

// GetDocHistoryDetail 获取某一条历史快照详情。
//
//	GET /api/v1/doc/:doc_id/history/:log_id
func (h *DocHandler) GetDocHistoryDetail(c *gin.Context) {
	docID, ok := parseDocID(c)
	if !ok {
		return
	}
	logID := common.Str2Int64(c.Param("log_id"))
	if logID <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "log_id 参数错误")
		return
	}
	detail, err := h.svc.Doc.GetDocHistoryDetail(c.Request.Context(), docID, logID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, detail)
}

// parsePage 解析分页参数。
func parsePage(c *gin.Context) (int, int) {
	page := 1
	pageSize := common.DefaultPageSize
	if v, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && v > 0 {
		page = v
	}
	if v, err := strconv.Atoi(c.DefaultQuery("page_size", "20")); err == nil && v > 0 {
		pageSize = v
	}
	if pageSize > common.MaxPageSize {
		pageSize = common.MaxPageSize
	}
	return page, pageSize
}
