package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Claude Code 进程池的执行原语。
//
// 一个进程只服务一段对话：官方 stream-json 协议没有「重置会话」控制消息，
// 所以互不相关的 API 请求必须各起一个进程。账号级并发靠多进程，
// token 刷新由上层（Go）在 spawn 前用账号级锁预刷新，避免 refresh token 轮换竞争。

const (
	claudeCodeMaxLineBytes = 16 << 20 // stream-json 单行上限
	claudeCodeStderrBytes  = 64 << 10
	defaultClaudeCodeRun   = 90 * time.Second
)

// ClaudeCodePart 是消息里的一个内容块：text 或 image。
type ClaudeCodePart struct {
	Text  string
	Image *ClaudeCodeImage
}

// ClaudeCodeImage 是 Anthropic image block 的 CC 输入形态。
type ClaudeCodeImage struct {
	MediaType string
	Data      string // base64
	URL       string
}

// ClaudeCodeMessage 是一条对话消息。Text 是文本摘要（计费/日志用），
// Parts 保留原始 text/image 顺序；Parts 为空时回退到 Text。
type ClaudeCodeMessage struct {
	Role  string
	Text  string
	Parts []ClaudeCodePart
}

// ClaudeCodeTurn 描述一次要交给 Claude Code 的请求。
type ClaudeCodeTurn struct {
	System          string
	Model           string
	Messages        []ClaudeCodeMessage
	DisallowedTools []string
	// ResumeSessionID 非空时用 --resume 接上已有 CC session，只发送新的 user turn。
	ResumeSessionID string
	// MCPConfigPath 非空时把客户端 tools 通过 MCP helper 暴露给 CC。
	MCPConfigPath string
	// AllowedTools 是 --tools 的值；为空表示禁用全部内置工具。
	AllowedTools []string
}

// ClaudeCodeResult 是 result 事件里我们关心的字段。
type ClaudeCodeResult struct {
	Text         string
	PendingTool  *ClaudeCodePendingTool
	SessionID    string
	Usage        json.RawMessage
	TotalCostUSD float64
	DurationMS   int64
	Events       []json.RawMessage
}

// ClaudeCodeRunner 启动官方 Claude Code 二进制，按 stream-json 协议喂一条消息、收最终结果。
type ClaudeCodeRunner struct {
	BinaryPath string
	HomeDir    string
	BaseURL    string // 通常指向该账号的本地 forwarder（https://127.0.0.1:<port>）
	ProxyURL   string // 可选：让 CC 的遥测/旁路流量也走账号代理
	Timeout    time.Duration
	ExtraEnv   []string

	// OnEvent 在收到每个 stream_event 时同步调用（网关用它边读边写 SSE）。
	// 返回错误会终止本次运行并杀掉子进程（通常是客户端已断开）。
	OnEvent func(json.RawMessage) error
}

// Run 执行一轮，返回 result 事件。调用方负责账号级并发与 token 预刷新。
func (r *ClaudeCodeRunner) Run(ctx context.Context, turn ClaudeCodeTurn) (*ClaudeCodeResult, error) {
	if r == nil || strings.TrimSpace(r.BinaryPath) == "" {
		return nil, errors.New("claude code runner: binary path is empty")
	}
	if strings.TrimSpace(r.HomeDir) == "" {
		return nil, errors.New("claude code runner: home dir is empty")
	}
	prompt, err := flattenClaudeCodePrompt(turn.Messages)
	if err != nil {
		return nil, err
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultClaudeCodeRun
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

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
		// 默认禁用全部内置工具；有客户端 tools 时只放行 MCP 工具。
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
	if strings.TrimSpace(turn.ResumeSessionID) != "" {
		args = append(args, "--resume", strings.TrimSpace(turn.ResumeSessionID))
	}
	// 客户端没带 system 时不传 --system-prompt：CC 会使用自己的默认主提示词（产品决定保留）。
	// 这段注入不计入客户账单，见 NormalizeClaudeCodeUsage。
	if strings.TrimSpace(turn.System) != "" {
		args = append(args, "--system-prompt", turn.System)
	}
	if tools := normalizeToolNames(turn.DisallowedTools); len(tools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(tools, ","))
	}

	cmd := exec.CommandContext(runCtx, r.BinaryPath, args...)
	cmd.Dir = r.HomeDir
	cmd.Env = r.environment()
	configureClaudeCodeProcess(cmd)
	cmd.Cancel = func() error {
		killClaudeCodeProcess(cmd)
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("claude code: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude code: stdout pipe: %w", err)
	}
	stderr := &limitedBuffer{limit: claudeCodeStderrBytes}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude code: start: %w", err)
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
		killClaudeCodeProcess(cmd)
		return nil, fmt.Errorf("claude code: encode input: %w", err)
	}
	if _, err := stdin.Write(append(input, '\n')); err != nil {
		killClaudeCodeProcess(cmd)
		return nil, fmt.Errorf("claude code: write input: %w", err)
	}
	_ = stdin.Close()

	result, streamErr := readClaudeCodeStream(stdout, r.OnEvent)
	if streamErr != nil {
		// 客户端断开或流解析失败时不要等 CC 自己退出（连同它拉起的子进程一起杀）。
		killClaudeCodeProcess(cmd)
	}
	waitErr := cmd.Wait()

	if runCtx.Err() != nil {
		return nil, fmt.Errorf("claude code: timed out after %s: %w", timeout, runCtx.Err())
	}
	if streamErr != nil {
		return nil, fmt.Errorf("claude code: read stream: %w (stderr: %s)", streamErr, stderr.String())
	}
	if waitErr != nil {
		return nil, fmt.Errorf("claude code: exit: %w (stderr: %s)", waitErr, stderr.String())
	}
	return result, nil
}

// flattenClaudeCodePrompt 把无状态 API 的完整消息历史压成一条用户消息。
// 单轮用户消息原样返回；多轮用 [role] 前缀保留说话人，MVP 不支持 tool_use / 图片。
func flattenClaudeCodePrompt(messages []ClaudeCodeMessage) (string, error) {
	if len(messages) == 0 {
		return "", errors.New("claude code: empty messages")
	}
	if len(messages) == 1 {
		role := strings.ToLower(strings.TrimSpace(messages[0].Role))
		if role != "" && role != "user" {
			return "", fmt.Errorf("claude code: single message must be from user, got %q", role)
		}
		text := strings.TrimSpace(messages[0].Text)
		if text == "" && len(messages[0].Parts) == 0 {
			return "", errors.New("claude code: empty message text")
		}
		if text == "" {
			return "[image]", nil
		}
		return text, nil
	}

	var b strings.Builder
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "user" && role != "assistant" {
			return "", fmt.Errorf("claude code: unsupported message role %q", role)
		}
		text := strings.TrimSpace(message.Text)
		if text == "" && len(message.Parts) == 0 {
			continue
		}
		if text == "" {
			text = "[image]"
		}
		fmt.Fprintf(&b, "[%s]\n%s\n\n", role, text)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", errors.New("claude code: empty message text")
	}
	return out, nil
}

// claudeCodeInputContent 构造 CC stream-json 的 content：
// 无图片时保持原来的单 text block；有图片时按 text + image 块发送。
func claudeCodeInputContent(messages []ClaudeCodeMessage, prompt string) any {
	images := make([]ClaudeCodeImage, 0)
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Image != nil {
				images = append(images, *part.Image)
			}
		}
	}
	if len(images) == 0 {
		return []map[string]string{{"type": "text", "text": prompt}}
	}
	blocks := make([]map[string]any, 0, 1+len(images))
	blocks = append(blocks, map[string]any{"type": "text", "text": prompt})
	for _, image := range images {
		source := map[string]any{"type": "base64", "media_type": image.MediaType, "data": image.Data}
		if image.URL != "" {
			source = map[string]any{"type": "url", "url": image.URL}
		}
		blocks = append(blocks, map[string]any{"type": "image", "source": source})
	}
	return blocks
}

func normalizeToolNames(names []string) []string {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func (r *ClaudeCodeRunner) environment() []string {
	overrides := map[string]string{
		"HOME":                         r.HomeDir,
		"NODE_TLS_REJECT_UNAUTHORIZED": "0",
		"DISABLE_AUTOUPDATER":          "1",
		"NO_PROXY":                     "127.0.0.1,localhost",
	}
	if r.BaseURL != "" {
		overrides["ANTHROPIC_BASE_URL"] = r.BaseURL
	}
	if r.ProxyURL != "" {
		overrides["HTTPS_PROXY"] = r.ProxyURL
		overrides["HTTP_PROXY"] = r.ProxyURL
	}

	// 这些变量必须由本进程显式决定，不能从网关进程继承：
	// 继承 ANTHROPIC_API_KEY / CLAUDE_CODE_OAUTH_TOKEN 会让 CC 绕开账号凭据；
	// 继承代理变量会让不同账号共用同一个出口。
	drop := map[string]string{
		"HOME":                         "",
		"ANTHROPIC_BASE_URL":           "",
		"ANTHROPIC_API_KEY":            "",
		"ANTHROPIC_AUTH_TOKEN":         "",
		"CLAUDE_CODE_OAUTH_TOKEN":      "",
		"NODE_TLS_REJECT_UNAUTHORIZED": "",
		"DISABLE_AUTOUPDATER":          "",
		"NO_PROXY":                     "",
		"HTTPS_PROXY":                  "",
		"HTTP_PROXY":                   "",
	}
	for key := range overrides {
		drop[key] = ""
	}

	out := make([]string, 0, len(os.Environ())+len(overrides)+len(r.ExtraEnv))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, removed := drop[key]; removed {
			continue
		}
		out = append(out, entry)
	}
	for key, value := range overrides {
		out = append(out, key+"="+value)
	}
	out = append(out, r.ExtraEnv...)
	return out
}

// readClaudeCodeStream 逐行读 stream-json，直到 result 或 EOF。
// onEvent 非空时，每个 stream_event 都会同步回调（用于 SSE 透传）。
func readClaudeCodeStream(reader io.Reader, onEvent func(json.RawMessage) error) (*ClaudeCodeResult, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), claudeCodeMaxLineBytes)

	var result *ClaudeCodeResult
	var assistantText strings.Builder
	seenResult := false

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
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
			continue // CC 可能混入非 JSON 日志行
		}

		switch envelope.Type {
		case "stream_event":
			if len(envelope.Event) > 0 {
				if result == nil {
					result = &ClaudeCodeResult{}
				}
				result.Events = append(result.Events, envelope.Event)
				if onEvent != nil {
					if err := onEvent(envelope.Event); err != nil {
						return nil, err
					}
				}
			}
		case "assistant":
			for _, block := range envelope.Message.Content {
				if block.Type == "text" {
					assistantText.WriteString(block.Text)
				}
			}
		case "result":
			if envelope.IsError {
				return nil, fmt.Errorf("claude code reported error: %s", strings.TrimSpace(envelope.Result))
			}
			seenResult = true
			if result == nil {
				result = &ClaudeCodeResult{}
			}
			result.Text = strings.TrimSpace(envelope.Result)
			result.SessionID = envelope.SessionID
			result.Usage = envelope.Usage
			result.TotalCostUSD = envelope.TotalCostUSD
			result.DurationMS = envelope.DurationMS
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if result == nil || !seenResult {
		return nil, errors.New("no result event")
	}
	if result.Text == "" {
		result.Text = strings.TrimSpace(assistantText.String())
	}
	return result, nil
}

// limitedBuffer 保留前 limit 字节，避免 stderr 撑爆内存。
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if remaining := b.limit - b.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.buf.Write(p)
	}
	return original, nil
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}
