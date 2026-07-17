package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

const validCalibratedProfileRawForTest = `{
  "schema_version": 1,
  "cli_version": "2.1.212",
  "captured_at": "2026-07-16T00:00:00Z",
  "source": "cc-calibrate",
  "headers": {
    "template": {"User-Agent": "claude-cli/2.1.212 (external, sdk-cli)", "X-Stainless-OS": "Linux"},
    "absent": ["x-client-request-id"]
  },
  "beta_rules": {"messages|sonnet|": ["claude-code-20250219", "interleaved-thinking-2025-05-14"]},
  "guard": {"salt_verified": true, "checked": 2, "ok": 2}
}`

func TestSettingService_ClaudeCalibratedProfile_Lifecycle(t *testing.T) {
	ctx := context.Background()
	repo := &gatewayTTLSettingRepo{data: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})

	// Unpublished -> nil (gateway falls back to compiled-in constants).
	require.Nil(t, svc.GetClaudeCalibratedProfile(ctx))

	// Publish a valid profile -> readable immediately, version comes through.
	published, err := svc.PublishClaudeCalibratedProfile(ctx, []byte(validCalibratedProfileRawForTest))
	require.NoError(t, err)
	require.Equal(t, "2.1.212", published.Version())

	got := svc.GetClaudeCalibratedProfile(ctx)
	require.NotNil(t, got)
	require.Equal(t, "2.1.212", got.Version())
	require.Equal(t, validCalibratedProfileRawForTest, repo.data[SettingKeyClaudeCodeCalibratedProfile])

	// Clear -> back to nil.
	require.NoError(t, svc.ClearClaudeCalibratedProfile(ctx))
	require.Nil(t, svc.GetClaudeCalibratedProfile(ctx))
}

func TestSettingService_PublishClaudeCalibratedProfile_RejectsGuardFailure(t *testing.T) {
	ctx := context.Background()
	repo := &gatewayTTLSettingRepo{data: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})

	guardFail := `{
      "schema_version": 1,
      "cli_version": "2.1.212",
      "headers": {"template": {"User-Agent": "claude-cli/2.1.212 (external, sdk-cli)"}, "absent": []},
      "beta_rules": {"messages|sonnet|": ["claude-code-20250219"]},
      "guard": {"salt_verified": false, "checked": 2, "ok": 0}
    }`
	_, err := svc.PublishClaudeCalibratedProfile(ctx, []byte(guardFail))
	require.Error(t, err, "a profile whose fingerprint guard failed must be rejected")
	require.Empty(t, repo.data[SettingKeyClaudeCodeCalibratedProfile], "rejected profile must not be persisted")
	require.Nil(t, svc.GetClaudeCalibratedProfile(ctx))
}

func TestSettingService_GetClaudeCalibratedProfile_InvalidStoredValueFallsBack(t *testing.T) {
	ctx := context.Background()
	// Simulate a corrupt/incompatible value written out-of-band (e.g. schema drift).
	repo := &gatewayTTLSettingRepo{data: map[string]string{
		SettingKeyClaudeCodeCalibratedProfile: `{"schema_version": 999, "cli_version": "9.9.9"}`,
	}}
	svc := NewSettingService(repo, &config.Config{})

	require.Nil(t, svc.GetClaudeCalibratedProfile(ctx),
		"an invalid stored profile must never reach the wire; loader returns nil to fall back to constants")
}
