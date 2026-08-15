package llm

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"ai-code-wiki/pkg/logger"
	"ai-code-wiki/pkg/redis"
)

// 熔断相关 Redis key 前缀。
const (
	circuitKeyFmt = "ai_code_wiki:model:circuit:%s"       // 熔断标记（存在即熔断）
	circuitCntFmt = "ai_code_wiki:model:circuit:count:%s" // 连续失败计数
	circuitLua    = `
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
if n >= tonumber(ARGV[2]) then
  redis.call('SET', KEYS[2], '1', 'EX', ARGV[3])
  redis.call('DEL', KEYS[1])
  return 1
end
return 0`
)

// CircuitBreaker 模型级熔断器（Redis 分布式状态）。
//
// 规则：
//   - 同一模型连续 circuitFailureThreshold 次可重试错误 → 写入熔断 key（TTL=circuitTTL），到期自动恢复；
//   - 鉴权失败（401）直接写入熔断 key，标记该模型不可用，不重试；
//   - 调用成功清空连续失败计数；
//   - Redis 不可用时 fail-open（不阻断调用），仅记录告警日志。
type CircuitBreaker struct {
	rdb       *redis.Client
	ttl       time.Duration
	threshold int
}

// NewCircuitBreaker 构建熔断器。rdb 为 nil 时降级为 no-op（Redis 未配置）。
func NewCircuitBreaker(rdb *redis.Client, ttl time.Duration, threshold int) *CircuitBreaker {
	if threshold < 1 {
		threshold = 1
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &CircuitBreaker{rdb: rdb, ttl: ttl, threshold: threshold}
}

// IsOpen 判断模型是否处于熔断状态。Redis 异常时返回 false（fail-open）。
func (b *CircuitBreaker) IsOpen(ctx context.Context, model string) bool {
	if b.rdb == nil {
		return false
	}
	n, err := b.rdb.Do(ctx, "EXISTS", circuitKey(model))
	if err != nil {
		logger.Warn(ctx, "[llm] 熔断状态查询失败，fail-open model=%s err=%v", model, err)
		return false
	}
	return n != nil && n.(int64) > 0
}

// RecordFailure 记录一次可重试失败；达到阈值写入熔断标记并清空计数。
func (b *CircuitBreaker) RecordFailure(ctx context.Context, model string) {
	if b.rdb == nil {
		return
	}
	_, err := b.rdb.Eval(ctx, circuitLua, []string{circuitCountKey(model), circuitKey(model)}, []string{
		strconv.Itoa(int(b.ttl.Seconds())), // ARGV[1] 计数过期（同熔断窗口）
		strconv.Itoa(b.threshold),          // ARGV[2] 熔断阈值
		strconv.Itoa(int(b.ttl.Seconds())), // ARGV[3] 熔断时长
	})
	if err != nil {
		logger.Warn(ctx, "[llm] 熔断计数失败 model=%s err=%v", model, err)
		return
	}
}

// RecordSuccess 调用成功：清空连续失败计数。
func (b *CircuitBreaker) RecordSuccess(ctx context.Context, model string) {
	if b.rdb == nil {
		return
	}
	if _, err := b.rdb.Do(ctx, "DEL", circuitCountKey(model)); err != nil {
		logger.Warn(ctx, "[llm] 熔断计数清空失败 model=%s err=%v", model, err)
	}
}

// MarkUnavailable 标记模型不可用（鉴权失败等场景），写入熔断 key 直接熔断。
func (b *CircuitBreaker) MarkUnavailable(ctx context.Context, model string) {
	if b.rdb == nil {
		return
	}
	if _, err := b.rdb.Do(ctx, "SET", circuitKey(model), "1", "EX", strconv.Itoa(int(b.ttl.Seconds()))); err != nil {
		logger.Warn(ctx, "[llm] 标记模型不可用失败 model=%s err=%v", model, err)
	}
}

func circuitKey(model string) string {
	return fmt.Sprintf(circuitKeyFmt, model)
}

func circuitCountKey(model string) string {
	return fmt.Sprintf(circuitCntFmt, model)
}
