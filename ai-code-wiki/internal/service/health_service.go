package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ai-code-wiki/pkg/version"
)

// 健康检查常量。
const (
	HealthOK   = "ok"
	HealthFail = "fail"
)

// healthProbeTimeout 单次探测超时。
const healthProbeTimeout = 3 * time.Second

// HealthStatus 健康检查结果。
type HealthStatus struct {
	Version    string `json:"version"`     // 服务版本号
	MySQL      string `json:"mysql"`       // 数据库连通性：ok / fail
	LLMService string `json:"llm_service"` // LLM 服务连通性：ok / fail
	Status     string `json:"status"`      // 服务运行状态：running
}

// CheckHealth 健康检查：探测 MySQL 与 LLM 服务连通性。
// 探测失败不 panic，仅返回各依赖状态字段；进程存活时 status 恒为 running。
func (s *Service) CheckHealth(ctx context.Context) HealthStatus {
	if ctx == nil {
		ctx = context.Background()
	}
	status := HealthStatus{Version: version.Version, MySQL: HealthOK, LLMService: HealthOK, Status: "running"}
	if err := s.pingMySQL(ctx); err != nil {
		status.MySQL = HealthFail
	}
	if err := s.pingLLM(ctx); err != nil {
		status.LLMService = HealthFail
	}
	return status
}

// pingMySQL 探测 MySQL 连通性（带超时，失败仅返回错误不 panic）。
func (s *Service) pingMySQL(ctx context.Context) error {
	if s.db == nil {
		return errors.New("数据库未初始化")
	}
	ctx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()
	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}
	return sqlDB.PingContext(ctx)
}

// pingLLM 探测 LLM 服务连通性（GET {baseURL}/health，带超时）。
func (s *Service) pingLLM(ctx context.Context) error {
	if strings.TrimSpace(s.llmBaseURL) == "" {
		return errors.New("LLM服务地址未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()
	url := strings.TrimRight(s.llmBaseURL, "/") + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("构建健康检查请求失败: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("访问LLM服务失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("LLM健康检查返回状态码 %d", resp.StatusCode)
	}
	return nil
}
