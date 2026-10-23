//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 调度投影契约：选号阶段读到的「精简账号」必须和完整账号得出同样的判定。
//
// 背景：候选过滤读的是 buildSchedulerMetadataAccount 产出的精简投影（调度快照命中时），
// 投影白名单漏掉的 extra 键在选号阶段读出来是空值。对限流类配置来说，空值等于「没配置」，
// 于是 RPM 门、动态降速等会静默放行（缓存未命中时回退读库，所以现象是间歇性的）。
// 单测直接构造完整账号，抓不到这类问题；这里用「完整账号 vs 经 JSON 往返的投影账号」对拍。
//
// 新增选号阶段读取的 extra 键时：登记到 service.SchedulerProjectedExtraKeys，并在
// contractScenarios 里加一行用例。

// capacityVerdict 汇总选号阶段会读取的容量判定。
type capacityVerdict struct {
	Concurrency  int
	LoadCapacity int
	BaseRPM      int
	RPMStrategy  string
	StickyBuffer int
	RPMBands     []service.WindowCostSchedulability
	MaxSessions  int
	IdleTimeout  int
	WindowLimit  float64
	WindowBands  []service.WindowCostSchedulability

	// IsSchedulable 里 API Key / Bedrock 账号自设额度（总 / 日 / 周）的判定。
	QuotaExceeded bool
	// 候选过滤的模型支持判断里 OpenAI 透传开关（新键 + 历史兼容键）的判定。
	OpenAIPassthrough bool
}

// verdictOf 是被对拍的判定函数，只调用选号阶段真正读取的账号方法：
// 硬并发上限 EffectiveConcurrency、负载分母 LoadBalanceCapacity、降速后的 RPM 上限 EffectiveBaseRPM，
// RPM / 窗口费用的区带判定，以及 IsSchedulable 里读的额度判定 IsQuotaExceeded、
// 候选过滤里读的 OpenAI 透传开关 IsOpenAIPassthroughEnabled。
var verdictOf = func(a *service.Account, rpmProbes []int, costProbes []float64) capacityVerdict {
	v := capacityVerdict{
		Concurrency:       a.EffectiveConcurrency(),
		LoadCapacity:      a.LoadBalanceCapacity(),
		BaseRPM:           a.EffectiveBaseRPM(),
		RPMStrategy:       a.GetRPMStrategy(),
		StickyBuffer:      a.GetRPMStickyBuffer(),
		MaxSessions:       a.GetMaxSessions(),
		IdleTimeout:       a.GetSessionIdleTimeoutMinutes(),
		WindowLimit:       a.GetWindowCostLimit(),
		QuotaExceeded:     a.IsQuotaExceeded(),
		OpenAIPassthrough: a.IsOpenAIPassthroughEnabled(),
	}
	for _, cur := range rpmProbes {
		v.RPMBands = append(v.RPMBands, a.CheckRPMSchedulability(cur))
	}
	for _, cost := range costProbes {
		v.WindowBands = append(v.WindowBands, a.CheckWindowCostSchedulability(cost))
	}
	return v
}

// rpmProbes 以完整账号的配置生成探针，两边用同一组探针，避免投影丢键后探针也跟着变。
// 探针既覆盖配置的 base_rpm 边界，也覆盖降速后的有效 RPM 边界（未降速时两组重合）。
func rpmProbes(a *service.Account) []int {
	base := a.GetBaseRPM()
	if base <= 0 {
		return []int{0, 1, 100, 9999}
	}
	buf := a.GetRPMStickyBuffer()
	effective := a.EffectiveBaseRPM()
	seen := map[int]struct{}{}
	for _, p := range []int{
		0, base - 1, base, base + buf - 1, base + buf, 9999,
		effective - 1, effective, effective + buf - 1, effective + buf,
	} {
		if p >= 0 {
			seen[p] = struct{}{}
		}
	}
	out := make([]int, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

func costProbes(a *service.Account) []float64 {
	limit := a.GetWindowCostLimit()
	if limit <= 0 {
		return []float64{0, 1, 1000}
	}
	reserve := a.GetWindowCostStickyReserve()
	return []float64{0, limit - 0.01, limit, limit + reserve - 0.01, limit + reserve}
}

// projectAccount 走真实的投影路径：buildSchedulerMetadataAccount 后经 JSON 序列化再反序列化，
// 与写入 sched:meta、再由 decodeCachedAccount 读回等价（int 会变 float64）。
func projectAccount(t *testing.T, a service.Account) service.Account {
	t.Helper()
	payload, err := json.Marshal(buildSchedulerMetadataAccount(a))
	require.NoError(t, err)
	var restored service.Account
	require.NoError(t, json.Unmarshal(payload, &restored))
	return restored
}

// effectiveWant 是完整账号应有的降速后容量，作为前置条件钉住动态降速曲线，
// 并证明带降速的用例不是空转（有效值确实不同于账号上配置的原始值）。
type effectiveWant struct {
	concurrency  int
	loadCapacity int
	baseRPM      int
}

type contractScenario struct {
	name    string
	account service.Account
	// probe/band 是前置条件：证明这行用例不是空转（完整账号在该探针点确实落在该区带）。
	probe int
	band  service.WindowCostSchedulability
	// checkBand 为 false 时跳过前置条件（只对拍，不断言区带）。
	checkBand bool
	// want 非 nil 时断言完整账号的有效并发 / 负载分母 / 有效 RPM（前置条件）。
	want *effectiveWant
	// precondition 非 nil 时在对拍前对完整账号做断言：证明这行用例确实依赖被测的键
	// （例如「额度已用完」只在键齐全时才成立），否则投影丢键后两边都是 false，用例就空转了。
	precondition func(t *testing.T, full *service.Account)
}

func anthropicOAuth(id int64, concurrency int, extra map[string]any) service.Account {
	return service.Account{
		ID:          id,
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeOAuth,
		Concurrency: concurrency,
		Extra:       extra,
	}
}

func apiKeyAccount(id int64, platform string, extra map[string]any) service.Account {
	return service.Account{
		ID:          id,
		Platform:    platform,
		Type:        service.AccountTypeAPIKey,
		Concurrency: 3,
		Extra:       extra,
	}
}

func contractScenarios() []contractScenario {
	load20 := 20
	loadAccount := anthropicOAuth(9007, 7, nil)
	loadAccount.LoadFactor = &load20

	// 降速 + load_factor：硬并发上限按并发列缩放（5×0.4=2），负载分母按 load_factor 缩放（20×0.4=8）。
	adaptiveLoadAccount := anthropicOAuth(9012, 5, map[string]any{
		"adaptive_throttle": true, "session_window_utilization": 0.85,
	})
	adaptiveLoadAccount.LoadFactor = &load20

	return []contractScenario{
		{
			name: "rpm_tiered_with_buffer",
			account: anthropicOAuth(9001, 3, map[string]any{
				"base_rpm": 10, "rpm_strategy": "tiered", "rpm_sticky_buffer": 1,
			}),
			probe: 11, band: service.WindowCostNotSchedulable, checkBand: true,
		},
		{
			// 缓冲缺省 = 并发 + 会话数（3+3=6），下限 base/5=6。
			name:    "rpm_default_buffer_from_concurrency_and_sessions",
			account: anthropicOAuth(9002, 3, map[string]any{"base_rpm": 30, "max_sessions": 3}),
			probe:   35, band: service.WindowCostStickyOnly, checkBand: true,
		},
		{
			name: "rpm_sticky_exempt",
			account: anthropicOAuth(9003, 3, map[string]any{
				"base_rpm": 10, "rpm_strategy": "sticky_exempt",
			}),
			probe: 100, band: service.WindowCostStickyOnly, checkBand: true,
		},
		{
			name: "max_sessions_and_idle_timeout",
			account: anthropicOAuth(9004, 3, map[string]any{
				"max_sessions": 5, "session_idle_timeout_minutes": 10,
			}),
		},
		{
			name: "window_cost_with_sticky_reserve",
			account: anthropicOAuth(9005, 3, map[string]any{
				"window_cost_limit": 60, "window_cost_sticky_reserve": 15,
			}),
		},
		{
			name:    "concurrency_column_zero_means_unlimited",
			account: anthropicOAuth(9006, 0, nil),
		},
		{
			name:    "concurrency_column_with_load_factor",
			account: loadAccount,
		},
		{
			name: "realistic_account_all_capacity_keys",
			account: anthropicOAuth(9008, 5, map[string]any{
				"base_rpm": 50, "rpm_strategy": "tiered", "max_sessions": 5,
				"session_idle_timeout_minutes": 5, "window_cost_limit": 100,
				"window_cost_sticky_reserve": 10,
			}),
			probe: 70, band: service.WindowCostNotSchedulable, checkBand: true,
		},
		{
			// 5h 0.85 / 7d 0.6 → 系数 min(0.4, 0.833) = 0.4：并发 5→2、RPM 50→20。
			// 开关或任一利用率键没进投影，缓存命中时这个账号就会按 5 / 50 放行。
			name: "adaptive_throttle_scales_under_high_utilization",
			account: anthropicOAuth(9009, 5, map[string]any{
				"adaptive_throttle": true, "base_rpm": 50, "max_sessions": 5,
				"session_window_utilization": 0.85, "passive_usage_7d_utilization": 0.6,
			}),
			probe: 20, band: service.WindowCostStickyOnly, checkBand: true,
			want: &effectiveWant{concurrency: 2, loadCapacity: 2, baseRPM: 20},
		},
		{
			// 只有 7d 利用率高（5h 很低）：7d 单独决定系数 f(0.85)=0.4。
			// 上面那行里 5h 的系数更小，盖住了 7d 键丢失的影响；这一行专门钉住 7d 利用率进了投影。
			name: "adaptive_throttle_driven_by_7d_utilization_alone",
			account: anthropicOAuth(9013, 5, map[string]any{
				"adaptive_throttle": true, "base_rpm": 50, "max_sessions": 5,
				"session_window_utilization": 0.3, "passive_usage_7d_utilization": 0.85,
			}),
			probe: 20, band: service.WindowCostStickyOnly, checkBand: true,
			want: &effectiveWant{concurrency: 2, loadCapacity: 2, baseRPM: 20},
		},
		{
			// 利用率为空（没有被动采样数据）→ 系数 1，不能因为缺数据把账号压到最低速。
			name: "adaptive_throttle_without_utilization_is_full_speed",
			account: anthropicOAuth(9010, 3, map[string]any{
				"adaptive_throttle": true, "base_rpm": 30, "max_sessions": 3,
			}),
			probe: 30, band: service.WindowCostStickyOnly, checkBand: true,
			want: &effectiveWant{concurrency: 3, loadCapacity: 3, baseRPM: 30},
		},
		{
			// 开关关闭 → 利用率再高也不缩放（守护行：证明上面的缩放来自开关而不是利用率本身）。
			name: "adaptive_throttle_off_ignores_utilization",
			account: anthropicOAuth(9011, 5, map[string]any{
				"adaptive_throttle": false, "base_rpm": 50, "max_sessions": 5,
				"session_window_utilization": 0.85, "passive_usage_7d_utilization": 0.6,
			}),
			probe: 50, band: service.WindowCostStickyOnly, checkBand: true,
			want: &effectiveWant{concurrency: 5, loadCapacity: 5, baseRPM: 50},
		},
		{
			name:    "adaptive_throttle_scales_load_factor_denominator",
			account: adaptiveLoadAccount,
			want:    &effectiveWant{concurrency: 2, loadCapacity: 8, baseRPM: 0},
		},

		// API Key 账号自设额度：IsSchedulable 在选号阶段读 IsQuotaExceeded。键没进投影时读出来是
		// 「没设额度」，额度已经用完的账号照样被选中（缓存命中时），而单测直接构造完整账号抓不到。
		{
			name: "quota_total_exhausted",
			account: apiKeyAccount(9020, service.PlatformAnthropic, map[string]any{
				"quota_limit": 10.0, "quota_used": 10.0,
			}),
			precondition: requireQuotaExceeded(true),
		},
		{
			// 滚动日额度：周期起点在 1 小时前，仍在 24h 周期内，已用 >= 上限。
			name: "quota_daily_exhausted_rolling_period",
			account: apiKeyAccount(9021, service.PlatformAnthropic, map[string]any{
				"quota_daily_limit": 5.0, "quota_daily_used": 5.0,
				"quota_daily_start": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
			}),
			precondition: requireQuotaExceeded(true),
		},
		{
			// 滚动周额度：周期起点在 1 天前，仍在 7d 周期内。
			name: "quota_weekly_exhausted_rolling_period",
			account: apiKeyAccount(9022, service.PlatformAnthropic, map[string]any{
				"quota_weekly_limit": 20.0, "quota_weekly_used": 20.0,
				"quota_weekly_start": time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			}),
			precondition: requireQuotaExceeded(true),
		},
		{
			// 守护行：额度没用完时不能被判超限（证明上面三行的 true 来自键本身，而不是判定恒真）。
			name: "quota_not_exhausted",
			account: apiKeyAccount(9023, service.PlatformAnthropic, map[string]any{
				"quota_limit": 10.0, "quota_used": 3.0,
			}),
			precondition: requireQuotaExceeded(false),
		},

		// OpenAI 透传开关：候选过滤的模型支持判断读它，没进投影会把透传账号误判为「不支持该模型」，
		// 现象是「单独测账号能通、走网关报 no available accounts」。
		{
			name:    "openai_passthrough_flag",
			account: apiKeyAccount(9024, service.PlatformOpenAI, map[string]any{"openai_passthrough": true}),
			precondition: func(t *testing.T, full *service.Account) {
				require.True(t, full.IsOpenAIPassthroughEnabled(), "前置条件：完整账号应判为透传")
			},
		},
		{
			name: "openai_oauth_passthrough_legacy_flag",
			account: service.Account{
				ID: 9025, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Concurrency: 3,
				Extra: map[string]any{"openai_oauth_passthrough": true},
			},
			precondition: func(t *testing.T, full *service.Account) {
				require.True(t, full.IsOpenAIPassthroughEnabled(), "前置条件：历史兼容键也应判为透传")
			},
		},
	}
}

func requireQuotaExceeded(want bool) func(t *testing.T, full *service.Account) {
	return func(t *testing.T, full *service.Account) {
		t.Helper()
		require.Equal(t, want, full.IsQuotaExceeded(), "前置条件：完整账号的额度判定")
	}
}

func TestSchedulerProjectionContract_VerdictsMatchFullAccount(t *testing.T) {
	for _, tc := range contractScenarios() {
		t.Run(tc.name, func(t *testing.T) {
			full := tc.account
			if tc.checkBand {
				require.Equal(t, tc.band, full.CheckRPMSchedulability(tc.probe),
					"前置条件：完整账号在探针点应落在该区带，否则这行用例是空转")
			}
			if tc.precondition != nil {
				tc.precondition(t, &full)
			}
			if tc.want != nil {
				require.Equal(t, tc.want.concurrency, full.EffectiveConcurrency(), "前置条件：有效并发")
				require.Equal(t, tc.want.loadCapacity, full.LoadBalanceCapacity(), "前置条件：负载分母")
				require.Equal(t, tc.want.baseRPM, full.EffectiveBaseRPM(), "前置条件：有效 RPM")
			}

			restored := projectAccount(t, full)
			rp, cp := rpmProbes(&full), costProbes(&full)

			require.Equal(t, verdictOf(&full, rp, cp), verdictOf(&restored, rp, cp),
				"精简投影丢了选号阶段要读的配置：缓存命中时这个账号会被当作「没配置限流」放行")
		})
	}
}

// 走真实的 Redis 快照读写路径（writeAccounts → sched:meta → GetSnapshot → decodeCachedAccount）。
func TestSchedulerProjectionContract_SnapshotRoundTripThroughRedis(t *testing.T) {
	ctx := context.Background()
	cache, _ := newSchedulerCacheUnitWithRedis(t)
	bucket := service.SchedulerBucket{GroupID: 9100, Platform: service.PlatformAnthropic, Mode: service.SchedulerModeSingle}

	scenarios := contractScenarios()
	fulls := make(map[int64]service.Account, len(scenarios))
	accounts := make([]service.Account, 0, len(scenarios))
	for _, tc := range scenarios {
		fulls[tc.account.ID] = tc.account
		accounts = append(accounts, tc.account)
	}

	token, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshot(ctx, bucket, token, accounts))

	got, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, got, len(accounts))

	for _, projected := range got {
		full, ok := fulls[projected.ID]
		require.True(t, ok, "snapshot 返回了未知账号 %d", projected.ID)
		rp, cp := rpmProbes(&full), costProbes(&full)
		require.Equal(t, verdictOf(&full, rp, cp), verdictOf(projected, rp, cp),
			"账号 %d：经 Redis 快照读回的精简账号与完整账号判定不一致", projected.ID)
	}
}

// 登记表里的每个键都必须能穿过投影，防止有人另起一份字面量白名单后与登记表分叉。
func TestSchedulerProjectionContract_RegistryKeysSurviveProjection(t *testing.T) {
	for _, key := range service.SchedulerProjectedExtraKeys() {
		t.Run(key, func(t *testing.T) {
			full := anthropicOAuth(9200, 3, map[string]any{key: "sentinel"})
			restored := projectAccount(t, full)
			require.Contains(t, restored.Extra, key, "登记表里的键必须在精简投影里保留")
		})
	}
}

func TestSchedulerProjectionContract_RegistryShape(t *testing.T) {
	keys := service.SchedulerProjectedExtraKeys()

	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		require.NotContains(t, seen, key, "登记表里不应有重复键")
		seen[key] = struct{}{}
	}

	// 官方已修过、本 fork 此前缺失的键：RPM 门与动态降速的利用率输入、动态降速开关本身、
	// API Key 账号自设额度的 14 个键（含固定重置模式要读的重置时刻与时区）、OpenAI 透传开关。
	// 「键能穿过投影」的测试只遍历登记表里已有的键，管不到被误删的键；这份清单才是删除防护。
	for _, must := range []string{
		"base_rpm", "rpm_strategy", "rpm_sticky_buffer",
		"session_window_utilization", "passive_usage_7d_utilization", "passive_usage_7d_reset",
		"adaptive_throttle",
		"quota_limit", "quota_used",
		"quota_daily_limit", "quota_daily_used", "quota_daily_start",
		"quota_daily_reset_mode", "quota_daily_reset_hour",
		"quota_weekly_limit", "quota_weekly_used", "quota_weekly_start",
		"quota_weekly_reset_mode", "quota_weekly_reset_day", "quota_weekly_reset_hour",
		"quota_reset_timezone",
		"openai_passthrough", "openai_oauth_passthrough",
	} {
		require.Contains(t, seen, must)
	}

	keys[0] = "mutated"
	require.NotEqual(t, "mutated", service.SchedulerProjectedExtraKeys()[0], "返回值必须是副本，外部修改不能污染登记表")
}
