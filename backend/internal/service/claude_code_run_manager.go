package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const (
	claudeCodeRunIdleTTL = 3 * time.Minute
	claudeCodeRunMaxTTL  = 15 * time.Minute
)

// ClaudeCodeTurnProcess 是 RunManager 持有的持久进程抽象。
type ClaudeCodeTurnProcess interface {
	Send(ctx context.Context, turn ClaudeCodeTurn) (*ClaudeCodeResult, error)
	Continue(ctx context.Context) (*ClaudeCodeResult, error)
	NotifyToolCall(tool *ClaudeCodePendingTool) bool
	SetOnEvent(onEvent func(json.RawMessage) error)
	Close() error
}

// ClaudeCodeProcessFactory 启动一个进程；manager 随后发送 first turn。
type ClaudeCodeProcessFactory func(ctx context.Context, account *Account, first ClaudeCodeTurn, onEvent func(json.RawMessage) error) (ClaudeCodeTurnProcess, error)

type claudeCodeRunEntry struct {
	accountID  int64
	release    func()
	createdAt  time.Time
	lastUsed   time.Time
	timer      *time.Timer
	process    ClaudeCodeTurnProcess
	toolBridge *ClaudeCodeToolBridge
	toolToken  string
	onEvent    func(json.RawMessage) error
	mu         sync.Mutex
	sessionID  string
}

// ClaudeCodeRunManager 持有活跃 run 的账号槽所有权。
// 当前先做内存版；scheduler 亲和与 handler 转移槽在后续接线。
type ClaudeCodeRunManager struct {
	mu         sync.Mutex
	runs       map[string]*claudeCodeRunEntry
	toolTokens map[string]string
	NewProcess ClaudeCodeProcessFactory
	OnRelease  func(sessionKey string)
	OnStart    func(sessionKey string, accountID int64)
}

func NewClaudeCodeRunManager() *ClaudeCodeRunManager {
	return &ClaudeCodeRunManager{runs: make(map[string]*claudeCodeRunEntry), toolTokens: make(map[string]string)}
}

// Bind 接管一个已预占的账号槽；同一 session 已有 run 时先释放旧的。
func (m *ClaudeCodeRunManager) Bind(sessionKey string, accountID int64, release func(), now time.Time) {
	if m == nil || sessionKey == "" || accountID <= 0 {
		if release != nil {
			release()
		}
		return
	}
	m.mu.Lock()
	if old := m.runs[sessionKey]; old != nil {
		if old.accountID == accountID {
			// 亲和路径传入的是 no-op release；保留原槽和原 release。
			old.lastUsed = now
			m.scheduleLocked(sessionKey, old)
			m.mu.Unlock()
			return
		}
		if old.timer != nil {
			old.timer.Stop()
		}
		if old.release != nil {
			old.release()
		}
		delete(m.runs, sessionKey)
	}
	entry := &claudeCodeRunEntry{
		accountID: accountID,
		release:   release,
		createdAt: now,
		lastUsed:  now,
	}
	m.runs[sessionKey] = entry
	m.scheduleLocked(sessionKey, entry)
	m.mu.Unlock()
}

// Lookup 返回 session 绑定的账号并续期 idle 计时。
func (m *ClaudeCodeRunManager) Lookup(sessionKey string) (int64, bool) {
	if m == nil || sessionKey == "" {
		return 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.runs[sessionKey]
	if entry == nil {
		return 0, false
	}
	now := time.Now()
	if now.Sub(entry.lastUsed) > claudeCodeRunIdleTTL || now.Sub(entry.createdAt) > claudeCodeRunMaxTTL {
		m.releaseLocked(sessionKey, entry)
		return 0, false
	}
	entry.lastUsed = now
	m.scheduleLocked(sessionKey, entry)
	return entry.accountID, true
}

// Release 主动结束 session 并释放账号槽。
func (m *ClaudeCodeRunManager) Release(sessionKey string) {
	if m == nil || sessionKey == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry := m.runs[sessionKey]; entry != nil {
		m.releaseLocked(sessionKey, entry)
	}
}

// RunTurn 在 session 的持久进程上执行一轮；首次调用负责启动进程并接管槽。
func (m *ClaudeCodeRunManager) RunTurn(
	ctx context.Context,
	sessionKey string,
	account *Account,
	release func(),
	turn ClaudeCodeTurn,
	onEvent func(json.RawMessage) error,
) (*ClaudeCodeResult, error) {
	if m == nil || sessionKey == "" || account == nil {
		if release != nil {
			release()
		}
		return nil, errors.New("claude code run manager: invalid session/account")
	}
	if m.NewProcess == nil {
		if release != nil {
			release()
		}
		return nil, errors.New("claude code run manager: process factory is not configured")
	}

	m.mu.Lock()
	entry := m.runs[sessionKey]
	if entry == nil {
		entry = &claudeCodeRunEntry{
			accountID: account.ID,
			release:   release,
			createdAt: time.Now(),
			lastUsed:  time.Now(),
		}
		m.runs[sessionKey] = entry
		if m.OnStart != nil {
			m.OnStart(sessionKey, account.ID)
		}
	} else if entry.accountID != account.ID {
		m.mu.Unlock()
		if release != nil {
			release()
		}
		return nil, errors.New("claude code run manager: session is bound to another account")
	}
	m.mu.Unlock()

	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.onEvent = onEvent

	if entry.process == nil {
		proc, err := m.NewProcess(ctx, account, turn, onEvent)
		if err != nil {
			m.mu.Lock()
			m.releaseLocked(sessionKey, entry)
			m.mu.Unlock()
			return nil, err
		}
		entry.process = proc
	} else {
		trimmed, err := latestClaudeCodeUserTurn(turn)
		if err != nil {
			return nil, err
		}
		turn = trimmed
	}

	entry.process.SetOnEvent(onEvent)
	result, err := entry.process.Send(ctx, turn)
	if err != nil {
		m.mu.Lock()
		m.releaseLocked(sessionKey, entry)
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Lock()
	if result.SessionID != "" {
		entry.sessionID = result.SessionID
	}
	entry.lastUsed = time.Now()
	m.scheduleLocked(sessionKey, entry)
	m.mu.Unlock()
	return result, nil
}

// RegisterToolToken 在进程启动前绑定 session 的 MCP token。
func (m *ClaudeCodeRunManager) RegisterToolToken(sessionKey, token string) {
	if m == nil || sessionKey == "" || token == "" {
		return
	}
	m.mu.Lock()
	m.toolTokens[token] = sessionKey
	m.mu.Unlock()
}

// UnregisterToolToken 删除未成功启动的预注册 token。
func (m *ClaudeCodeRunManager) UnregisterToolToken(token string) {
	if m == nil || token == "" {
		return
	}
	m.mu.Lock()
	delete(m.toolTokens, token)
	m.mu.Unlock()
}

// ToolToken 返回 session 的工具回调 token；不存在时生成。
func (m *ClaudeCodeRunManager) ToolToken(sessionKey string) (string, bool) {
	if m == nil || sessionKey == "" {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.runs[sessionKey]
	if entry == nil || entry.process == nil {
		return "", false
	}
	if entry.toolToken == "" {
		for token, key := range m.toolTokens {
			if key == sessionKey {
				entry.toolToken = token
				break
			}
		}
	}
	if entry.toolToken == "" {
		buf := make([]byte, 24)
		if _, err := rand.Read(buf); err != nil {
			return "", false
		}
		entry.toolToken = hex.EncodeToString(buf)
		m.toolTokens[entry.toolToken] = sessionKey
	}
	return entry.toolToken, true
}

// HandleToolCall 按 token 找到 run，等待客户端 tool_result。
func (m *ClaudeCodeRunManager) HandleToolCall(ctx context.Context, token, name string, arguments json.RawMessage) (ClaudeCodeToolResult, error) {
	if m == nil || token == "" {
		return ClaudeCodeToolResult{}, errors.New("claude code run manager: missing tool token")
	}
	m.mu.Lock()
	sessionKey := m.toolTokens[token]
	var entry *claudeCodeRunEntry
	if sessionKey != "" {
		entry = m.runs[sessionKey]
	}
	if entry == nil {
		for _, candidate := range m.runs {
			if candidate.toolToken == token {
				entry = candidate
				break
			}
		}
	}
	m.mu.Unlock()
	if entry == nil {
		return ClaudeCodeToolResult{}, errors.New("claude code run manager: unknown tool token")
	}
	entry.mu.Lock()
	if entry.toolBridge == nil {
		entry.toolBridge = NewClaudeCodeToolBridge()
	}
	bridge := entry.toolBridge
	entry.mu.Unlock()

	tool := bridge.Start(name, arguments)
	entry.mu.Lock()
	process := entry.process
	entry.mu.Unlock()
	if process == nil || !process.NotifyToolCall(tool) {
		bridge.Cancel(tool.ID)
		return ClaudeCodeToolResult{}, errors.New("claude code run manager: run is not waiting for tools")
	}
	return bridge.Wait(ctx, tool)
}

// ContinueSession 在工具结果回填后继续读同一个 CC 进程。
func (m *ClaudeCodeRunManager) ContinueSession(ctx context.Context, sessionKey string, onEvent func(json.RawMessage) error) (*ClaudeCodeResult, error) {
	if m == nil || sessionKey == "" {
		return nil, errors.New("claude code run manager: missing session")
	}
	m.mu.Lock()
	entry := m.runs[sessionKey]
	m.mu.Unlock()
	if entry == nil || entry.process == nil {
		return nil, errors.New("claude code run manager: no active run")
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.process.SetOnEvent(onEvent)
	result, err := entry.process.Continue(ctx)
	if err != nil {
		m.mu.Lock()
		m.releaseLocked(sessionKey, entry)
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Lock()
	if result.SessionID != "" {
		entry.sessionID = result.SessionID
	}
	entry.lastUsed = time.Now()
	m.scheduleLocked(sessionKey, entry)
	m.mu.Unlock()
	return result, nil
}

// ClaudeCodeRunSnapshot 是运行中 run 的只读快照（不含 token/凭据）。
type ClaudeCodeRunSnapshot struct {
	SessionKey  string    `json:"session_key"`
	AccountID   int64     `json:"account_id"`
	StartedAt   time.Time `json:"started_at"`
	LastUsedAt  time.Time `json:"last_used_at"`
	ToolPending int       `json:"tool_pending"`
	State       string    `json:"state"`
}

// Snapshot 返回当前 run 列表。
func (m *ClaudeCodeRunManager) Snapshot() []ClaudeCodeRunSnapshot {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	entries := make(map[string]*claudeCodeRunEntry, len(m.runs))
	for k, v := range m.runs {
		entries[k] = v
	}
	m.mu.Unlock()
	out := make([]ClaudeCodeRunSnapshot, 0, len(entries))
	for key, entry := range entries {
		entry.mu.Lock()
		pending := 0
		if entry.toolBridge != nil {
			pending = entry.toolBridge.PendingCount()
		}
		state := "active"
		if pending > 0 {
			state = "waiting_tool"
		}
		out = append(out, ClaudeCodeRunSnapshot{
			SessionKey:  key,
			AccountID:   entry.accountID,
			StartedAt:   entry.createdAt,
			LastUsedAt:  entry.lastUsed,
			ToolPending: pending,
			State:       state,
		})
		entry.mu.Unlock()
	}
	return out
}

// ReleaseByToolToken 在工具回调超时/失败时结束对应 run。
func (m *ClaudeCodeRunManager) ReleaseByToolToken(token string) {
	if m == nil || token == "" {
		return
	}
	m.mu.Lock()
	sessionKey := m.toolTokens[token]
	m.mu.Unlock()
	if sessionKey != "" {
		m.Release(sessionKey)
	}
}

// ResolveTool 由客户端 tool_result 调用。
func (m *ClaudeCodeRunManager) ResolveTool(sessionKey, toolUseID string, result ClaudeCodeToolResult) error {
	if m == nil || sessionKey == "" {
		return errors.New("claude code run manager: missing session")
	}
	m.mu.Lock()
	entry := m.runs[sessionKey]
	m.mu.Unlock()
	if entry == nil {
		return errors.New("claude code run manager: unknown session")
	}
	entry.mu.Lock()
	bridge := entry.toolBridge
	entry.mu.Unlock()
	if bridge == nil {
		return errors.New("claude code run manager: tool bridge is not ready")
	}
	return bridge.Resolve(toolUseID, result)
}

func (m *ClaudeCodeRunManager) scheduleLocked(sessionKey string, entry *claudeCodeRunEntry) {
	if entry.timer != nil {
		entry.timer.Stop()
	}
	remaining := claudeCodeRunIdleTTL
	if maxRemaining := claudeCodeRunMaxTTL - time.Since(entry.createdAt); maxRemaining < remaining {
		remaining = maxRemaining
	}
	if remaining <= 0 {
		m.releaseLocked(sessionKey, entry)
		return
	}
	entry.timer = time.AfterFunc(remaining, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if current := m.runs[sessionKey]; current == entry {
			m.releaseLocked(sessionKey, entry)
		}
	})
}

func (m *ClaudeCodeRunManager) releaseLocked(sessionKey string, entry *claudeCodeRunEntry) {
	if entry == nil {
		return
	}
	if entry.timer != nil {
		entry.timer.Stop()
	}
	if entry.process != nil {
		_ = entry.process.Close()
		entry.process = nil
	}
	if entry.toolToken != "" {
		delete(m.toolTokens, entry.toolToken)
	}
	if m.OnRelease != nil {
		m.OnRelease(sessionKey)
	}
	if entry.release != nil {
		entry.release()
		entry.release = nil
	}
	delete(m.runs, sessionKey)
}
