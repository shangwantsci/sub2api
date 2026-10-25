//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClaudeCodeRunner struct {
	onEvent func(json.RawMessage) error
	text    string
}

func (r *fakeClaudeCodeRunner) Run(context.Context, service.ClaudeCodeTurn) (*service.ClaudeCodeResult, error) {
	if r.onEvent != nil {
		_ = r.onEvent(json.RawMessage(`{"type":"message_start"}`))
		_ = r.onEvent(json.RawMessage(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pong"}}`))
		_ = r.onEvent(json.RawMessage(`{"type":"message_stop"}`))
	}
	return &service.ClaudeCodeResult{
		Text:  r.text,
		Usage: json.RawMessage(`{"input_tokens":2,"cache_creation_input_tokens":19205,"output_tokens":4}`),
	}, nil
}

func newClaudeCodeHandlerForTest(t *testing.T) (*GatewayHandler, *service.ClaudeCodeRuntime) {
	t.Helper()
	return newClaudeCodeHandlerForTestWithFlag(t, true)
}

func newClaudeCodeHandlerForTestWithFlag(t *testing.T, enabled bool) (*GatewayHandler, *service.ClaudeCodeRuntime) {
	t.Helper()
	cfg := &config.Config{}
	cfg.ProcessPool.DataDir = t.TempDir()
	cfg.ProcessPool.CCTransportEnabled = enabled
	cfg.ProcessPool.ClaudeBinary = "/bin/true"
	cfg.ProcessPool.ForwarderJS = "/tmp/forwarder.js"

	processPool := service.NewProcessPool(cfg)
	runtime := service.NewClaudeCodeRuntime(cfg, nil, nil, processPool, nil)
	runtime.Pool().EnsureFresh = nil
	runtime.Pool().NewRunner = func(_ *service.Account, onEvent func(json.RawMessage) error) (service.ClaudeCodeTurnRunner, error) {
		return &fakeClaudeCodeRunner{onEvent: onEvent, text: "pong"}, nil
	}

	gw := service.NewGatewayService(
		nil, // accountRepo
		nil, // groupRepo
		nil, // usageLogRepo
		nil, // usageBillingRepo
		nil, // userRepo
		nil, // userSubRepo
		nil, // userGroupRateRepo
		nil, // cache
		cfg,
		nil, // schedulerSnapshot
		nil, // concurrencyService
		nil, // billingService
		nil, // rateLimitService
		nil, // billingCacheService
		nil, // httpUpstream
		nil, // deferredService
		nil, // claudeTokenProvider
		nil, // sessionLimitCache
		nil, // rpmCache
		nil, // digestStore
		nil, // settingService
		nil, // tlsFPProfileService
		nil, // channelService
		nil, // resolver
		nil, // balanceNotifyService
		nil, // userPlatformQuotaRepo
		runtime,
	)
	return &GatewayHandler{gatewayService: gw}, runtime
}

func claudeCodeTestAccount() *service.Account {
	return &service.Account{ID: 7001, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth}
}

func TestGatewayHandler_IsClaudeCodeTransport(t *testing.T) {
	h, _ := newClaudeCodeHandlerForTest(t)
	assert.True(t, h.isClaudeCodeTransport(claudeCodeTestAccount()))
	assert.False(t, h.isClaudeCodeTransport(&service.Account{ID: 1, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey}))
}

func TestGatewayHandler_ServeClaudeCodeMessages_NonStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newClaudeCodeHandlerForTest(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}"))

	body := []byte(`{"system":"be brief","messages":[{"role":"user","content":"hi"}]}`)
	streamStarted := false
	result, _, err := h.serveClaudeCodeMessages(c, claudeCodeTestAccount(), body, "claude-opus-5-5", "session-1", false, true, func() {}, &streamStarted)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 200, w.Code)
	assert.False(t, streamStarted)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	content := response["content"].([]any)
	first := content[0].(map[string]any)
	assert.Equal(t, "pong", first["text"])

	usage := response["usage"].(map[string]any)
	assert.Greater(t, usage["input_tokens"].(float64), 0.0)
	assert.Equal(t, 4.0, usage["output_tokens"])
	assert.Zero(t, result.Usage.CacheCreationInputTokens, "CC prefix must not be billed")
}

func TestGatewayHandler_ServeClaudeCodeMessages_Stream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newClaudeCodeHandlerForTest(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}"))

	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	streamStarted := false
	_, _, err := h.serveClaudeCodeMessages(c, claudeCodeTestAccount(), body, "claude-opus-5-5", "session-1", true, true, func() {}, &streamStarted)
	require.NoError(t, err)
	assert.True(t, streamStarted)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/event-stream")
	assert.Contains(t, w.Body.String(), "event: message_start")
	assert.Contains(t, w.Body.String(), `"type":"content_block_delta"`)
	assert.Contains(t, w.Body.String(), "event: message_stop")
}

func TestClassifyClaudeCodeHandlerError(t *testing.T) {
	badRequest, failover := classifyClaudeCodeHandlerError(&service.ClaudeCodeError{
		Kind:    service.ClaudeCodeFailureRequest,
		Message: "tools are not supported",
	})
	assert.Equal(t, "tools are not supported", badRequest)
	assert.Nil(t, failover)

	badRequest, failover = classifyClaudeCodeHandlerError(&service.ClaudeCodeError{
		Kind:       service.ClaudeCodeFailureRateLimit,
		StatusCode: 429,
		Message:    "rate limited",
		Retryable:  true,
	})
	assert.Empty(t, badRequest)
	require.NotNil(t, failover)
	assert.Equal(t, 429, failover.StatusCode)

	badRequest, failover = classifyClaudeCodeHandlerError(errors.New("plain error"))
	assert.Empty(t, badRequest)
	assert.Nil(t, failover)
}

func TestClaudeCodeEndpointRejection(t *testing.T) {
	h, _ := newClaudeCodeHandlerForTest(t)
	cc := claudeCodeTestAccount()
	apiKey := &service.Account{ID: 7002, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey}

	status, _, _, reject := h.claudeCodeEndpointRejection(apiKey, "/v1/chat/completions")
	assert.False(t, reject)
	assert.Zero(t, status)

	status, _, _, reject = h.claudeCodeEndpointRejection(cc, "/v1/messages")
	assert.False(t, reject, "enabled runtime must allow /v1/messages")
	assert.Zero(t, status)

	status, _, message, reject := h.claudeCodeEndpointRejection(cc, "/v1/chat/completions")
	assert.True(t, reject)
	assert.Equal(t, 400, status)
	assert.Contains(t, message, "/v1/messages")

	disabled, _ := newClaudeCodeHandlerForTestWithFlag(t, false)
	status, _, message, reject = disabled.claudeCodeEndpointRejection(cc, "/v1/messages")
	assert.True(t, reject, "disabled CC transport must not fall back to the Go path")
	assert.Equal(t, 503, status)
	assert.Contains(t, message, "disabled")
}
