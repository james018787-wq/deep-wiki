package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrorKind 模型调用错误分类（调度器据此决定是否降级切换）。
type ErrorKind int

const (
	// ErrKindNetwork 网络错误（连接失败等）。
	ErrKindNetwork ErrorKind = iota
	// ErrKindTimeout 调用超时。
	ErrKindTimeout
	// ErrKindRateLimited 上游限流（429）或本地配额不足。
	ErrKindRateLimited
	// ErrKindAuth 鉴权失败（401/403）：直接标记模型不可用，不重试。
	ErrKindAuth
	// ErrKindBadRequest 业务拒绝（400/参数错误/上下文超限/内容拒绝）：不切换模型。
	ErrKindBadRequest
	// ErrKindUpstream 上游服务异常（5xx）。
	ErrKindUpstream
	// ErrKindParse 响应解析失败。
	ErrKindParse
)

// CallError 模型调用错误。
type CallError struct {
	Kind       ErrorKind
	StatusCode int // 上游 HTTP 状态码；网络/超时为 0
	Message    string
}

func (e *CallError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("LLM调用失败[%d]: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("LLM调用失败: %s", e.Message)
}

// Usage 单次调用 token 消耗。
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// Client 模型调用客户端抽象（便于调度器注入 mock 做单元测试）。
type Client interface {
	// Chat 调用 OpenAI 兼容 /chat/completions，返回回答文本与 token 消耗。
	Chat(ctx context.Context, m *ModelItem, system, user string) (string, Usage, error)
}

// HTTPClient 基于标准库 HTTP 的 OpenAI 兼容客户端实现。
type HTTPClient struct {
	client *http.Client
}

// NewHTTPClient 构建客户端。timeout<=0 时使用默认 60s。
func NewHTTPClient(timeout time.Duration) *HTTPClient {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &HTTPClient{client: &http.Client{Timeout: timeout}}
}

// Message OpenAI 兼容消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest chat/completions 请求体。
type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
}

// chatResponse chat/completions 响应体。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Chat 调用 OpenAI 兼容接口，解析回答与 token 消耗，并统一错误分类。
func (c *HTTPClient) Chat(ctx context.Context, m *ModelItem, system, user string) (string, Usage, error) {
	if m == nil || strings.TrimSpace(m.BaseUrl) == "" {
		return "", Usage{}, &CallError{Kind: ErrKindBadRequest, Message: "模型配置缺少 base_url"}
	}
	apiURL := strings.TrimRight(m.BaseUrl, "/") + "/chat/completions"

	body, err := json.Marshal(chatRequest{
		Model: m.Name,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
	})
	if err != nil {
		return "", Usage{}, &CallError{Kind: ErrKindParse, Message: "请求序列化失败"}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return "", Usage{}, &CallError{Kind: ErrKindNetwork, Message: "构建请求失败: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	if m.ApiKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.ApiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", Usage{}, classifyTransportErr(err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", Usage{}, &CallError{Kind: ErrKindNetwork, Message: "读取响应失败: " + err.Error()}
	}

	if resp.StatusCode != http.StatusOK {
		return "", Usage{}, classifyStatus(resp.StatusCode, truncateBody(respBody))
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", Usage{}, &CallError{Kind: ErrKindParse, Message: "解析响应失败: " + err.Error()}
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", Usage{}, &CallError{Kind: ErrKindUpstream, Message: "模型返回空回答"}
	}
	return parsed.Choices[0].Message.Content, Usage{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}, nil
}

// classifyTransportErr 网络/超时错误分类。
func classifyTransportErr(err error) *CallError {
	if errors.Is(err, context.DeadlineExceeded) {
		return &CallError{Kind: ErrKindTimeout, Message: "调用超时"}
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &CallError{Kind: ErrKindTimeout, Message: "调用超时: " + err.Error()}
	}
	return &CallError{Kind: ErrKindNetwork, Message: err.Error()}
}

// classifyStatus HTTP 状态码错误分类。
func classifyStatus(code int, respBody string) *CallError {
	msg := respBody
	if msg == "" {
		msg = "上游返回异常状态码"
	}
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return &CallError{Kind: ErrKindAuth, StatusCode: code, Message: "鉴权失败: " + msg}
	case code == http.StatusTooManyRequests:
		return &CallError{Kind: ErrKindRateLimited, StatusCode: code, Message: "上游限流: " + msg}
	case code >= 500:
		return &CallError{Kind: ErrKindUpstream, StatusCode: code, Message: "上游服务异常: " + msg}
	default:
		return &CallError{Kind: ErrKindBadRequest, StatusCode: code, Message: msg}
	}
}

// truncateBody 截断错误响应体，避免日志/错误信息过大。
func truncateBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
