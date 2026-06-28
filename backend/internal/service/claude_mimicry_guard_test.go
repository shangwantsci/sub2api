package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestEvaluateClaudeMimicryGuard_BlockModeBlocksMissingBillingBlock(t *testing.T) {
	profile := claude.DefaultClaudeCodeMimicryProfile()
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages?beta=true", strings.NewReader(`{}`))
	require.NoError(t, err)
	for key, value := range profile.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("anthropic-beta", strings.Join(profile.MessageBetas, ","))
	req.Header.Set("X-Claude-Code-Session-Id", "fe01a97f-b8c7-4ff4-8a9b-7c8a17d35680")
	body := []byte(`{"model":"claude-sonnet-4-6","system":[{"type":"text","text":"You are a Claude agent, built on Anthropic's Claude Agent SDK."}],"metadata":{"user_id":"{\"device_id\":\"816a2a272e95ed4ac75d4b1dee8d8a7daaf716677c8b11a4b1d8b73a05fa37d6\",\"account_uuid\":\"\",\"session_id\":\"fe01a97f-b8c7-4ff4-8a9b-7c8a17d35680\"}"}}`)

	audit := evaluateClaudeMimicryGuard(req, body, profile, "block")

	require.False(t, audit.OK)
	require.True(t, audit.ShouldBlock)
	require.Contains(t, strings.Join(audit.Findings, ","), "missing_billing_block")
}

func TestEvaluateClaudeMimicryGuard_WarnModeDoesNotBlock(t *testing.T) {
	profile := claude.DefaultClaudeCodeMimicryProfile()
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages?beta=true", strings.NewReader(`{}`))
	require.NoError(t, err)
	body := []byte(`{"model":"claude-sonnet-4-6"}`)

	audit := evaluateClaudeMimicryGuard(req, body, profile, "warn")

	require.False(t, audit.OK)
	require.False(t, audit.ShouldBlock)
	require.NotEmpty(t, audit.Findings)
}

func TestBuildCountTokensRequest_ClaudeMimicryGuardBlockModeAllowsRepairedMimic(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	svc := &GatewayService{
		cfg: &config.Config{},
		settingService: NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
			SettingKeyClaudeMimicryGuardMode: "block",
		}}, &config.Config{}),
		identityService: NewIdentityService(&identityCacheStub{}),
	}
	account := &Account{
		ID:       123,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"account_uuid": "acc-uuid"},
	}

	req, wireBody, err := svc.buildCountTokensRequest(
		context.Background(),
		nil,
		account,
		[]byte(`{"model":"claude-sonnet-4-6","system":"project rules","messages":[{"role":"user","content":"count me"}]}`),
		"oauth-token",
		"oauth",
		"claude-sonnet-4-6",
		true,
	)

	require.NoError(t, err)
	require.Len(t, gjson.GetBytes(wireBody, "system").Array(), 3)
	userID := gjson.GetBytes(wireBody, "metadata.user_id").String()
	parsed := ParseMetadataUserID(userID)
	require.NotNil(t, parsed)
	require.Equal(t, "acc-uuid", parsed.AccountUUID)
	require.Equal(t, parsed.SessionID, getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
	beta := getHeaderRaw(req.Header, "anthropic-beta")
	require.Contains(t, beta, claude.BetaTokenCounting)
	require.NotContains(t, beta, claude.BetaOAuth)
}
