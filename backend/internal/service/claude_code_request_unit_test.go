//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildClaudeCodeTurn_StringContent(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-5-5",
		"system":"be brief",
		"max_tokens":4096,
		"messages":[{"role":"user","content":"hello"}]
	}`)

	turn, err := BuildClaudeCodeTurn(body, "claude-sonnet-5-5")
	require.NoError(t, err)
	assert.Equal(t, "be brief", turn.System)
	assert.Equal(t, "sonnet", turn.Model)
	require.Len(t, turn.Messages, 1)
	assert.Equal(t, "user", turn.Messages[0].Role)
	assert.Equal(t, "hello", turn.Messages[0].Text)
}

func TestBuildClaudeCodeTurn_ArraySystemAndContent(t *testing.T) {
	body := []byte(`{
		"system":[{"type":"text","text":"part one"},{"type":"text","text":"part two"}],
		"messages":[
			{"role":"user","content":[{"type":"text","text":"first"}]},
			{"role":"assistant","content":[{"type":"text","text":"answer"},{"type":"thinking","thinking":"hidden"}]},
			{"role":"user","content":[{"type":"text","text":"second"}]}
		]
	}`)

	turn, err := BuildClaudeCodeTurn(body, "claude-opus-5-5")
	require.NoError(t, err)
	assert.Equal(t, "part one\n\npart two", turn.System)
	assert.Equal(t, "opus", turn.Model)
	require.Len(t, turn.Messages, 3)
	assert.Equal(t, "first", turn.Messages[0].Text)
	assert.Equal(t, "answer", turn.Messages[1].Text)
	assert.Equal(t, "second", turn.Messages[2].Text)
}

func TestBuildClaudeCodeTurn_RejectsToolUse(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"x","content":"y"}]}]}`)
	_, err := BuildClaudeCodeTurn(body, "claude-opus-5-5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool_result")
}

func TestBuildClaudeCodeTurn_AcceptsBase64Image(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}]}`)
	turn, err := BuildClaudeCodeTurn(body, "claude-opus-5-5")
	require.NoError(t, err)
	require.Len(t, turn.Messages, 1)
	require.Len(t, turn.Messages[0].Parts, 2)
	require.NotNil(t, turn.Messages[0].Parts[1].Image)
	assert.Equal(t, "image/png", turn.Messages[0].Parts[1].Image.MediaType)
	assert.Equal(t, "aGVsbG8=", turn.Messages[0].Parts[1].Image.Data)
}

func TestBuildClaudeCodeTurn_AcceptsURLImage(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}}]}]}`)
	turn, err := BuildClaudeCodeTurn(body, "claude-opus-5-5")
	require.NoError(t, err)
	require.Len(t, turn.Messages[0].Parts, 1)
	assert.Equal(t, "https://example.com/a.png", turn.Messages[0].Parts[0].Image.URL)
}

func TestBuildClaudeCodeTurn_RejectsInvalidImageMediaType(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/svg+xml","data":"aGVsbG8="}}]}]}`)
	_, err := BuildClaudeCodeTurn(body, "claude-opus-5-5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media_type")
}

func TestBuildClaudeCodeTurn_RejectsAssistantImage(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}]}`)
	_, err := BuildClaudeCodeTurn(body, "claude-opus-5-5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user messages")
}

func TestBuildClaudeCodeTurn_RejectsUnknownModel(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	_, err := BuildClaudeCodeTurn(body, "gpt-4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestBuildClaudeCodeTurn_RejectsEmptyMessages(t *testing.T) {
	_, err := BuildClaudeCodeTurn([]byte(`{"messages":[]}`), "claude-opus-5-5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "messages")
}

func TestBuildClaudeCodeTurn_RejectsThinkingOnlyMessage(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"x"}]}]}`)
	_, err := BuildClaudeCodeTurn(body, "claude-opus-5-5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no text content")
}

func TestBuildClaudeCodeTurn_RejectsTools(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Bash","input_schema":{"type":"object"}}]}`)
	_, err := BuildClaudeCodeTurn(body, "claude-opus-5-5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tools")
}

func TestBuildClaudeCodeTurn_RejectsTemperature(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"}],"temperature":0}`)
	_, err := BuildClaudeCodeTurn(body, "claude-opus-5-5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "temperature")
}

func TestBuildClaudeCodeTurn_AllowsEmptyToolsArray(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"}],"tools":[]}`)
	_, err := BuildClaudeCodeTurn(body, "claude-opus-5-5")
	require.NoError(t, err)
}

func TestBuildClaudeCodeTurn_NullSystem(t *testing.T) {
	body := []byte(`{"system":null,"messages":[{"role":"user","content":"hi"}]}`)
	turn, err := BuildClaudeCodeTurn(body, "claude-haiku-5-5")
	require.NoError(t, err)
	assert.Empty(t, turn.System)
	assert.Equal(t, "haiku", turn.Model)
}
