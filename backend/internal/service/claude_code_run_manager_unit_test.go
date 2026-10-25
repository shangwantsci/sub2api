//go:build unit

package service

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeRunManager_BindLookupRelease(t *testing.T) {
	m := NewClaudeCodeRunManager()
	var released atomic.Int32
	release := func() { released.Add(1) }

	now := time.Now()
	m.Bind("s1", 7, release, now)
	id, ok := m.Lookup("s1")
	require.True(t, ok)
	assert.Equal(t, int64(7), id)

	m.Release("s1")
	assert.Equal(t, int32(1), released.Load())
	_, ok = m.Lookup("s1")
	assert.False(t, ok)
}

func TestClaudeCodeRunManager_IdleRelease(t *testing.T) {
	m := NewClaudeCodeRunManager()
	var released atomic.Int32
	m.Bind("s1", 7, func() { released.Add(1) }, time.Now())

	// 直接驱动过期而不是 sleep 3 分钟。
	m.mu.Lock()
	entry := m.runs["s1"]
	entry.lastUsed = time.Now().Add(-claudeCodeRunIdleTTL - time.Second)
	m.mu.Unlock()

	_, ok := m.Lookup("s1")
	assert.False(t, ok)
	assert.Equal(t, int32(1), released.Load())
}

func TestClaudeCodeRunManager_BindReplacesOldRun(t *testing.T) {
	m := NewClaudeCodeRunManager()
	var released atomic.Int32
	m.Bind("s1", 7, func() { released.Add(1) }, time.Now())
	m.Bind("s1", 8, func() {}, time.Now())
	assert.Equal(t, int32(1), released.Load())
	id, ok := m.Lookup("s1")
	require.True(t, ok)
	assert.Equal(t, int64(8), id)
}

type fakeTurnProcess struct {
	mu     sync.Mutex
	turns  []ClaudeCodeTurn
	closed bool
}

func (p *fakeTurnProcess) Send(_ context.Context, turn ClaudeCodeTurn) (*ClaudeCodeResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.turns = append(p.turns, turn)
	return &ClaudeCodeResult{Text: "ok", SessionID: "sess-1"}, nil
}

func (p *fakeTurnProcess) SetOnEvent(func(json.RawMessage) error) {}

func (p *fakeTurnProcess) NotifyToolCall(*ClaudeCodePendingTool) bool { return true }

func (p *fakeTurnProcess) Continue(context.Context) (*ClaudeCodeResult, error) {
	return &ClaudeCodeResult{Text: "continued"}, nil
}

func (p *fakeTurnProcess) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func TestClaudeCodeRunManager_RunTurnReusesProcess(t *testing.T) {
	m := NewClaudeCodeRunManager()
	var released atomic.Int32
	proc := &fakeTurnProcess{}
	m.NewProcess = func(context.Context, *Account, ClaudeCodeTurn, func(json.RawMessage) error) (ClaudeCodeTurnProcess, error) {
		return proc, nil
	}
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	first := ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "first"}}}
	_, err := m.RunTurn(context.Background(), "s1", account, func() { released.Add(1) }, first, nil)
	require.NoError(t, err)

	second := ClaudeCodeTurn{Messages: []ClaudeCodeMessage{
		{Role: "user", Text: "first"},
		{Role: "assistant", Text: "answer"},
		{Role: "user", Text: "second"},
	}}
	_, err = m.RunTurn(context.Background(), "s1", account, func() {}, second, nil)
	require.NoError(t, err)

	proc.mu.Lock()
	require.Len(t, proc.turns, 2)
	assert.Equal(t, "second", proc.turns[1].Messages[0].Text)
	proc.mu.Unlock()

	m.Release("s1")
	assert.Equal(t, int32(1), released.Load())
	proc.mu.Lock()
	assert.True(t, proc.closed)
	proc.mu.Unlock()
}

func TestClaudeCodeRunManager_ToolTokenAndResolve(t *testing.T) {
	m := NewClaudeCodeRunManager()
	proc := &fakeTurnProcess{}
	m.NewProcess = func(context.Context, *Account, ClaudeCodeTurn, func(json.RawMessage) error) (ClaudeCodeTurnProcess, error) {
		return proc, nil
	}
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	_, err := m.RunTurn(context.Background(), "s1", account, func() {}, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}}, nil)
	require.NoError(t, err)

	token, ok := m.ToolToken("s1")
	require.True(t, ok)
	require.NotEmpty(t, token)

	done := make(chan ClaudeCodeToolResult, 1)
	go func() {
		result, _ := m.HandleToolCall(context.Background(), token, "Bash", json.RawMessage(`{"command":"ls"}`))
		done <- result
	}()

	var toolID string
	require.Eventually(t, func() bool {
		m.mu.Lock()
		entry := m.runs["s1"]
		m.mu.Unlock()
		if entry == nil {
			return false
		}
		entry.mu.Lock()
		bridge := entry.toolBridge
		entry.mu.Unlock()
		if bridge == nil {
			return false
		}
		bridge.mu.Lock()
		defer bridge.mu.Unlock()
		for id := range bridge.pending {
			toolID = id
			return true
		}
		return false
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, m.ResolveTool("s1", toolID, ClaudeCodeToolResult{Content: []map[string]any{{"type": "text", "text": "ok"}}}))
	result := <-done
	assert.Equal(t, "ok", result.Content[0]["text"])
}

func TestClaudeCodeRunManager_RegisterToolTokenBeforeRun(t *testing.T) {
	m := NewClaudeCodeRunManager()
	proc := &fakeTurnProcess{}
	m.NewProcess = func(context.Context, *Account, ClaudeCodeTurn, func(json.RawMessage) error) (ClaudeCodeTurnProcess, error) {
		return proc, nil
	}
	m.RegisterToolToken("s1", "pre-token")
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	_, err := m.RunTurn(context.Background(), "s1", account, func() {}, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}}, nil)
	require.NoError(t, err)
	token, ok := m.ToolToken("s1")
	require.True(t, ok)
	assert.Equal(t, "pre-token", token)
}

func TestClaudeCodeRunManager_ContinueAfterTool(t *testing.T) {
	m := NewClaudeCodeRunManager()
	proc := &fakeTurnProcess{}
	m.NewProcess = func(context.Context, *Account, ClaudeCodeTurn, func(json.RawMessage) error) (ClaudeCodeTurnProcess, error) {
		return proc, nil
	}
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	_, err := m.RunTurn(context.Background(), "s1", account, func() {}, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}}, nil)
	require.NoError(t, err)

	result, err := m.ContinueSession(context.Background(), "s1", nil)
	require.NoError(t, err)
	assert.Equal(t, "continued", result.Text)

	_, err = m.ContinueSession(context.Background(), "missing", nil)
	require.Error(t, err)
}

func TestClaudeCodeRunManager_UnknownToolResult(t *testing.T) {
	m := NewClaudeCodeRunManager()
	require.Error(t, m.ResolveTool("s1", "toolu_missing", ClaudeCodeToolResult{}))
}

func TestClaudeCodeRunManager_ReleaseByToolToken(t *testing.T) {
	m := NewClaudeCodeRunManager()
	proc := &fakeTurnProcess{}
	m.NewProcess = func(context.Context, *Account, ClaudeCodeTurn, func(json.RawMessage) error) (ClaudeCodeTurnProcess, error) {
		return proc, nil
	}
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	_, err := m.RunTurn(context.Background(), "s1", account, func() {}, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}}, nil)
	require.NoError(t, err)
	token, ok := m.ToolToken("s1")
	require.True(t, ok)
	m.ReleaseByToolToken(token)
	proc.mu.Lock()
	assert.True(t, proc.closed)
	proc.mu.Unlock()
	_, ok = m.ToolToken("s1")
	assert.False(t, ok)
}

func TestClaudeCodeRuntime_ReleaseRun(t *testing.T) {
	m := NewClaudeCodeRunManager()
	proc := &fakeTurnProcess{}
	m.NewProcess = func(context.Context, *Account, ClaudeCodeTurn, func(json.RawMessage) error) (ClaudeCodeTurnProcess, error) {
		return proc, nil
	}
	runtime := &ClaudeCodeRuntime{runs: m}
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	_, err := m.RunTurn(context.Background(), "s1", account, func() {}, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}}, nil)
	require.NoError(t, err)
	require.True(t, runtime.ReleaseRun("s1"))
	require.False(t, runtime.ReleaseRun("s1"))
}

func TestClaudeCodeRuntime_EventLog(t *testing.T) {
	r := &ClaudeCodeRuntime{events: newClaudeCodeEventLog(2)}
	r.RecordEvent("run_started", "abcdef123456789", 7, "")
	r.RecordEvent("manual_release", "abcdef123456789", 0, "")
	r.RecordEvent("tool_timeout", "", 0, "timeout")
	events := r.Events()
	require.Len(t, events, 2)
	assert.Equal(t, "manual_release", events[0].Type)
	assert.Equal(t, "tool_timeout", events[1].Type)
}
