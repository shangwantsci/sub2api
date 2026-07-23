//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelAccessDenial_FableFamilyOnly(t *testing.T) {
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			modelAccessDenialsKey: map[string]any{
				anthropicFableRateLimitKey: map[string]any{
					"reason":      anthropicFableCreditsRequiredDenialReason,
					"observed_at": "2026-07-23T00:00:00Z",
				},
			},
		},
	}

	require.True(t, account.isModelAccessDeniedWithContext(context.Background(), "claude-fable-5"))
	require.True(t, account.isModelAccessDeniedWithContext(context.Background(), "Claude-Fable-5[1m]"))
	require.False(t, account.isModelAccessDeniedWithContext(context.Background(), "claude-opus-4-8"))
	require.False(t, account.isModelAccessDeniedWithContext(context.Background(), "claude-sonnet-4-6"))
}

func TestGatewayScheduling_ModelAccessDenialOverridesEmptyModelMapping(t *testing.T) {
	svc := &GatewayService{}
	account := &Account{
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{},
		Extra: map[string]any{
			modelAccessDenialsKey: map[string]any{
				anthropicFableRateLimitKey: map[string]any{
					"reason":      anthropicFableCreditsRequiredDenialReason,
					"observed_at": "2026-07-23T00:00:00Z",
				},
			},
		},
	}

	require.False(t, svc.isModelSupportedByAccountWithContext(context.Background(), account, "claude-fable-5"))
	require.True(t, svc.isModelSupportedByAccountWithContext(context.Background(), account, "claude-sonnet-4-6"))
}

func TestSelectAnthropicFableCreditsRequired_LegacyMessageFallback(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Usage credits are required for this model."}}`)

	denial := selectAnthropicFableCreditsRequired(body, "claude-fable-5[1m]", now)

	require.NotNil(t, denial)
	require.Equal(t, anthropicFableCreditsRequiredDenialReason, denial.Reason)
	require.Equal(t, "claude-fable-5[1m]", denial.Model)
	require.Equal(t, "2026-07-23T00:00:00Z", denial.ObservedAt)
	require.Nil(t, selectAnthropicFableCreditsRequired(body, "claude-sonnet-4-6", now))
}
