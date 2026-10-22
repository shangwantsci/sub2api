package service

import (
	"context"
	"math"
	"sort"
)

// 「应用到存量」的计划与预览。
//
// 管理员改了某个容量模板之后，可以把模板（并发、会话数、RPM、5h 额度、动态降速，外加与并发对齐的
// 负载因子）回填给 accounts.provider_tier 等于该模板 key 的存量账号。回填会覆盖账号上手工调过的值，
// 所以先算出「哪些账号的哪几项会变」给管理员看，确认后才真正写入。
//
// 计划（PlanPresetApplication）是纯函数，预览与落库共用同一份：不会出现预览说改 5 个、
// 实际改了 8 个。

// 计划里的字段名。同时是 apply-preview 响应里 changes[].field 的取值，属于与前端的契约，
// 改名或增减必须同步前端。changes 里各项的先后顺序固定为这里的声明顺序。
const (
	presetFieldConcurrency      = "concurrency"
	presetFieldMaxSessions      = "max_sessions"
	presetFieldBaseRPM          = "base_rpm"
	presetFieldWindowCostLimit  = "window_cost_limit"
	presetFieldAdaptiveThrottle = "adaptive_throttle"
	presetFieldLoadFactor       = "load_factor"
)

// presetApplySampleLimit 是预览响应最多带多少个将改动的账号。
const presetApplySampleLimit = 20

// FieldChange 是一个账号上某一项参数「当前值 → 模板值」。
type FieldChange struct {
	Field string `json:"field"`
	From  any    `json:"from"`
	To    any    `json:"to"`
}

// AccountChange 是一个将被「应用到存量」改动的账号，只列出确实不同的项。
type AccountChange struct {
	AccountID int64         `json:"account_id"`
	Name      string        `json:"name"`
	Changes   []FieldChange `json:"changes"`
}

// PlanPresetApplication 算出把模板套到这批账号上会改动哪些账号的哪几项。
//
// 比较 6 项，changes 里按这个顺序列出确实不同的项：并发（accounts.concurrency 列）、max_sessions、
// base_rpm、window_cost_limit、adaptive_throttle（严格 bool，缺失按 false）、load_factor。
// extra 里的数值一律经账号上现有的 getter 读取，JSON 里 int / float64 / 字符串的写法差异
// 与网关实际读到的值保持一致。
//
// load_factor 比的是「有效负载因子」（EffectiveLoadFactor：load_factor 为空时等于并发）与档位并发：
// 落库时会把将改动账号的 load_factor 显式写成档位并发（保持负载排序的分母与并发对齐），
// 管理员手调过的负载因子会因此被重置，这次覆盖必须在预览里看得见。
// load_factor 为空且并发已等于档位并发时二者相等，不算变化；账号并发与档位并发不同、load_factor 为空时，
// 有效负载因子就是旧并发，会同时出现 concurrency 与 load_factor 两项（应用后 load_factor 被显式写成新并发）。
//
// 只返回至少有一项不同的账号，按账号 id 升序，不依赖调用方传入的顺序。
//
// tier.AdaptiveThrottle 为 nil 表示该档位没有定义动态降速，此时不比较也不改这一项。
func PlanPresetApplication(tier ResolvedProviderTier, accounts []Account) []AccountChange {
	plan := make([]AccountChange, 0)
	for i := range accounts {
		a := &accounts[i]
		var changes []FieldChange

		if a.Concurrency != tier.Concurrency {
			changes = append(changes, FieldChange{Field: presetFieldConcurrency, From: a.Concurrency, To: tier.Concurrency})
		}
		if cur := a.GetMaxSessions(); cur != tier.MaxSessions {
			changes = append(changes, FieldChange{Field: presetFieldMaxSessions, From: cur, To: tier.MaxSessions})
		}
		if cur := a.GetBaseRPM(); cur != tier.BaseRPM {
			changes = append(changes, FieldChange{Field: presetFieldBaseRPM, From: cur, To: tier.BaseRPM})
		}
		// 金额是 float64，JSON 里 60 与 60.0 都可能出现，用容差比（口径同 providerTierMatchesAccount）。
		// NaN 与任何数比较都是 false，单靠 |差| >= 容差 会把 NaN 当成「一致」，所以单独判。
		if cur := a.GetWindowCostLimit(); math.IsNaN(cur) || math.Abs(cur-tier.WindowCostLimit) >= 1e-9 {
			changes = append(changes, FieldChange{Field: presetFieldWindowCostLimit, From: cur, To: tier.WindowCostLimit})
		}
		if tier.AdaptiveThrottle != nil {
			if cur := a.IsAdaptiveThrottleEnabled(); cur != *tier.AdaptiveThrottle {
				changes = append(changes, FieldChange{Field: presetFieldAdaptiveThrottle, From: cur, To: *tier.AdaptiveThrottle})
			}
		}
		if cur := a.EffectiveLoadFactor(); cur != tier.Concurrency {
			changes = append(changes, FieldChange{Field: presetFieldLoadFactor, From: cur, To: tier.Concurrency})
		}

		if len(changes) == 0 {
			continue
		}
		plan = append(plan, AccountChange{AccountID: a.ID, Name: a.Name, Changes: changes})
	}
	sort.SliceStable(plan, func(i, j int) bool { return plan[i].AccountID < plan[j].AccountID })
	return plan
}

// ProviderTierApplyPreview 是「应用到存量」的预览：这个档位有多少账号、其中多少会被改动。
type ProviderTierApplyPreview struct {
	Tier string `json:"tier"`
	// Matched 是 provider_tier 等于该档位的账号总数。
	Matched int `json:"matched"`
	// WillChange 是其中至少有一项参数与模板不同的账号数，也就是确认后真正会被写的账号数。
	WillChange int `json:"will_change"`
	// Samples 是将改动的账号，最多 presetApplySampleLimit 个，按账号 id 升序。永远不是 nil。
	Samples []AccountChange `json:"samples"`
}

// PreviewTierApplication 只读地算出把某档位模板套到存量账号上的影响范围，不写任何数据。
func (s *adminServiceImpl) PreviewTierApplication(
	ctx context.Context,
	tier ResolvedProviderTier,
) (ProviderTierApplyPreview, error) {
	accounts, err := s.accountRepo.ListByProviderTier(ctx, tier.Tier)
	if err != nil {
		return ProviderTierApplyPreview{}, err
	}
	plan := PlanPresetApplication(tier, accounts)
	samples := plan
	if len(samples) > presetApplySampleLimit {
		samples = samples[:presetApplySampleLimit]
	}
	return ProviderTierApplyPreview{
		Tier:       tier.Tier,
		Matched:    len(accounts),
		WillChange: len(plan),
		Samples:    samples,
	}, nil
}
