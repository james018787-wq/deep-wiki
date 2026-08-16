package chatstore

import (
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// New 创建会话存储：优先 Redis，Redis 不可用或未配置地址时降级为内存实现。
// 返回的 Store 供调用方持有；仅当实现为 *RedisStore 时可显式 Close。
func New(addr, password string, db, ttlDays int) Store {
	if strings.TrimSpace(addr) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		probe := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db, DialTimeout: 2 * time.Second})
		pingErr := probe.Ping(ctx).Err()
		cancel()
		probe.Close()
		if pingErr == nil {
			if st, err := NewRedis(addr, password, db, ttlDays); err == nil {
				return st
			}
		}
	}
	return NewMemory()
}
