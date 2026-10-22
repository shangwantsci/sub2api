package service

import (
	"math"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// throttleAccount 构造一个 Anthropic OAuth 账号，extra 里带动态降速开关与利用率。
func throttleAccount(concurrency int, extra map[string]any) *Account {
	return &Account{
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: concurrency,
		Extra:       extra,
	}
}

func TestThrottleFactor(t *testing.T) {
	// 无数据 → 全速
	require.InDelta(t, 1.0, throttleFactor(0), 0.01)
	require.InDelta(t, 1.0, throttleFactor(-1), 0.01)

	// 低利用率 → 全速
	require.InDelta(t, 1.0, throttleFactor(0.20), 0.01)
	require.InDelta(t, 1.0, throttleFactor(0.49), 0.01)

	// 50% 边界
	require.InDelta(t, 1.0, throttleFactor(0.50), 0.01)

	// 中等利用率 → 线性下降
	require.InDelta(t, 0.75, throttleFactor(0.65), 0.01) // 50-80 区间中点

	// 80% 边界
	require.InDelta(t, 0.50, throttleFactor(0.80), 0.01)

	// 高利用率 → 继续下降
	require.InDelta(t, 0.30, throttleFactor(0.90), 0.05) // 大约在中间

	// 95% 边界（>= 0.95 进入最低档 0.1）
	require.InDelta(t, 0.10, throttleFactor(0.95), 0.01)

	// 超高 → 最低
	require.InDelta(t, 0.10, throttleFactor(0.99), 0.01)
	require.InDelta(t, 0.10, throttleFactor(1.00), 0.01)
}

// NaN 与任何数比较都是 false，旧写法 `utilization <= 0` 挡不住它，会一路落到 default 返回 0.1，
// 把账号压到最低速。加固后必须当作「无数据」全速放行。
func TestThrottleFactor_NaNMeansNoData(t *testing.T) {
	require.Equal(t, 1.0, throttleFactor(math.NaN()))
	// +Inf 是「很高」而不是「无数据」，落最低档。
	require.Equal(t, 0.1, throttleFactor(math.Inf(1)))
	// -Inf 与负数一样是无数据。
	require.Equal(t, 1.0, throttleFactor(math.Inf(-1)))
}

// 取值约定为 0-1 的比例（三条写入路径写的都是 0-1）。超过 100% 一律按 100% 处理落最低档，
// 曲线在 1.0 附近必须连续：1.02 不能被读成别的刻度而放开降速（旧的「>1 按百分数」规则会让
// 1.02 变成 0.0102 → 系数 1.0，恰好在最危险的区间不降速）。
func TestThrottleFactor_AboveOneIsClampedToLowest(t *testing.T) {
	for _, u := range []float64{1.0, 1.0001, 1.02, 1.5, 2, 85, 100, 250} {
		require.Equal(t, 0.1, throttleFactor(u), "利用率 %v 已超过 100%%，必须落最低档", u)
	}
	// 0-1 比例内保持原曲线不变。
	require.Equal(t, 1.0, throttleFactor(0.49))
	require.InDelta(t, 0.5, throttleFactor(0.80), 1e-9)
	require.Equal(t, 0.1, throttleFactor(0.95))
}

func TestScaleByThrottle(t *testing.T) {
	// base <= 0 表示「不限」，原样返回，不能被系数变成 1。
	require.Equal(t, 0, scaleByThrottle(0, 0.1))
	require.Equal(t, -3, scaleByThrottle(-3, 0.5))

	// 系数 1 → 原值。
	require.Equal(t, 5, scaleByThrottle(5, 1.0))

	// 向下取整。
	require.Equal(t, 2, scaleByThrottle(5, 0.5))   // 2.5 → 2
	require.Equal(t, 20, scaleByThrottle(50, 0.4)) // 20.0

	// 下限 1：base=1 乘 0.1 不会变成 0。
	require.Equal(t, 1, scaleByThrottle(1, 0.1))
	require.Equal(t, 1, scaleByThrottle(5, 0.1)) // 0.5 → 1

	// 浮点误差不能把恰好的整数算小一位：系数差一个 ulp（0.4 的前一个浮点数）时，
	// 5×f = 1.9999999999999998、50×f = 19.999999999999996，仍应分别得到 2 与 20。
	justBelow := math.Nextafter(0.4, 0)
	require.Less(t, 5*justBelow, 2.0, "前提：乘积确实略小于 2，否则这条用例没有意义")
	require.Equal(t, 2, scaleByThrottle(5, justBelow))
	require.Equal(t, 20, scaleByThrottle(50, justBelow))
}

func TestIsAdaptiveThrottleEnabled_StrictBool(t *testing.T) {
	require.True(t, throttleAccount(5, map[string]any{"adaptive_throttle": true}).IsAdaptiveThrottleEnabled())
	require.False(t, throttleAccount(5, map[string]any{"adaptive_throttle": false}).IsAdaptiveThrottleEnabled())
	// 字符串 "true"、数字 1 都不是严格 bool，一律当作未开启。
	require.False(t, throttleAccount(5, map[string]any{"adaptive_throttle": "true"}).IsAdaptiveThrottleEnabled())
	require.False(t, throttleAccount(5, map[string]any{"adaptive_throttle": 1}).IsAdaptiveThrottleEnabled())
	require.False(t, throttleAccount(5, map[string]any{"adaptive_throttle": float64(1)}).IsAdaptiveThrottleEnabled())
	// 缺省与空 extra
	require.False(t, throttleAccount(5, map[string]any{}).IsAdaptiveThrottleEnabled())
	require.False(t, throttleAccount(5, nil).IsAdaptiveThrottleEnabled())
	// nil 账号
	var nilAccount *Account
	require.False(t, nilAccount.IsAdaptiveThrottleEnabled())
}

func TestIsNurturingEnabled_StrictBool(t *testing.T) {
	require.True(t, throttleAccount(5, map[string]any{"nurturing_enabled": true}).IsNurturingEnabled())
	require.False(t, throttleAccount(5, map[string]any{"nurturing_enabled": false}).IsNurturingEnabled())
	require.False(t, throttleAccount(5, map[string]any{"nurturing_enabled": "true"}).IsNurturingEnabled())
	require.False(t, throttleAccount(5, nil).IsNurturingEnabled())
	var nilAccount *Account
	require.False(t, nilAccount.IsNurturingEnabled())
}

func TestThrottleFactor_Account(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
		want  float64
	}{
		{"开关缺省 → 1（即使利用率很高）", map[string]any{"session_window_utilization": 0.99}, 1.0},
		{"开关关闭 → 1", map[string]any{"adaptive_throttle": false, "session_window_utilization": 0.99}, 1.0},
		{"开启但无利用率数据 → 1", map[string]any{"adaptive_throttle": true}, 1.0},
		{"开启 + 5h 低利用率 → 1", map[string]any{"adaptive_throttle": true, "session_window_utilization": 0.2}, 1.0},
		{"开启 + 只有 5h 高利用率", map[string]any{"adaptive_throttle": true, "session_window_utilization": 0.80}, 0.5},
		{"开启 + 只有 7d 高利用率", map[string]any{"adaptive_throttle": true, "passive_usage_7d_utilization": 0.80}, 0.5},
		{"两个都有 → 取更低的系数", map[string]any{
			"adaptive_throttle": true, "session_window_utilization": 0.60, "passive_usage_7d_utilization": 0.80,
		}, 0.5},
		{"极高 → 最低 0.1", map[string]any{
			"adaptive_throttle": true, "session_window_utilization": 0.99, "passive_usage_7d_utilization": 0.98,
		}, 0.1},
		{"利用率超过 100%（1.02）→ 最低 0.1，不放开降速", map[string]any{"adaptive_throttle": true, "session_window_utilization": 1.02}, 0.1},
		{"利用率是 NaN → 视为无数据", map[string]any{"adaptive_throttle": true, "session_window_utilization": math.NaN()}, 1.0},
		{"利用率是字符串 → parseExtraFloat64 能解析就生效", map[string]any{"adaptive_throttle": true, "session_window_utilization": "0.80"}, 0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.InDelta(t, tc.want, throttleAccount(5, tc.extra).ThrottleFactor(), 1e-9)
		})
	}

	var nilAccount *Account
	require.Equal(t, 1.0, nilAccount.ThrottleFactor())
}

func TestEffectiveConcurrency(t *testing.T) {
	// nil 账号防御
	var nilAccount *Account
	require.Equal(t, 0, nilAccount.EffectiveConcurrency())

	// 开关关闭 → 原值，利用率再高也不动
	require.Equal(t, 5, throttleAccount(5, map[string]any{
		"adaptive_throttle": false, "session_window_utilization": 0.99,
	}).EffectiveConcurrency())
	require.Equal(t, 5, throttleAccount(5, nil).EffectiveConcurrency())

	// 开启 + 无利用率数据 → 原值
	require.Equal(t, 5, throttleAccount(5, map[string]any{"adaptive_throttle": true}).EffectiveConcurrency())

	// 开启 + 低利用率 → 原值
	require.Equal(t, 5, throttleAccount(5, map[string]any{
		"adaptive_throttle": true, "session_window_utilization": 0.3, "passive_usage_7d_utilization": 0.2,
	}).EffectiveConcurrency())

	// 开启 + 5h 0.85、7d 0.6：取 min(0.4, 0.833) = 0.4 → floor(5×0.4) = 2
	require.Equal(t, 2, throttleAccount(5, map[string]any{
		"adaptive_throttle": true, "session_window_utilization": 0.85, "passive_usage_7d_utilization": 0.6,
	}).EffectiveConcurrency())

	// 7d 高利用率同样生效
	require.True(t, throttleAccount(5, map[string]any{
		"adaptive_throttle": true, "passive_usage_7d_utilization": 0.90,
	}).EffectiveConcurrency() < 5)

	// 极高利用率 → 最低并发 1，不会降到 0（0 在槽位逻辑里是「不限」）
	require.Equal(t, 1, throttleAccount(5, map[string]any{
		"adaptive_throttle": true, "session_window_utilization": 0.99, "passive_usage_7d_utilization": 0.99,
	}).EffectiveConcurrency())

	// 并发=0（不限）的账号保持不限，不会被降成 1
	require.Equal(t, 0, throttleAccount(0, map[string]any{
		"adaptive_throttle": true, "session_window_utilization": 0.99,
	}).EffectiveConcurrency())

}

func TestEffectiveBaseRPM(t *testing.T) {
	var nilAccount *Account
	require.Equal(t, 0, nilAccount.EffectiveBaseRPM())

	// 开关关闭 → base_rpm 原值
	require.Equal(t, 20, throttleAccount(5, map[string]any{"base_rpm": 20}).EffectiveBaseRPM())
	require.Equal(t, 20, throttleAccount(5, map[string]any{
		"base_rpm": 20, "adaptive_throttle": false, "session_window_utilization": 0.99,
	}).EffectiveBaseRPM())

	// 开启 + 低利用率 → 原值
	require.Equal(t, 20, throttleAccount(5, map[string]any{
		"base_rpm": 20, "adaptive_throttle": true, "session_window_utilization": 0.2,
	}).EffectiveBaseRPM())

	// 开启 + 高利用率 → 降低：floor(20×0.4) = 8
	require.Equal(t, 8, throttleAccount(5, map[string]any{
		"base_rpm": 20, "adaptive_throttle": true, "session_window_utilization": 0.85,
	}).EffectiveBaseRPM())

	// 极高利用率 → 最低 1
	require.Equal(t, 1, throttleAccount(5, map[string]any{
		"base_rpm": 10, "adaptive_throttle": true, "session_window_utilization": 0.99, "passive_usage_7d_utilization": 0.99,
	}).EffectiveBaseRPM())

	// base_rpm 未配置（0 = 不限）→ 保持 0，不会因为开了降速就凭空出现限制
	require.Equal(t, 0, throttleAccount(5, map[string]any{
		"adaptive_throttle": true, "session_window_utilization": 0.99,
	}).EffectiveBaseRPM())
	require.Equal(t, 0, throttleAccount(5, nil).EffectiveBaseRPM())

	// 字符串形态的 base_rpm（旧导入数据）仍按 GetBaseRPM 的解析规则处理
	require.Equal(t, 8, throttleAccount(5, map[string]any{
		"base_rpm": "20", "adaptive_throttle": true, "session_window_utilization": 0.85,
	}).EffectiveBaseRPM())
}

func TestLoadBalanceCapacity(t *testing.T) {
	var nilAccount *Account
	require.Equal(t, 1, nilAccount.LoadBalanceCapacity())

	// 官方语义：没有 load_factor 时用并发列值作分母。
	require.Equal(t, 5, throttleAccount(5, nil).LoadBalanceCapacity())

	// 并发=0 且没有 load_factor：官方 EffectiveLoadFactor 返回 1，分母就是 1（钉住，不是 0 也不是不限）。
	require.Equal(t, 1, throttleAccount(0, nil).LoadBalanceCapacity())
	require.Equal(t, 1, throttleAccount(0, map[string]any{
		"adaptive_throttle": true, "session_window_utilization": 0.99,
	}).LoadBalanceCapacity())

	// load_factor 优先于并发列。
	loadFactor := 20
	withLoad := throttleAccount(5, nil)
	withLoad.LoadFactor = &loadFactor
	require.Equal(t, 20, withLoad.LoadBalanceCapacity())

	// 降速系数同样作用在负载分母上：floor(20×0.4) = 8；硬并发上限仍按并发列算：floor(5×0.4) = 2。
	withLoad.Extra = map[string]any{"adaptive_throttle": true, "session_window_utilization": 0.85}
	require.Equal(t, 8, withLoad.LoadBalanceCapacity())
	require.Equal(t, 2, withLoad.EffectiveConcurrency())

	// 开关关闭 → 不缩放。
	withLoad.Extra = map[string]any{"adaptive_throttle": false, "session_window_utilization": 0.85}
	require.Equal(t, 20, withLoad.LoadBalanceCapacity())
}

// 动态降速作用于 RPM 门本身：三区（绿/黄/红）与粘性豁免都按降速后的上限算，
// 而粘性缓冲不随降速缩放（已建立的会话要靠它继续被服务）。
func TestCheckRPMSchedulability_UsesThrottledBase(t *testing.T) {
	// base_rpm=50、并发 3、会话 3：缓冲 = max(3+3, 50/5=10) = 10。
	base := map[string]any{"base_rpm": 50, "max_sessions": 3}

	plain := throttleAccount(3, base)
	require.Equal(t, 10, plain.GetRPMStickyBuffer())
	require.Equal(t, WindowCostSchedulable, plain.CheckRPMSchedulability(49))
	require.Equal(t, WindowCostStickyOnly, plain.CheckRPMSchedulability(50))
	require.Equal(t, WindowCostStickyOnly, plain.CheckRPMSchedulability(59))
	require.Equal(t, WindowCostNotSchedulable, plain.CheckRPMSchedulability(60))

	// 5h 0.85 / 7d 0.6 → 系数 0.4 → 有效 RPM 20，缓冲仍是 10。
	throttled := throttleAccount(3, map[string]any{
		"base_rpm": 50, "max_sessions": 3,
		"adaptive_throttle": true, "session_window_utilization": 0.85, "passive_usage_7d_utilization": 0.6,
	})
	require.Equal(t, 20, throttled.EffectiveBaseRPM())
	require.Equal(t, 10, throttled.GetRPMStickyBuffer(), "粘性缓冲不随降速缩放")
	require.Equal(t, WindowCostSchedulable, throttled.CheckRPMSchedulability(19))
	require.Equal(t, WindowCostStickyOnly, throttled.CheckRPMSchedulability(20))
	require.Equal(t, WindowCostStickyOnly, throttled.CheckRPMSchedulability(29))
	require.Equal(t, WindowCostNotSchedulable, throttled.CheckRPMSchedulability(30))

	// 粘性豁免策略对降速后的 RPM 同样生效：到顶只能走粘性，永远没有红区。
	exempt := throttleAccount(3, map[string]any{
		"base_rpm": 50, "rpm_strategy": "sticky_exempt",
		"adaptive_throttle": true, "session_window_utilization": 0.85,
	})
	require.Equal(t, WindowCostSchedulable, exempt.CheckRPMSchedulability(19))
	require.Equal(t, WindowCostStickyOnly, exempt.CheckRPMSchedulability(20))
	require.Equal(t, WindowCostStickyOnly, exempt.CheckRPMSchedulability(9999))

	// 没配置 base_rpm（不限）→ 一律可调度，开了降速也一样。
	unlimited := throttleAccount(3, map[string]any{"adaptive_throttle": true, "session_window_utilization": 0.99})
	require.Equal(t, WindowCostSchedulable, unlimited.CheckRPMSchedulability(9999))
}

func TestValidateCapacityExtra(t *testing.T) {
	// 空与缺省都放行
	require.NoError(t, ValidateCapacityExtra(nil))
	require.NoError(t, ValidateCapacityExtra(map[string]any{}))
	require.NoError(t, ValidateCapacityExtra(map[string]any{"base_rpm": 30, "max_sessions": 3}))

	// 严格 bool 放行
	require.NoError(t, ValidateCapacityExtra(map[string]any{"adaptive_throttle": true, "nurturing_enabled": false}))
	require.NoError(t, ValidateCapacityExtra(map[string]any{"adaptive_throttle": false}))

	// 非 bool 一律 400：getExtraBool 会把它们静默当成 false，开关悄悄失效。
	for _, key := range []string{"adaptive_throttle", "nurturing_enabled"} {
		for name, bad := range map[string]any{
			"string true":  "true",
			"string false": "false",
			"number 1":     1,
			"float 1":      float64(1),
			"null":         nil,
			"object":       map[string]any{},
		} {
			err := ValidateCapacityExtra(map[string]any{key: bad})
			require.Error(t, err, "%s=%s 应被拒绝", key, name)
			require.Equal(t, http.StatusBadRequest, infraerrors.Code(err), "%s=%s 应返回 400", key, name)
			require.Contains(t, err.Error(), key)
		}
	}
}

func TestStripCapacityExtra(t *testing.T) {
	in := map[string]any{
		"max_sessions":                 3,
		"session_idle_timeout_minutes": 5,
		"window_cost_limit":            60,
		"window_cost_sticky_reserve":   10,
		"base_rpm":                     30,
		"rpm_strategy":                 "tiered",
		"rpm_sticky_buffer":            6,
		"adaptive_throttle":            true,
		"nurturing_enabled":            true,
		"privacy_mode":                 "off",
		"enable_tls_fingerprint":       true,
	}
	out, removed := StripCapacityExtra(in)
	require.True(t, removed)
	require.Equal(t, map[string]any{"privacy_mode": "off", "enable_tls_fingerprint": true}, out)
	// 不修改入参
	require.Len(t, in, 11)

	// 没有容量键 → 原样返回、removed=false
	plain := map[string]any{"privacy_mode": "off"}
	out, removed = StripCapacityExtra(plain)
	require.False(t, removed)
	require.Equal(t, plain, out)

	out, removed = StripCapacityExtra(nil)
	require.False(t, removed)
	require.Nil(t, out)
}
