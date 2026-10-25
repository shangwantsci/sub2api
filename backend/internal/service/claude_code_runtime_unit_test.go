//go:build unit

package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClaudeCodeRuntime(t *testing.T) (*ClaudeCodeRuntime, *config.Config) {
	t.Helper()
	cfg := &config.Config{}
	cfg.ProcessPool.DataDir = t.TempDir()
	cfg.ProcessPool.CCTransportEnabled = true
	cfg.ProcessPool.ClaudeBinary = "/bin/true"
	cfg.ProcessPool.ForwarderJS = "/tmp/forwarder.js"
	pool := NewProcessPool(cfg)
	return NewClaudeCodeRuntime(cfg, nil, nil, pool, nil), cfg
}

func testClaudeCodeAccount(id int64) *Account {
	return &Account{
		ID:       id,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":      "sk-ant-oat01-access",
			"refresh_token":     "sk-ant-ort01-refresh",
			"expires_at":        float64(time.Now().Add(time.Hour).Unix()),
			"scopes":            []any{"user:inference", "user:sessions:claude_code"},
			"subscription_type": "pro",
			"account_uuid":      "acct-uuid",
			"email_address":     "user@example.test",
			"org_uuid":          "org-uuid",
		},
	}
}

func TestClaudeCodeRuntime_PrepareHomeWritesCredentials(t *testing.T) {
	runtime, _ := newTestClaudeCodeRuntime(t)
	account := testClaudeCodeAccount(42)

	home, err := runtime.prepareHome(account)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(runtime.cfg.ProcessPool.DataDir, "cc", "acct-42"), home)

	credentialsPath := filepath.Join(home, ".claude", ".credentials.json")
	info, err := os.Stat(credentialsPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	var payload struct {
		ClaudeAIOAuth struct {
			AccessToken      string   `json:"accessToken"`
			RefreshToken     string   `json:"refreshToken"`
			ExpiresAt        int64    `json:"expiresAt"`
			Scopes           []string `json:"scopes"`
			SubscriptionType string   `json:"subscriptionType"`
			AccountUUID      string   `json:"accountUuid"`
			Email            string   `json:"email"`
		} `json:"claudeAiOauth"`
	}
	raw, err := os.ReadFile(credentialsPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &payload))
	assert.Equal(t, "sk-ant-oat01-access", payload.ClaudeAIOAuth.AccessToken)
	assert.Equal(t, "sk-ant-ort01-refresh", payload.ClaudeAIOAuth.RefreshToken)
	assert.Greater(t, payload.ClaudeAIOAuth.ExpiresAt, time.Now().UnixMilli())
	assert.Equal(t, []string{"user:inference", "user:sessions:claude_code"}, payload.ClaudeAIOAuth.Scopes)
	assert.Equal(t, "pro", payload.ClaudeAIOAuth.SubscriptionType)
	assert.Equal(t, "acct-uuid", payload.ClaudeAIOAuth.AccountUUID)

	var claudeConfig map[string]any
	rawConfig, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(rawConfig, &claudeConfig))
	assert.Equal(t, true, claudeConfig["hasCompletedOnboarding"])
	assert.NotEmpty(t, claudeConfig["userID"])
}

func TestClaudeCodeRuntime_PrepareHomeKeepsExistingClaudeConfig(t *testing.T) {
	runtime, _ := newTestClaudeCodeRuntime(t)
	account := testClaudeCodeAccount(43)

	home, err := runtime.prepareHome(account)
	require.NoError(t, err)
	configPath := filepath.Join(home, ".claude.json")
	original, err := os.ReadFile(configPath)
	require.NoError(t, err)

	// 模拟 CC 自己写入了设备/会话状态；再次 prepare 不得覆盖。
	updated := append(append([]byte(nil), original...), []byte("\n")...)
	require.NoError(t, os.WriteFile(configPath, updated, 0o600))

	_, err = runtime.prepareHome(account)
	require.NoError(t, err)
	after, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, string(updated), string(after), "existing .claude.json must be preserved")
}

func TestClaudeCodeRuntime_RequiresAccessToken(t *testing.T) {
	runtime, _ := newTestClaudeCodeRuntime(t)
	account := &Account{ID: 44, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{}}

	_, err := runtime.prepareHome(account)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access_token")
}

func TestClaudeCodeRuntime_Enabled(t *testing.T) {
	runtime, cfg := newTestClaudeCodeRuntime(t)
	assert.True(t, runtime.Enabled())

	cfg.ProcessPool.CCTransportEnabled = false
	assert.False(t, runtime.Enabled())

	cfg.ProcessPool.CCTransportEnabled = true
	cfg.ProcessPool.ClaudeBinary = ""
	assert.False(t, runtime.Enabled())

	cfg.ProcessPool.ClaudeBinary = "/bin/true"
	cfg.ProcessPool.ForwarderJS = ""
	runtimeWithoutForwarder := NewClaudeCodeRuntime(cfg, nil, nil, NewProcessPool(cfg), nil)
	assert.False(t, runtimeWithoutForwarder.Enabled())
}

func TestCredentialStringSlice(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, credentialStringSlice([]any{"a", "b"}))
	assert.Equal(t, []string{"a", "b"}, credentialStringSlice([]string{"a", "b"}))
	assert.Equal(t, []string{"a", "b"}, credentialStringSlice("a b"))
	assert.Equal(t, []string{"a", "b"}, credentialStringSlice("a,b"))
	assert.Empty(t, credentialStringSlice(nil))
	assert.Empty(t, credentialStringSlice(123))
}

func TestClaudeTokenProvider_EnsureFreshWithoutRefreshAPIReturnsAccount(t *testing.T) {
	provider := &ClaudeTokenProvider{}
	account := &Account{ID: 99}
	got, err := provider.EnsureFresh(context.Background(), account, time.Hour)
	require.NoError(t, err)
	assert.Same(t, account, got)
}
