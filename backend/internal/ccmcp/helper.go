// Package ccmcp implements the stdio MCP helper used by Claude Code.
// CC calls tools on this helper; the helper forwards each call to the gateway
// run callback and returns the client's tool_result as MCP content.
package ccmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Config struct {
	CallbackURL string
	Token       string
	ToolsPath   string
	Client      *http.Client
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Run is the CLI entry: parse flags and serve stdio.
func Run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("cc-mcp-helper", flag.ContinueOnError)
	cfg := Config{}
	fs.StringVar(&cfg.CallbackURL, "url", "", "gateway run callback URL")
	fs.StringVar(&cfg.Token, "token", "", "callback bearer token")
	fs.StringVar(&cfg.ToolsPath, "tools", "", "path to client tools JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return Serve(ctx, os.Stdin, os.Stdout, cfg)
}

// Serve handles one MCP stdio session.
func Serve(ctx context.Context, in io.Reader, out io.Writer, cfg Config) error {
	if strings.TrimSpace(cfg.CallbackURL) == "" {
		return errors.New("cc-mcp-helper: callback URL is required")
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 10 * time.Minute}
	}
	tools, err := LoadTools(cfg.ToolsPath)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if len(req.ID) == 0 {
			continue // notification
		}
		result, rpcErr := handle(ctx, cfg, tools, req)
		resp := jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func handle(ctx context.Context, cfg Config, tools []Tool, req jsonRPCRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "sub2api-cc-mcp", "version": "1"},
		}, nil
	case "tools/list":
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid tools/call params"}
		}
		if strings.TrimSpace(params.Name) == "" {
			return nil, &rpcError{Code: -32602, Message: "tool name is required"}
		}
		content, isError, err := callGateway(ctx, cfg, params.Name, params.Arguments)
		if err != nil {
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": err.Error()}},
				"isError": true,
			}, nil
		}
		return map[string]any{"content": content, "isError": isError}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
}

func callGateway(ctx context.Context, cfg Config, name string, arguments json.RawMessage) ([]map[string]any, bool, error) {
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	body, err := json.Marshal(map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	resp, err := cfg.Client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, true, fmt.Errorf("gateway callback HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var result struct {
		Content []map[string]any `json:"content"`
		IsError bool             `json:"is_error"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, true, fmt.Errorf("gateway callback decode: %w", err)
	}
	if len(result.Content) == 0 {
		result.Content = []map[string]any{{"type": "text", "text": ""}}
	}
	return result.Content, result.IsError, nil
}

// LoadTools reads an Anthropic tools array and converts it to MCP tools.
func LoadTools(path string) ([]Tool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("cc-mcp-helper: tools path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cc-mcp-helper: read tools: %w", err)
	}
	var source []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, fmt.Errorf("cc-mcp-helper: decode tools: %w", err)
	}
	out := make([]Tool, 0, len(source))
	for _, tool := range source {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		schema := tool.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, Tool{Name: name, Description: tool.Description, InputSchema: schema})
	}
	return out, nil
}
