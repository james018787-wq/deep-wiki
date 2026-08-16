package chatstore

import (
	"sort"
	"sync"
)

// MemoryStore 进程内内存实现的会话存储（单实例/降级场景）。
type MemoryStore struct {
	mu       sync.RWMutex
	metas    map[string]*SessionMeta
	messages map[string][]Message
}

// NewMemory 创建内存会话存储。
func NewMemory() *MemoryStore {
	return &MemoryStore{
		metas:    make(map[string]*SessionMeta),
		messages: make(map[string][]Message),
	}
}

// GetMeta 读取会话元信息。
func (s *MemoryStore) GetMeta(sessionID string) (*SessionMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m, ok := s.metas[sessionID]; ok {
		cp := *m
		return &cp, nil
	}
	return nil, nil
}

// SaveMeta 写入/更新会话元信息。
func (s *MemoryStore) SaveMeta(m *SessionMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *m
	s.metas[m.SessionID] = &cp
	return nil
}

// AppendMessage 追加一条消息。
func (s *MemoryStore) AppendMessage(sessionID string, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[sessionID] = append(s.messages[sessionID], msg)
	return nil
}

// Messages 返回最近 lastN 条消息（时间正序）。
func (s *MemoryStore) Messages(sessionID string, lastN int) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := s.messages[sessionID]
	if lastN <= 0 || lastN >= len(all) {
		cp := make([]Message, len(all))
		copy(cp, all)
		return cp, nil
	}
	cp := make([]Message, lastN)
	copy(cp, all[len(all)-lastN:])
	return cp, nil
}

// Count 返回消息总数。
func (s *MemoryStore) Count(sessionID string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.messages[sessionID])), nil
}

// Trim 仅保留最近 keepN 条消息。
func (s *MemoryStore) Trim(sessionID string, keepN int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.messages[sessionID]
	if keepN <= 0 || keepN >= len(all) {
		return nil
	}
	s.messages[sessionID] = all[len(all)-keepN:]
	return nil
}

// ListSessions 列出指定仓库的会话（按更新时间倒序）。
func (s *MemoryStore) ListSessions(repoID int64) ([]*SessionMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*SessionMeta
	for _, m := range s.metas {
		if repoID > 0 && m.RepoID != repoID {
			continue
		}
		cp := *m
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// Close 无操作。
func (s *MemoryStore) Close() error { return nil }
