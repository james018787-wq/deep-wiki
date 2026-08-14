// ai-code-wiki 服务入口。
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ai-code-wiki/internal/config"
	"ai-code-wiki/internal/handler"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/internal/router"
	"ai-code-wiki/internal/service"
	"ai-code-wiki/pkg/logger"

	"github.com/gin-gonic/gin"
)

func main() {
	ctx := context.Background()

	// 1. 加载配置
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		logger.Error(ctx, "加载配置失败: %v", err)
		os.Exit(1)
	}

	// 2. 初始化数据库连接
	db, err := repo.InitDB(&cfg.MySQL)
	if err != nil {
		logger.Error(ctx, "初始化数据库失败: %v", err)
		os.Exit(1)
	}
	_ = db

	// 3. 初始化业务服务与处理器
	svc, err := service.NewService(db, cfg)
	if err != nil {
		logger.Error(ctx, "初始化业务服务失败: %v", err)
		os.Exit(1)
	}
	h := handler.NewHandler(svc)

	// 4. 初始化 Gin 引擎并注册路由
	gin.SetMode(cfg.Server.Mode)
	engine := gin.New()
	router.Register(engine, h)

	// 5. 启动异步任务消费 Worker（独立后台协程消费解析/向量任务）
	workerCtx, workerCancel := context.WithCancel(ctx)
	svc.TaskWorker.Start(workerCtx)

	// 6. 启动 HTTP 服务
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{Addr: addr, Handler: engine}
	logger.Info(ctx, "%s 服务启动，监听 %s", cfg.Server.Name, addr)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "服务启动失败: %v", err)
			os.Exit(1)
		}
	}()

	// 7. 等待退出信号，优雅关闭：先停消费 Worker，再关闭 HTTP
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info(ctx, "收到退出信号，正在优雅关闭...")
	workerCancel()
	svc.TaskWorker.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	logger.Info(ctx, "服务已关闭")
}
