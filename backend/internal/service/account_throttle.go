package service

import (
	"fmt"
	"math"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 动态降速（adaptive throttle）。
//
// 账号上一个独立开关 extra.adaptive_throttle（严格 JSON bool）：开启后，账号自己的并发与 RPM
// 按 Anthropic 5h / 7d 利用率实时打折。数值本身只存在账号字段里（accounts.concurrency 列与
// extra 的 base_rpm 等官方键），这里只负责「在选号时把它们乘一个系数」，不存任何档位名。
//
//	有效值 = 数值 × min(f(5h 利用率), f(7d 利用率))，下限 1
//	数值 <= 0（不限）原样不动，系数不会把「不限」变成「限 1」
//
// 与请求伪装（TLS、会话 ID、机器档案）完全无关，互不耦合。

const (
	// extra 键：动态降速与养号都必须是严格 JSON bool。
	extraKeyAdaptiveThrottle = "adaptive_throttle"
	extraKeyNurturingEnabled = "nurturing_enabled"

	// 利用率数据键（由 ratelimit_service 被动采样写入，此处只读）。取值 0-1。
	extraKeySessionWindowUtilization  = "session_window_utilization"   // 5h 窗口
	extraKeyPassiveUsage7dUtilization = "passive_usage_7d_utilization" // 7d 窗口
)

// capacityExtraKeys 是账号「容量与节流」相关的全部 extra 键。
//
// 只对 Claude OAuth / SetupToken 账号有意义：批量更新时，其它账号会被剔除这些键
// （见 StripCapacityExtra）。并发 concurrency 是账号表的列，不在这里，对所有平台都有效。
var capacityExtraKeys = []string{
	extraKeyMaxSessions,
	extraKeySessionIdleTimeoutMin,
	"window_cost_limit",
	"window_cost_sticky_reserve",
	extraKeyBaseRPM,
	extraKeyRPMStrategy,
	"rpm_sticky_buffer",
	extraKeyAdaptiveThrottle,
	extraKeyNurturingEnabled,
}

// throttleFactor 根据利用率返回降速系数（0.1 - 1.0）。
//
// 曲线（分段线性，95% 处有一次跳变）：
//   - u < 50%:        1.0        （全速）
//   - 50% ≤ u < 80%:  1.0 → 0.5  （线性，开始明显减速）
//   - 80% ≤ u < 95%:  0.5 → 0.2  （线性，接近上限大幅减速；趋近 95% 时约 0.2）
//   - u ≥ 95%:        0.1        （直接跳到最低，几乎暂停，保留最低值防止账号完全闲置）
//
// 利用率 <= 0 或 NaN 视为无数据，返回 1.0。取值约定为 0-1 的比例（响应头、主动查询、429 body
// 三条写入路径写的都是 0-1）；大于 1 表示已超过 100%，按 100% 处理落最低档，
// 不能把 1.02 这样的「超限」读成别的刻度而放开降速。
func throttleFactor(utilization float64) float64 {
	if !(utilization > 0) { // 同时挡住 <=0 与 NaN（NaN 与任何数比较都是 false）
		return 1.0
	}
	if utilization > 1 {
		utilization = 1
	}
	switch {
	case utilization < 0.50:
		return 1.0
	case utilization < 0.80:
		// 线性 1.0 → 0.5
		return 1.0 - (utilization-0.50)/(0.80-0.50)*0.5
	case utilization < 0.95:
		// 线性 0.5 → 0.2
		return 0.5 - (utilization-0.80)/(0.95-0.80)*0.3
	default:
		return 0.1
	}
}

// throttleRoundingEpsilon 抵消浮点误差：系数是分段线性公式算出来的，恰好落在整数边界时
// （5×0.4、50×0.4）可能是 1.9999999999999998，而不同 CPU 架构的乘加融合（FMA）结果还不一样，
// 直接 Floor 会让同一个账号在不同机器上差 1。远小于 1 的容差让结果在所有架构上一致。
const throttleRoundingEpsilon = 1e-9

// scaleByThrottle 把数值乘降速系数并向下取整，结果至少为 1。
// base <= 0 表示「不限」（并发 0、RPM 0），原样返回，不参与缩放。
func scaleByThrottle(base int, factor float64) int {
	if base <= 0 {
		return base
	}
	scaled := int(math.Floor(float64(base)*factor + throttleRoundingEpsilon))
	if scaled < 1 {
		return 1
	}
	return scaled
}

// sessionWindowUtilization 返回 Anthropic 5h 窗口利用率（0-1），无数据返回 0。
func (a *Account) sessionWindowUtilization() float64 {
	if a == nil || a.Extra == nil {
		return 0
	}
	return parseExtraFloat64(a.Extra[extraKeySessionWindowUtilization])
}

// passiveUsage7dUtilization 返回 Anthropic 7d 窗口利用率（0-1），无数据返回 0。
func (a *Account) passiveUsage7dUtilization() float64 {
	if a == nil || a.Extra == nil {
		return 0
	}
	return parseExtraFloat64(a.Extra[extraKeyPassiveUsage7dUtilization])
}

// IsAdaptiveThrottleEnabled 报告账号是否开启动态降速。只认严格 JSON bool true：
// 字符串 "true"、数字 1 一律不识别，避免误开导致线上并发被悄悄压低。
func (a *Account) IsAdaptiveThrottleEnabled() bool {
	return a != nil && a.getExtraBool(extraKeyAdaptiveThrottle)
}

// IsNurturingEnabled 报告账号是否参与养号（账号级开关；还需全局开关 account_nurturing_enabled 同时开启）。
func (a *Account) IsNurturingEnabled() bool {
	return a != nil && a.getExtraBool(extraKeyNurturingEnabled)
}

// ThrottleFactor 返回当前降速系数：开关关闭为 1，开启为 min(f(5h), f(7d))。
func (a *Account) ThrottleFactor() float64 {
	if !a.IsAdaptiveThrottleEnabled() {
		return 1.0
	}
	return math.Min(throttleFactor(a.sessionWindowUtilization()), throttleFactor(a.passiveUsage7dUtilization()))
}

// EffectiveConcurrency 返回降速后的硬并发上限（拿槽位用）。concurrency <= 0（不限）原样返回。
func (a *Account) EffectiveConcurrency() int {
	if a == nil {
		return 0
	}
	return scaleByThrottle(a.Concurrency, a.ThrottleFactor())
}

// EffectiveBaseRPM 返回降速后的 RPM 上限。base_rpm 未配置（0）原样返回 0。
func (a *Account) EffectiveBaseRPM() int {
	if a == nil {
		return 0
	}
	return scaleByThrottle(a.GetBaseRPM(), a.ThrottleFactor())
}

// LoadBalanceCapacity 返回负载率排序用的分母：官方的 EffectiveLoadFactor() 再乘降速系数。
// 它只影响账号间的负载排序，不是硬上限；硬上限见 EffectiveConcurrency。
func (a *Account) LoadBalanceCapacity() int {
	if a == nil {
		return 1
	}
	return scaleByThrottle(a.EffectiveLoadFactor(), a.ThrottleFactor())
}

// ValidateCapacityExtra 校验账号 extra（整体写入或增量补丁）里出现的容量开关键。
//
// adaptive_throttle / nurturing_enabled 必须是严格 JSON bool：getExtraBool 会把字符串 "true"
// 静默当成 false，与其让开关悄悄失效，不如在入口直接拒绝。关闭请写显式 false
// （批量更新是 JSONB 顶层合并，删不了键）。
func ValidateCapacityExtra(extra map[string]any) error {
	for _, key := range []string{extraKeyAdaptiveThrottle, extraKeyNurturingEnabled} {
		v, ok := extra[key]
		if !ok {
			continue
		}
		if _, isBool := v.(bool); !isBool {
			return infraerrors.BadRequest("INVALID_CAPACITY_EXTRA",
				fmt.Sprintf("%s must be a boolean (true or false)", key))
		}
	}
	return nil
}

// StripCapacityExtra 返回去掉全部容量键后的 extra 副本，以及是否真的去掉了键。
// 不修改入参。批量更新用它给「容量键不适用」的账号剔除补丁里的容量键；
// 会话批量导入的「更新已存在账号」分支用它避免覆盖存量账号手调过的容量设置。
func StripCapacityExtra(extra map[string]any) (stripped map[string]any, removed bool) {
	if len(extra) == 0 {
		return extra, false
	}
	stripped = make(map[string]any, len(extra))
	for k, v := range extra {
		stripped[k] = v
	}
	for _, key := range capacityExtraKeys {
		if _, ok := stripped[key]; ok {
			delete(stripped, key)
			removed = true
		}
	}
	return stripped, removed
}

// hasCapacityExtraKeys 报告 extra 补丁里是否带了任何容量键。
func hasCapacityExtraKeys(extra map[string]any) bool {
	for _, key := range capacityExtraKeys {
		if _, ok := extra[key]; ok {
			return true
		}
	}
	return false
}

// splitCapacityBulkTargets 把批量更新的目标账号分成两组：
//   - applicable：容量键对它有意义的账号（Claude OAuth / SetupToken），以及这次没查到信息的 id
//     （已被删除的 id 在 SQL 层本来就匹配不到行，放哪组都无害）；
//   - skipped：查到了、但不是 Claude OAuth / SetupToken 的账号，容量键对它们要被剔除。
//
// 并发是账号表的列，对所有平台都有意义，不在这套判断里。
func splitCapacityBulkTargets(ids []int64, loaded []*Account) (applicable, skipped []int64) {
	notApplicable := make(map[int64]struct{}, len(loaded))
	for _, account := range loaded {
		if account != nil && !account.IsAnthropicOAuthOrSetupToken() {
			notApplicable[account.ID] = struct{}{}
		}
	}
	applicable = make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue // 请求里重复的 id 只算一个账号，否则 Skipped 会重复计数
		}
		seen[id] = struct{}{}
		if _, no := notApplicable[id]; no {
			skipped = append(skipped, id)
		} else {
			applicable = append(applicable, id)
		}
	}
	return applicable, skipped
}
