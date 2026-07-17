package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type claudeWireRecordedRequest struct {
	req        *http.Request
	body       []byte
	tlsProfile *tlsfingerprint.Profile
}

type sequentialClaudeWireRecorder struct {
	requests  []claudeWireRecordedRequest
	responses []*http.Response
	errs      []error
}

func (r *sequentialClaudeWireRecorder) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return r.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (r *sequentialClaudeWireRecorder) DoWithTLS(req *http.Request, _ string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	var body []byte
	if req != nil && req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	clonedReq := req
	if req != nil {
		clonedReq = req.Clone(req.Context())
		clonedReq.Header = req.Header.Clone()
		if req.URL != nil {
			u := *req.URL
			clonedReq.URL = &u
		}
	}
	r.requests = append(r.requests, claudeWireRecordedRequest{
		req:        clonedReq,
		body:       append([]byte(nil), body...),
		tlsProfile: profile,
	})

	idx := len(r.requests) - 1
	if idx < len(r.errs) && r.errs[idx] != nil {
		return nil, r.errs[idx]
	}
	if idx >= len(r.responses) {
		return nil, fmt.Errorf("unexpected upstream request %d", idx+1)
	}
	return r.responses[idx], nil
}

func TestGatewayService_ClaudeOAuthSyntheticMimicMessagesWireRequestUsesCapturedClaudeCode2206ModelProfiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		model      string
		wantHeader string
	}{
		{
			model:      "claude-sonnet-4-6",
			wantHeader: "claude-code-20250219,interleaved-thinking-2025-05-14,tool-search-tool-2025-10-19,effort-2025-11-24",
		},
		{
			model:      "claude-opus-4-6",
			wantHeader: "claude-code-20250219,interleaved-thinking-2025-05-14,tool-search-tool-2025-10-19,effort-2025-11-24",
		},
		{
			model:      "claude-haiku-4-5",
			wantHeader: "claude-code-20250219,tool-search-tool-2025-10-19",
		},
		{
			model:      "claude-fable-5",
			wantHeader: "claude-code-20250219,interleaved-thinking-2025-05-14,tool-search-tool-2025-10-19,effort-2025-11-24,fallback-credit-2026-06-01",
		},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			recorder := &sequentialClaudeWireRecorder{
				responses: []*http.Response{claudeWireMessageOKResponse()},
			}
			svc := newClaudeWireGatewayService(t, recorder)
			account := claudeWireOAuthAccount()
			c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages?beta=true")
			body := []byte(fmt.Sprintf(`{"model":%q,"system":"project rules","messages":[{"role":"user","content":"hello"}],"stream":false}`, tt.model))
			parsed := mustParseClaudeWireRequest(t, body)

			result, err := svc.Forward(context.Background(), c, account, parsed)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, recorder.requests, 1)
			assertClaudeCodeWireRequest(t, recorder.requests[0], "/v1/messages?beta=true", false, "", true, true)
			require.Equal(t, tt.wantHeader, getHeaderRaw(recorder.requests[0].req.Header, "anthropic-beta"))
			billingText := findClaudeWireBillingText(gjson.GetBytes(recorder.requests[0].body, "system"))
			require.Contains(t, billingText, "cc_version=2.1.211.")
			require.Contains(t, billingText, "cc_entrypoint=sdk-cli;")
			require.False(t, gjson.GetBytes(recorder.requests[0].body, "context_management").Exists())
			assertClaudeWireMigratedSystemMessages(t, recorder.requests[0].body, "project rules", "hello")
			require.False(t, gjson.GetBytes(recorder.requests[0].body, "temperature").Exists())
			require.False(t, gjson.GetBytes(recorder.requests[0].body, "fallbacks").Exists())
		})
	}
}

func TestGatewayService_ClaudeOAuthSyntheticMimicMessagesWireRequestWithoutSystemUsesFinalUserFingerprint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &sequentialClaudeWireRecorder{
		responses: []*http.Response{claudeWireMessageOKResponse()},
	}
	svc := newClaudeWireGatewayService(t, recorder)
	account := claudeWireOAuthAccount()
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages?beta=true")
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello without system"}],"stream":false}`)
	parsed := mustParseClaudeWireRequest(t, body)

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, recorder.requests, 1)
	assertClaudeCodeWireRequest(t, recorder.requests[0], "/v1/messages?beta=true", false, "", true, true)
	assertClaudeWireUnmigratedFirstUser(t, recorder.requests[0].body, "hello without system")
	require.False(t, gjson.GetBytes(recorder.requests[0].body, "temperature").Exists())
}

func TestGatewayService_ClaudeOAuthSyntheticMimicMigratesSystemCacheControlToInstructionMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &sequentialClaudeWireRecorder{
		responses: []*http.Response{claudeWireMessageOKResponse()},
	}
	svc := newClaudeWireGatewayService(t, recorder)
	account := claudeWireOAuthAccount()
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages?beta=true")
	body := []byte(`{"model":"claude-sonnet-4-6","system":[{"type":"text","text":"cached project rules","cache_control":{"type":"ephemeral","ttl":"5m"}}],"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	parsed := mustParseClaudeWireRequest(t, body)

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, recorder.requests, 1)
	assertClaudeCodeWireRequest(t, recorder.requests[0], "/v1/messages?beta=true", false, "", true, true)
	assertClaudeWireMigratedSystemMessages(t, recorder.requests[0].body, "cached project rules", "hello")
	require.Equal(t, "ephemeral", gjson.GetBytes(recorder.requests[0].body, "messages.0.content.0.cache_control.type").String())
	require.Equal(t, "5m", gjson.GetBytes(recorder.requests[0].body, "messages.0.content.0.cache_control.ttl").String())
}

func TestGatewayService_ClaudeOAuthSyntheticMimicCountTokensWireRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &sequentialClaudeWireRecorder{
		responses: []*http.Response{claudeWireCountTokensOKResponse()},
	}
	svc := newClaudeWireGatewayService(t, recorder)
	account := claudeWireOAuthAccount()
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages/count_tokens?beta=true")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"count me"}],"stream":false}`)
	parsed := mustParseClaudeWireRequest(t, body)

	err := svc.ForwardCountTokens(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.Len(t, recorder.requests, 1)
	assertClaudeCodeWireRequest(t, recorder.requests[0], "/v1/messages/count_tokens?beta=true", true, "", true, false)
	assertClaudeWireMigratedSystemMessages(t, recorder.requests[0].body, "project rules", "count me")
	require.False(t, gjson.GetBytes(recorder.requests[0].body, "temperature").Exists())
}

// This lightweight recorder covers service-level retry/rebuild wire requests.
// Handler-level failover would require the full handler account-selection loop and scheduler mocks.
func TestGatewayService_ClaudeOAuthSyntheticMimicOrdinaryRetryPreservesBodyDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &sequentialClaudeWireRecorder{
		responses: []*http.Response{
			claudeWireErrorResponse(http.StatusForbidden, "temporary oauth authorization failure"),
			claudeWireMessageOKResponse(),
		},
	}
	svc := newClaudeWireGatewayService(t, recorder)
	account := claudeWireOAuthAccount()
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages?beta=true")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	parsed := mustParseClaudeWireRequest(t, body)

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, recorder.requests, 2)
	assertClaudeCodeWireRequest(t, recorder.requests[1], "/v1/messages?beta=true", false, "", true, true)
}

func TestGatewayService_ClaudeOAuthSyntheticMimicRetryRebuildPreservesWireRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &sequentialClaudeWireRecorder{
		responses: []*http.Response{
			claudeWireErrorResponse(http.StatusBadRequest, "messages.1.content.0.thinking.signature: Field required"),
			claudeWireMessageOKResponse(),
		},
	}
	svc := newClaudeWireGatewayService(t, recorder)
	account := claudeWireOAuthAccount()
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages?beta=true")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":[{"type":"thinking","thinking":"private chain","signature":"not-a-real-upstream-signature"},{"type":"text","text":"answer"}]},{"role":"user","content":"continue"}],"stream":false}`)
	parsed := mustParseClaudeWireRequest(t, body)

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, recorder.requests, 2)
	assertClaudeCodeWireRequest(t, recorder.requests[1], "/v1/messages?beta=true", false, "", false, false)
	require.False(t, gjson.GetBytes(recorder.requests[1].body, "thinking").Exists(), "retry body should remove top-level thinking after signature rectification")
	require.Equal(t, "high", gjson.GetBytes(recorder.requests[1].body, "output_config.effort").String())
}

func TestGatewayService_ClaudeOAuthSyntheticMimicCountTokensSignatureRetryPreservesWireRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &sequentialClaudeWireRecorder{
		responses: []*http.Response{
			claudeWireErrorResponse(http.StatusBadRequest, "messages.1.content.0.thinking.signature: Field required"),
			claudeWireCountTokensOKResponse(),
		},
	}
	svc := newClaudeWireGatewayService(t, recorder)
	account := claudeWireOAuthAccount()
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages/count_tokens?beta=true")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":[{"type":"thinking","thinking":"private chain","signature":"not-a-real-upstream-signature"},{"type":"text","text":"answer"}]},{"role":"user","content":"count again"}],"stream":false}`)
	parsed := mustParseClaudeWireRequest(t, body)

	err := svc.ForwardCountTokens(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.Len(t, recorder.requests, 2)
	assertClaudeCodeWireRequest(t, recorder.requests[1], "/v1/messages/count_tokens?beta=true", true, "", false, false)
	require.False(t, gjson.GetBytes(recorder.requests[1].body, "thinking").Exists(), "count_tokens signature retry should remove top-level thinking after signature rectification")
	require.Equal(t, "high", gjson.GetBytes(recorder.requests[1].body, "output_config.effort").String())
}

func TestGatewayService_ClaudeOAuthFakeClaudeCLIUserAgentStillUsesSyntheticMimicWireRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &sequentialClaudeWireRecorder{
		responses: []*http.Response{claudeWireMessageOKResponse()},
	}
	svc := newClaudeWireGatewayService(t, recorder)
	account := claudeWireOAuthAccount()
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages?beta=true")
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.195 (external, cli)")
	fakeUserID := FormatMetadataUserID(
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"fake-account",
		"11111111-2222-4333-8444-555555555555",
		"2.1.195",
	)
	body := []byte(`{"model":"claude-sonnet-4-6","metadata":{"user_id":` + strconvQuote(fakeUserID) + `},"system":"fake claude code system","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	parsed := mustParseClaudeWireRequest(t, body)

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, recorder.requests, 1)
	assertClaudeCodeWireRequest(t, recorder.requests[0], "/v1/messages?beta=true", false, fakeUserID, true, true)
	assertClaudeWireMigratedSystemMessages(t, recorder.requests[0].body, "fake claude code system", "hello")
}

func newClaudeWireGatewayService(t *testing.T, upstream HTTPUpstream) *GatewayService {
	t.Helper()
	resetGatewayForwardingSettingsCacheForTest(t)
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	return &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		settingService:       NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, &config.Config{}),
		identityService:      NewIdentityService(&identityCacheStub{}),
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
		tlsFPProfileService:  &TLSFingerprintProfileService{},
	}
}

func claudeWireOAuthAccount() *Account {
	return &Account{
		ID:          601,
		Name:        "anthropic-oauth-claude-wire",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Extra:       map[string]any{"account_uuid": "acc-uuid"},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func mustParseClaudeWireRequest(t *testing.T, body []byte) *ParsedRequest {
	t.Helper()
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	return parsed
}

func claudeWireMessageOKResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"msg_wire","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
	}
}

func claudeWireCountTokensOKResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"input_tokens":42}`)),
	}
}

func claudeWireErrorResponse(status int, message string) *http.Response {
	payload, _ := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": message,
		},
	})
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(payload)),
	}
}

func assertClaudeCodeWireRequest(t *testing.T, got claudeWireRecordedRequest, wantPathQuery string, wantTokenCounting bool, forbiddenMetadataUserID string, wantThinkingDefaults bool, wantStreamFalse bool) {
	t.Helper()
	require.NotNil(t, got.req)
	require.NotNil(t, got.req.URL)
	require.Equal(t, wantPathQuery, got.req.URL.RequestURI())
	require.Equal(t, claude.DefaultHeaders["User-Agent"], getHeaderRaw(got.req.Header, "User-Agent"))
	for key, want := range claude.DefaultHeaders {
		if strings.HasPrefix(key, "X-Stainless-") {
			require.Equal(t, want, getHeaderRaw(got.req.Header, key), "header %s", key)
		}
	}
	require.Equal(t, "application/json", getHeaderRaw(got.req.Header, "Accept"))
	require.Contains(t, getHeaderRaw(got.req.Header, "Accept-Encoding"), "gzip, deflate, br, zstd")
	require.Empty(t, getHeaderRaw(got.req.Header, "x-stainless-helper-method"))

	modelProfile := claude.ResolveClaudeCodeMimicryModelProfile(gjson.GetBytes(got.body, "model").String())
	wantBetas := modelProfile.MessageBetas
	if wantTokenCounting {
		wantBetas = modelProfile.CountTokensBetas
	}
	gotBetas := parseClaudeWireBetaTokens(getHeaderRaw(got.req.Header, "anthropic-beta"))
	require.Equal(t, wantBetas, gotBetas)
	require.NotContains(t, gotBetas, claude.BetaOAuth)

	system := gjson.GetBytes(got.body, "system")
	require.True(t, system.IsArray(), "system should be an array: %s", string(got.body))
	require.Len(t, system.Array(), 3)
	systemBlocks := system.Array()
	require.Contains(t, systemBlocks[0].Get("text").String(), "x-anthropic-billing-header:")
	require.Equal(t, strings.TrimSpace(claudeCodeSystemPrompt), strings.TrimSpace(systemBlocks[1].Get("text").String()))
	require.NotEmpty(t, strings.TrimSpace(systemBlocks[2].Get("text").String()))
	require.Equal(t, "ephemeral", systemBlocks[2].Get("cache_control.type").String())
	billingText := findClaudeWireBillingText(system)
	require.Contains(t, billingText, "cc_version="+claude.CLICurrentVersion+"."+computeClaudeCodeFingerprint(got.body, claude.CLICurrentVersion))
	require.Contains(t, billingText, "cc_entrypoint=sdk-cli")
	require.NotContains(t, billingText, "cch=")

	userID := gjson.GetBytes(got.body, "metadata.user_id").String()
	require.NotEmpty(t, userID)
	if forbiddenMetadataUserID != "" {
		require.NotEqual(t, forbiddenMetadataUserID, userID)
	}
	parsedUserID := ParseMetadataUserID(userID)
	require.NotNil(t, parsedUserID)
	require.True(t, parsedUserID.IsNewFormat)
	require.NotEmpty(t, parsedUserID.SessionID)
	require.Equal(t, parsedUserID.SessionID, getHeaderRaw(got.req.Header, "X-Claude-Code-Session-Id"))

	if wantThinkingDefaults {
		if modelProfile.DefaultThinkingBudgetTokens > 0 {
			require.JSONEq(t, fmt.Sprintf(`{"type":%q,"budget_tokens":%d}`, modelProfile.DefaultThinkingType, modelProfile.DefaultThinkingBudgetTokens), gjson.GetBytes(got.body, "thinking").Raw)
		} else {
			require.JSONEq(t, fmt.Sprintf(`{"type":%q}`, modelProfile.DefaultThinkingType), gjson.GetBytes(got.body, "thinking").Raw)
		}
		edits := gjson.GetBytes(got.body, "context_management.edits")
		if wantTokenCounting {
			require.True(t, edits.IsArray())
			require.Len(t, edits.Array(), 1)
			require.JSONEq(t, `[{"type":"clear_thinking_20251015","keep":"all"}]`, edits.Raw)
		} else {
			require.False(t, gjson.GetBytes(got.body, "context_management").Exists())
		}
	}
	wantMaxTokens := modelProfile.DefaultMaxTokens
	if wantTokenCounting && modelProfile.CountTokensDefaultMaxTokens > 0 {
		wantMaxTokens = modelProfile.CountTokensDefaultMaxTokens
	}
	require.Equal(t, int64(wantMaxTokens), gjson.GetBytes(got.body, "max_tokens").Int())
	if modelProfile.DefaultOutputConfigEffort != "" {
		require.JSONEq(t, fmt.Sprintf(`{"effort":%q}`, modelProfile.DefaultOutputConfigEffort), gjson.GetBytes(got.body, "output_config").Raw)
	} else {
		require.False(t, gjson.GetBytes(got.body, "output_config").Exists())
	}
	if wantStreamFalse {
		require.True(t, gjson.GetBytes(got.body, "stream").Exists())
		require.False(t, gjson.GetBytes(got.body, "stream").Bool())
	}
	require.NotNil(t, got.tlsProfile)
	require.Equal(t, builtInClaudeCodeTLSProfileName, got.tlsProfile.Name)
}

func assertClaudeWireMigratedSystemMessages(t *testing.T, body []byte, originalSystem string, originalUser string) {
	t.Helper()
	require.Equal(t, "user", gjson.GetBytes(body, "messages.0.role").String())
	require.Equal(t, "[System Instructions]\n"+originalSystem, claudeWireMessageFirstText(body, 0))
	require.Equal(t, "assistant", gjson.GetBytes(body, "messages.1.role").String())
	require.Equal(t, "Understood. I will follow these instructions.", claudeWireMessageFirstText(body, 1))
	require.Equal(t, "user", gjson.GetBytes(body, "messages.2.role").String())
	require.Equal(t, originalUser, claudeWireMessageFirstText(body, 2))
}

func assertClaudeWireUnmigratedFirstUser(t *testing.T, body []byte, originalUser string) {
	t.Helper()
	require.Equal(t, "user", gjson.GetBytes(body, "messages.0.role").String())
	require.Equal(t, originalUser, claudeWireMessageFirstText(body, 0))
	require.False(t, gjson.GetBytes(body, "messages.1.role").Exists())
}

func claudeWireMessageFirstText(body []byte, index int) string {
	content := gjson.GetBytes(body, fmt.Sprintf("messages.%d.content", index))
	if content.Type == gjson.String {
		return content.String()
	}
	if content.IsArray() {
		for _, block := range content.Array() {
			if block.Get("type").String() == "text" {
				return block.Get("text").String()
			}
		}
	}
	return ""
}

func findClaudeWireBillingText(system gjson.Result) string {
	for _, block := range system.Array() {
		text := block.Get("text").String()
		if strings.Contains(text, "x-anthropic-billing-header:") {
			return text
		}
	}
	return ""
}

func parseClaudeWireBetaTokens(header string) []string {
	parts := strings.Split(header, ",")
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}
