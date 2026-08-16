package chatstore

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore 基于 Redis 的会话存储。
// key 设计：
//
//	chat:meta:{session_id}   hash：repo_id / title / summary / created_at / updated_at
//	chat:msgs:{session_id}   list：消息 JSON（RPUSH 追加，LRANGE 倒序取最近）
type RedisStore struct {
	client  *redis.Client
	ttlDays int
}

// NewRedis 创建 Redis 会话存储并做连通性探测。
func NewRedis(addr, password string, db, ttlDays int) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, err
	}
	if ttlDays <= 0 {
		ttlDays = 7
	}
	return &RedisStore{client: client, ttlDays: ttlDays}, nil
}

func metaKey(id string) string { return "chat:meta:" + id }
func msgsKey(id string) string { return "chat:msgs:" + id }
func ttl(s *RedisStore) time.Duration {
	return time.Duration(s.ttlDays) * 24 * time.Hour
}

// GetMeta 读取会话元信息。
func (s *RedisStore) GetMeta(sessionID string) (*SessionMeta, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := s.client.HGetAll(ctx, metaKey(sessionID)).Result()
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	return &SessionMeta{
		SessionID: sessionID,
		RepoID:    atoi64(raw["repo_id"]),
		Title:     raw["title"],
		Summary:   raw["summary"],
		CreatedAt: atoi64(raw["created_at"]),
		UpdatedAt: atoi64(raw["updated_at"]),
	}, nil
}

// SaveMeta 写入/更新会话元信息并刷新过期时间。
func (s *RedisStore) SaveMeta(m *SessionMeta) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	key := metaKey(m.SessionID)
	if err := s.client.HSet(ctx, key, map[string]interface{}{
		"repo_id":    m.RepoID,
		"title":      m.Title,
		"summary":    m.Summary,
		"created_at": m.CreatedAt,
		"updated_at": m.UpdatedAt,
	}).Err(); err != nil {
		return err
	}
	return s.client.Expire(ctx, key, ttl(s)).Err()
}

// AppendMessage 追加消息并刷新会话过期时间。
func (s *RedisStore) AppendMessage(sessionID string, msg Message) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	key := msgsKey(sessionID)
	if err := s.client.RPush(ctx, key, data).Err(); err != nil {
		return err
	}
	return s.client.Expire(ctx, key, ttl(s)).Err()
}

// Messages 返回最近 lastN 条消息（时间正序）。
func (s *RedisStore) Messages(sessionID string, lastN int) ([]Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	key := msgsKey(sessionID)
	var raw []string
	var err error
	if lastN > 0 {
		raw, err = s.client.LRange(ctx, key, int64(-lastN), -1).Result()
	} else {
		raw, err = s.client.LRange(ctx, key, 0, -1).Result()
	}
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(raw))
	for _, line := range raw {
		var m Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// Count 返回消息总数。
func (s *RedisStore) Count(sessionID string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.client.LLen(ctx, msgsKey(sessionID)).Result()
}

// Trim 仅保留最近 keepN 条消息。
func (s *RedisStore) Trim(sessionID string, keepN int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.client.LTrim(ctx, msgsKey(sessionID), int64(-keepN), -1).Err()
}

// ListSessions 列出指定仓库的会话（按更新时间倒序）。
func (s *RedisStore) ListSessions(repoID int64) ([]*SessionMeta, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out []*SessionMeta
	iter := s.client.Scan(ctx, 0, "chat:meta:*", 100).Iterator()
	for iter.Next(ctx) {
		raw, err := s.client.HGetAll(ctx, iter.Val()).Result()
		if err != nil || len(raw) == 0 {
			continue
		}
		if repoID > 0 && atoi64(raw["repo_id"]) != repoID {
			continue
		}
		id := iter.Val()[len("chat:meta:"):]
		cnt, _ := s.client.LLen(ctx, msgsKey(id)).Result()
		out = append(out, &SessionMeta{
			SessionID:    id,
			RepoID:       atoi64(raw["repo_id"]),
			Title:        raw["title"],
			Summary:      raw["summary"],
			CreatedAt:    atoi64(raw["created_at"]),
			UpdatedAt:    atoi64(raw["updated_at"]),
			MessageCount: cnt,
		})
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// DeleteSession 删除会话（元信息 + 全部消息），幂等。
func (s *RedisStore) DeleteSession(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.client.Del(ctx, metaKey(sessionID), msgsKey(sessionID)).Err()
}

// Close 关闭连接。
func (s *RedisStore) Close() error { return s.client.Close() }

func atoi64(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n
}
