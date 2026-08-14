// ai-code-wiki 服务入口。
package main

import (
	"context"
	"fmt"
	"os"

	"ai-code-wiki/internal/config"
	"ai-code-wiki/internal/handler"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/internal/router"
	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/logger"

	"github.com/gin-gonic/gin"
)

func main() {
	// 1. 加载配置
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		logger.Error(context.Background(), "加载配置失败: %v", err)
		os.Exit(1)
	}

	// 2. 初始化数据库连接
	db, err := repo.InitDB(&cfg.MySQL)
	if err != nil {
		logger.Error(context.Background(), "初始化数据库失败: %v", err)
		os.Exit(1)
	}
	_ = db

	// 3. 初始化业务服务与处理器
	svc := service.NewService(db, cfg)
	h := handler.NewHandler(svc)

	// 4. 初始化 Gin 引擎并注册路由
	gin.SetMode(cfg.Server.Mode)
	engine := gin.New()
	router.Register(engine, h)

	// 5. 启动 HTTP 服务
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	logger.Info(context.Background(), "%s 服务启动，监听 %s", cfg.Server.Name, addr)
	if err := engine.Run(addr); err != nil {
		logger.Error(context.Background(), "服务启动失败: %v", err)
		os.Exit(1)
	}
}