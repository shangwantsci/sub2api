//go:build unit

package ccmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServe_ToolsListAndCall(t *testing.T) {
	var gotName string
	var gotArgs json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		var payload struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		gotName = payload.Name
		gotArgs = payload.Arguments
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"tool result"}],"is_error":false}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	toolsPath := filepath.Join(dir, "tools.json")
	require.NoError(t, os.WriteFile(toolsPath, []byte(`[{"name":"Bash","description":"run","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}}]`), 0o600))

	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Bash","arguments":{"command":"ls"}}}` + "\n")
	var output bytes.Buffer
	err := Serve(context.Background(), input, &output, Config{
		CallbackURL: server.URL,
		Token:       "token",
		ToolsPath:   toolsPath,
	})
	require.NoError(t, err)

	scanner := bufio.NewScanner(&output)
	var responses []map[string]any
	for scanner.Scan() {
		var resp map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &resp))
		responses = append(responses, resp)
	}
	require.Len(t, responses, 3)
	assert.Equal(t, "2024-11-05", responses[0]["result"].(map[string]any)["protocolVersion"])
	tools := responses[1]["result"].(map[string]any)["tools"].([]any)
	require.Len(t, tools, 1)
	assert.Equal(t, "Bash", tools[0].(map[string]any)["name"])
	assert.Equal(t, "Bash", gotName)
	assert.JSONEq(t, `{"command":"ls"}`, string(gotArgs))
	assert.Equal(t, "tool result", responses[2]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"])
}

func TestLoadTools_RejectsMissing(t *testing.T) {
	_, err := LoadTools("")
	require.Error(t, err)
}
