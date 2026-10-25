//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodePool_RunRequestEndToEnd(t *testing.T) {
	pool := NewClaudeCodePool(0, 2)
	var gotTurn ClaudeCodeTurn

	pool.NewRunner = func(_ *Account, onEvent func(json.RawMessage) error) (ClaudeCodeTurnRunner, error) {
		return poolTestRunner{fn: func(_ context.Context, turn ClaudeCodeTurn) (*ClaudeCodeResult, error) {
			gotTurn = turn
			if onEvent != nil {
				_ = onEvent(json.RawMessage(`{"type":"message_start"}`))
			}
			return &ClaudeCodeResult{
				Text:  "pong",
				Usage: json.RawMessage(`{"input_tokens":2,"cache_creation_input_tokens":19205,"output_tokens":4}`),
			}, nil
		}}, nil
	}

	body := []byte(`{"system":"be brief","messages":[{"role":"user","content":"hello"}]}`)
	var events int
	result, err := pool.RunRequest(context.Background(),
		&Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		body, "claude-opus-5-5",
		func(json.RawMessage) error { events++; return nil },
	)
	require.NoError(t, err)
	assert.Equal(t, "be brief", gotTurn.System)
	assert.Equal(t, "opus", gotTurn.Model)
	require.Len(t, gotTurn.Messages, 1)
	assert.Equal(t, "pong", result.Result.Text)
	assert.Equal(t, 1, events)
	assert.Greater(t, result.Usage.InputTokens, 0)
	assert.Zero(t, result.Usage.CacheCreationInputTokens)
	assert.Equal(t, 4, result.Usage.OutputTokens)
}

func TestClaudeCodePool_RunRequestRejectsToolsBeforeSpawning(t *testing.T) {
	pool := NewClaudeCodePool(0, 2)
	called := false
	pool.NewRunner = func(*Account, func(json.RawMessage) error) (ClaudeCodeTurnRunner, error) {
		called = true
		return poolTestRunner{}, nil
	}

	body := []byte(`{"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Bash"}]}`)
	_, err := pool.RunRequest(context.Background(),
		&Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		body, "claude-opus-5-5", nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tools")
	assert.False(t, called, "unsupported requests must not spawn a CC process")
}
