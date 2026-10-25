//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeToolCallback_Resolve(t *testing.T) {
	bridge := NewClaudeCodeToolBridge()
	callback := &ClaudeCodeToolCallback{Bridge: bridge, Token: "tok"}
	var pending *ClaudeCodePendingTool
	callback.OnTool = func(tool *ClaudeCodePendingTool) error {
		pending = tool
		return nil
	}

	body := bytes.NewBufferString(`{"name":"Bash","arguments":{"command":"ls"}}`)
	req := httptest.NewRequest(http.MethodPost, "/tools", body)
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		callback.ServeHTTP(rec, req)
		close(done)
	}()

	require.Eventually(t, func() bool { return pending != nil }, time.Second, 10*time.Millisecond)
	require.NoError(t, bridge.Resolve(pending.ID, ClaudeCodeToolResult{Content: []map[string]any{{"type": "text", "text": "ok"}}}))
	<-done
	assert.Equal(t, http.StatusOK, rec.Code)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	assert.Equal(t, "ok", payload["content"].([]any)[0].(map[string]any)["text"])
}

func TestClaudeCodeToolCallback_RejectsBadToken(t *testing.T) {
	callback := &ClaudeCodeToolCallback{Bridge: NewClaudeCodeToolBridge(), Token: "tok"}
	req := httptest.NewRequest(http.MethodPost, "/tools", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	callback.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestClaudeCodeToolCallback_CancelContext(t *testing.T) {
	callback := &ClaudeCodeToolCallback{Bridge: NewClaudeCodeToolBridge()}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/tools", bytes.NewBufferString(`{"name":"Bash"}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	callback.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusGatewayTimeout, rec.Code)
}
