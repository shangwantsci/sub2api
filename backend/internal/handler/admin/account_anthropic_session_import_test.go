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

// 「更新已存在账号」只刷新凭据、伪装与归属类设置，绝不覆盖存量账号的并发与容量键：
// 导入弹窗里的容量卡只是给「新建」用的初值，否则每次重新导入同一个 SessionKey 都会把这个号
// 手调过的节流设置悄悄重置成弹窗默认值。
func TestAnthropicSessionImportUpdateExistingKeepsConcurrencyAndCapacityKeys(t *testing.T) {
	accountConcurrency := 8
	loadFactor := 200
	priority := 3

	existing := service.Account{
		ID:          55,
		Name:        "existing",
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeSetupToken,
		Concurrency: 7,
		Credentials: map[string]any{"session_key": "old-session-key", "access_token": "old-access"},
		Extra: map[string]any{
			"account_uuid":      "acct-123",
			"max_sessions":      4,
			"base_rpm":          40,
			"window_cost_limit": 80,
			"adaptive_throttle": false,
			"nurturing_enabled": true,
			"privacy_mode":      "on",
		},
	}

	req := AnthropicSessionImportRequest{
		SessionKeys:        []string{"sk-ant-sid01"},
		AccountConcurrency: &accountConcurrency,
		LoadFactor:         &loadFactor,
		Priority:           &priority,
		Extra: map[string]any{
			// 导入弹窗里容量卡的值：对存量账号一个都不能生效。
			"max_sessions":      3,
			"base_rpm":          30,
			"window_cost_limit": 60,
			"adaptive_throttle": true,
			"nurturing_enabled": false,
			// 伪装类设置仍然随导入刷新。
			"session_id_masking_enabled": true,
		},
	}

	var updatedID int64
	var updateInput *service.UpdateAccountInput
	executor := newAnthropicSessionImportExecutor(anthropicSessionImportExecutorDeps{
		cookieAuth: func(ctx context.Context, sessionKey string, proxyID *int64) (*service.TokenInfo, error) {
			return &service.TokenInfo{
				AccessToken: "new-access", RefreshToken: "new-refresh",
				OrgUUID: "org-123", AccountUUID: "acct-123", EmailAddress: "user@example.com",
			}, nil
		},
		listAccounts: func(ctx context.Context) ([]service.Account, error) {
			return []service.Account{existing}, nil
		},
		listProxies: func(ctx context.Context) ([]service.ProxyWithAccountCount, error) { return nil, nil },
		createAccount: func(ctx context.Context, input *service.CreateAccountInput) (*service.Account, error) {
			t.Fatalf("existing account must be updated, not created")
			return nil, nil
		},
		updateAccount: func(ctx context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
			updatedID = id
			updateInput = input
			return &service.Account{ID: id, Name: "existing"}, nil
		},
	})

	result := executor.run(context.Background(), req, []anthropicSessionImportEntry{{Index: 1, SessionKey: "sk-ant-sid01"}})

	require.Equal(t, 1, result.Updated)
	require.Equal(t, 0, result.Created)
	require.Equal(t, 0, result.Failed)
	require.Equal(t, int64(55), updatedID)
	require.NotNil(t, updateInput)

	// 并发与负载因子都不下发（nil = 不改）。
	require.Nil(t, updateInput.Concurrency, "不能用导入弹窗的并发覆盖存量账号")
	require.Nil(t, updateInput.LoadFactor, "负载因子与并发绑定，同样不动")

	// 容量键保持存量账号自己的值，既不被导入值覆盖，也不会丢。
	extra := updateInput.Extra
	require.Equal(t, 4, extra["max_sessions"])
	require.Equal(t, 40, extra["base_rpm"])
	require.Equal(t, 80, extra["window_cost_limit"])
	require.Equal(t, false, extra["adaptive_throttle"])
	require.Equal(t, true, extra["nurturing_enabled"])

	// 与容量无关的存量设置原样保留，伪装与导入来源类设置照常刷新。
	require.Equal(t, "on", extra["privacy_mode"])
	require.Equal(t, true, extra["session_id_masking_enabled"])
	require.Equal(t, "bulk_session_key", extra["import_source"])
	require.Equal(t, "org-123", extra["org_uuid"])

	// 凭据照常刷新；归属类字段（优先级）照常应用。
	require.Equal(t, "new-access", updateInput.Credentials["access_token"])
	require.NotNil(t, updateInput.Priority)
	require.Equal(t, priority, *updateInput.Priority)
}
