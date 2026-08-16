package handler

import (
	"strconv"

	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"

	"github.com/gin-gonic/gin"
)

// ChatHandler 多轮对话 HTTP 处理器。
type ChatHandler struct {
	svc *service.Service
}

// NewChatHandler 构建多轮对话处理器。
func NewChatHandler(svc *service.Service) *ChatHandler {
	return &ChatHandler{svc: svc}
}

// Ask 多轮问答（Redis 会话记忆）。
//
//	POST /api/v1/chat/ask
//
// body: {"repo_id":1, "session_id":"可选", "query":"下单模块的详细逻辑"}
func (h *ChatHandler) Ask(c *gin.Context) {
	var req service.ChatAskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	result, err := h.svc.Chat.Ask(c.Request.Context(), &req)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, result)
}

// ListSessions 会话列表。
//
//	GET /api/v1/chat/sessions?repo_id=1
func (h *ChatHandler) ListSessions(c *gin.Context) {
	repoID := parseInt64(c.Query("repo_id"))
	if repoID <= 0 {
		common.Fail(c, 400, common.CodeBadRequest, "repo_id 参数错误")
		return
	}
	list, err := h.svc.Chat.ListSessions(c.Request.Context(), repoID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, list)
}

// History 会话历史消息。
//
//	GET /api/v1/chat/history?session_id=xxx
func (h *ChatHandler) History(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		common.Fail(c, 400, common.CodeBadRequest, "session_id 参数错误")
		return
	}
	msgs, err := h.svc.Chat.History(c.Request.Context(), sessionID)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, msgs)
}

// DeleteSession 删除会话。
//
//	DELETE /api/v1/chat/session?session_id=xxx
func (h *ChatHandler) DeleteSession(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		common.Fail(c, 400, common.CodeBadRequest, "session_id 参数错误")
		return
	}
	if err := h.svc.Chat.DeleteSession(c.Request.Context(), sessionID); err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, nil)
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
