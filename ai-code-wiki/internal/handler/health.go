package handler

import (
	"net/http"

	"ai-code-wiki/internal/service"

	"github.com/gin-gonic/gin"
)

// Health 健康检查接口。
// 探测 MySQL 与 LLM 服务连通性，返回：
//
//	{"mysql":"ok/fail","llm_service":"ok/fail","status":"running"}
//
// 任一依赖不可用时返回 HTTP 503，供 docker-compose healthcheck（wget --spider）判定。
func (h *Handler) Health(c *gin.Context) {
	status := h.Service.CheckHealth(c.Request.Context())
	code := http.StatusOK
	if status.MySQL == service.HealthFail || status.LLMService == service.HealthFail {
		code = http.StatusServiceUnavailable
	}
	c.JSON(code, status)
}