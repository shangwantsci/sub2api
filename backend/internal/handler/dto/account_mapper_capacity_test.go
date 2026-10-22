package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func claudeAccount(accountType string, concurrency int, extra map[string]any) *service.Account {
	return &service.Account{
		ID:          1,
		Platform:    service.PlatformAnthropic,
		Type:        accountType,
		Concurrency: concurrency,
		Extra:       extra,
	}
}

func marshalAccountDTO(t *testing.T, account *service.Account) map[string]any {
	t.Helper()
	raw, err := json.Marshal(AccountFromServiceShallow(account))
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// 有效容量三项（effective_concurrency / effective_base_rpm / throttle_factor）只对
// Claude OAuth / SetupToken 账号返回，前端的容量列直接显示它们，不再自己重算后端公式。
func TestAccountDTO_EffectiveCapacityForClaudeOAuth(t *testing.T) {
	for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeSetupToken} {
		t.Run(accountType, func(t *testing.T) {
			// 未开启动态降速：有效值 = 配置值，系数 1。
			plain := AccountFromServiceShallow(claudeAccount(accountType, 5, map[string]any{
				"base_rpm": 50, "session_window_utilization": 0.85,
			}))
			require.NotNil(t, plain.EffectiveConcurrency)
			require.NotNil(t, plain.EffectiveBaseRPM)
			require.NotNil(t, plain.ThrottleFactor)
			require.Equal(t, 5, *plain.EffectiveConcurrency)
			require.Equal(t, 50, *plain.EffectiveBaseRPM)
			require.Equal(t, 1.0, *plain.ThrottleFactor)

			// 开启动态降速 + 5h 0.85 / 7d 0.6 → 系数 0.4：并发 5→2、RPM 50→20。
			throttled := AccountFromServiceShallow(claudeAccount(accountType, 5, map[string]any{
				"adaptive_throttle": true, "base_rpm": 50,
				"session_window_utilization": 0.85, "passive_usage_7d_utilization": 0.6,
			}))
			require.Equal(t, 2, *throttled.EffectiveConcurrency)
			require.Equal(t, 20, *throttled.EffectiveBaseRPM)
			require.InDelta(t, 0.4, *throttled.ThrottleFactor, 1e-9)
		})
	}
}

// 并发 0（不限）与 base_rpm 未配置时，有效值照样是 0 而不是被「下限 1」变成限 1。
// 字段必须仍然出现（值为 0），不能因为是零值就被省略——前端要靠它显示「不限」。
func TestAccountDTO_EffectiveCapacityKeepsUnlimitedZeros(t *testing.T) {
	out := marshalAccountDTO(t, claudeAccount(service.AccountTypeOAuth, 0, map[string]any{
		"adaptive_throttle": true, "session_window_utilization": 0.99,
	}))

	require.Contains(t, out, "effective_concurrency")
	require.Contains(t, out, "effective_base_rpm")
	require.Contains(t, out, "throttle_factor")
	require.EqualValues(t, 0, out["effective_concurrency"])
	require.EqualValues(t, 0, out["effective_base_rpm"])
	require.InDelta(t, 0.1, out["throttle_factor"], 1e-9)
}

// 其它平台与 Claude 的 API Key 账号一律省略这三个字段。
func TestAccountDTO_EffectiveCapacityOmittedForOtherAccounts(t *testing.T) {
	others := []*service.Account{
		{ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Concurrency: 5},
		{ID: 3, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Concurrency: 5},
		{ID: 4, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Concurrency: 5,
			Extra: map[string]any{"adaptive_throttle": true}},
	}
	for _, account := range others {
		out := marshalAccountDTO(t, account)
		require.NotContains(t, out, "effective_concurrency", "%s/%s", account.Platform, account.Type)
		require.NotContains(t, out, "effective_base_rpm", "%s/%s", account.Platform, account.Type)
		require.NotContains(t, out, "throttle_factor", "%s/%s", account.Platform, account.Type)
	}
}

// enable_tls_fingerprint 是三态 *bool：extra 里没有这个键就不输出（缺省，由运行时的
// 伪装路径决定默认值），显式 true / false 原样输出；以前 false 会被折叠成「不输出」，
// 导致前端区分不了「缺省」与「显式关闭」，TLS 开关因此关不掉。
func TestAccountDTO_TLSFingerprintIsTriState(t *testing.T) {
	// 缺省：不输出。
	absent := marshalAccountDTO(t, claudeAccount(service.AccountTypeOAuth, 3, map[string]any{}))
	require.NotContains(t, absent, "enable_tls_fingerprint")
	require.Nil(t, AccountFromServiceShallow(claudeAccount(service.AccountTypeOAuth, 3, nil)).EnableTLSFingerprint)

	// 显式 true：输出 true。
	on := marshalAccountDTO(t, claudeAccount(service.AccountTypeOAuth, 3, map[string]any{"enable_tls_fingerprint": true}))
	require.Equal(t, true, on["enable_tls_fingerprint"])

	// 显式 false：输出 false（不是省略）。
	off := marshalAccountDTO(t, claudeAccount(service.AccountTypeOAuth, 3, map[string]any{"enable_tls_fingerprint": false}))
	require.Contains(t, off, "enable_tls_fingerprint")
	require.Equal(t, false, off["enable_tls_fingerprint"])

	// 键存在但不是 bool：运行时当作显式关闭，展示也必须是 false，不能显示成缺省（默认开）。
	for _, garbage := range []any{"true", 1, nil} {
		out := marshalAccountDTO(t, claudeAccount(service.AccountTypeOAuth, 3, map[string]any{"enable_tls_fingerprint": garbage}))
		require.Equal(t, false, out["enable_tls_fingerprint"], "raw=%v", garbage)
	}

	// 非 Claude OAuth / SetupToken 账号恒不输出。
	apiKey := marshalAccountDTO(t, &service.Account{
		ID: 9, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Extra: map[string]any{"enable_tls_fingerprint": true},
	})
	require.NotContains(t, apiKey, "enable_tls_fingerprint")
}
