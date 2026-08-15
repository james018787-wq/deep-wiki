package llm

import (
	"context"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"

	"ai-code-wiki/pkg/logger"
)

// SchedulerOption 单次调度的覆盖参数。
type SchedulerOption struct {
	ForceModel        string // 强制指定模型（非空则直连，不做降级/熔断/限流）
	ForceHighQuality  bool   // 仅用高配模型（过滤低于 high_quality_price_threshold 的低价模型）
	MaxSwitchTimes    int    // 最多降级切换次数；<=0 时取 global.max_retry_switch
	EstimatedTokenLen int    // 预估上下文 token 数（用于 max_context 过滤与 TPM 配额）
}

// SchedulerResult 调度结果。
type SchedulerResult struct {
	Content       string   // 生成的回答文本
	UsedModelName string   // 实际使用的模型
	SwitchedCount int      // 实际降级切换次数
	TokenInput    int      // 输入 token
	TokenOutput   int      // 输出 token
	Cost          float64  // 估算成本（元）
	RetriedModels []string // 曾尝试但失败/被跳过（熔断、限流）的模型
}

// ErrNoModel 模型池为空或无可候选模型。
var ErrNoModel = errors.New("模型池未配置或无可用的候选模型")

// ErrAllFailed 所有候选模型均调用失败。
var ErrAllFailed = errors.New("所有模型服务暂时不可用，请稍后重试")

// Scheduler 多模型调度器：优先低价，失败自动降级切换。
type Scheduler struct {
	pool   *ModelPool
	client Client
	cb     *CircuitBreaker
	rl     *RateLimiter
}

// NewScheduler 构建调度器。cb/rl 可为 nil（Redis 未配置时跳过熔断/限流）。
func NewScheduler(pool *ModelPool, client Client, cb *CircuitBreaker, rl *RateLimiter) *Scheduler {
	return &Scheduler{pool: pool, client: client, cb: cb, rl: rl}
}

// Chat 按调度策略调用模型。
//
// 流程：
//  1. force_model 非空 → 直连该模型（不降级、不熔断、不限流），模型不存在/未启用返回错误；
//  2. 过滤候选（enable、max_context、force_high_quality），按平均单价升序；
//  3. 依序尝试：熔断/限流命中则跳过并记录；调用失败按错误分类处理：
//     - 可重试（429/5xx/超时/网络）：记失败计数 + 切换下一档；
//     - 鉴权（401/403）：直接标记模型不可用 + 切换；
//     - 业务错误（400/参数/上下文超限/解析）：立即返回，不切换；
//  4. 全部尝试仍失败 → 返回 ErrAllFailed（包装为上游错误，提示稍后重试）。
func (s *Scheduler) Chat(ctx context.Context, system, user string, opt SchedulerOption) (*SchedulerResult, error) {
	// 1. force_model 直连
	if opt.ForceModel != "" {
		m := s.pool.Get(opt.ForceModel)
		if m == nil {
			return nil, fmt.Errorf("指定模型不存在或未启用: %s", opt.ForceModel)
		}
		content, usage, err := s.client.Chat(ctx, m, system, user)
		if err != nil {
			return nil, err
		}
		return &SchedulerResult{
			Content:       content,
			UsedModelName: m.Name,
			TokenInput:    usage.PromptTokens,
			TokenOutput:   usage.CompletionTokens,
			Cost:          calcCost(m, usage),
		}, nil
	}

	// 2. 候选模型（低价优先）
	candidates := s.pool.Candidates(opt)
	if len(candidates) == 0 {
		return nil, ErrNoModel
	}

	// 3. 依序尝试 + 降级
	maxSwitch := opt.MaxSwitchTimes
	if maxSwitch <= 0 {
		maxSwitch = s.pool.Global().MaxRetrySwitch
	}
	res := &SchedulerResult{}
	for _, m := range candidates {
		if res.SwitchedCount > maxSwitch {
			break
		}
		if s.cb != nil && s.cb.IsOpen(ctx, m.Name) {
			res.RetriedModels = append(res.RetriedModels, m.Name)
			continue
		}
		if s.rl != nil && !s.rl.Allow(ctx, m.Name, m.Rpm, m.Tpm, opt.EstimatedTokenLen) {
			res.RetriedModels = append(res.RetriedModels, m.Name)
			continue
		}

		content, usage, err := s.client.Chat(ctx, m, system, user)
		if err == nil {
			res.Content = content
			res.UsedModelName = m.Name
			res.TokenInput = usage.PromptTokens
			res.TokenOutput = usage.CompletionTokens
			res.Cost = calcCost(m, usage)
			if s.cb != nil {
				s.cb.RecordSuccess(ctx, m.Name)
			}
			return res, nil
		}

		// 错误分类
		kind := classifyErrKind(err)
		switch kind {
		case ErrKindBadRequest, ErrKindParse:
			// 业务错误：不切换模型，直接返回
			return nil, err
		case ErrKindAuth:
			// 鉴权失败：标记模型不可用
			if s.cb != nil {
				s.cb.MarkUnavailable(ctx, m.Name)
			}
		default:
			// 可重试错误：记失败计数
			if s.cb != nil {
				s.cb.RecordFailure(ctx, m.Name)
			}
		}
		res.RetriedModels = append(res.RetriedModels, m.Name)
		res.SwitchedCount++
		logger.Warn(ctx, "[llm] 模型调用失败，切换下一档 model=%s err=%v", m.Name, err)
	}

	return nil, ErrAllFailed
}

// classifyErrKind 将调用错误映射为错误分类（网络错误返回可重试类别）。
func classifyErrKind(err error) ErrorKind {
	var ce *CallError
	if errors.As(err, &ce) {
		return ce.Kind
	}
	return ErrKindNetwork
}

// calcCost 按实际消耗与单价估算成本（元）。
func calcCost(m *ModelItem, usage Usage) float64 {
	return (float64(usage.PromptTokens)*m.InputPrice + float64(usage.CompletionTokens)*m.OutputPrice) / 1000
}

// EstimateTokens 估算文本 token 数：CJK/非 ASCII 字符计 1，其余每 4 字符计 1（近似）。
func EstimateTokens(s string) int {
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
