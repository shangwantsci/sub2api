package service

import (
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	claudeCodeSessionIdleTTL = 3 * time.Minute
	claudeCodeSessionMaxTTL  = 15 * time.Minute
)

type claudeCodeSessionEntry struct {
	accountID int64
	sessionID string
	createdAt time.Time
	lastUsed  time.Time
}

// ClaudeCodeSessionStore 保存 session_key → CC session 的映射。
// 当前是进程内实现；RunManager 持久化阶段再换 Redis。
type ClaudeCodeSessionStore struct {
	mu      sync.Mutex
	entries map[string]claudeCodeSessionEntry
}

func NewClaudeCodeSessionStore() *ClaudeCodeSessionStore {
	return &ClaudeCodeSessionStore{entries: make(map[string]claudeCodeSessionEntry)}
}

// Lookup 返回可恢复的 CC session；idle 超过 3 分钟或总寿命超过 15 分钟即失效。
func (s *ClaudeCodeSessionStore) Lookup(key string, accountID int64, now time.Time) (string, bool) {
	if s == nil || strings.TrimSpace(key) == "" || accountID <= 0 {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return "", false
	}
	if entry.accountID != accountID ||
		now.Sub(entry.lastUsed) > claudeCodeSessionIdleTTL ||
		now.Sub(entry.createdAt) > claudeCodeSessionMaxTTL ||
		entry.sessionID == "" {
		delete(s.entries, key)
		return "", false
	}
	return entry.sessionID, true
}

// Record 保存/刷新 session；空 sessionID 表示删除。
func (s *ClaudeCodeSessionStore) Record(key string, accountID int64, sessionID string, now time.Time) {
	if s == nil || strings.TrimSpace(key) == "" || accountID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(sessionID) == "" {
		delete(s.entries, key)
		return
	}
	entry, ok := s.entries[key]
	if !ok || entry.accountID != accountID {
		entry = claudeCodeSessionEntry{accountID: accountID, createdAt: now}
	}
	entry.sessionID = sessionID
	entry.lastUsed = now
	s.entries[key] = entry
}

// latestClaudeCodeUserTurn 把完整对话裁成“只发最新 user turn”，供 --resume 使用。
func latestClaudeCodeUserTurn(turn ClaudeCodeTurn) (ClaudeCodeTurn, error) {
	for i := len(turn.Messages) - 1; i >= 0; i-- {
		message := turn.Messages[i]
		if strings.EqualFold(strings.TrimSpace(message.Role), "user") &&
			(strings.TrimSpace(message.Text) != "" || len(message.Parts) > 0) {
			turn.Messages = []ClaudeCodeMessage{message}
			return turn, nil
		}
	}
	return ClaudeCodeTurn{}, errors.New("claude code: resume requires a user message")
}

// 测试辅助：当前时间可由测试直接传入，避免 sleep。
