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

type writebackRepo struct {
	AccountRepository
	account  *Account
	applied  bool
	calls    int
	gotCreds map[string]any
}

func (r *writebackRepo) GetByID(context.Context, int64) (*Account, error) { return r.account, nil }

func (r *writebackRepo) UpdateClaudeOAuthCredentialsIfUnchanged(
	_ context.Context,
	_ int64,
	_ map[string]any,
	_ *int64,
	_ string,
	credentials map[string]any,
) (bool, error) {
	r.calls++
	r.gotCreds = credentials
	return r.applied, nil
}

func newWritebackRuntime(t *testing.T, repo *writebackRepo, account *Account) *ClaudeCodeRuntime {
	t.Helper()
	cfg := &config.Config{}
	cfg.ProcessPool.DataDir = t.TempDir()
	runtime := NewClaudeCodeRuntime(cfg, repo, nil, nil, nil)
	home := runtime.homeDir(account)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o700))
	return runtime
}

func writeHomeCredentials(t *testing.T, runtime *ClaudeCodeRuntime, account *Account, access, refresh string, expiresAtMillis int64) {
	t.Helper()
	payload := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  access,
			"refreshToken": refresh,
			"expiresAt":    expiresAtMillis,
			"scopes":       []string{"user:inference", "user:sessions:claude_code"},
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runtime.homeDir(account), ".claude", ".credentials.json"), raw, 0o600))
}

func writebackAccount(expiresAt time.Time) *Account {
	return &Account{
		ID:       77,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "old-access",
			"refresh_token": "old-refresh",
			"expires_at":    float64(expiresAt.Unix()),
			"scopes":        []any{"user:inference"},
		},
	}
}

func TestClaudeCodeRuntime_WritebackPersistsNewerCredentials(t *testing.T) {
	account := writebackAccount(time.Now().Add(time.Hour))
	repo := &writebackRepo{account: account, applied: true}
	runtime := newWritebackRuntime(t, repo, account)
	newExpiry := time.Now().Add(4 * time.Hour).UnixMilli()
	writeHomeCredentials(t, runtime, account, "new-access", "new-refresh", newExpiry)

	runtime.persistRefreshedCredentials(context.Background(), account)

	require.Equal(t, 1, repo.calls)
	assert.Equal(t, "new-access", repo.gotCreds["access_token"])
	assert.Equal(t, "new-refresh", repo.gotCreds["refresh_token"])
	assert.Equal(t, newExpiry/1000, repo.gotCreds["expires_at"])
	scopes, ok := repo.gotCreds["scopes"].([]string)
	require.True(t, ok)
	assert.Contains(t, scopes, "user:inference")
}

func TestClaudeCodeRuntime_WritebackSkipsUnchangedCredentials(t *testing.T) {
	account := writebackAccount(time.Now().Add(time.Hour))
	repo := &writebackRepo{account: account, applied: true}
	runtime := newWritebackRuntime(t, repo, account)
	writeHomeCredentials(t, runtime, account, "old-access", "old-refresh", time.Now().Add(time.Hour).UnixMilli())

	runtime.persistRefreshedCredentials(context.Background(), account)

	assert.Zero(t, repo.calls)
}

func TestClaudeCodeRuntime_WritebackSkipsDatabaseNewer(t *testing.T) {
	account := writebackAccount(time.Now().Add(4 * time.Hour))
	repo := &writebackRepo{account: account, applied: true}
	runtime := newWritebackRuntime(t, repo, account)
	writeHomeCredentials(t, runtime, account, "stale-access", "stale-refresh", time.Now().Add(time.Hour).UnixMilli())

	runtime.persistRefreshedCredentials(context.Background(), account)

	assert.Zero(t, repo.calls)
}

func TestClaudeCodeRuntime_WritebackHonorsCASMiss(t *testing.T) {
	account := writebackAccount(time.Now().Add(time.Hour))
	repo := &writebackRepo{account: account, applied: false}
	runtime := newWritebackRuntime(t, repo, account)
	writeHomeCredentials(t, runtime, account, "new-access", "new-refresh", time.Now().Add(2*time.Hour).UnixMilli())

	runtime.persistRefreshedCredentials(context.Background(), account)

	assert.Equal(t, 1, repo.calls)
}
