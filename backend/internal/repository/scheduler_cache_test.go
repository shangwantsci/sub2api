package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFilterSchedulerCredentialsKeepsSubscriptionPlanType(t *testing.T) {
	filtered := filterSchedulerCredentials(map[string]any{
		"plan_type":     "plus",
		"access_token":  "secret-access-token",
		"refresh_token": "secret-refresh-token",
	})

	require.Equal(t, "plus", filtered["plan_type"])
	require.NotContains(t, filtered, "access_token")
	require.NotContains(t, filtered, "refresh_token")
}

func TestFilterSchedulerExtraKeepsModelAccessDenials(t *testing.T) {
	denials := map[string]any{
		"claude-fable-5": map[string]any{
			"reason": "anthropic_fable_credits_required",
		},
	}
	filtered := filterSchedulerExtra(map[string]any{
		"model_access_denials": denials,
		"unrelated_secret":     "do-not-copy",
	})

	require.Equal(t, denials, filtered["model_access_denials"])
	require.NotContains(t, filtered, "unrelated_secret")
}

func TestSchedulerMetadataAccountKeepsOpenAISubscriptionIdentity(t *testing.T) {
	account := service.Account{
		ID:       24,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"plan_type":    "plus",
			"access_token": "secret-access-token",
		},
	}

	metadata := buildSchedulerMetadataAccount(account)

	require.True(t, metadata.IsOpenAIChatGPTSubscription())
	require.Empty(t, metadata.GetCredential("access_token"))
}
