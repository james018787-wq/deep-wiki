// Package middleware 全局中间件。
package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"

	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/logger"

	"github.com/gin-gonic/gin"
)

// NoCache 静态资源禁用浏览器缓存。
// 前端通过 bind mount 挂载实时开发，浏览器强缓存会导致页面/脚本不一致（如新增导航、脚本引用），
// 统一响应 Cache-Control: no-cache 要求每次重新验证。
func NoCache() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Next()
	}
}

// Recovery 全局异常恢复中间件。
// 捕获 handler 层 panic，统一转换为 JSON 返回，避免 500 空白页，
// 并记录 panic 堆栈到统一日志。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				logger.Error(c.Request.Context(), "捕获 panic: %v", err)
				common.Fail(c, http.StatusInternalServerError, common.CodeInternalError, "系统内部错误")
				c.Abort()
			}
		}()
		c.Next()
	}
}

// RequestID 请求追踪中间件。
// 生成/透传 request_id 注入请求 context（供日志打印），并回写响应头 X-Request-Id。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-Id")
		if id == "" {
			id = logger.GenerateRequestID()
		}
		c.Header("X-Request-Id", id)
		// 注入 context，下游 handler/service 的日志可携带 request_id
		c.Request = c.Request.WithContext(logger.WithRequestID(c.Request.Context(), id))
		c.Next()
	}
}

// NotFoundHandler 处理未匹配路由，返回统一 JSON。
func NotFoundHandler(c *gin.Context) {
	common.Fail(c, http.StatusNotFound, common.CodeNotFound, "接口不存在")
}

// NoMethodHandler 处理方法不允许，返回统一 JSON。
func NoMethodHandler(c *gin.Context) {
	common.Fail(c, http.StatusMethodNotAllowed, common.CodeForbidden, "请求方法不允许")
}

// APIAuth 简易 API 密钥鉴权中间件（MVP 单密钥，无 RBAC）。
//
// 鉴权规则：
//  1. 读取环境变量 API_SECRET_KEY；为空时鉴权关闭，方便开发环境。
//  2. 校验请求头 X-Api-Secret 与 API_SECRET_KEY 是否一致（常量时间比较）；
//     不匹配返回 401（CodeUnauthorized）。
//
// 使用方式：挂载在 /api/v1 分组下（router.Register 中 api.Use(middleware.APIAuth())）。
// /health 等根路径接口不经过本分组，天然跳过鉴权。
func APIAuth() gin.HandlerFunc {
	secret := os.Getenv("API_SECRET_KEY")
	return func(c *gin.Context) {
		// 密钥未配置：鉴权关闭
		if secret == "" {
			c.Next()
			return
		}
		provided := c.GetHeader("X-Api-Secret")
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			common.Fail(c, http.StatusUnauthorized, common.CodeUnauthorized, "API密钥无效")
			c.Abort()
			return
		}
		c.Next()
	}
}