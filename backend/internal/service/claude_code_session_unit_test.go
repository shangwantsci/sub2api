//go:build unit

package service

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeSessionStore_LookupAndTTL(t *testing.T) {
	store := NewClaudeCodeSessionStore()
	now := time.Unix(1000, 0)
	store.Record("s1", 7, "cc-session", now)

	id, ok := store.Lookup("s1", 7, now.Add(time.Minute))
	require.True(t, ok)
	assert.Equal(t, "cc-session", id)

	_, ok = store.Lookup("s1", 8, now.Add(time.Minute))
	assert.False(t, ok, "account mismatch")

	_, ok = store.Lookup("s1", 7, now.Add(claudeCodeSessionIdleTTL+time.Second))
	assert.False(t, ok, "idle TTL")

	store.Record("s2", 7, "cc-session-2", now)
	_, ok = store.Lookup("s2", 7, now.Add(claudeCodeSessionMaxTTL+time.Second))
	assert.False(t, ok, "absolute TTL")
}

func TestLatestClaudeCodeUserTurn(t *testing.T) {
	turn := ClaudeCodeTurn{
		System: "sys",
		Model:  "sonnet",
		Messages: []ClaudeCodeMessage{
			{Role: "user", Text: "old"},
			{Role: "assistant", Text: "answer"},
			{Role: "user", Text: "new", Parts: []ClaudeCodePart{{Text: "new"}}},
		},
	}
	got, err := latestClaudeCodeUserTurn(turn)
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "new", got.Messages[0].Text)
	assert.Equal(t, "sys", got.System)
	assert.Equal(t, "sonnet", got.Model)
}

func TestClaudeCodeRuntime_StickySessionAllowed(t *testing.T) {
	r := &ClaudeCodeRuntime{stickyFirst: make(map[string]time.Time)}
	require.True(t, r.StickySessionAllowed("s1"))
	require.True(t, r.StickySessionAllowed("s1"))

	r.stickyMu.Lock()
	r.stickyFirst["s1"] = time.Now().Add(-2*time.Hour - time.Minute)
	r.stickyMu.Unlock()
	require.False(t, r.StickySessionAllowed("s1"))
	require.True(t, r.StickySessionAllowed("s1"), "expired binding starts a new window")
}

func TestClaudeCodeRuntime_PrepareMCPRun(t *testing.T) {
	r := &ClaudeCodeRuntime{
		cfg: &config.Config{ProcessPool: config.ProcessPoolConfig{
			DataDir:           t.TempDir(),
			CCCallbackBaseURL: "http://127.0.0.1:1",
		}},
		runs: NewClaudeCodeRunManager(),
	}
	body := []byte(`{"tools":[{"name":"Bash","description":"run","input_schema":{"type":"object"}}],"messages":[]}`)
	configPath, allowed, token, err := r.prepareMCPRun("sess-1", body)
	require.NoError(t, err)
	require.NotEmpty(t, configPath)
	require.NotEmpty(t, token)
	require.Equal(t, []string{"mcp__sub2api__Bash"}, allowed)
	raw, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Contains(t, string(raw), "cc-mcp-helper")
}
