//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeClaudeCodeUsage_ExcludesInjectedPrefix(t *testing.T) {
	turn := ClaudeCodeTurn{
		System:   "client system",
		Messages: []ClaudeCodeMessage{{Role: "user", Text: "hello"}},
	}
	result := &ClaudeCodeResult{
		Usage: json.RawMessage(`{"input_tokens":2,"cache_creation_input_tokens":19205,"cache_read_input_tokens":0,"output_tokens":4}`),
	}

	usage := NormalizeClaudeCodeUsage(turn, result)

	assert.Greater(t, usage.InputTokens, 0)
	assert.Less(t, usage.InputTokens, 100, "only the client's own text should be billed")
	assert.Zero(t, usage.CacheCreationInputTokens, "CC's injected prefix must not be billed")
	assert.Zero(t, usage.CacheReadInputTokens)
	assert.Equal(t, 4, usage.OutputTokens)
	assert.Contains(t, string(usage.Raw), "19205", "raw upstream usage stays available for audit")
}

func TestNormalizeClaudeCodeUsage_NoSystemCountsOnlyClientMessages(t *testing.T) {
	turn := ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "just this"}}}
	result := &ClaudeCodeResult{Usage: json.RawMessage(`{"input_tokens":1,"cache_creation_input_tokens":19205,"output_tokens":2}`)}

	usage := NormalizeClaudeCodeUsage(turn, result)

	assert.Greater(t, usage.InputTokens, 0)
	assert.Less(t, usage.InputTokens, 50)
	assert.Zero(t, usage.CacheCreationInputTokens)
	assert.Equal(t, 2, usage.OutputTokens)
}

func TestNormalizeClaudeCodeUsage_FallsBackToResultTextForOutput(t *testing.T) {
	turn := ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}}
	result := &ClaudeCodeResult{Text: "some answer", Usage: json.RawMessage(`{"input_tokens":1}`)}

	usage := NormalizeClaudeCodeUsage(turn, result)

	assert.Greater(t, usage.OutputTokens, 0)
}

func TestNormalizeClaudeCodeUsage_NilResult(t *testing.T) {
	usage := NormalizeClaudeCodeUsage(ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}}, nil)
	assert.Greater(t, usage.InputTokens, 0)
	assert.Zero(t, usage.OutputTokens)
}
