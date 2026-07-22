//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type rateLimit429AccountRepoStub struct {
	mockAccountRepoForGemini
	rateLimitCalls     int
	lastRateLimitID    int64
	lastRateLimitReset time.Time
}

type adaptiveAnthropic429CacheStub struct {
	TempUnschedCache
	streak     int
	resetCalls int
	state      *TempUnschedState
}

func (c *adaptiveAnthropic429CacheStub) SetTempUnsched(_ context.Context, _ int64, state *TempUnschedState) error {
	c.state = state
	return nil
}

func (c *adaptiveAnthropic429CacheStub) GetTempUnsched(context.Context, int64) (*TempUnschedState, error) {
	return c.state, nil
}

func (c *adaptiveAnthropic429CacheStub) DeleteTempUnsched(context.Context, int64) error {
	c.state = nil
	return nil
}

func (c *adaptiveAnthropic429CacheStub) NextAnthropicOpaque429Backoff(context.Context, int64) (int, error) {
	c.streak++
	return c.streak, nil
}

func (c *adaptiveAnthropic429CacheStub) ResetAnthropicOpaque429Backoff(context.Context, int64) error {
	c.resetCalls++
	c.streak = 0
	return nil
}

func (r *rateLimit429AccountRepoStub) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.rateLimitCalls++
	r.lastRateLimitID = id
	r.lastRateLimitReset = resetAt
	return nil
}

func TestGetRateLimit429CooldownSettings_DefaultsWhenNotSet(t *testing.T) {
	repo := newMockSettingRepo()
	svc := NewSettingService(repo, &config.Config{})

	settings, err := svc.GetRateLimit429CooldownSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.Enabled)
	require.Equal(t, 5, settings.CooldownSeconds)
}

func TestGetRateLimit429CooldownSettings_ReadsFromDB(t *testing.T) {
	repo := newMockSettingRepo()
	data, _ := json.Marshal(RateLimit429CooldownSettings{Enabled: false, CooldownSeconds: 12})
	repo.data[SettingKeyRateLimit429CooldownSettings] = string(data)
	svc := NewSettingService(repo, &config.Config{})

	settings, err := svc.GetRateLimit429CooldownSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, 12, settings.CooldownSeconds)
}

func TestSetRateLimit429CooldownSettings_EnabledRejectsOutOfRange(t *testing.T) {
	svc := NewSettingService(newMockSettingRepo(), &config.Config{})

	for _, seconds := range []int{0, -1, 7201, 99999} {
		err := svc.SetRateLimit429CooldownSettings(context.Background(), &RateLimit429CooldownSettings{
			Enabled: true, CooldownSeconds: seconds,
		})
		require.Error(t, err, "should reject enabled=true + cooldown_seconds=%d", seconds)
		require.Contains(t, err.Error(), "cooldown_seconds must be between 1-7200")
	}
}

func TestHandle429_FallbackUsesDBSeconds(t *testing.T) {
	accountRepo := &rateLimit429AccountRepoStub{}
	settingRepo := newMockSettingRepo()
	data, _ := json.Marshal(RateLimit429CooldownSettings{Enabled: true, CooldownSeconds: 12})
	settingRepo.data[SettingKeyRateLimit429CooldownSettings] = string(data)

	settingSvc := NewSettingService(settingRepo, &config.Config{})
	svc := NewRateLimitService(accountRepo, nil, &config.Config{}, nil, nil)
	svc.SetSettingService(settingSvc)

	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	before := time.Now()
	svc.handle429(context.Background(), account, http.Header{}, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, accountRepo.rateLimitCalls)
	require.Equal(t, int64(42), accountRepo.lastRateLimitID)
	require.True(t, !accountRepo.lastRateLimitReset.Before(before.Add(12*time.Second)) && !accountRepo.lastRateLimitReset.After(after.Add(12*time.Second)))
}

func TestHandle429_FallbackDisabledSkipsLocalMark(t *testing.T) {
	accountRepo := &rateLimit429AccountRepoStub{}
	settingRepo := newMockSettingRepo()
	data, _ := json.Marshal(RateLimit429CooldownSettings{Enabled: false, CooldownSeconds: 12})
	settingRepo.data[SettingKeyRateLimit429CooldownSettings] = string(data)

	settingSvc := NewSettingService(settingRepo, &config.Config{})
	svc := NewRateLimitService(accountRepo, nil, &config.Config{}, nil, nil)
	svc.SetSettingService(settingSvc)

	account := &Account{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	svc.handle429(context.Background(), account, http.Header{}, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))

	require.Zero(t, accountRepo.rateLimitCalls)
}

// 管理端关闭兜底冷却时，Anthropic 无 reset 头的 429 保持旧行为：不标记账号。
func TestHandle429_AnthropicNoResetTimeFallbackDisabledSkipsMark(t *testing.T) {
	accountRepo := &rateLimit429AccountRepoStub{}
	settingRepo := newMockSettingRepo()
	data, _ := json.Marshal(RateLimit429CooldownSettings{Enabled: false, CooldownSeconds: 12})
	settingRepo.data[SettingKeyRateLimit429CooldownSettings] = string(data)

	settingSvc := NewSettingService(settingRepo, &config.Config{})
	svc := NewRateLimitService(accountRepo, nil, &config.Config{}, nil, nil)
	svc.SetSettingService(settingSvc)

	account := &Account{ID: 46, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	svc.handle429(context.Background(), account, http.Header{}, []byte(`{"error":{"type":"rate_limit_error","message":"Extra usage required"}}`))

	require.Zero(t, accountRepo.rateLimitCalls)
}

func TestHandle429_FallbackUsesDefaultSecondsWhenSettingServiceMissing(t *testing.T) {
	accountRepo := &rateLimit429AccountRepoStub{}
	cfg := &config.Config{}
	svc := NewRateLimitService(accountRepo, nil, cfg, nil, nil)

	account := &Account{ID: 44, Platform: PlatformGemini, Type: AccountTypeAPIKey}
	before := time.Now()
	svc.handle429(context.Background(), account, http.Header{}, []byte(`{"error":{"message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, accountRepo.rateLimitCalls)
	require.Equal(t, int64(44), accountRepo.lastRateLimitID)
	require.True(t, !accountRepo.lastRateLimitReset.Before(before.Add(5*time.Second)) && !accountRepo.lastRateLimitReset.After(after.Add(5*time.Second)))
}

func TestHandle429_AnthropicNoResetUsesRuntimeCooldownOnly(t *testing.T) {
	accountRepo := &rateLimit429AccountRepoStub{}
	svc := NewRateLimitService(accountRepo, nil, &config.Config{}, nil, nil)

	account := &Account{ID: 45, Platform: PlatformAnthropic, Type: AccountTypeSetupToken}
	svc.handle429(context.Background(), account, http.Header{}, []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))

	require.Zero(t, accountRepo.rateLimitCalls, "无 reset 的 Anthropic 429 不应写入数据库长期限流")
	require.True(t, svc.IsAccountRuntimeSchedulingBlocked(context.Background(), account), "无 reset 的 Anthropic 429 应进入短运行时冷却")
}

func TestHandle429_AnthropicOpaque429UsesAdaptiveCooldownAndResetsOnSuccess(t *testing.T) {
	accountRepo := &rateLimit429AccountRepoStub{}
	cache := &adaptiveAnthropic429CacheStub{}
	svc := NewRateLimitService(accountRepo, nil, &config.Config{}, nil, cache)
	account := &Account{ID: 45, Platform: PlatformAnthropic, Type: AccountTypeSetupToken}
	steps := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute}

	for i, minimum := range steps {
		before := time.Now()
		svc.handle429(context.Background(), account, http.Header{}, []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Error"}}`))

		raw, ok := svc.accountRuntimeBlocks.Load(account.ID)
		require.True(t, ok)
		block, ok := raw.(accountRuntimeBlock)
		require.True(t, ok)
		cooldown := block.Until.Sub(before)
		require.GreaterOrEqual(t, cooldown, minimum, "streak %d", i+1)
		require.LessOrEqual(t, cooldown, minimum+minimum/10+time.Second, "streak %d", i+1)
	}

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	svc.UpdateSessionWindow(context.Background(), account, headers)
	require.Equal(t, 1, cache.resetCalls)
	require.Zero(t, cache.streak)
	require.Nil(t, cache.state)
	require.False(t, svc.IsAccountRuntimeSchedulingBlocked(context.Background(), account))
}
