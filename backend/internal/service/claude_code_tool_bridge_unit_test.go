//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeToolBridge_Resolve(t *testing.T) {
	bridge := NewClaudeCodeToolBridge()
	tool := bridge.Start("Bash", json.RawMessage(`{"command":"ls"}`))
	require.NotEmpty(t, tool.ID)

	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = bridge.Resolve(tool.ID, ClaudeCodeToolResult{Content: []map[string]any{{"type": "text", "text": "ok"}}})
	}()
	result, err := bridge.Wait(context.Background(), tool)
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Content[0]["text"])
}

func TestClaudeCodeToolBridge_Timeout(t *testing.T) {
	bridge := NewClaudeCodeToolBridge()
	tool := bridge.Start("Bash", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := bridge.Wait(ctx, tool)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Error(t, bridge.Resolve(tool.ID, ClaudeCodeToolResult{}))
}

func TestBuildClaudeCodeMCPConfig(t *testing.T) {
	raw, err := BuildClaudeCodeMCPConfig("/bin/sub2api", "http://127.0.0.1:1/run", "tok", "/tmp/tools.json")
	require.NoError(t, err)
	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(raw, &cfg))
	server := cfg.MCPServers["sub2api"]
	assert.Equal(t, "/bin/sub2api", server.Command)
	assert.Equal(t, []string{"cc-mcp-helper", "--url", "http://127.0.0.1:1/run", "--tools", "/tmp/tools.json", "--token", "tok"}, server.Args)
}
