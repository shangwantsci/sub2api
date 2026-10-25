package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ClaudeCodeClientToolResult 是客户端回填的一次工具执行结果。
type ClaudeCodeClientToolResult struct {
	ToolUseID string
	Content   []map[string]any
	IsError   bool
}

type claudeCodeToolResultBlock struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// ExtractClaudeCodeToolResults 从请求体最后一条 user message 中提取 tool_result。
// 只处理 content block 形态；文本形态不包含 tool_result。
func ExtractClaudeCodeToolResults(body []byte) ([]ClaudeCodeClientToolResult, error) {
	var payload struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("claude code: invalid request body")
	}
	if len(payload.Messages) == 0 {
		return nil, nil
	}
	last := payload.Messages[len(payload.Messages)-1]
	if !strings.EqualFold(strings.TrimSpace(last.Role), "user") {
		return nil, nil
	}
	var blocks []claudeCodeToolResultBlock
	if err := json.Unmarshal(last.Content, &blocks); err != nil {
		return nil, nil
	}
	results := make([]ClaudeCodeClientToolResult, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "tool_result" {
			continue
		}
		if strings.TrimSpace(block.ToolUseID) == "" {
			return nil, errors.New("claude code: tool_result.tool_use_id is required")
		}
		content, err := normalizeClaudeCodeToolResultContent(block.Content)
		if err != nil {
			return nil, err
		}
		results = append(results, ClaudeCodeClientToolResult{
			ToolUseID: block.ToolUseID,
			Content:   content,
			IsError:   block.IsError,
		})
	}
	return results, nil
}

func normalizeClaudeCodeToolResultContent(raw json.RawMessage) ([]map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []map[string]any{{"type": "text", "text": text}}, nil
	}
	var blocks []struct {
		Type   string `json:"type"`
		Text   string `json:"text"`
		Source struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
			URL       string `json:"url"`
		} `json:"source"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("claude code: invalid tool_result content: %w", err)
	}
	out := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			out = append(out, map[string]any{"type": "text", "text": block.Text})
		case "image":
			if block.Source.Type == "base64" {
				out = append(out, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type": "base64", "media_type": block.Source.MediaType, "data": block.Source.Data,
					},
				})
			} else if block.Source.Type == "url" {
				out = append(out, map[string]any{
					"type":   "image",
					"source": map[string]any{"type": "url", "url": block.Source.URL},
				})
			}
		}
	}
	return out, nil
}
