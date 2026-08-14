// Package llm 大模型调用封装，统一经由 Python LLM 微服务调用。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Chat 调用大模型对话接口。
// 接口：POST {baseURL}/api/chat，入参 {system, user}，返回 data.answer。
// 用于 RAG 问答等需要传入上下文与用户问题的场景。
// ctx 支持调用方超时控制，HTTP 客户端另有 60s 兜底超时。
func Chat(ctx context.Context, baseURL, system, user string) (string, error) {
	if baseURL == "" {
		return "", fmt.Errorf("LLM服务地址未配置")
	}
	apiURL := strings.TrimRight(baseURL, "/") + "/api/chat"

	body, err := json.Marshal(map[string]string{
		"system": system,
		"user":   user,
	})
	if err != nil {
		return "", fmt.Errorf("对话请求序列化失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构建对话请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用LLM服务失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM服务返回异常状态码: %d", resp.StatusCode)
	}

	var apiResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Answer string `json:"answer"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("解析对话响应失败: %w", err)
	}
	if apiResp.Code != 0 {
		return "", fmt.Errorf("LLM服务返回业务错误 code=%d msg=%s", apiResp.Code, apiResp.Message)
	}
	return apiResp.Data.Answer, nil
}