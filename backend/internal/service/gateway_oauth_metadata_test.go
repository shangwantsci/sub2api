package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildOAuthMetadataUserID_FallbackWithoutAccountUUID(t *testing.T) {
	svc := &GatewayService{}

	parsed := &ParsedRequest{
		Model:          "claude-sonnet-4-5",
		Stream:         true,
		MetadataUserID: "",
	}

	account := &Account{
		ID:    123,
		Type:  AccountTypeOAuth,
		Extra: map[string]any{}, // intentionally missing account_uuid / claude_user_id
	}

	fp := &Fingerprint{ClientID: "deadbeef"} // should be used as user id in legacy format

	got := svc.buildOAuthMetadataUserID(parsed, account, fp)
	require.NotEmpty(t, got)

	// Legacy format: user_{client}_account__session_{uuid}
	re := regexp.MustCompile(`^user_[a-zA-Z0-9]+_account__session_[a-f0-9-]{36}$`)
	require.True(t, re.MatchString(got), "unexpected user_id format: %s", got)
}

func TestBuildOAuthMetadataUserID_UsesAccountUUIDWhenPresent(t *testing.T) {
	svc := &GatewayService{}

	parsed := &ParsedRequest{
		Model:          "claude-sonnet-4-5",
		Stream:         true,
		MetadataUserID: "",
	}

	account := &Account{
		ID:   123,
		Type: AccountTypeOAuth,
		Extra: map[string]any{
			"account_uuid":      "acc-uuid",
			"claude_user_id":    "clientid123",
			"anthropic_user_id": "",
		},
	}

	got := svc.buildOAuthMetadataUserID(parsed, account, nil)
	require.NotEmpty(t, got)

	// New format: user_{client}_account_{account_uuid}_session_{uuid}
	re := regexp.MustCompile(`^user_clientid123_account_acc-uuid_session_[a-f0-9-]{36}$`)
	require.True(t, re.MatchString(got), "unexpected user_id format: %s", got)
}

// TestBuildOAuthMetadataUserID_SessionIDStableAcrossTurns 验证伪装路径合成的
// metadata.user_id 在同一会话多轮请求间保持不变（session_id 稳定），贴近真实 Claude Code
// 进程级稳定的 session。账号 / 指纹 / UA 版本均相同，唯一可能变化的就是 session_id，
// 因此直接比较完整 user_id 字符串即可判定 session_id 是否稳定。
func TestBuildOAuthMetadataUserID_SessionIDStableAcrossTurns(t *testing.T) {
	svc := &GatewayService{}
	account := &Account{ID: 777, Type: AccountTypeOAuth, Extra: map[string]any{"account_uuid": "acc-uuid"}}
	fp := &Fingerprint{ClientID: "clientid777", UserAgent: "claude-cli/2.1.161 (external, cli)"}

	mustParse := func(body string) *ParsedRequest {
		parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(body)), PlatformAnthropic)
		require.NoError(t, err)
		return parsed
	}

	round1 := mustParse(`{"model":"claude-sonnet-4-5","system":"sys","messages":[` +
		`{"role":"user","content":"first question"}]}`)
	round2 := mustParse(`{"model":"claude-sonnet-4-5","system":"sys","messages":[` +
		`{"role":"user","content":"first question"},` +
		`{"role":"assistant","content":"answer 1"},` +
		`{"role":"user","content":"second question"}]}`)
	round3 := mustParse(`{"model":"claude-sonnet-4-5","system":"sys","messages":[` +
		`{"role":"user","content":"first question"},` +
		`{"role":"assistant","content":"answer 1"},` +
		`{"role":"user","content":"second question"},` +
		`{"role":"assistant","content":"answer 2"},` +
		`{"role":"user","content":"third question"}]}`)

	id1 := svc.buildOAuthMetadataUserID(round1, account, fp)
	id2 := svc.buildOAuthMetadataUserID(round2, account, fp)
	id3 := svc.buildOAuthMetadataUserID(round3, account, fp)

	require.NotEmpty(t, id1)
	require.Equal(t, id1, id2, "session_id 应随对话增长保持不变")
	require.Equal(t, id2, id3, "session_id 应跨所有轮次保持不变")

	// 不同的首条 user 消息应派生出不同的 session_id（不同会话）。
	other := mustParse(`{"model":"claude-sonnet-4-5","system":"sys","messages":[` +
		`{"role":"user","content":"a completely different opener"}]}`)
	idOther := svc.buildOAuthMetadataUserID(other, account, fp)
	require.NotEqual(t, id1, idOther, "不同首条消息应派生不同 session_id")
}

func TestBuildUpstreamRequest_OAuthMimicGeneratesMissingMetadataAndSessionHeader(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	svc := &GatewayService{
		cfg:             &config.Config{},
		settingService:  NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, &config.Config{}),
		identityService: NewIdentityService(&identityCacheStub{}),
	}
	account := &Account{
		ID:       501,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"account_uuid": "acc-uuid"},
	}
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	req, wireBody, err := svc.buildUpstreamRequest(
		context.Background(),
		c,
		account,
		body,
		"oauth-token",
		"oauth",
		"claude-sonnet-4-6",
		true,
		true,
	)

	require.NoError(t, err)
	userID := gjson.GetBytes(wireBody, "metadata.user_id").String()
	parsed := ParseMetadataUserID(userID)
	require.NotNil(t, parsed)
	require.True(t, parsed.IsNewFormat)
	require.Equal(t, "acc-uuid", parsed.AccountUUID)
	require.NotEmpty(t, parsed.SessionID)
	require.Equal(t, parsed.SessionID, getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
}

func TestBuildUpstreamRequest_OAuthMimicRepairsInvalidMetadataUserID(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	svc := &GatewayService{
		cfg:             &config.Config{},
		settingService:  NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, &config.Config{}),
		identityService: NewIdentityService(&identityCacheStub{}),
	}
	account := &Account{
		ID:       502,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"account_uuid": "acc-uuid"},
	}
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-6","metadata":{"user_id":"not-a-claude-code-user"},"messages":[{"role":"user","content":"hello"}]}`)

	req, wireBody, err := svc.buildUpstreamRequest(
		context.Background(),
		c,
		account,
		body,
		"oauth-token",
		"oauth",
		"claude-sonnet-4-6",
		true,
		true,
	)

	require.NoError(t, err)
	userID := gjson.GetBytes(wireBody, "metadata.user_id").String()
	require.NotEqual(t, "not-a-claude-code-user", userID)
	parsed := ParseMetadataUserID(userID)
	require.NotNil(t, parsed)
	require.True(t, parsed.IsNewFormat)
	require.Equal(t, "acc-uuid", parsed.AccountUUID)
	require.Equal(t, parsed.SessionID, getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
}

func TestBuildUpstreamRequest_OAuthMimicThirdPartyUAUsesMimicProfileMetadata(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	svc := &GatewayService{
		cfg:             &config.Config{},
		settingService:  NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, &config.Config{}),
		identityService: NewIdentityService(&identityCacheStub{}),
	}
	account := &Account{
		ID:       503,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"account_uuid":               "123e4567-e89b-12d3-a456-426614174000",
			"session_id_masking_enabled": true,
		},
	}
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	c.Request.Header.Set("User-Agent", "opencode/0.6.4")
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)

	req, wireBody, err := svc.buildUpstreamRequest(
		context.Background(),
		c,
		account,
		body,
		"oauth-token",
		"oauth",
		"claude-sonnet-4-6",
		true,
		true,
	)

	require.NoError(t, err)
	require.Equal(t, claude.DefaultHeaders["User-Agent"], getHeaderRaw(req.Header, "User-Agent"))
	userID := gjson.GetBytes(wireBody, "metadata.user_id").String()
	parsed := ParseMetadataUserID(userID)
	require.NotNil(t, parsed)
	require.True(t, parsed.IsNewFormat)
	require.Equal(t, "123e4567-e89b-12d3-a456-426614174000", parsed.AccountUUID)
	require.Equal(t, parsed.SessionID, getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
}

func TestBuildCountTokensRequest_OAuthMimicThirdPartyUAUsesMimicProfileMetadata(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	svc := &GatewayService{
		cfg:             &config.Config{},
		settingService:  NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, &config.Config{}),
		identityService: NewIdentityService(&identityCacheStub{}),
	}
	account := &Account{
		ID:       504,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"account_uuid":               "123e4567-e89b-12d3-a456-426614174000",
			"session_id_masking_enabled": true,
		},
	}
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages/count_tokens")
	c.Request.Header.Set("User-Agent", "opencode/0.6.4")
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"count me"}]}`)

	req, wireBody, err := svc.buildCountTokensRequest(
		context.Background(),
		c,
		account,
		body,
		"oauth-token",
		"oauth",
		"claude-sonnet-4-6",
		true,
	)

	require.NoError(t, err)
	require.Equal(t, claude.DefaultHeaders["User-Agent"], getHeaderRaw(req.Header, "User-Agent"))
	userID := gjson.GetBytes(wireBody, "metadata.user_id").String()
	parsed := ParseMetadataUserID(userID)
	require.NotNil(t, parsed)
	require.True(t, parsed.IsNewFormat)
	require.Equal(t, "123e4567-e89b-12d3-a456-426614174000", parsed.AccountUUID)
	require.Equal(t, parsed.SessionID, getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
	beta := getHeaderRaw(req.Header, "anthropic-beta")
	require.Contains(t, beta, claude.BetaTokenCounting)
	require.NotContains(t, beta, claude.BetaOAuth)
}

func TestGatewayService_AnthropicOAuthFakeClaudeCodeMetadataStillMimics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.195 (external, cli)")

	fakeUserID := FormatMetadataUserID(
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"fake-account",
		"11111111-2222-4333-8444-555555555555",
		"2.1.195",
	)
	body := []byte(`{"model":"claude-sonnet-4-6","metadata":{"user_id":` + strconvQuote(fakeUserID) + `},"system":"project rules","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
	}
	account := &Account{
		ID:          506,
		Name:        "anthropic-oauth-fake-cc",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, claude.DefaultHeaders["User-Agent"], getHeaderRaw(upstream.lastReq.Header, "User-Agent"))

	system := gjson.GetBytes(upstream.lastBody, "system")
	require.True(t, system.IsArray())
	require.Len(t, system.Array(), 3)
	require.Contains(t, system.Array()[0].Get("text").String(), "x-anthropic-billing-header:")
	require.Equal(t, claudeCodeSystemPrompt, system.Array()[1].Get("text").String())
	require.Equal(t, claudeCodeSystemPromptExpansion, system.Array()[2].Get("text").String())
}

func TestGatewayService_AnthropicOAuthCountTokensFakeClaudeCodeMetadataStillMimics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.195 (external, cli)")

	fakeUserID := FormatMetadataUserID(
		"fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
		"fake-account",
		"22222222-3333-4444-8555-666666666666",
		"2.1.195",
	)
	body := []byte(`{"model":"claude-sonnet-4-6","metadata":{"user_id":` + strconvQuote(fakeUserID) + `},"system":"project rules","messages":[{"role":"user","content":"count me"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"input_tokens":42}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:              cfg,
		httpUpstream:     upstream,
		rateLimitService: &RateLimitService{},
		deferredService:  &DeferredService{},
	}
	account := &Account{
		ID:          507,
		Name:        "anthropic-oauth-count-tokens-fake-cc",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}

	err = svc.ForwardCountTokens(context.Background(), c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, claude.DefaultHeaders["User-Agent"], getHeaderRaw(upstream.lastReq.Header, "User-Agent"))
	beta := getHeaderRaw(upstream.lastReq.Header, "anthropic-beta")
	require.Contains(t, beta, claude.BetaTokenCounting)
	require.Contains(t, beta, claude.BetaContextManagement)

	system := gjson.GetBytes(upstream.lastBody, "system")
	require.True(t, system.IsArray())
	require.Len(t, system.Array(), 3)
	require.Contains(t, system.Array()[0].Get("text").String(), "x-anthropic-billing-header:")
}

func TestGatewayService_AnthropicOAuthClaudeMimicBodyDefaults_MessagesMissingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
	}
	account := &Account{
		ID:          508,
		Name:        "anthropic-oauth-mimic-body-defaults",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)

	require.Equal(t, "adaptive", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
	require.Equal(t, "clear_thinking_20251015",
		gjson.GetBytes(upstream.lastBody, "context_management.edits.0.type").String())
	require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "output_config.effort").String())
	require.Equal(t, int64(64000), gjson.GetBytes(upstream.lastBody, "max_tokens").Int())
	require.False(t, gjson.GetBytes(upstream.lastBody, "temperature").Exists())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
}

func TestGatewayService_AnthropicOAuthClaudeMimicBodyDefaults_HaikuUsesCapturedModelDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"model":"claude-haiku-4-5-20251001","system":"project rules","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
	}
	account := &Account{
		ID:          518,
		Name:        "anthropic-oauth-mimic-body-defaults-haiku",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)

	require.Equal(t, "enabled", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
	require.Equal(t, int64(31999), gjson.GetBytes(upstream.lastBody, "thinking.budget_tokens").Int())
	require.Equal(t, int64(32000), gjson.GetBytes(upstream.lastBody, "max_tokens").Int())
	require.Equal(t, "clear_thinking_20251015",
		gjson.GetBytes(upstream.lastBody, "context_management.edits.0.type").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "output_config").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "temperature").Exists())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
}

func TestGatewayService_AnthropicOAuthClaudeMimicBodyDefaults_RemovesInvalidTemperatureWhenThinkingIsOn(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"model":"claude-opus-4-8","system":"project rules","messages":[{"role":"user","content":"hello"}],"temperature":0.4,"stream":false}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4-8","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
	}
	account := &Account{
		ID:          519,
		Name:        "anthropic-oauth-mimic-invalid-temperature",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)

	require.Equal(t, "adaptive", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "temperature").Exists())
}

func TestGatewayService_AnthropicOAuthClaudeMimicBodyDefaults_HaikuRewritesAdaptiveThinking(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"model":"claude-haiku-4-5-20251001","system":"project rules","messages":[{"role":"user","content":"hello"}],"thinking":{"type":"adaptive"},"stream":false}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
	}
	account := &Account{
		ID:          525,
		Name:        "anthropic-oauth-mimic-haiku-adaptive",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)

	require.Equal(t, "enabled", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
	require.Equal(t, int64(31999), gjson.GetBytes(upstream.lastBody, "thinking.budget_tokens").Int())
	require.False(t, gjson.GetBytes(upstream.lastBody, "output_config").Exists())
}

func TestGatewayService_AnthropicOAuthClaudeMimicBodyDefaults_PreservesExplicitFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"}],"thinking":{"type":"disabled","budget_tokens":777},"context_management":{"edits":[{"type":"client_strategy","keep":"client"}]},"output_config":{"effort":"medium","extra":true},"max_tokens":1234,"temperature":0.4,"stream":false}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
	}
	account := &Account{
		ID:          509,
		Name:        "anthropic-oauth-mimic-body-defaults-explicit",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)

	require.Equal(t, "disabled", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
	require.Equal(t, float64(777), gjson.GetBytes(upstream.lastBody, "thinking.budget_tokens").Float())
	require.Equal(t, "client_strategy", gjson.GetBytes(upstream.lastBody, "context_management.edits.0.type").String())
	require.Equal(t, "client", gjson.GetBytes(upstream.lastBody, "context_management.edits.0.keep").String())
	require.Len(t, gjson.GetBytes(upstream.lastBody, "context_management.edits").Array(), 1)
	require.Equal(t, "medium", gjson.GetBytes(upstream.lastBody, "output_config.effort").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "output_config.extra").Bool())
	require.Equal(t, int64(1234), gjson.GetBytes(upstream.lastBody, "max_tokens").Int())
	require.Equal(t, 0.4, gjson.GetBytes(upstream.lastBody, "temperature").Float())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
}

func TestNormalizeClaudeOAuthMetadataClaudeMimicBodyDefaults_AppendsMissingClearThinkingEdit(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		wantEdits       int
		wantFirstType   string
		wantSecondType  string
		wantSecondKeep  string
		wantClearCopies int
	}{
		{
			name:            "empty context_management gets clear thinking edit",
			body:            `{"model":"claude-sonnet-4-6","thinking":{"type":"adaptive"},"context_management":{},"messages":[]}`,
			wantEdits:       1,
			wantFirstType:   "clear_thinking_20251015",
			wantClearCopies: 1,
		},
		{
			name:            "existing edits are preserved and clear thinking edit is appended",
			body:            `{"model":"claude-sonnet-4-6","thinking":{"type":"adaptive"},"context_management":{"edits":[{"type":"client_strategy","keep":"client"}]},"messages":[]}`,
			wantEdits:       2,
			wantFirstType:   "client_strategy",
			wantSecondType:  "clear_thinking_20251015",
			wantSecondKeep:  "all",
			wantClearCopies: 1,
		},
		{
			name:            "existing clear thinking edit is not duplicated",
			body:            `{"model":"claude-sonnet-4-6","thinking":{"type":"adaptive"},"context_management":{"edits":[{"type":"clear_thinking_20251015","keep":"client"}]},"messages":[]}`,
			wantEdits:       1,
			wantFirstType:   "clear_thinking_20251015",
			wantClearCopies: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := normalizeClaudeOAuthRequestBody([]byte(tt.body), "claude-sonnet-4-6", claudeOAuthNormalizeOptions{
				ensureMimicBodyDefaults: true,
			})

			edits := gjson.GetBytes(out, "context_management.edits").Array()
			require.Len(t, edits, tt.wantEdits)
			require.Equal(t, tt.wantFirstType, edits[0].Get("type").String())
			if tt.wantSecondType != "" {
				require.Equal(t, tt.wantSecondType, edits[1].Get("type").String())
				require.Equal(t, tt.wantSecondKeep, edits[1].Get("keep").String())
			}

			clearCopies := 0
			for _, edit := range edits {
				if edit.Get("type").String() == "clear_thinking_20251015" {
					clearCopies++
				}
			}
			require.Equal(t, tt.wantClearCopies, clearCopies)
		})
	}
}

func TestNormalizeClaudeOAuthMetadataClaudeMimicBodyDefaults_PreservesExplicitNonObjectFields(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		assert func(t *testing.T, out []byte)
	}{
		{
			name: "thinking null stays null",
			body: `{"model":"claude-sonnet-4-6","thinking":null,"messages":[]}`,
			assert: func(t *testing.T, out []byte) {
				require.Equal(t, gjson.Null, gjson.GetBytes(out, "thinking").Type)
				require.False(t, gjson.GetBytes(out, "thinking.type").Exists())
				require.False(t, gjson.GetBytes(out, "context_management").Exists())
			},
		},
		{
			name: "context management edits string stays string",
			body: `{"model":"claude-sonnet-4-6","thinking":{"type":"adaptive"},"context_management":{"edits":"client-value"},"messages":[]}`,
			assert: func(t *testing.T, out []byte) {
				require.Equal(t, "adaptive", gjson.GetBytes(out, "thinking.type").String())
				require.Equal(t, "client-value", gjson.GetBytes(out, "context_management.edits").String())
			},
		},
		{
			name: "output config null stays null",
			body: `{"model":"claude-sonnet-4-6","output_config":null,"messages":[]}`,
			assert: func(t *testing.T, out []byte) {
				require.Equal(t, gjson.Null, gjson.GetBytes(out, "output_config").Type)
				require.False(t, gjson.GetBytes(out, "output_config.effort").Exists())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := normalizeClaudeOAuthRequestBody([]byte(tt.body), "claude-sonnet-4-6", claudeOAuthNormalizeOptions{
				ensureMimicBodyDefaults: true,
			})
			tt.assert(t, out)
		})
	}
}

func TestGatewayService_AnthropicOAuthRealClaudeCodeForwardDoesNotForceMimicBodyDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.195 (external, cli)")

	account := &Account{
		ID:          510,
		Name:        "anthropic-oauth-real-cc-forward",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}
	body := []byte(`{"model":"claude-sonnet-4-6","system":"real claude code system","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
	}
	ctx := SetClaudeCodeClient(context.Background(), true)

	result, err := svc.Forward(ctx, c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)

	system := gjson.GetBytes(upstream.lastBody, "system")
	require.False(t, system.IsArray())
	require.Equal(t, "real claude code system", system.String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "thinking").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "context_management").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "output_config").Exists())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
}

func TestGatewayService_ClaudeMimicTLSProfile_DefaultsForSyntheticMimic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
		tlsFPProfileService:  &TLSFingerprintProfileService{},
	}
	account := &Account{
		ID:          520,
		Name:        "anthropic-oauth-synthetic-tls-default",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastTLSProfile)
	require.Equal(t, builtInClaudeCodeTLSProfileName, upstream.lastTLSProfile.Name)
}

func TestGatewayService_ClaudeMimicTLSProfile_DoesNotDefaultForAPIKeyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
		tlsFPProfileService:  &TLSFingerprintProfileService{},
	}
	account := &Account{
		ID:          521,
		Name:        "anthropic-apikey-no-tls-default",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "upstream-key"},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, upstream.lastTLSProfile)
}

func TestGatewayService_ClaudeMimicTLSProfile_DoesNotDefaultForRealClaudeCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"real claude code system","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
		tlsFPProfileService:  &TLSFingerprintProfileService{},
	}
	account := &Account{
		ID:          522,
		Name:        "anthropic-oauth-real-cc-no-tls-default",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}
	ctx := SetClaudeCodeClient(context.Background(), true)

	result, err := svc.Forward(ctx, c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, upstream.lastTLSProfile)
}

func TestGatewayService_ClaudeMimicTLSProfile_ExplicitFalseDisablesDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
		tlsFPProfileService:  &TLSFingerprintProfileService{},
	}
	account := &Account{
		ID:          523,
		Name:        "anthropic-oauth-explicit-false-no-tls-default",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Extra:       map[string]any{"enable_tls_fingerprint": false},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, upstream.lastTLSProfile)
}

func TestGatewayService_ClaudeMimicTLSProfile_ExplicitTrueUsesConfiguredDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
		tlsFPProfileService:  &TLSFingerprintProfileService{},
	}
	account := &Account{
		ID:          524,
		Name:        "anthropic-oauth-explicit-true-tls-default",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Extra:       map[string]any{"enable_tls_fingerprint": true},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastTLSProfile)
	require.Equal(t, builtInClaudeCodeTLSProfileName, upstream.lastTLSProfile.Name)
}

func TestGatewayService_ClaudeMimicTLSProfile_ProfileIDWithoutEnableUsesBoundProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
		tlsFPProfileService: &TLSFingerprintProfileService{
			localCache: map[int64]*model.TLSFingerprintProfile{
				42: {
					ID:            42,
					Name:          "bound-profile",
					CipherSuites:  []uint16{0x1302},
					ALPNProtocols: []string{"h2", "http/1.1"},
				},
			},
		},
	}
	account := &Account{
		ID:          526,
		Name:        "anthropic-oauth-profile-id-without-enable",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Extra:       map[string]any{"tls_fingerprint_profile_id": 42},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastTLSProfile)
	require.Equal(t, "bound-profile", upstream.lastTLSProfile.Name)
	require.Equal(t, []uint16{0x1302}, upstream.lastTLSProfile.CipherSuites)
	require.Equal(t, []string{"h2", "http/1.1"}, upstream.lastTLSProfile.ALPNProtocols)
}

func TestGatewayService_ClaudeMimicTLSProfile_RealClaudeCodeProfileIDWithoutEnableUsesBoundProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"real claude code system","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
		tlsFPProfileService: &TLSFingerprintProfileService{
			localCache: map[int64]*model.TLSFingerprintProfile{
				42: {
					ID:            42,
					Name:          "bound-profile",
					CipherSuites:  []uint16{0x1302},
					ALPNProtocols: []string{"h2", "http/1.1"},
				},
			},
		},
	}
	account := &Account{
		ID:          527,
		Name:        "anthropic-oauth-real-cc-profile-id-without-enable",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Extra:       map[string]any{"tls_fingerprint_profile_id": 42},
		Status:      StatusActive,
		Schedulable: true,
	}
	ctx := SetClaudeCodeClient(context.Background(), true)

	result, err := svc.Forward(ctx, c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastTLSProfile)
	require.Equal(t, "bound-profile", upstream.lastTLSProfile.Name)
	require.Equal(t, []uint16{0x1302}, upstream.lastTLSProfile.CipherSuites)
	require.Equal(t, []string{"h2", "http/1.1"}, upstream.lastTLSProfile.ALPNProtocols)
}

func TestGatewayService_ClaudeMimicTLSProfile_DefaultsForCountTokensSyntheticMimic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages/count_tokens")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"input_tokens":42}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                 cfg,
		httpUpstream:        upstream,
		rateLimitService:    &RateLimitService{},
		deferredService:     &DeferredService{},
		tlsFPProfileService: &TLSFingerprintProfileService{},
	}
	account := &Account{
		ID:          525,
		Name:        "anthropic-oauth-count-tokens-synthetic-tls-default",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}

	err = svc.ForwardCountTokens(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, upstream.lastTLSProfile)
	require.Equal(t, builtInClaudeCodeTLSProfileName, upstream.lastTLSProfile.Name)
}

func TestGatewayService_AnthropicOAuthCountTokensClaudeMimicBodyDefaultsMatchMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	messageBody := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"}]}`)
	messageParsed, err := ParseGatewayRequest(NewRequestBodyRef(messageBody), PlatformAnthropic)
	require.NoError(t, err)
	messageUpstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	messageSvc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         messageUpstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
	}
	account := &Account{
		ID:          511,
		Name:        "anthropic-oauth-mimic-body-defaults-both",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}
	messageCtx := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")

	_, err = messageSvc.Forward(context.Background(), messageCtx, account, messageParsed)
	require.NoError(t, err)
	require.NotNil(t, messageUpstream.lastReq)

	countBody := []byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello"}]}`)
	countParsed, err := ParseGatewayRequest(NewRequestBodyRef(countBody), PlatformAnthropic)
	require.NoError(t, err)
	countUpstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"input_tokens":42}`)),
		},
	}
	countSvc := &GatewayService{
		cfg:              cfg,
		httpUpstream:     countUpstream,
		rateLimitService: &RateLimitService{},
		deferredService:  &DeferredService{},
	}
	countCtx := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages/count_tokens")

	err = countSvc.ForwardCountTokens(context.Background(), countCtx, account, countParsed)
	require.NoError(t, err)
	require.NotNil(t, countUpstream.lastReq)

	for _, got := range [][]byte{messageUpstream.lastBody, countUpstream.lastBody} {
		require.Equal(t, "adaptive", gjson.GetBytes(got, "thinking.type").String())
		require.Equal(t, "clear_thinking_20251015",
			gjson.GetBytes(got, "context_management.edits.0.type").String())
		require.Equal(t, "high", gjson.GetBytes(got, "output_config.effort").String())
	}
}

func TestGatewayService_AnthropicOAuthCountTokensRealClaudeCodeDoesNotForceMimicDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages/count_tokens")
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.195 (external, cli)")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"real claude code system","messages":[{"role":"user","content":"count me"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"input_tokens":42}`)),
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:              cfg,
		httpUpstream:     upstream,
		rateLimitService: &RateLimitService{},
		deferredService:  &DeferredService{},
	}
	account := &Account{
		ID:          512,
		Name:        "anthropic-oauth-real-cc-count-tokens",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Status:      StatusActive,
		Schedulable: true,
	}
	ctx := SetClaudeCodeClient(context.Background(), true)

	err = svc.ForwardCountTokens(ctx, c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)

	require.Equal(t, "real claude code system", gjson.GetBytes(upstream.lastBody, "system").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "thinking").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "context_management").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "output_config").Exists())
}

func TestBuildUpstreamRequest_OAuthMimicThirdPartyClaudeUAKeepsBillingProfileVersion(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	svc := &GatewayService{
		cfg:             &config.Config{},
		settingService:  NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, &config.Config{}),
		identityService: NewIdentityService(&identityCacheStub{}),
	}
	account := &Account{
		ID:       505,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"account_uuid":               "123e4567-e89b-12d3-a456-426614174000",
			"session_id_masking_enabled": true,
		},
	}
	c := ginContextForOAuthMetadataTest(t, http.MethodPost, "/v1/messages")
	c.Request.Header.Set("User-Agent", "claude-cli/0.6.4 (third-party)")
	body := rewriteSystemForNonClaudeCodeWithPromptBlocks(
		[]byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"hello billing"}]}`),
		"project rules",
		"",
		"",
	)

	_, wireBody, err := svc.buildUpstreamRequest(
		context.Background(),
		c,
		account,
		body,
		"oauth-token",
		"oauth",
		"claude-sonnet-4-6",
		true,
		true,
	)

	require.NoError(t, err)
	billingText := ""
	gjson.GetBytes(wireBody, "system").ForEach(func(_, block gjson.Result) bool {
		text := block.Get("text").String()
		if strings.Contains(text, "x-anthropic-billing-header:") {
			billingText = text
			return false
		}
		return true
	})
	require.Contains(t, billingText, "cc_version="+claude.CLICurrentVersion+".")
	require.NotContains(t, billingText, "cc_version=0.6.4.")
}

func ginContextForOAuthMetadataTest(t *testing.T, method, target string) *gin.Context {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	return c
}
