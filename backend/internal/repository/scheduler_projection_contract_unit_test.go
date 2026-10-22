//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

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
}

// verdictOf 是被对拍的判定函数，只调用选号阶段真正读取的账号方法。
var verdictOf = func(a *service.Account, rpmProbes []int, costProbes []float64) capacityVerdict {
	concurrency := a.Concurrency
	if a.IsPersonaEnabled() {
		concurrency = a.EffectivePersonaMaxConcurrency()
	}
	v := capacityVerdict{
		Concurrency:  concurrency,
		LoadCapacity: a.EffectiveLoadFactor(),
		BaseRPM:      a.RPMCeiling(),
		RPMStrategy:  a.GetRPMStrategy(),
		StickyBuffer: a.GetRPMStickyBuffer(),
		MaxSessions:  a.GetMaxSessions(),
		IdleTimeout:  a.GetSessionIdleTimeoutMinutes(),
		WindowLimit:  a.GetWindowCostLimit(),
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
func rpmProbes(a *service.Account) []int {
	base := a.GetBaseRPM()
	if base <= 0 {
		return []int{0, 1, 100, 9999}
	}
	buf := a.GetRPMStickyBuffer()
	seen := map[int]struct{}{}
	for _, p := range []int{0, base - 1, base, base + buf - 1, base + buf, 9999} {
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

type contractScenario struct {
	name    string
	account service.Account
	// probe/band 是前置条件：证明这行用例不是空转（完整账号在该探针点确实落在该区带）。
	probe int
	band  service.WindowCostSchedulability
	// checkBand 为 false 时跳过前置条件（只对拍，不断言区带）。
	checkBand bool
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

func contractScenarios() []contractScenario {
	load20 := 20
	loadAccount := anthropicOAuth(9007, 7, nil)
	loadAccount.LoadFactor = &load20

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

	// 官方已修过、本 fork 此前缺失的键：RPM 门与动态降速的利用率输入。
	for _, must := range []string{
		"base_rpm", "rpm_strategy", "rpm_sticky_buffer",
		"session_window_utilization", "passive_usage_7d_utilization", "passive_usage_7d_reset",
	} {
		require.Contains(t, seen, must)
	}

	keys[0] = "mutated"
	require.NotEqual(t, "mutated", service.SchedulerProjectedExtraKeys()[0], "返回值必须是副本，外部修改不能污染登记表")
}
