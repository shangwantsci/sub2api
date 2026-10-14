//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompleteClaudeMachineProfileStableForDeviceID(t *testing.T) {
	deviceID := strings.Repeat("ab", 32)
	extra := map[string]any{extraClaudeDeviceID: deviceID}

	first := CompleteClaudeMachineProfile(extra, "")
	second := CompleteClaudeMachineProfile(first.extraPatch(), "")

	require.Equal(t, first, second)
	require.Equal(t, deviceID, first.DeviceID)
	require.Equal(t, "linux", first.Platform)
	require.True(t, first.Shell == "bash" || first.Shell == "zsh")
	require.True(t, strings.HasPrefix(first.OSVersion, "Linux "))
	require.True(t, strings.HasPrefix(first.Home, "/home/"))
	require.Equal(t, first.Home+"/projects", first.WorkingDir())
}

func TestCompleteClaudeMachineProfileAdoptsExistingDeviceID(t *testing.T) {
	adopted := strings.Repeat("cd", 32)
	profile := CompleteClaudeMachineProfile(nil, adopted)
	require.Equal(t, adopted, profile.DeviceID)

	again := CompleteClaudeMachineProfile(profile.extraPatch(), strings.Repeat("ef", 32))
	require.Equal(t, profile, again)
}

func TestCreateAccountWritesClaudeMachineProfileOnce(t *testing.T) {
	repo := &longContextBillingRepoStub{}
	svc := &adminServiceImpl{accountRepo: repo}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "claude",
		Platform:             PlatformAnthropic,
		Type:                 AccountTypeOAuth,
		Credentials:          map[string]any{"access_token": "token"},
		SkipDefaultGroupBind: true,
	})
	require.NoError(t, err)
	profile, ok := ParseClaudeMachineProfile(account.Extra)
	require.True(t, ok)

	again := applyClaudeMachineProfile(account.Extra)
	second, ok := ParseClaudeMachineProfile(again)
	require.True(t, ok)
	require.Equal(t, profile, second)
}

func TestCreateAccountSkipsMachineProfileForAPIKey(t *testing.T) {
	repo := &longContextBillingRepoStub{}
	svc := &adminServiceImpl{accountRepo: repo}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "key",
		Platform:             PlatformAnthropic,
		Type:                 AccountTypeAPIKey,
		Credentials:          map[string]any{"api_key": "sk"},
		SkipDefaultGroupBind: true,
	})
	require.NoError(t, err)
	_, ok := ParseClaudeMachineProfile(account.Extra)
	require.False(t, ok)
}

func TestUpdateAccountPreservesClaudeMachineProfile(t *testing.T) {
	deviceID := strings.Repeat("ab", 32)
	extra := CompleteClaudeMachineProfile(map[string]any{extraClaudeDeviceID: deviceID}, "").extraPatch()
	repo := &longContextBillingRepoStub{account: &Account{
		ID:       1,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra:    extra,
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	updated, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		Extra: map[string]any{"unrelated": true},
	})
	require.NoError(t, err)
	require.Equal(t, deviceID, updated.Extra[extraClaudeDeviceID])
	require.Equal(t, extra[extraClaudeEnvHome], updated.Extra[extraClaudeEnvHome])
	require.Equal(t, extra[extraClaudeEnvShell], updated.Extra[extraClaudeEnvShell])
	require.Equal(t, true, updated.Extra["unrelated"])
}

type machineProfileRepo struct {
	accountRepoStub
	updates int
	err     error
}

func (r *machineProfileRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	r.updates++
	return r.err
}

type machineProfileCache struct {
	fp   *Fingerprint
	sets int
}

func (c *machineProfileCache) GetFingerprint(context.Context, int64) (*Fingerprint, error) {
	if c.fp == nil {
		return nil, errors.New("missing")
	}
	return c.fp, nil
}
func (c *machineProfileCache) SetFingerprint(_ context.Context, _ int64, fp *Fingerprint) error {
	c.fp = fp
	c.sets++
	return nil
}
func (c *machineProfileCache) GetMaskedSessionID(context.Context, int64) (string, error) {
	return "", nil
}
func (c *machineProfileCache) SetMaskedSessionID(context.Context, int64, string) error {
	return nil
}

func TestEnsureClaudeMachineProfileAdoptsRedisClientID(t *testing.T) {
	adopted := strings.Repeat("ef", 32)
	cache := &machineProfileCache{fp: &Fingerprint{ClientID: adopted}}
	account := &Account{ID: 4, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{}}
	repo := &machineProfileRepo{}
	svc := &GatewayService{accountRepo: repo, identityService: NewIdentityService(cache)}

	profile, ok := svc.ensureClaudeMachineProfile(context.Background(), account)
	require.True(t, ok)
	require.Equal(t, adopted, profile.DeviceID)
	require.Equal(t, adopted, account.Extra[extraClaudeDeviceID])
	require.Equal(t, 1, repo.updates)

	again, ok := svc.ensureClaudeMachineProfile(context.Background(), account)
	require.True(t, ok)
	require.Equal(t, profile, again)
	require.Equal(t, 1, repo.updates)
}

func TestEnsureClaudeMachineProfilePersistFailureLeavesAccountUntouched(t *testing.T) {
	account := &Account{ID: 4, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{}}
	repo := &machineProfileRepo{err: errors.New("db down")}
	svc := &GatewayService{accountRepo: repo, identityService: NewIdentityService(&machineProfileCache{})}

	_, ok := svc.ensureClaudeMachineProfile(context.Background(), account)
	require.False(t, ok)
	_, parsed := ParseClaudeMachineProfile(account.Extra)
	require.False(t, parsed)
	require.NotContains(t, account.Extra, extraClaudeDeviceID)
}

func TestAlignClientIDRewritesMetadataDeviceID(t *testing.T) {
	deviceID := strings.Repeat("ab", 32)
	oldID := strings.Repeat("cd", 32)
	cache := &machineProfileCache{fp: &Fingerprint{ClientID: oldID}}
	identity := NewIdentityService(cache)
	fp := &Fingerprint{ClientID: oldID, UserAgent: "claude-cli/2.1.78 (external, cli)"}
	account := &Account{ID: 9, Extra: map[string]any{extraClaudeDeviceID: deviceID}}
	alignClaudeDeviceID(context.Background(), &GatewayService{identityService: identity}, account, fp)

	require.Equal(t, deviceID, fp.ClientID)
	require.Equal(t, deviceID, cache.fp.ClientID)

	original := FormatMetadataUserID(oldID, "acct", "7578cf37-aaca-46e4-a45c-71285d9dbb83", "2.1.78")
	body := []byte(`{"metadata":{"user_id":` + strconvQuote(original) + `}}`)
	out, err := identity.RewriteUserID(body, 9, "acct-uuid", fp.ClientID, fp.UserAgent)
	require.NoError(t, err)
	require.Contains(t, string(out), deviceID)
	require.NotContains(t, string(out), oldID)
}

func TestPinClaudeMachineHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	require.NoError(t, err)
	req.Header.Set("X-Stainless-OS", "MacOS")
	req.Header.Set("X-Stainless-Arch", "arm64")

	pinClaudeMachineHeaders(req)

	require.Equal(t, "Linux", getHeaderRaw(req.Header, "X-Stainless-OS"))
	require.Equal(t, "x64", getHeaderRaw(req.Header, "X-Stainless-Arch"))
}
