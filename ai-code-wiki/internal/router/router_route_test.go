package router

import (
	"testing"

	"ai-code-wiki/internal/handler"

	"github.com/gin-gonic/gin"
)

func TestDocHistoryRoutesNoConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// svc 为 nil 的处理器仅用于路由注册冲突校验（方法不会被调用）
	Register(engine, handler.NewHandler(nil))
	t.Log("路由注册成功，无冲突")
}
