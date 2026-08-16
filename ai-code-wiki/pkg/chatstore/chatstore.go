// Package chatstore 提供基于 Redis 的对话会话记忆存储，
// 无 Redis 时自动降级为进程内内存实现（单实例可用）。
package chatstore

import "time"

// Message 单条对话消息。
type Message struct {
	Role    string `json:"role"` // user / assistant
	Content string `json:"content"`
	Ts      int64  `json:"ts"` // unix 秒
}

// SessionMeta 会话元信息。
type SessionMeta struct {
	SessionID    string `json:"session_id"`
	RepoID       int64  `json:"repo_id"`
	Title        string `json:"title"`
	Summary      string `json:"summary"` // 滚动摘要：窗口外早期对话的压缩
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	MessageCount int64  `json:"message_count"`
}

// Store 会话存储抽象。
type Store interface {
	// GetMeta 读取会话元信息，不存在时返回 (nil, nil)。
	GetMeta(sessionID string) (*SessionMeta, error)
	// SaveMeta 写入/更新会话元信息（title/summary/时间戳等）。
	SaveMeta(m *SessionMeta) error
	// AppendMessage 追加一条消息并刷新过期时间。
	AppendMessage(sessionID string, msg Message) error
	// Messages 返回最近 lastN 条消息（按时间正序），lastN<=0 表示全部。
	Messages(sessionID string, lastN int) ([]Message, error)
	// Count 返回会话消息总数。
	Count(sessionID string) (int64, error)
	// Trim 仅保留最近 keepN 条消息（滑动窗口裁剪，供滚动摘要后使用）。
	Trim(sessionID string, keepN int) error
	// ListSessions 列出指定仓库的会话，按更新时间倒序。
	ListSessions(repoID int64) ([]*SessionMeta, error)
	Close() error
}

// NowUnix 当前 unix 秒。
func NowUnix() int64 { return time.Now().Unix() }
