package service

import (
	"encoding/json"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const CodeClaudeCodeUnsupportedContent = "CLAUDE_CODE_UNSUPPORTED_CONTENT"

// BuildClaudeCodeTurn 把客户端发来的 Anthropic Messages body 转成 CC 的一轮输入。
//
// 映射规则：
//   - system 的所有 text 块拼成 --system-prompt；CC 自己的 billing/identity 块仍由 CC 生成；
//   - messages 里只允许 text 块；tool_use / tool_result / image 等一律拒绝（CC 信封塞不进去）；
//   - max_tokens 忽略（CC 固定 128000），temperature 由入口校验拒绝；
//   - 模型名映射成 CC 别名，未知模型直接拒绝，避免静默落到默认模型。
func BuildClaudeCodeTurn(body []byte, model string) (ClaudeCodeTurn, error) {
	return BuildClaudeCodeTurnWithTools(body, model, false)
}

// BuildClaudeCodeTurnWithTools 与 BuildClaudeCodeTurn 相同，但 allowTools=true 时
// 允许 tools 进入 MCP 桥（调用方负责准备 --mcp-config）。
func BuildClaudeCodeTurnWithTools(body []byte, model string, allowTools bool) (ClaudeCodeTurn, error) {
	alias, ok := ClaudeCodeModelAlias(model)
	if !ok {
		return ClaudeCodeTurn{}, infraerrors.BadRequest(
			"CLAUDE_CODE_MODEL_UNSUPPORTED",
			fmt.Sprintf("model %q is not supported for Claude Code transport", model),
		)
	}

	var envelope struct {
		System      json.RawMessage   `json:"system"`
		Tools       []json.RawMessage `json:"tools"`
		Temperature *float64          `json:"temperature"`
		Messages    []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ClaudeCodeTurn{}, infraerrors.BadRequest("INVALID_REQUEST_BODY", "failed to parse request body for Claude Code transport")
	}
	if len(envelope.Tools) > 0 && !allowTools {
		return ClaudeCodeTurn{}, infraerrors.BadRequest(CodeClaudeCodeToolsUnsupported,
			"tools are not supported for Claude Code transport; use an API-key group instead")
	}
	if envelope.Temperature != nil {
		return ClaudeCodeTurn{}, infraerrors.BadRequest(CodeClaudeCodeTemperatureUnsupported,
			"temperature is not supported for Claude Code transport; use an API-key group instead")
	}

	system, err := extractClaudeCodeSystem(envelope.System)
	if err != nil {
		return ClaudeCodeTurn{}, err
	}
	if len(envelope.Messages) == 0 {
		return ClaudeCodeTurn{}, infraerrors.BadRequest("INVALID_REQUEST_BODY", "messages is required")
	}

	messages := make([]ClaudeCodeMessage, 0, len(envelope.Messages))
	for _, message := range envelope.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "user" && role != "assistant" {
			return ClaudeCodeTurn{}, infraerrors.BadRequest(CodeClaudeCodeUnsupportedContent,
				fmt.Sprintf("message role %q is not supported for Claude Code transport", message.Role))
		}
		text, parts, err := extractClaudeCodeContent(message.Content, role)
		if err != nil {
			return ClaudeCodeTurn{}, err
		}
		if strings.TrimSpace(text) == "" && len(parts) == 0 {
			continue
		}
		messages = append(messages, ClaudeCodeMessage{Role: role, Text: text, Parts: parts})
	}
	if len(messages) == 0 {
		return ClaudeCodeTurn{}, infraerrors.BadRequest("INVALID_REQUEST_BODY", "messages contains no text content")
	}

	return ClaudeCodeTurn{
		System:   system,
		Model:    alias,
		Messages: messages,
	}, nil
}

func extractClaudeCodeSystem(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text), nil
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", infraerrors.BadRequest(CodeClaudeCodeUnsupportedContent, "system must be a string or an array of text blocks")
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "" && block.Type != "text" {
			return "", infraerrors.BadRequest(CodeClaudeCodeUnsupportedContent,
				fmt.Sprintf("system block type %q is not supported for Claude Code transport", block.Type))
		}
		if strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n")), nil
}

func extractClaudeCodeContent(raw json.RawMessage, role string) (string, []ClaudeCodePart, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text), nil, nil
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
		return "", nil, infraerrors.BadRequest(CodeClaudeCodeUnsupportedContent, "message content must be a string or an array of blocks")
	}

	parts := make([]ClaudeCodePart, 0, len(blocks))
	textParts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "", "text":
			if strings.TrimSpace(block.Text) != "" {
				textParts = append(textParts, block.Text)
				parts = append(parts, ClaudeCodePart{Text: block.Text})
			}
		case "thinking", "redacted_thinking":
			// 历史 thinking 块不参与新的 prompt；CC 会自己生成。
			continue
		case "image":
			if role != "user" {
				return "", nil, infraerrors.BadRequest(CodeClaudeCodeUnsupportedContent,
					"image blocks are only supported in user messages for Claude Code transport")
			}
			image := &ClaudeCodeImage{}
			switch block.Source.Type {
			case "base64":
				if block.Source.Data == "" {
					return "", nil, infraerrors.BadRequest(CodeClaudeCodeUnsupportedContent, "image source data is required")
				}
				if !isSupportedClaudeCodeImageType(block.Source.MediaType) {
					return "", nil, infraerrors.BadRequest(CodeClaudeCodeUnsupportedContent,
						"image media_type must be image/jpeg, image/png, image/gif, or image/webp")
				}
				image.MediaType = block.Source.MediaType
				image.Data = block.Source.Data
			case "url":
				if strings.TrimSpace(block.Source.URL) == "" {
					return "", nil, infraerrors.BadRequest(CodeClaudeCodeUnsupportedContent, "image source url is required")
				}
				image.URL = block.Source.URL
			default:
				return "", nil, infraerrors.BadRequest(CodeClaudeCodeUnsupportedContent,
					"image source type must be base64 or url")
			}
			parts = append(parts, ClaudeCodePart{Image: image})
		default:
			return "", nil, infraerrors.BadRequest(CodeClaudeCodeUnsupportedContent,
				fmt.Sprintf("content block type %q is not supported for Claude Code transport", block.Type))
		}
	}
	text = strings.TrimSpace(strings.Join(textParts, "\n\n"))
	return text, parts, nil
}

func isSupportedClaudeCodeImageType(mediaType string) bool {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}
