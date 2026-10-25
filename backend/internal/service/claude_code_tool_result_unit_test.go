//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractClaudeCodeToolResults(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"},{"type":"tool_result","tool_use_id":"toolu_2","is_error":true,"content":[{"type":"text","text":"failed"}]}]}]}`)
	results, err := ExtractClaudeCodeToolResults(body)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "toolu_1", results[0].ToolUseID)
	assert.Equal(t, "ok", results[0].Content[0]["text"])
	assert.True(t, results[1].IsError)
}

func TestExtractClaudeCodeToolResults_NoToolResult(t *testing.T) {
	results, err := ExtractClaudeCodeToolResults([]byte(`{"messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	assert.Empty(t, results)
}
