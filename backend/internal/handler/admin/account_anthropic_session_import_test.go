package admin

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAnthropicSessionImportCreatesSetupTokenWithAutoProxyAndAccountSettings(t *testing.T) {
	accountConcurrency := 8
	priority := 3
	rateMultiplier := 1.25
	loadFactor := 200
	autoPause := false
	expiresAt := int64(1917216000)

	req := AnthropicSessionImportRequest{
		SessionKeys:        []string{" sk-ant-sid01 "},
		GroupIDs:           []int64{7},
		ProxyMode:          anthropicSessionImportProxyModeAuto,
		AccountConcurrency: &accountConcurrency,
		Priority:           &priority,
		RateMultiplier:     &rateMultiplier,
		LoadFactor:         &loadFactor,
		ExpiresAt:          &expiresAt,
		AutoPauseOnExpired: &autoPause,
		Extra: map[string]any{
			"session_id_masking_enabled":  true,
			"anthropic_subscription_type": "Max",
		},
		CredentialExtras: map[string]any{
			"custom_base_url": "https://example.invalid",
		},
	}

	var created *service.CreateAccountInput
	executor := newAnthropicSessionImportExecutor(anthropicSessionImportExecutorDeps{
		cookieAuth: func(ctx context.Context, sessionKey string, proxyID *int64) (*service.TokenInfo, error) {
			require.Equal(t, "sk-ant-sid01", sessionKey)
			require.NotNil(t, proxyID)
			require.Equal(t, int64(2), *proxyID)
			return &service.TokenInfo{
				AccessToken:  "access-token",
				TokenType:    "Bearer",
				ExpiresIn:    3600,
				ExpiresAt:    1917216000,
				RefreshToken: "refresh-token",
				Scope:        "inference",
				OrgUUID:      "org-123",
				AccountUUID:  "acct-123",
				EmailAddress: "user@example.com",
			}, nil
		},
		listAccounts: func(ctx context.Context) ([]service.Account, error) {
			return nil, nil
		},
		listProxies: func(ctx context.Context) ([]service.ProxyWithAccountCount, error) {
			return []service.ProxyWithAccountCount{
				{
					Proxy: service.Proxy{
						ID:       1,
						Name:     "busy-proxy",
						Protocol: "http",
						Host:     "127.0.0.1",
						Port:     8080,
						Status:   service.StatusActive,
					},
					AccountCount: 10,
				},
				{
					Proxy: service.Proxy{
						ID:       2,
						Name:     "free-proxy",
						Protocol: "http",
						Host:     "127.0.0.2",
						Port:     8080,
						Status:   service.StatusActive,
					},
					AccountCount: 1,
				},
				{
					Proxy: service.Proxy{
						ID:       3,
						Name:     "disabled-proxy",
						Protocol: "http",
						Host:     "127.0.0.3",
						Port:     8080,
						Status:   service.StatusDisabled,
					},
					AccountCount: 0,
				},
			}, nil
		},
		createAccount: func(ctx context.Context, input *service.CreateAccountInput) (*service.Account, error) {
			created = input
			return &service.Account{ID: 99, Name: input.Name}, nil
		},
	})

	result := executor.run(context.Background(), req, []anthropicSessionImportEntry{
		{Index: 1, SessionKey: "sk-ant-sid01"},
	})

	require.Equal(t, 1, result.Total)
	require.Equal(t, 1, result.Created)
	require.Equal(t, 0, result.Failed)
	require.Len(t, result.Items, 1)
	require.Equal(t, "created", result.Items[0].Action)
	require.NotContains(t, result.Items[0].Message, "sk-ant-sid01")
	require.NotNil(t, created)
	require.Equal(t, "user@example.com Max", created.Name)
	require.Equal(t, service.PlatformAnthropic, created.Platform)
	require.Equal(t, service.AccountTypeSetupToken, created.Type)
	require.Equal(t, []int64{7}, created.GroupIDs)
	require.NotNil(t, created.ProxyID)
	require.Equal(t, int64(2), *created.ProxyID)
	require.Equal(t, accountConcurrency, created.Concurrency)
	require.Equal(t, priority, created.Priority)
	require.NotNil(t, created.RateMultiplier)
	require.Equal(t, rateMultiplier, *created.RateMultiplier)
	require.NotNil(t, created.LoadFactor)
	require.Equal(t, loadFactor, *created.LoadFactor)
	require.NotNil(t, created.ExpiresAt)
	require.Equal(t, expiresAt, *created.ExpiresAt)
	require.NotNil(t, created.AutoPauseOnExpired)
	require.Equal(t, autoPause, *created.AutoPauseOnExpired)
	require.Equal(t, "sk-ant-sid01", created.Credentials["session_key"])
	require.Equal(t, "access-token", created.Credentials["access_token"])
	require.Equal(t, "org-123", created.Credentials["org_uuid"])
	require.Equal(t, "acct-123", created.Credentials["account_uuid"])
	require.Equal(t, "user@example.com", created.Credentials["email_address"])
	require.Equal(t, "https://example.invalid", created.Credentials["custom_base_url"])
	require.Equal(t, true, created.Extra["session_id_masking_enabled"])
	require.Equal(t, "bulk_session_key", created.Extra["import_source"])
}
