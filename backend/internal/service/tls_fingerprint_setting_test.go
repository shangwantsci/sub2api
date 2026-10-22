//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func tlsAccount(accountType string, extra map[string]any) *Account {
	return &Account{ID: 1, Platform: PlatformAnthropic, Type: accountType, Extra: extra}
}

// TLS 开关是三态：缺省 / 显式 true / 显式 false。两个 Resolve 对三态的语义必须钉死：
//   - ResolveTLSProfile：只有显式 true 才伪装；缺省与 false 都不伪装；
//   - ResolveTLSProfileForClaudeMimic：显式配置优先（false 就是不伪装）；缺省时仅 Claude Code
//     伪装路径默认使用内置 TLS 指纹。
//
// 前端把开关做成三态（读 extra 原值、没动不写不删、动了才写显式布尔），正是建立在这个语义上。
func TestResolveTLSProfile_TriStateSemantics(t *testing.T) {
	svc := &TLSFingerprintProfileService{}

	cases := []struct {
		name         string
		extra        map[string]any
		plain        bool // ResolveTLSProfile 是否返回 profile
		mimic        bool // ResolveTLSProfileForClaudeMimic(account, true)
		notMimic     bool // ResolveTLSProfileForClaudeMimic(account, false)
		wantSettings *bool
	}{
		{"缺省（没有这个键）", map[string]any{}, false, true, false, nil},
		{"显式 true", map[string]any{"enable_tls_fingerprint": true}, true, true, true, boolPtr(true)},
		{"显式 false", map[string]any{"enable_tls_fingerprint": false}, false, false, false, boolPtr(false)},
		{"非 bool 的垃圾值按显式关闭处理", map[string]any{"enable_tls_fingerprint": "true"}, false, false, false, boolPtr(false)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := tlsAccount(AccountTypeOAuth, tc.extra)

			require.Equal(t, tc.plain, svc.ResolveTLSProfile(account) != nil, "ResolveTLSProfile")
			require.Equal(t, tc.mimic, svc.ResolveTLSProfileForClaudeMimic(account, true) != nil, "ClaudeMimic(mimic=true)")
			require.Equal(t, tc.notMimic, svc.ResolveTLSProfileForClaudeMimic(account, false) != nil, "ClaudeMimic(mimic=false)")

			// 管理端展示用的三态原值必须与上面的运行时语义一致。
			got := account.TLSFingerprintSetting()
			if tc.wantSettings == nil {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.Equal(t, *tc.wantSettings, *got)
			}
		})
	}
}

// setup-token 与 oauth 一样适用；其它类型的账号永远不伪装，也没有三态可言。
func TestResolveTLSProfile_SetupTokenAndOtherAccountTypes(t *testing.T) {
	svc := &TLSFingerprintProfileService{}

	setup := tlsAccount(AccountTypeSetupToken, map[string]any{})
	require.NotNil(t, svc.ResolveTLSProfileForClaudeMimic(setup, true))
	require.Nil(t, svc.ResolveTLSProfile(setup))
	require.Nil(t, setup.TLSFingerprintSetting())

	apiKey := tlsAccount(AccountTypeAPIKey, map[string]any{"enable_tls_fingerprint": true})
	require.Nil(t, svc.ResolveTLSProfile(apiKey))
	require.Nil(t, svc.ResolveTLSProfileForClaudeMimic(apiKey, true))
	require.Nil(t, apiKey.TLSFingerprintSetting())

	require.Nil(t, svc.ResolveTLSProfile(nil))
	require.Nil(t, svc.ResolveTLSProfileForClaudeMimic(nil, true))
}
