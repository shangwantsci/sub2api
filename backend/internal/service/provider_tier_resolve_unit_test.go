//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func tierSettings() ProviderSettings {
	return ProviderSettings{CapacityTiers: DefaultProviderCapacityTiers()}
}

// 参数正好等于某个固定档时，标签就是那个档。
func TestResolveProviderAccountTierMatchesFixedTier(t *testing.T) {
	// 3 档种子值：并发 3 / 会话 3 / RPM 30 / 5h 上限 60。
	acc := &Account{
		Concurrency: 3,
		Extra: map[string]any{
			"max_sessions":      3,
			"base_rpm":          30,
			"window_cost_limit": 60,
		},
	}

	tier, label := ResolveProviderAccountTier(tierSettings(), acc)
	require.Equal(t, "3", tier)
	require.Equal(t, "3 档", label)
}

// 最高档的 5h 上限种子值就是 0（表示不限），必须能正常匹配上，
// 不能因为「0 看着像没设置」就判成自定义。
func TestResolveProviderAccountTierMatchesTierWithZeroWindowLimit(t *testing.T) {
	acc := &Account{
		Concurrency: 8,
		Extra: map[string]any{
			"max_sessions":      8,
			"base_rpm":          80,
			"window_cost_limit": 0,
		},
	}

	tier, label := ResolveProviderAccountTier(tierSettings(), acc)
	require.Equal(t, "5", tier)
	require.Equal(t, "5 档", label)
}

// 这是整个改动的理由：管理员在管理端把账号参数改掉之后，标签不能再撒谎。
//
// provider_tier 列仍然写着 "3"（管理端既没有 API 也没有 UI 能改它），
// 但实际并发已经是 1000 —— 生产上真实存在这样的账号。
func TestResolveProviderAccountTierIgnoresStaleProviderTierColumn(t *testing.T) {
	stale := "3"
	acc := &Account{
		ProviderTier: &stale,
		Concurrency:  1000,
		Extra:        map[string]any{},
	}

	tier, label := ResolveProviderAccountTier(tierSettings(), acc)
	require.Equal(t, ProviderTierCustom, tier,
		"参数早就不是 3 档了，不能因为列里写着 3 就显示 3 档")
	require.Empty(t, label, "自定义档的文案交给前端 i18n，后端不回硬编码中文")
}

// 四项里任何一项对不上都不算这个档 —— 宁可显示自定义，也不给只对了一半的标签。
func TestResolveProviderAccountTierRequiresAllFourParams(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"max_sessions": 3, "base_rpm": 30, "window_cost_limit": 60}
	}

	cases := []struct {
		name        string
		concurrency int
		mutate      func(map[string]any)
	}{
		{"并发对不上", 4, func(map[string]any) {}},
		{"会话数对不上", 3, func(e map[string]any) { e["max_sessions"] = 5 }},
		{"RPM 对不上", 3, func(e map[string]any) { e["base_rpm"] = 60 }},
		{"5h 上限对不上", 3, func(e map[string]any) { e["window_cost_limit"] = 100 }},
		{"缺一项", 3, func(e map[string]any) { delete(e, "max_sessions") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			extra := base()
			tc.mutate(extra)
			acc := &Account{Concurrency: tc.concurrency, Extra: extra}

			tier, _ := ResolveProviderAccountTier(tierSettings(), acc)
			require.Equal(t, ProviderTierCustom, tier)
		})
	}
}

// 档位被管理员停用后，已经在用它的账号参数并没有变，显示原档位名仍然准确。
// 停用只该影响「能不能换到该档」，不该让存量账号的标签突然变成自定义。
func TestResolveProviderAccountTierStillLabelsDisabledTier(t *testing.T) {
	settings := tierSettings()
	for i := range settings.CapacityTiers {
		settings.CapacityTiers[i].Enabled = false
	}

	acc := &Account{
		Concurrency: 3,
		Extra: map[string]any{
			"max_sessions":      3,
			"base_rpm":          30,
			"window_cost_limit": 60,
		},
	}

	tier, label := ResolveProviderAccountTier(settings, acc)
	require.Equal(t, "3", tier)
	require.Equal(t, "3 档", label)
}

func TestResolveProviderAccountTierHandlesNilAccount(t *testing.T) {
	tier, label := ResolveProviderAccountTier(tierSettings(), nil)
	require.Empty(t, tier)
	require.Empty(t, label)
}
