package handler

import (
	"strings"

	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"

	"github.com/gin-gonic/gin"
)

// bearerToken 从请求头提取 Bearer token。
func bearerToken(c *gin.Context) string {
	ah := c.GetHeader("Authorization")
	if strings.HasPrefix(ah, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(ah, "Bearer "))
	}
	return c.GetHeader("X-Auth-Token")
}

// AuthHandler 登录鉴权 HTTP 处理器。
type AuthHandler struct {
	svc *service.Service
}

// NewAuthHandler 构建鉴权处理器。
func NewAuthHandler(svc *service.Service) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Login 登录。
//
//	POST /api/v1/auth/login （公开，不经过鉴权中间件）
//
// body: {"username":"admin","password":"admin123"}
func (h *AuthHandler) Login(c *gin.Context) {
	var req service.LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, 400, common.CodeBadRequest, "参数错误: "+err.Error())
		return
	}
	result, err := h.svc.Auth.Login(c.Request.Context(), &req)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, result)
}

// Logout 登出（使当前令牌失效）。
//
//	POST /api/v1/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	token := bearerToken(c)
	h.svc.Auth.Logout(c.Request.Context(), token)
	common.Success(c, gin.H{})
}

// Me 当前登录用户信息。
//
//	GET /api/v1/auth/me
func (h *AuthHandler) Me(c *gin.Context) {
	token := bearerToken(c)
	u, err := h.svc.Auth.Validate(c.Request.Context(), token)
	if err != nil {
		handleError(c, err)
		return
	}
	common.Success(c, gin.H{
		"username": u.Username,
		"nickname": u.Nickname,
		"is_admin": u.Username == "admin",
	})
}
