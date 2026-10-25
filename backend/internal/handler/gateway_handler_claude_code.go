package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// isClaudeCodeTransport 报告这次请求是否应交给 CC 进程池。
// 运行时未启用（默认）时恒为 false，请求继续走原来的 Go 路径。
func (h *GatewayHandler) isClaudeCodeTransport(account *service.Account) bool {
	runtime := h.gatewayService.ClaudeCodeRuntime()
	return runtime != nil && runtime.Enabled() && account != nil && account.Transport() == service.TransportClaudeCode
}

// serveClaudeCodeMessages 执行一次 CC 请求并把结果写给客户端。
//
// 流式：CC 的 stream_event.event 就是原始 Anthropic SSE 事件，按 event/data 原样转发；
// 非流式：用 result 文本拼一个 Messages API 响应。
// usage 语义见 service.NormalizeClaudeCodeUsage：只算客户自己的输入 + 输出。
func (h *GatewayHandler) serveClaudeCodeMessages(
	c *gin.Context,
	account *service.Account,
	body []byte,
	model string,
	sessionKey string,
	stream bool,
	reserved bool,
	release func(),
	streamStarted *bool,
) (*service.ForwardResult, bool, error) {
	runtime := h.gatewayService.ClaudeCodeRuntime()
	if runtime == nil || !runtime.Enabled() {
		return nil, false, errors.New("claude code transport is not enabled")
	}

	start := time.Now()
	wroteHeaders := false
	onEvent := func(event json.RawMessage) error {
		if !stream {
			return nil
		}
		if !wroteHeaders {
			writeClaudeCodeSSEHeaders(c)
			wroteHeaders = true
			if streamStarted != nil {
				*streamStarted = true
			}
		}
		eventName := "message"
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(event, &envelope); err == nil && envelope.Type != "" {
			eventName = envelope.Type
		}
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", eventName, event); err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}

	forwarded, slotOwned, err := runtime.RunRequestWithSessionManaged(c.Request.Context(), account, body, model, sessionKey, onEvent, release, reserved)
	if err != nil {
		return nil, slotOwned, err
	}

	// 非持久路径：把 scheduler 预占的槽转交给 session run；handler 不再释放。
	if !slotOwned && reserved && sessionKey != "" && release != nil && runtime.RunManager() != nil {
		runtime.BindSessionSlot(sessionKey, account.ID, release)
		slotOwned = true
	}

	usage := forwarded.Usage
	if forwarded.Result != nil && forwarded.Result.PendingTool != nil {
		tool := forwarded.Result.PendingTool
		if stream {
			if !wroteHeaders {
				writeClaudeCodeSSEHeaders(c)
				wroteHeaders = true
				if streamStarted != nil {
					*streamStarted = true
				}
			}
			writeClaudeCodeToolUseEvents(c, tool)
			return &service.ForwardResult{RequestID: claudeCodeRequestID(c), Model: model, Stream: true, Duration: time.Since(start)}, slotOwned, nil
		}
		c.JSON(http.StatusOK, map[string]any{
			"id": claudeCodeRequestID(c), "type": "message", "role": "assistant",
			"content": []map[string]any{{"type": "tool_use", "id": tool.ID, "name": tool.Name, "input": json.RawMessage(tool.Arguments)}},
			"model":   model, "stop_reason": "tool_use", "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens},
		})
		return &service.ForwardResult{RequestID: claudeCodeRequestID(c), Model: model, Duration: time.Since(start)}, slotOwned, nil
	}
	result := &service.ForwardResult{
		RequestID: claudeCodeRequestID(c),
		Usage: service.ClaudeUsage{
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
		},
		Model:    model,
		Stream:   stream,
		Duration: time.Since(start),
	}

	if stream {
		if !wroteHeaders {
			return nil, false, errors.New("claude code produced no stream events")
		}
		return result, slotOwned, nil
	}

	text := forwarded.Result.Text
	c.JSON(http.StatusOK, map[string]any{
		"id":            result.RequestID,
		"type":          "message",
		"role":          "assistant",
		"content":       []map[string]any{{"type": "text", "text": text}},
		"model":         model,
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  usage.InputTokens,
			"output_tokens": usage.OutputTokens,
		},
	})
	return result, slotOwned, nil
}

// classifyClaudeCodeHandlerError 把 CC 错误分成“直接回 400”和“交给换号循环”两类。
func classifyClaudeCodeHandlerError(err error) (badRequestMessage string, failoverErr *service.UpstreamFailoverError) {
	var ccErr *service.ClaudeCodeError
	if !errors.As(err, &ccErr) {
		return "", nil
	}
	if ccErr.Kind == service.ClaudeCodeFailureRequest {
		return ccErr.Message, nil
	}
	return "", ccErr.AsFailoverError()
}

// claudeCodeEndpointRejection 统一处理“OAuth 账号走 CC”时的入口准入：
//   - /v1/messages：CC 开启则放行；关闭则 503（绝不回退 Go 伪装）
//   - 其它端点：CC 不支持，明确 400
func (h *GatewayHandler) claudeCodeEndpointRejection(account *service.Account, endpoint string) (status int, code, message string, reject bool) {
	if account == nil || account.Transport() != service.TransportClaudeCode {
		return 0, "", "", false
	}
	if endpoint == "/v1/messages" {
		if h.isClaudeCodeTransport(account) {
			return 0, "", "", false
		}
		return http.StatusServiceUnavailable, "api_error",
			"OAuth accounts require the Claude Code transport, but it is currently disabled. Enable process_pool.cc_transport_enabled or use an API-key group.", true
	}
	return http.StatusBadRequest, "invalid_request_error",
		"OAuth accounts use the Claude Code transport and only support /v1/messages. Use an API-key group for this endpoint.", true
}

func writeClaudeCodeSSEHeaders(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
}

func claudeCodeRequestID(c *gin.Context) string {
	if id := c.GetString("request_id"); id != "" {
		return id
	}
	return fmt.Sprintf("cc_%d", time.Now().UnixNano())
}

// ClaudeCodeToolCallback 是 MCP helper 的 HTTP 入口；token 定位 run。
func (h *GatewayHandler) ClaudeCodeToolCallback(c *gin.Context) {
	runtime := h.gatewayService.ClaudeCodeRuntime()
	if runtime == nil || !runtime.Enabled() || runtime.RunManager() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "claude code transport is not enabled"})
		return
	}
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	var payload struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || strings.TrimSpace(payload.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tool call"})
		return
	}
	toolWait := 5 * time.Minute
	if h.cfg != nil && h.cfg.ProcessPool.CCToolWaitSeconds > 0 {
		toolWait = time.Duration(h.cfg.ProcessPool.CCToolWaitSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), toolWait)
	defer cancel()
	result, err := runtime.RunManager().HandleToolCall(ctx, token, payload.Name, payload.Arguments)
	if err != nil {
		runtime.RecordEvent("tool_timeout", "", 0, err.Error())
		runtime.RunManager().ReleaseByToolToken(token)
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": result.Content, "is_error": result.IsError})
}

func writeClaudeCodeSSEEvent(c *gin.Context, event string, payload any) {
	raw, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, raw)
	c.Writer.Flush()
}

func writeClaudeCodeToolUseEvents(c *gin.Context, tool *service.ClaudeCodePendingTool) {
	if tool == nil {
		return
	}
	args := string(tool.Arguments)
	if args == "" {
		args = "{}"
	}
	writeClaudeCodeSSEEvent(c, "content_block_start", map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "tool_use", "id": tool.ID, "name": tool.Name, "input": map[string]any{}},
	})
	writeClaudeCodeSSEEvent(c, "content_block_delta", map[string]any{
		"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": args},
	})
	writeClaudeCodeSSEEvent(c, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	writeClaudeCodeSSEEvent(c, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}})
	writeClaudeCodeSSEEvent(c, "message_stop", map[string]any{"type": "message_stop"})
}

// serveClaudeCodeToolContinue 处理客户端回填 tool_result 的请求。
// 返回 handled=false 时交回普通请求路径。
func (h *GatewayHandler) serveClaudeCodeToolContinue(
	c *gin.Context,
	apiKey *service.APIKey,
	sessionKey string,
	results []service.ClaudeCodeClientToolResult,
	body []byte,
	model string,
	subscription *service.UserSubscription,
	channelMapping service.ChannelMappingResult,
	stream bool,
	streamStarted *bool,
) bool {
	runtime := h.gatewayService.ClaudeCodeRuntime()
	if runtime == nil || !runtime.Enabled() || runtime.RunManager() == nil || sessionKey == "" || len(results) == 0 {
		return false
	}
	start := time.Now()
	accountID, ok := runtime.RunManager().Lookup(sessionKey)
	if !ok {
		return false
	}
	account, err := h.gatewayService.GetAccountByID(c.Request.Context(), accountID)
	if err != nil || account == nil {
		h.errorResponse(c, http.StatusConflict, "session_expired", "Claude Code session is no longer available")
		return true
	}
	for _, result := range results {
		if err := runtime.RunManager().ResolveTool(sessionKey, result.ToolUseID, service.ClaudeCodeToolResult{Content: result.Content, IsError: result.IsError}); err != nil {
			_ = account
			h.errorResponse(c, http.StatusConflict, "session_expired", "Tool result does not match an active Claude Code run")
			return true
		}
	}

	wroteHeaders := false
	onEvent := func(event json.RawMessage) error {
		if !stream {
			return nil
		}
		if !wroteHeaders {
			writeClaudeCodeSSEHeaders(c)
			wroteHeaders = true
			if streamStarted != nil {
				*streamStarted = true
			}
		}
		eventName := "message"
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(event, &envelope); err == nil && envelope.Type != "" {
			eventName = envelope.Type
		}
		_, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", eventName, event)
		if err == nil {
			c.Writer.Flush()
		}
		return err
	}
	forwarded, err := runtime.ContinueToolSession(c.Request.Context(), sessionKey, onEvent)
	if err != nil {
		h.handleStreamingAwareError(c, http.StatusBadGateway, "api_error", err.Error(), *streamStarted)
		return true
	}
	if forwarded.Result != nil && forwarded.Result.PendingTool != nil {
		if !wroteHeaders {
			writeClaudeCodeSSEHeaders(c)
			wroteHeaders = true
			if streamStarted != nil {
				*streamStarted = true
			}
		}
		writeClaudeCodeToolUseEvents(c, forwarded.Result.PendingTool)
		return true
	}
	forwardResult := &service.ForwardResult{
		RequestID: claudeCodeRequestID(c),
		Usage: service.ClaudeUsage{
			InputTokens:  forwarded.Usage.InputTokens,
			OutputTokens: forwarded.Usage.OutputTokens,
		},
		Model:         model,
		UpstreamModel: channelMapping.MappedModel,
		Stream:        stream,
		Duration:      time.Since(start),
	}
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	requestPayloadHash := service.HashUsageRequestPayload(body)
	userAgent := c.GetHeader("User-Agent")
	clientIP := c.ClientIP()
	h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
			Result:             forwardResult,
			APIKey:             apiKey,
			User:               apiKey.User,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    GetInboundEndpoint(c),
			UpstreamEndpoint:   GetUpstreamEndpoint(c, account.Platform),
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			RequestPayloadHash: requestPayloadHash,
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			ChannelUsageFields: channelMapping.ToUsageFields(model, channelMapping.MappedModel),
		}); err != nil {
			logger.L().Error("gateway.claude_code_tool_usage_record_failed", zap.Error(err))
		}
	})
	if stream {
		if !wroteHeaders {
			h.errorResponse(c, http.StatusBadGateway, "api_error", "Claude Code produced no stream events")
		}
		return true
	}
	c.JSON(http.StatusOK, map[string]any{
		"id": claudeCodeRequestID(c), "type": "message", "role": "assistant",
		"content": []map[string]any{{"type": "text", "text": forwarded.Result.Text}},
		"model":   "", "stop_reason": "end_turn", "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": forwarded.Usage.InputTokens, "output_tokens": forwarded.Usage.OutputTokens},
	})
	return true
}
