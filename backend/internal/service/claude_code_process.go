package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ClaudeCodeProcess 是一个长期存活的 CC stream-json 进程。
// Start 后可以多次 Send，每次读到 result 为止；Close 幂等。
type ClaudeCodeProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  *limitedBuffer
	onEvent func(json.RawMessage) error

	closeOnce sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
	waitErr   error

	readerMu     sync.Mutex
	readerActive bool
	readerCh     chan processReadResult
	toolCalls    chan *ClaudeCodePendingTool
	eventMu      sync.RWMutex
}

type processReadResult struct {
	result *ClaudeCodeResult
	err    error
}

// StartClaudeCodeProcess 启动一个进程；turn 决定 model/system/resume 等固定参数。
// 后续 Send 只能发送同一 session 的新 user turn。
func StartClaudeCodeProcess(ctx context.Context, runner *ClaudeCodeRunner, turn ClaudeCodeTurn) (*ClaudeCodeProcess, error) {
	if runner == nil || strings.TrimSpace(runner.BinaryPath) == "" {
		return nil, errors.New("claude code process: binary path is empty")
	}
	if strings.TrimSpace(runner.HomeDir) == "" {
		return nil, errors.New("claude code process: home dir is empty")
	}

	processCtx, cancel := context.WithCancel(ctx)
	toolsArg := ""
	if len(turn.AllowedTools) > 0 {
		toolsArg = strings.Join(normalizeToolNames(turn.AllowedTools), ",")
	}
	args := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--tools", toolsArg,
		"--restricted",
		"--safe-mode",
	}
	if strings.TrimSpace(turn.MCPConfigPath) != "" {
		args = append(args, "--mcp-config", turn.MCPConfigPath, "--strict-mcp-config")
	}
	if strings.TrimSpace(turn.Model) != "" {
		args = append(args, "--model", strings.TrimSpace(turn.Model))
	}
	if strings.TrimSpace(turn.System) != "" {
		args = append(args, "--system-prompt", turn.System)
	}
	if strings.TrimSpace(turn.ResumeSessionID) != "" {
		args = append(args, "--resume", strings.TrimSpace(turn.ResumeSessionID))
	}
	if tools := normalizeToolNames(turn.DisallowedTools); len(tools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(tools, ","))
	}

	cmd := exec.Command(runner.BinaryPath, args...)
	cmd.Dir = runner.HomeDir
	cmd.Env = runner.environment()
	configureClaudeCodeProcess(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("claude code process: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("claude code process: stdout pipe: %w", err)
	}
	stderr := &limitedBuffer{limit: claudeCodeStderrBytes}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("claude code process: start: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), claudeCodeMaxLineBytes)
	p := &ClaudeCodeProcess{
		cmd:       cmd,
		stdin:     stdin,
		scanner:   scanner,
		stderr:    stderr,
		onEvent:   runner.OnEvent,
		cancel:    cancel,
		done:      make(chan struct{}),
		readerCh:  make(chan processReadResult, 4),
		toolCalls: make(chan *ClaudeCodePendingTool, 8),
	}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	go func() {
		<-processCtx.Done()
		killClaudeCodeProcess(cmd)
	}()
	return p, nil
}

// SetOnEvent 更新当前 SSE 回调；同一进程跨请求时使用。
func (p *ClaudeCodeProcess) SetOnEvent(onEvent func(json.RawMessage) error) {
	if p == nil {
		return
	}
	p.eventMu.Lock()
	p.onEvent = onEvent
	p.eventMu.Unlock()
}

func (p *ClaudeCodeProcess) currentOnEvent() func(json.RawMessage) error {
	p.eventMu.RLock()
	defer p.eventMu.RUnlock()
	return p.onEvent
}

// NotifyToolCall 由 MCP callback 调用，让阻塞中的 Send 返回 tool_use。
func (p *ClaudeCodeProcess) NotifyToolCall(tool *ClaudeCodePendingTool) bool {
	if p == nil || tool == nil {
		return false
	}
	select {
	case p.toolCalls <- tool:
		return true
	default:
		return false
	}
}

// Send 写入一个 user turn，并读到该 turn 的 result。
func (p *ClaudeCodeProcess) Send(ctx context.Context, turn ClaudeCodeTurn) (*ClaudeCodeResult, error) {
	if p == nil {
		return nil, errors.New("claude code process: nil process")
	}
	prompt, err := flattenClaudeCodePrompt(turn.Messages)
	if err != nil {
		return nil, err
	}
	content := claudeCodeInputContent(turn.Messages, prompt)
	input, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": content,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("claude code process: encode input: %w", err)
	}
	if _, err := p.stdin.Write(append(input, '\n')); err != nil {
		return nil, fmt.Errorf("claude code process: write input: %w", err)
	}

	p.startReader()
	return p.await(ctx)
}

// Continue 不写输入，等待当前 turn 的 result 或下一个工具调用。
func (p *ClaudeCodeProcess) Continue(ctx context.Context) (*ClaudeCodeResult, error) {
	if p == nil {
		return nil, errors.New("claude code process: nil process")
	}
	p.startReader()
	return p.await(ctx)
}

func (p *ClaudeCodeProcess) startReader() {
	p.readerMu.Lock()
	if p.readerActive {
		p.readerMu.Unlock()
		return
	}
	p.readerActive = true
	p.readerMu.Unlock()
	go func() {
		result, err := p.readUntilResult()
		p.readerMu.Lock()
		p.readerActive = false
		p.readerMu.Unlock()
		p.readerCh <- processReadResult{result: result, err: err}
	}()
}

func (p *ClaudeCodeProcess) await(ctx context.Context) (*ClaudeCodeResult, error) {
	select {
	case tool := <-p.toolCalls:
		return &ClaudeCodeResult{PendingTool: tool}, nil
	case got := <-p.readerCh:
		return got.result, got.err
	case <-ctx.Done():
		p.Close()
		return nil, ctx.Err()
	}
}

func (p *ClaudeCodeProcess) readUntilResult() (*ClaudeCodeResult, error) {
	var result *ClaudeCodeResult
	var assistantText strings.Builder
	for p.scanner.Scan() {
		line := bytes.TrimSpace(p.scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var envelope struct {
			Type         string          `json:"type"`
			Result       string          `json:"result"`
			IsError      bool            `json:"is_error"`
			SessionID    string          `json:"session_id"`
			DurationMS   int64           `json:"duration_ms"`
			TotalCostUSD float64         `json:"total_cost_usd"`
			Usage        json.RawMessage `json:"usage"`
			Event        json.RawMessage `json:"event"`
			Message      struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			continue
		}
		if onEvent := p.currentOnEvent(); onEvent != nil && envelope.Type != "result" {
			if err := onEvent(json.RawMessage(line)); err != nil {
				return nil, err
			}
		}
		switch envelope.Type {
		case "assistant":
			for _, block := range envelope.Message.Content {
				if block.Type == "text" {
					assistantText.WriteString(block.Text)
				}
			}
		case "result":
			result = &ClaudeCodeResult{
				Text:         envelope.Result,
				SessionID:    envelope.SessionID,
				Usage:        envelope.Usage,
				TotalCostUSD: envelope.TotalCostUSD,
				DurationMS:   envelope.DurationMS,
			}
			if result.Text == "" {
				result.Text = assistantText.String()
			}
			return result, nil
		}
	}
	if err := p.scanner.Err(); err != nil {
		return nil, fmt.Errorf("claude code process: read stream: %w (stderr: %s)", err, p.stderr.String())
	}
	return nil, fmt.Errorf("claude code process: stream ended before result (stderr: %s)", p.stderr.String())
}

// Close 关闭 stdin、终止进程组并等待退出；可重复调用。
func (p *ClaudeCodeProcess) Close() error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		p.cancel()
		_ = p.stdin.Close()
		killClaudeCodeProcess(p.cmd)
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
		}
	})
	return nil
}
