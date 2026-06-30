//go:build unit

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestApplyBedrockCCCompat_SkipsAnthropicOAuthAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, groupID := newGatewayServiceWithBedrockCCCompatChannel(t)
	ctx := newBedrockCCCompatGinContext(t)
	ctx.Request.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	body := []byte(`{"model":"claude-opus-4-6","service_tier":"standard","anthropic_beta":["prompt-caching-2024-07-31"],"messages":[]}`)
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	got := svc.ApplyBedrockCCCompat(ctx, body, "claude-opus-4-6", account, &groupID)

	require.JSONEq(t, string(body), string(got))
	require.Equal(t, "prompt-caching-2024-07-31", ctx.Request.Header.Get("anthropic-beta"))
}

func TestApplyBedrockCCCompat_AppliesToBedrockAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, groupID := newGatewayServiceWithBedrockCCCompatChannel(t)
	ctx := newBedrockCCCompatGinContext(t)
	ctx.Request.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	body := []byte(`{"model":"claude-opus-4-6","service_tier":"standard","anthropic_beta":["prompt-caching-2024-07-31"],"messages":[]}`)
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}

	got := svc.ApplyBedrockCCCompat(ctx, body, "claude-opus-4-6", account, &groupID)

	require.False(t, gjson.GetBytes(got, "service_tier").Exists())
	require.Equal(t, int64(defaultCCMaxTokens), gjson.GetBytes(got, "max_tokens").Int())
	require.Equal(t, "bedrock-2023-05-31", gjson.GetBytes(got, "anthropic_version").String())
	require.False(t, gjson.GetBytes(got, "anthropic_beta").Exists())
	require.Empty(t, ctx.Request.Header.Get("anthropic-beta"))
}

func newGatewayServiceWithBedrockCCCompatChannel(t *testing.T) (*GatewayService, int64) {
	t.Helper()

	const groupID int64 = 10
	channel := Channel{
		ID:       1,
		Status:   StatusActive,
		GroupIDs: []int64{groupID},
		FeaturesConfig: map[string]any{
			featureKeyBedrockCCCompat: true,
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(channel, map[int64]string{groupID: PlatformAnthropic}))

	return &GatewayService{channelService: channelSvc}, groupID
}

func newBedrockCCCompatGinContext(t *testing.T) *gin.Context {
	t.Helper()

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	return ctx
}
