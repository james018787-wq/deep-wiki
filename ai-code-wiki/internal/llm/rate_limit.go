package llm

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"ai-code-wiki/pkg/logger"
	"ai-code-wiki/pkg/redis"
)

// 限流相关 Redis key 前缀。
const (
	rpmKeyFmt = "ai_code_wiki:model:rpm:%s"
	tpmKeyFmt = "ai_code_wiki:model:tpm:%s"
	// 滑动窗口：RPM 按请求计数；TPM 按 token 累计（成员带 token 前缀）。
	rateLimitLua = `
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', tonumber(ARGV[1]) - tonumber(ARGV[2]))
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[3]) then return 0 end
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[6])
redis.call('EXPIRE', KEYS[1], ARGV[7])

redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', tonumber(ARGV[1]) - tonumber(ARGV[2]))
local vals = redis.call('ZRANGE', KEYS[2], 0, -1)
local sum = 0
for _, v in ipairs(vals) do
  local sep = string.find(v, ':')
  if sep then sum = sum + tonumber(string.sub(v, 1, sep - 1)) end
end
if sum + tonumber(ARGV[4]) > tonumber(ARGV[5]) then return 0 end
redis.call('ZADD', KEYS[2], ARGV[1], ARGV[4] .. ':' .. ARGV[6])
redis.call('EXPIRE', KEYS[2], ARGV[7])
return 1`
)

var reqSeq int64

// RateLimiter 模型级 RPM/TPM 分布式限流（Redis ZSET 滑动窗口）。
//
// 规则：
//   - RPM/TPM 任一配置 <=0 视为不限制；
//   - 命中限流时本次调用返回 false（调度器跳过该模型并降级切换）；
//   - Redis 不可用时 fail-open（放行），仅记录告警日志。
type RateLimiter struct {
	rdb    *redis.Client
	window time.Duration
}

// NewRateLimiter 构建限流器。rdb 为 nil 时降级为 no-op（Redis 未配置）。
func NewRateLimiter(rdb *redis.Client, window time.Duration) *RateLimiter {
	if window <= 0 {
		window = 60 * time.Second
	}
	return &RateLimiter{rdb: rdb, window: window}
}

// Allow 判断是否允许一次估算 tokens 的调用（原子占用配额）。
func (l *RateLimiter) Allow(ctx context.Context, model string, rpm, tpm, tokens int) bool {
	if l.rdb == nil {
		return true
	}
	if rpm <= 0 && tpm <= 0 {
		return true
	}
	now := time.Now().Unix()
	seq := atomic.AddInt64(&reqSeq, 1)
	member := fmt.Sprintf("%d-%d", time.Now().UnixNano(), seq)

	n, err := l.rdb.Eval(ctx, rateLimitLua,
		[]string{rpmKey(model), tpmKey(model)},
		[]string{
			fmt.Sprintf("%d", now),                          // ARGV[1] 当前时间(秒)
			fmt.Sprintf("%d", int(l.window.Seconds())),      // ARGV[2] 窗口
			fmt.Sprintf("%d", rpm),                          // ARGV[3] RPM 上限
			fmt.Sprintf("%d", tokens),                       // ARGV[4] 本次 token
			fmt.Sprintf("%d", tpm),                          // ARGV[5] TPM 上限
			member,                                          // ARGV[6] 唯一成员
			fmt.Sprintf("%d", int(l.window.Seconds())*2+60), // ARGV[7] key 过期
		})
	if err != nil {
		logger.Warn(ctx, "[llm] 限流判断失败，fail-open model=%s err=%v", model, err)
		return true
	}
	allowed, ok := n.(int64)
	if !ok {
		logger.Warn(ctx, "[llm] 限流返回类型异常，fail-open model=%s", model)
		return true
	}
	return allowed == 1
}

func rpmKey(model string) string {
	return fmt.Sprintf(rpmKeyFmt, model)
}

func tpmKey(model string) string {
	return fmt.Sprintf(tpmKeyFmt, model)
}
