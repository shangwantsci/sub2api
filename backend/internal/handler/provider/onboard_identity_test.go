//go:build unit

package provider

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 批量上号时供号商一次粘贴多行 session key，没法逐个填名字。
// 自动命名必须给出能和他手上号单对照的标识，而不是「账号 1 / 账号 2」。
func TestResolveOnboardAccountName(t *testing.T) {
	cases := []struct {
		name  string
		input string
		token *service.TokenInfo
		want  string
	}{
		{
			name:  "填了名字就用填的，不被邮箱覆盖",
			input: "  我的主力号  ",
			token: &service.TokenInfo{EmailAddress: "a@example.com"},
			want:  "我的主力号",
		},
		{
			name:  "留空时用换票拿到的邮箱",
			input: "",
			token: &service.TokenInfo{EmailAddress: " supplier@example.com "},
			want:  "supplier@example.com",
		},
		{
			name:  "邮箱为空时退回账号 UUID 前 8 位",
			input: "   ",
			token: &service.TokenInfo{AccountUUID: "abcdef01-2345-6789"},
			want:  "Anthropic abcdef01",
		},
		{
			// 不编造名字：交给 BuildProviderAccountInput 报 NAME_REQUIRED，
			// 让供号商知道这条要手填，而不是拿到一个无从对照的占位名。
			name:  "邮箱与 UUID 都拿不到时返回空",
			input: "",
			token: &service.TokenInfo{},
			want:  "",
		},
		{
			name:  "换票结果为 nil 时不 panic",
			input: "",
			token: nil,
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, resolveOnboardAccountName(tc.input, tc.token))
		})
	}
}

// 身份字段优先读 extra（网关只认那一份），但必须能回落 credentials：
// MirrorProviderIdentityToExtra 上线之前建的存量账号 extra 里没有这些键。
func TestProviderAccountIdentityFallsBackToCredentials(t *testing.T) {
	t.Run("extra 优先", func(t *testing.T) {
		acc := &service.Account{
			Extra:       map[string]any{"account_uuid": "extra-uuid", "email_address": "extra@example.com"},
			Credentials: map[string]any{"account_uuid": "creds-uuid", "email_address": "creds@example.com"},
		}
		require.Equal(t, "extra-uuid", providerAccountUUID(acc))
		require.Equal(t, "extra@example.com", providerAccountEmail(acc))
	})

	t.Run("extra 缺失时回落 credentials", func(t *testing.T) {
		acc := &service.Account{
			Credentials: map[string]any{"account_uuid": " creds-uuid ", "email_address": " creds@example.com "},
		}
		require.Equal(t, "creds-uuid", providerAccountUUID(acc))
		require.Equal(t, "creds@example.com", providerAccountEmail(acc))
	})

	t.Run("两边都没有时返回空而不是 panic", func(t *testing.T) {
		require.Empty(t, providerAccountUUID(&service.Account{}))
		require.Empty(t, providerAccountEmail(&service.Account{}))
		require.Empty(t, providerAccountUUID(nil))
		require.Empty(t, providerAccountEmail(nil))
	})
}

// 这轮改动之前上的号（extra 里没有身份镜像、从未跑过流量）必须照常显示，
// 不能因为缺字段就渲染成空白或炸掉。
//
// 生产上确实存在这批账号：MirrorProviderIdentityToExtra 是后加的，
// 在那之前建的账号身份字段只在 credentials 里。
func TestAccountViewHandlesPreExistingAccounts(t *testing.T) {
	legacy := &service.Account{
		ID:       7,
		Name:     "老账号",
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeOAuth,
		Status:   service.StatusActive,
		// extra 完全是空的：没有身份镜像，也没有被动采样。
		Extra: map[string]any{},
		Credentials: map[string]any{
			"access_token":  "tok",
			"email_address": "legacy@example.com",
			"account_uuid":  "legacy-uuid",
		},
		Schedulable: true,
		// SessionWindowStart/End 为 nil：这个号从没被调度过。
		// ProviderTier 为 nil：档位列是后加的。
	}

	view := AccountViewFromService(legacy, service.ProviderSettings{}, nil)

	require.Equal(t, "老账号", view.Name)
	require.Equal(t, "legacy@example.com", view.Email, "邮箱要能从 credentials 回落读出来")
	require.Equal(t, "active", view.Status)
	require.Empty(t, view.Tier, "档位为空是允许的，编辑时供号商自己选一个")
	require.True(t, view.PeriodCost.IsZero())

	// 去重也要能认出这个号，否则同一个 Anthropic 账号会被重复上一遍。
	require.Equal(t, "legacy-uuid", providerAccountUUID(legacy))
}

// 额度用量视图是白名单收敛，不是排除法。
//
// service.UsageInfo 上挂着各平台的一堆字段和三种口径的金额，其中 Cost 含账号倍率、
// UserCost 是向客户收的价 —— 任何一个漏出去都等于把平台定价告诉供号商。
func TestAccountUsageViewCarriesNoMoney(t *testing.T) {
	info := &service.UsageInfo{
		Source:   "passive",
		FiveHour: &service.UsageProgress{Utilization: 42.5},
	}
	// WindowStats 走公开构造，确保金额字段确实有值、不是因为零值才没漏。
	info.FiveHour.WindowStats = &service.WindowStats{
		Requests: 120, Tokens: 34000,
		Cost: 9.99, StandardCost: 8.88, UserCost: 12.34,
	}

	view := AccountUsageViewFromService(info)
	require.NotNil(t, view.FiveHour)
	require.Equal(t, 42.5, view.FiveHour.Utilization)
	require.Equal(t, int64(120), view.FiveHour.Requests)
	require.Equal(t, int64(34000), view.FiveHour.Tokens)

	raw, err := json.Marshal(view)
	require.NoError(t, err)
	payload := string(raw)
	for _, needle := range []string{"cost", "9.99", "8.88", "12.34"} {
		require.NotContains(t, payload, needle,
			"usage view must not carry any money field; payload=%s", payload)
	}
}

// 上游没给某个窗口时不能凭空造一个 0%，那会显示成「这个号完全没用过」。
func TestAccountUsageViewKeepsMissingWindowsNil(t *testing.T) {
	view := AccountUsageViewFromService(&service.UsageInfo{Source: "passive"})
	require.Nil(t, view.FiveHour)
	require.Nil(t, view.SevenDay)
	require.Nil(t, view.SevenDaySonnet)
	require.Nil(t, view.SevenDayFable)

	require.Equal(t, AccountUsageView{}, AccountUsageViewFromService(nil))
}
