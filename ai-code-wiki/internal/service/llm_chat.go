package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"ai-code-wiki/pkg/common"
)

// chatLLMResult ai-wiki-llm /api/chat 返回的回答与调度元信息。
type chatLLMResult struct {
	Answer        string
	UsedModel     string
	SwitchCount   int
	TokenInput    int
	TokenOutput   int
	Cost          float64
	RetriedModels []string
}

// usageRecorder LLM 消耗记录器（NewService 注入），所有经 chatLLM 的调用完成后记录消耗。
var usageRecorder *UsageService

// SetUsageRecorder 注入消耗记录器（由 NewService 调用）。
func SetUsageRecorder(u *UsageService) {
	usageRecorder = u
}

// chatLLM 经 ai-wiki-llm 多模型调度器调用 LLM。
//
// 多模型调度（低价优先、失败自动降级、Redis 熔断/限流）全部在 Python 侧完成，
// Go 仅透传调度参数并透出返回的调度元信息，保持「Go 编排 / Python 调模型」的单一路径。
// scenario 标记调用场景（doc/chat/search/impact/...），调用成功后记录 token/cost 消耗。
func chatLLM(ctx context.Context, baseURL string, timeout time.Duration, system, user string,
	forceModel string, forceHighQuality bool, estimatedTokens int, scenario string) (*chatLLMResult, error) {

	if strings.TrimSpace(baseURL) == "" {
		return nil, common.NewError(common.CodeInvalidState, "AI 服务未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	apiURL := strings.TrimRight(baseURL, "/") + "/api/chat"
	body, err := json.Marshal(map[string]any{
		"system":             system,
		"user":               user,
		"force_model":        forceModel,
		"force_high_quality": forceHighQuality,
		"estimated_tokens":   estimatedTokens,
	})
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "构造对话请求失败", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "AI 服务暂时不可用，请稍后重试", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "AI 服务暂时不可用，请稍后重试", err)
	}
	defer resp.Body.Close()

	var apiResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Answer        string   `json:"answer"`
			UsedModel     string   `json:"used_model"`
			SwitchCount   int      `json:"switch_count"`
			TokenInput    int      `json:"input_token"`
			TokenOutput   int      `json:"output_token"`
			Cost          float64  `json:"cost"`
			RetriedModels []string `json:"retried_models"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&apiResp); err != nil {
		return nil, common.WrapError(common.CodeUpstreamError, "AI 服务响应解析失败", err)
	}
	if apiResp.Code != 0 {
		msg := apiResp.Message
		if msg == "" {
			msg = "AI 服务调用失败"
		}
		return nil, common.WrapError(common.CodeUpstreamError, msg,
			fmt.Errorf("ai-wiki-llm code=%d msg=%s", apiResp.Code, apiResp.Message))
	}

	// 记录本次 LLM 调用消耗（best-effort，落库失败不影响业务）
	if usageRecorder != nil {
		usageRecorder.Record(ctx, apiResp.Data.UsedModel, scenario,
			apiResp.Data.TokenInput, apiResp.Data.TokenOutput, apiResp.Data.Cost)
	}

	return &chatLLMResult{
		Answer:        apiResp.Data.Answer,
		UsedModel:     apiResp.Data.UsedModel,
		SwitchCount:   apiResp.Data.SwitchCount,
		TokenInput:    apiResp.Data.TokenInput,
		TokenOutput:   apiResp.Data.TokenOutput,
		Cost:          apiResp.Data.Cost,
		RetriedModels: apiResp.Data.RetriedModels,
	}, nil
}

// estimateTokens 估算文本 token 数：CJK/非 ASCII 字符计 1，其余每 4 字符计 1（近似）。
func estimateTokens(s string) int {
	cjk, ascii := 0, 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || utf8.RuneLen(r) > 1 {
			cjk++
		} else {
			ascii++
		}
	}
	return cjk + (ascii+3)/4
}
