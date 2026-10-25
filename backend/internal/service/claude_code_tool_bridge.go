package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// ClaudeCodeToolResult 是客户端 tool_result 回填给 MCP helper 的结果。
type ClaudeCodeToolResult struct {
	Content []map[string]any
	IsError bool
}

// ClaudeCodePendingTool 是一次等待客户端结果的工具调用。
type ClaudeCodePendingTool struct {
	ID        string
	Name      string
	Arguments json.RawMessage
	result    chan ClaudeCodeToolResult
}

// ClaudeCodeToolBridge 管理 run 内 tool_use_id → 客户端结果的等待关系。
type ClaudeCodeToolBridge struct {
	mu      sync.Mutex
	pending map[string]*ClaudeCodePendingTool
	seq     atomic.Uint64
}

func NewClaudeCodeToolBridge() *ClaudeCodeToolBridge {
	return &ClaudeCodeToolBridge{pending: make(map[string]*ClaudeCodePendingTool)}
}

// Start 注册一次工具调用并返回 pending；调用方负责把 ID 发给客户端。
func (b *ClaudeCodeToolBridge) Start(name string, arguments json.RawMessage) *ClaudeCodePendingTool {
	id := fmt.Sprintf("toolu_cc_%d", b.seq.Add(1))
	tool := &ClaudeCodePendingTool{
		ID:        id,
		Name:      name,
		Arguments: arguments,
		result:    make(chan ClaudeCodeToolResult, 1),
	}
	b.mu.Lock()
	b.pending[id] = tool
	b.mu.Unlock()
	return tool
}

// Resolve 把客户端结果投递给等待中的工具调用。
func (b *ClaudeCodeToolBridge) Resolve(id string, result ClaudeCodeToolResult) error {
	b.mu.Lock()
	tool := b.pending[id]
	delete(b.pending, id)
	b.mu.Unlock()
	if tool == nil {
		return errors.New("claude code tool bridge: unknown or expired tool_use_id")
	}
	tool.result <- result
	return nil
}

// Wait 等待客户端结果；超时/取消时移除 pending。
func (b *ClaudeCodeToolBridge) Wait(ctx context.Context, tool *ClaudeCodePendingTool) (ClaudeCodeToolResult, error) {
	if tool == nil {
		return ClaudeCodeToolResult{}, errors.New("claude code tool bridge: nil tool")
	}
	select {
	case result := <-tool.result:
		return result, nil
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, tool.ID)
		b.mu.Unlock()
		return ClaudeCodeToolResult{}, ctx.Err()
	}
}

// PendingCount 返回当前等待客户端结果的工具数。
func (b *ClaudeCodeToolBridge) PendingCount() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

// Cancel 丢弃一个 pending。
func (b *ClaudeCodeToolBridge) Cancel(id string) {
	b.mu.Lock()
	delete(b.pending, id)
	b.mu.Unlock()
}

// BuildClaudeCodeMCPConfig 生成 CC --mcp-config 的 JSON。
// executable 通常是当前 server 二进制，args 里带 cc-mcp-helper 子命令。
func BuildClaudeCodeMCPConfig(executable, callbackURL, token, toolsPath string) ([]byte, error) {
	if executable == "" || callbackURL == "" || toolsPath == "" {
		return nil, errors.New("claude code mcp config: executable, callback URL and tools path are required")
	}
	args := []string{"cc-mcp-helper", "--url", callbackURL, "--tools", toolsPath}
	if token != "" {
		args = append(args, "--token", token)
	}
	return json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"sub2api": map[string]any{
				"command": executable,
				"args":    args,
			},
		},
	})
}
