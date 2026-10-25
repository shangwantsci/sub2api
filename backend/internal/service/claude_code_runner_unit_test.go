//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeClaudeStub 生成一个最小可执行文件，模拟 Claude Code 的 stream-json 行为。
func writeClaudeStub(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude-stub")
	script := "#!/bin/sh\n" + body
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

const stubResultBody = `printf '%s\n' "$@" > "$HOME/args.txt"
env | grep -E '^(HOME|ANTHROPIC_BASE_URL|NODE_TLS_REJECT_UNAUTHORIZED|DISABLE_AUTOUPDATER|NO_PROXY)=' | sort > "$HOME/env.txt" || true
cat > "$HOME/stdin.txt"
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"pong"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"pong","session_id":"sess-1","duration_ms":12,"total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":2}}'
`

func TestClaudeCodeRunner_RunParsesResultAndArguments(t *testing.T) {
	home := t.TempDir()
	stub := writeClaudeStub(t, stubResultBody)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-must-not-leak")

	runner := &ClaudeCodeRunner{
		BinaryPath: stub,
		HomeDir:    home,
		BaseURL:    "https://127.0.0.1:19446",
		Timeout:    5 * time.Second,
	}
	result, err := runner.Run(context.Background(), ClaudeCodeTurn{
		System:          "be brief",
		Model:           "sonnet",
		Messages:        []ClaudeCodeMessage{{Role: "user", Text: "hello"}},
		DisallowedTools: []string{"Bash", "Read", "Bash"},
	})
	require.NoError(t, err)
	assert.Equal(t, "pong", result.Text)
	assert.Equal(t, "sess-1", result.SessionID)
	assert.Equal(t, int64(12), result.DurationMS)
	assert.Contains(t, string(result.Usage), "input_tokens")

	args, err := os.ReadFile(filepath.Join(home, "args.txt"))
	require.NoError(t, err)
	argText := string(args)
	assert.Contains(t, argText, "--input-format\nstream-json")
	assert.Contains(t, argText, "--output-format\nstream-json")
	assert.Contains(t, argText, "--include-partial-messages")
	assert.Contains(t, argText, "--model\nsonnet")
	assert.Contains(t, argText, "--system-prompt\nbe brief")
	assert.Contains(t, argText, "--tools\n\n--restricted")
	assert.Contains(t, argText, "--safe-mode")
	assert.Contains(t, argText, "--disallowedTools\nBash,Read")

	stdin, err := os.ReadFile(filepath.Join(home, "stdin.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(stdin), `"type":"user"`)
	assert.Contains(t, string(stdin), "hello")

	env, err := os.ReadFile(filepath.Join(home, "env.txt"))
	require.NoError(t, err)
	envText := string(env)
	assert.Contains(t, envText, "HOME="+home)
	assert.Contains(t, envText, "ANTHROPIC_BASE_URL=https://127.0.0.1:19446")
	assert.Contains(t, envText, "NODE_TLS_REJECT_UNAUTHORIZED=0")
	assert.Contains(t, envText, "DISABLE_AUTOUPDATER=1")
	assert.NotContains(t, envText, "must-not-leak", "inherited API key must not reach the child")
}

func TestClaudeCodeRunner_FlattensMultiTurnMessages(t *testing.T) {
	home := t.TempDir()
	stub := writeClaudeStub(t, stubResultBody)

	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: home, Timeout: 5 * time.Second}
	_, err := runner.Run(context.Background(), ClaudeCodeTurn{
		Messages: []ClaudeCodeMessage{
			{Role: "user", Text: "first"},
			{Role: "assistant", Text: "answer"},
			{Role: "user", Text: "second"},
		},
	})
	require.NoError(t, err)

	stdin, err := os.ReadFile(filepath.Join(home, "stdin.txt"))
	require.NoError(t, err)
	text := string(stdin)
	assert.Contains(t, text, "[user]\\nfirst")
	assert.Contains(t, text, "[assistant]\\nanswer")
	assert.Contains(t, text, "[user]\\nsecond")

	args, err := os.ReadFile(filepath.Join(home, "args.txt"))
	require.NoError(t, err)
	assert.NotContains(t, string(args), "--system-prompt")
	assert.NotContains(t, string(args), "--model")
}

func TestClaudeCodeRunner_RejectsEmptyMessages(t *testing.T) {
	runner := &ClaudeCodeRunner{BinaryPath: "/bin/true", HomeDir: t.TempDir()}
	_, err := runner.Run(context.Background(), ClaudeCodeTurn{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty messages")
}

func TestClaudeCodeRunner_Timeout(t *testing.T) {
	home := t.TempDir()
	stub := writeClaudeStub(t, "sleep 5\n")

	runner := &ClaudeCodeRunner{
		BinaryPath: stub,
		HomeDir:    home,
		Timeout:    200 * time.Millisecond,
	}
	start := time.Now()
	_, err := runner.Run(context.Background(), ClaudeCodeTurn{
		Messages: []ClaudeCodeMessage{{Role: "user", Text: "hang"}},
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.Less(t, elapsed, 3*time.Second, "timeout must not wait for the child to exit on its own")
}

func TestClaudeCodeRunner_ErrorResult(t *testing.T) {
	stub := writeClaudeStub(t, `cat >/dev/null
printf '%s\n' '{"type":"result","is_error":true,"result":"boom"}'
`)
	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: t.TempDir(), Timeout: 5 * time.Second}

	_, err := runner.Run(context.Background(), ClaudeCodeTurn{
		Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestClaudeCodeRunner_MissingResultEvent(t *testing.T) {
	stub := writeClaudeStub(t, `cat >/dev/null
printf '%s\n' '{"type":"system","subtype":"init"}'
`)
	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: t.TempDir(), Timeout: 5 * time.Second}

	_, err := runner.Run(context.Background(), ClaudeCodeTurn{
		Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}},
	})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "no result event")
}

func TestClaudeCodeRunner_StreamEventsWithoutResultAreNotSuccess(t *testing.T) {
	// 有 stream_event 但没有 result：进程可能中途崩溃，不能当成成功。
	stub := writeClaudeStub(t, `cat >/dev/null
printf '%s\n' '{"type":"stream_event","event":{"type":"message_start"}}'
`)
	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: t.TempDir(), Timeout: 5 * time.Second}

	_, err := runner.Run(context.Background(), ClaudeCodeTurn{
		Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}},
	})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "no result event")
}

func TestClaudeCodeRunner_OnEventReceivesStreamEvents(t *testing.T) {
	stub := writeClaudeStub(t, `cat >/dev/null
printf '%s\n' '{"type":"stream_event","event":{"type":"message_start"}}'
printf '%s\n' '{"type":"result","is_error":false,"result":"ok"}'
`)
	var events []string
	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: t.TempDir(), Timeout: 5 * time.Second}
	runner.OnEvent = func(event json.RawMessage) error {
		events = append(events, string(event))
		return nil
	}

	result, err := runner.Run(context.Background(), ClaudeCodeTurn{
		Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Text)
	require.Len(t, events, 1)
	assert.Contains(t, events[0], "message_start")
}

func TestClaudeCodeRunner_OnEventErrorStopsAndKillsProcess(t *testing.T) {
	stub := writeClaudeStub(t, `cat >/dev/null
printf '%s\n' '{"type":"stream_event","event":{"type":"message_start"}}'
sleep 5
`)
	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: t.TempDir(), Timeout: 10 * time.Second}
	runner.OnEvent = func(json.RawMessage) error { return errors.New("client gone") }

	start := time.Now()
	_, err := runner.Run(context.Background(), ClaudeCodeTurn{
		Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}},
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "client gone")
	assert.Less(t, elapsed, 3*time.Second, "callback error must not wait for the child to finish")
}

func TestClaudeCodeRunner_SendsImageContentBlocks(t *testing.T) {
	home := t.TempDir()
	stub := writeClaudeStub(t, stubResultBody)
	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: home, Timeout: 5 * time.Second}
	_, err := runner.Run(context.Background(), ClaudeCodeTurn{
		Messages: []ClaudeCodeMessage{{
			Role: "user",
			Text: "look",
			Parts: []ClaudeCodePart{
				{Text: "look"},
				{Image: &ClaudeCodeImage{MediaType: "image/png", Data: "aGVsbG8="}},
			},
		}},
	})
	require.NoError(t, err)

	stdin, err := os.ReadFile(filepath.Join(home, "stdin.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(stdin), `"type":"image"`)
	assert.Contains(t, string(stdin), `"media_type":"image/png"`)
	assert.Contains(t, string(stdin), `"data":"aGVsbG8="`)
}

func TestClaudeCodeRunner_PassesResumeSession(t *testing.T) {
	home := t.TempDir()
	stub := writeClaudeStub(t, stubResultBody)
	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: home, Timeout: 5 * time.Second}
	_, err := runner.Run(context.Background(), ClaudeCodeTurn{
		ResumeSessionID: "sess-existing",
		Messages:        []ClaudeCodeMessage{{Role: "user", Text: "next"}},
	})
	require.NoError(t, err)
	args, err := os.ReadFile(filepath.Join(home, "args.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(args), "--resume\nsess-existing")
}

func TestClaudeCodeRunner_PassesMCPConfigAndTools(t *testing.T) {
	home := t.TempDir()
	stub := writeClaudeStub(t, stubResultBody)
	runner := &ClaudeCodeRunner{BinaryPath: stub, HomeDir: home, Timeout: 5 * time.Second}
	_, err := runner.Run(context.Background(), ClaudeCodeTurn{
		Messages:      []ClaudeCodeMessage{{Role: "user", Text: "hi"}},
		MCPConfigPath: "/tmp/mcp.json",
		AllowedTools:  []string{"mcp__sub2api__Bash"},
	})
	require.NoError(t, err)
	args, err := os.ReadFile(filepath.Join(home, "args.txt"))
	require.NoError(t, err)
	text := string(args)
	assert.Contains(t, text, "--tools\nmcp__sub2api__Bash")
	assert.Contains(t, text, "--mcp-config\n/tmp/mcp.json")
	assert.Contains(t, text, "--strict-mcp-config")
}
