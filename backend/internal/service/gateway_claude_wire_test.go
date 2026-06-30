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

func TestGatewayService_ClaudeOAuthSyntheticMimicMessagesWireRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &sequentialClaudeWireRecorder{
		responses: []*http.Response{claudeWireMessageOKResponse()},
	}
	svc := newClaudeWireGatewayService(t, recorder)
	account := claudeWireOAuthAccount()
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages?beta=true")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	parsed := mustParseClaudeWireRequest(t, body)

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, recorder.requests, 1)
	assertClaudeCodeWireRequest(t, recorder.requests[0], "/v1/messages?beta=true", false, "", true, true)
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

	profile := claude.DefaultClaudeCodeMimicryProfile()
	wantBetas := profile.MessageBetas
	if wantTokenCounting {
		wantBetas = profile.CountTokensBetas
	}
	gotBetas := parseClaudeWireBetaTokens(getHeaderRaw(got.req.Header, "anthropic-beta"))
	require.ElementsMatch(t, wantBetas, gotBetas)
	require.NotContains(t, gotBetas, claude.BetaOAuth)

	system := gjson.GetBytes(got.body, "system")
	require.True(t, system.IsArray(), "system should be an array: %s", string(got.body))
	require.Len(t, system.Array(), 3)
	billingText := findClaudeWireBillingText(system)
	require.Contains(t, billingText, "cc_version=2.1.195.")
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
		require.JSONEq(t, `{"type":"adaptive"}`, gjson.GetBytes(got.body, "thinking").Raw)
		edits := gjson.GetBytes(got.body, "context_management.edits")
		require.True(t, edits.IsArray())
		require.Len(t, edits.Array(), 1)
		require.JSONEq(t, `[{"type":"clear_thinking_20251015","keep":"all"}]`, edits.Raw)
	}
	require.JSONEq(t, `{"effort":"high"}`, gjson.GetBytes(got.body, "output_config").Raw)
	if wantStreamFalse {
		require.True(t, gjson.GetBytes(got.body, "stream").Exists())
		require.False(t, gjson.GetBytes(got.body, "stream").Bool())
	}
	require.NotNil(t, got.tlsProfile)
	require.Equal(t, "Built-in Default (Claude Code 2.1.195)", got.tlsProfile.Name)
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
