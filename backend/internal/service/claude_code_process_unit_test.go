//go:build unit

package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeProcess_MultipleTurns(t *testing.T) {
	home := t.TempDir()
	body := `IFS= read -r l1
printf '%s\n' "$l1" >> "$HOME/stdin.txt"
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"one"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"one","session_id":"sess-1","usage":{"input_tokens":1,"output_tokens":1}}'
IFS= read -r l2
printf '%s\n' "$l2" >> "$HOME/stdin.txt"
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"two"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"two","session_id":"sess-1","usage":{"input_tokens":1,"output_tokens":1}}'
`
	stub := writeClaudeStub(t, body)
	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: home, Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	proc, err := StartClaudeCodeProcess(ctx, runner, ClaudeCodeTurn{
		Model:    "sonnet",
		Messages: []ClaudeCodeMessage{{Role: "user", Text: "first"}},
	})
	require.NoError(t, err)
	defer proc.Close()

	first, err := proc.Send(ctx, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "first"}}})
	require.NoError(t, err)
	assert.Equal(t, "one", first.Text)

	second, err := proc.Send(ctx, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "second"}}})
	require.NoError(t, err)
	assert.Equal(t, "two", second.Text)

	stdin, err := os.ReadFile(filepath.Join(home, "stdin.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(stdin), "first")
	assert.Contains(t, string(stdin), "second")
}

func TestClaudeCodeProcess_ReturnsPendingTool(t *testing.T) {
	home := t.TempDir()
	stub := writeClaudeStub(t, "IFS= read -r l\nsleep 2\n")
	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: home, Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proc, err := StartClaudeCodeProcess(ctx, runner, ClaudeCodeTurn{})
	require.NoError(t, err)
	defer proc.Close()

	done := make(chan *ClaudeCodeResult, 1)
	go func() {
		result, _ := proc.Send(ctx, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}})
		done <- result
	}()
	require.Eventually(t, func() bool {
		return proc.NotifyToolCall(&ClaudeCodePendingTool{ID: "toolu_1", Name: "Bash"})
	}, time.Second, 10*time.Millisecond)
	result := <-done
	require.NotNil(t, result)
	require.NotNil(t, result.PendingTool)
	assert.Equal(t, "toolu_1", result.PendingTool.ID)
}

func TestClaudeCodeProcess_ContinueAfterPendingTool(t *testing.T) {
	home := t.TempDir()
	stub := writeClaudeStub(t, "IFS= read -r l\nwhile [ ! -f \"$HOME/release\" ]; do sleep 0.02; done\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"done\",\"session_id\":\"sess-1\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n")
	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: home, Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proc, err := StartClaudeCodeProcess(ctx, runner, ClaudeCodeTurn{})
	require.NoError(t, err)
	defer proc.Close()

	sent := make(chan *ClaudeCodeResult, 1)
	go func() {
		result, _ := proc.Send(ctx, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}})
		sent <- result
	}()
	require.Eventually(t, func() bool {
		return proc.NotifyToolCall(&ClaudeCodePendingTool{ID: "toolu_1", Name: "Bash"})
	}, time.Second, 10*time.Millisecond)
	pending := <-sent
	require.NotNil(t, pending)
	require.NotNil(t, pending.PendingTool)

	require.NoError(t, os.WriteFile(filepath.Join(home, "release"), []byte("1"), 0o600))
	final, err := proc.Continue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "done", final.Text)
}
