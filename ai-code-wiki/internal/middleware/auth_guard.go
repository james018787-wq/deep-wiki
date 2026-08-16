package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/logger"

	"github.com/gin-gonic/gin"
)

// AuthGuard 登录/API 密钥统一鉴权中间件。
//
// 鉴权规则（任一满足即放行）：
//  1. API 密钥：请求头 X-Api-Secret == 环境变量 API_SECRET_KEY（server-to-server，CI/webhook 场景）；
//     为空时该通道自动关闭，不影响用户登录。
//  2. 用户登录：请求头 Authorization: Bearer <token>，经 AuthService 校验（MySQL user_token，可主动登出失效）。
//
// 通过后用户信息写入 gin context（UserID / Username），供 handler 读取。
// 登录接口 /auth/login 挂载在本中间件之前，天然跳过。
func AuthGuard(auth *service.AuthService) gin.HandlerFunc {
	secret := os.Getenv("API_SECRET_KEY")
	return func(c *gin.Context) {
		// 通道1：API 密钥（server-to-server）
		if secret != "" {
			provided := c.GetHeader("X-Api-Secret")
			if provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1 {
				c.Set("auth_by", "api_key")
				c.Next()
				return
			}
		}

		// 通道2：用户登录 Bearer token
		token := ""
		ah := c.GetHeader("Authorization")
		if strings.HasPrefix(ah, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(ah, "Bearer "))
		}
		if token == "" {
			token = c.GetHeader("X-Auth-Token") // 兼容旧前端可能使用的头
		}
		if token != "" {
			u, err := auth.Validate(c.Request.Context(), token)
			if err == nil {
				c.Set("auth_by", "user")
				c.Set("user_id", u.ID)
				c.Set("username", u.Username)
				c.Next()
				return
			}
			logger.Warn(c.Request.Context(), "[auth] Bearer token 校验失败: %v", err)
		}

		common.Fail(c, http.StatusUnauthorized, common.CodeUnauthorized, "未登录或登录已失效")
		c.Abort()
	}
}
