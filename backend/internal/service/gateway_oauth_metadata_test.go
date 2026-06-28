package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
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

func ginContextForOAuthMetadataTest(t *testing.T, method, target string) *gin.Context {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	return c
}
