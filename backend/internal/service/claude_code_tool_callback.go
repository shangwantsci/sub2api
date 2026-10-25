package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// ClaudeCodeToolCallback 是 MCP helper 调用的 HTTP 入口。
// 它把工具调用登记到 bridge，通知 run（通常发 tool_use SSE），然后阻塞等客户端结果。
type ClaudeCodeToolCallback struct {
	Bridge *ClaudeCodeToolBridge
	Token  string
	OnTool func(*ClaudeCodePendingTool) error
}

func (c *ClaudeCodeToolCallback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if c == nil || c.Bridge == nil {
		http.Error(w, "tool bridge unavailable", http.StatusServiceUnavailable)
		return
	}
	if c.Token != "" {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(c.Token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var payload struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || strings.TrimSpace(payload.Name) == "" {
		http.Error(w, "invalid tool call", http.StatusBadRequest)
		return
	}

	tool := c.Bridge.Start(payload.Name, payload.Arguments)
	if c.OnTool != nil {
		if err := c.OnTool(tool); err != nil {
			c.Bridge.Cancel(tool.ID)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	result, err := c.Bridge.Wait(r.Context(), tool)
	if err != nil {
		status := http.StatusGatewayTimeout
		if errors.Is(err, context.Canceled) {
			status = http.StatusRequestTimeout
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"content":  result.Content,
		"is_error": result.IsError,
	})
}
