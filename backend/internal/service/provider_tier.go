package service

import (
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 档位到账号参数的翻译。
//
// 注意三个伪装项分属两条写入路径，这是本文件最容易写错的地方：
//   - intercept_warmup_requests 在 credentials
//   - enable_tls_fingerprint / session_id_masking_enabled 在 extra
//
// 供号商既看不到也改不了这三项，一律强制开启。

const (
	// credentials 键
	credKeyInterceptWarmupRequests = "intercept_warmup_requests"

	// extra 键（档位相关，「应用到存量」只回填这几个）
	extraKeyMaxSessions     = "max_sessions"
	extraKeyBaseRPM         = "base_rpm"
	extraKeyWindowCostLimit = "window_cost_limit"

	// extra 键（强制伪装项）
	extraKeyEnableTLSFingerprint  = "enable_tls_fingerprint"
	extraKeySessionIDMasking      = "session_id_masking_enabled"
	extraKeySessionIdleTimeoutMin = "session_idle_timeout_minutes"
	extraKeyRPMStrategy           = "rpm_strategy"

	// personaKeyPrefix 前缀的键在 provider 链路一律丢弃。人格属于内部处理方式，
	// 由管理员事后单独配置，绝不能让供号商设置或察觉。
	personaKeyPrefix = "persona_"
)

// providerTierExtraKeys 是档位写入 extra 的键集合。
//
// 「应用到存量」严格只增量合并这几个键，绝不整体覆盖 extra，否则会清掉
// persona_*、window_cost_sticky_reserve、quota_*、privacy_mode 等另行配置的持久设置。
var providerTierExtraKeys = []string{
	extraKeyMaxSessions,
	extraKeyBaseRPM,
	extraKeyWindowCostLimit,
}

// ProviderCustomTierInput 是供号商在自定义档下可以自行设置的参数。
//
// 不含任何 persona、rate_multiplier、priority、pool_weight、load_factor：
// 这些属于平台调度权与计费权，不开放。
//
// 也不含 rpm_strategy：tiered 与 sticky_exempt 决定的是调度器如何处理粘性会话，
// 属于平台的调度策略而不是供号商的速率参数，开放它等于让供号商改调度行为。
type ProviderCustomTierInput struct {
	Concurrency     int     `json:"concurrency"`
	MaxSessions     int     `json:"max_sessions"`
	BaseRPM         int     `json:"base_rpm"`
	WindowCostLimit float64 `json:"window_cost_limit"`
	IdleTimeoutMin  int     `json:"session_idle_timeout_minutes"`
}

// ResolvedProviderTier 是档位解析后的账号参数。
type ResolvedProviderTier struct {
	Tier            string
	Concurrency     int
	MaxSessions     int
	BaseRPM         int
	WindowCostLimit float64
	IdleTimeoutMin  int    // 0 = 用系统默认
	RPMStrategy     string // "" = 用系统默认 (tiered)
}

// ResolveProviderTier 把所选档位翻译成建账号参数。
//
// tier 为 ProviderTierCustom 时使用 custom 的入参并施加护栏；否则查固定档定义。
func ResolveProviderTier(
	settings ProviderSettings,
	tier string,
	custom *ProviderCustomTierInput,
) (ResolvedProviderTier, error) {
	tier = strings.TrimSpace(tier)
	if tier == "" {
		tier = settings.DefaultTier
	}

	if tier == ProviderTierCustom {
		if !settings.CustomTierEnabled {
			return ResolvedProviderTier{}, infraerrors.BadRequest("CUSTOM_TIER_DISABLED",
				"custom capacity tier is not available")
		}
		if custom == nil {
			return ResolvedProviderTier{}, infraerrors.BadRequest("MISSING_CUSTOM_TIER",
				"custom tier parameters are required")
		}
		return resolveCustomTier(settings.CustomTierCaps, *custom)
	}

	def, ok := settings.FindTier(tier)
	if !ok {
		return ResolvedProviderTier{}, infraerrors.BadRequest("INVALID_TIER",
			fmt.Sprintf("capacity tier %q is not available", tier))
	}
	return ResolvedProviderTier{
		Tier:            def.Tier,
		Concurrency:     def.Concurrency,
		MaxSessions:     def.MaxSessions,
		BaseRPM:         def.BaseRPM,
		WindowCostLimit: def.WindowCostLimit,
	}, nil
}

// resolveCustomTier 校验供号商自填的速率参数。
//
// 关键陷阱：这几项的 0 在运行时一律表示「不启用该限制」，而不是「限为 0」：
//   - max_sessions / base_rpm / window_cost_limit 见 Account.GetMaxSessions /
//     GetBaseRPM / GetWindowCostLimit；
//   - concurrency 见 ConcurrencyService.AcquireAccountSlot（maxConcurrency <= 0 直接放行）。
//
// 因此自定义档要求这几项严格大于 0，否则供号商填 0 就等于关掉限制，护栏上限形同虚设。
//
// 固定档由管理员配置，允许用 0 表达「不限」——但并发除外，见 validateTierNumbers：
// 并发填 0 会让本该最小的档位变成不限并发，与管理员直觉相反，故下限为 1。
func resolveCustomTier(caps ProviderCustomTierCaps, in ProviderCustomTierInput) (ResolvedProviderTier, error) {
	type boundedInt struct {
		name  string
		code  string
		value int
		cap   int
	}
	for _, field := range []boundedInt{
		{"concurrency", "INVALID_TIER_CONCURRENCY", in.Concurrency, caps.Concurrency},
		{"max sessions", "INVALID_TIER_SESSIONS", in.MaxSessions, caps.MaxSessions},
		{"base RPM", "INVALID_TIER_RPM", in.BaseRPM, caps.BaseRPM},
	} {
		if field.value <= 0 {
			return ResolvedProviderTier{}, infraerrors.BadRequest(field.code,
				fmt.Sprintf("%s must be at least 1", field.name))
		}
		if field.value > field.cap {
			return ResolvedProviderTier{}, infraerrors.BadRequest("TIER_CAP_EXCEEDED",
				fmt.Sprintf("%s must not exceed %d", field.name, field.cap))
		}
	}
	if in.WindowCostLimit <= 0 {
		return ResolvedProviderTier{}, infraerrors.BadRequest("INVALID_TIER_WINDOW_COST",
			"window cost limit must be greater than 0")
	}
	if in.WindowCostLimit > caps.WindowCostLimit {
		return ResolvedProviderTier{}, infraerrors.BadRequest("TIER_CAP_EXCEEDED",
			fmt.Sprintf("window cost limit must not exceed %.0f", caps.WindowCostLimit))
	}
	if in.IdleTimeoutMin < 0 || in.IdleTimeoutMin > providerMaxIdleTimeoutMin {
		return ResolvedProviderTier{}, infraerrors.BadRequest("INVALID_TIER_IDLE_TIMEOUT",
			fmt.Sprintf("session idle timeout must be between 0 and %d minutes", providerMaxIdleTimeoutMin))
	}

	return ResolvedProviderTier{
		Tier:            ProviderTierCustom,
		Concurrency:     in.Concurrency,
		MaxSessions:     in.MaxSessions,
		BaseRPM:         in.BaseRPM,
		WindowCostLimit: in.WindowCostLimit,
		IdleTimeoutMin:  in.IdleTimeoutMin,
	}, nil
}

// ApplyProviderTierToExtra 把档位参数写进 extra，返回新 map，不修改入参。
//
// 只触碰档位相关的键，其余键原样保留 —— 这正是「应用到存量」不会清掉 persona
// 等持久设置的原因。
func ApplyProviderTierToExtra(extra map[string]any, tier ResolvedProviderTier) map[string]any {
	out := make(map[string]any, len(extra)+len(providerTierExtraKeys)+3)
	for k, v := range extra {
		out[k] = v
	}

	out[extraKeyMaxSessions] = tier.MaxSessions
	out[extraKeyBaseRPM] = tier.BaseRPM
	out[extraKeyWindowCostLimit] = tier.WindowCostLimit

	if tier.IdleTimeoutMin > 0 {
		out[extraKeySessionIdleTimeoutMin] = tier.IdleTimeoutMin
	}
	if tier.RPMStrategy != "" {
		out[extraKeyRPMStrategy] = tier.RPMStrategy
	}
	return out
}

// ApplyProviderForcedExtra 写入 extra 中强制开启的伪装项。
func ApplyProviderForcedExtra(extra map[string]any) map[string]any {
	out := make(map[string]any, len(extra)+2)
	for k, v := range extra {
		out[k] = v
	}
	out[extraKeyEnableTLSFingerprint] = true
	out[extraKeySessionIDMasking] = true
	return out
}

// providerIdentityMirrorKeys 是必须从 credentials 复刻到 extra 的身份字段。
//
// 网关读的是 extra 而不是 credentials：
//   - `gateway_upstream_request.go` 用 `GetExtraString("account_uuid")`，
//     且以 `accountUUID != ""` 为硬前提，为空时 RewriteUserIDWithMasking 整段跳过——
//     也就是说会话 ID 伪装虽然开关为 true 也不会执行；
//   - `gateway_claude_oauth_body.go` 的 FormatMetadataUserID 同样从 extra 取，
//     缺失时拼出来的 metadata.user_id 少一段账号 UUID，本身就是破绽。
//
// 管理端走 `buildExtraInfo`（useAccountOAuth.ts）写 extra，CRS 同步也专门把这两个键
// 从 credentials 复制进 extra。供号商上号必须对齐，否则伪装静默失效。
var providerIdentityMirrorKeys = []string{"account_uuid", "org_uuid", "email_address"}

// RefreshProviderIdentityExtra 清掉 extra 里已过期的身份字段，供重新授权时使用。
//
// 换票可能返回不同的 account_uuid（例如供号商换了个 Anthropic 账号重新授权）。
// 只补不删的话 extra 会留着上一个账号的 UUID，伪装用错身份比没有身份更糟。
// 这里先按新 credentials 把有冲突的键清掉，再交给 MirrorProviderIdentityToExtra 写新值。
func RefreshProviderIdentityExtra(extra map[string]any, creds map[string]any) map[string]any {
	out := make(map[string]any, len(extra))
	for k, v := range extra {
		out[k] = v
	}
	for _, key := range providerIdentityMirrorKeys {
		raw, ok := creds[key]
		if !ok {
			continue
		}
		if s, isStr := raw.(string); isStr && strings.TrimSpace(s) == "" {
			continue
		}
		delete(out, key)
	}
	return out
}

// MirrorProviderIdentityToExtra 把 OAuth 换票拿到的身份字段从 credentials 复刻进 extra。
//
// 必须在 SanitizeProviderExtra 之后调用：这些值来自我们自己的换票流程而非供号商提交，
// 不该被白名单丢弃。只在 extra 尚无该键且 credentials 有非空值时写入。
func MirrorProviderIdentityToExtra(extra map[string]any, creds map[string]any) map[string]any {
	out := make(map[string]any, len(extra)+len(providerIdentityMirrorKeys))
	for k, v := range extra {
		out[k] = v
	}
	for _, key := range providerIdentityMirrorKeys {
		if _, exists := out[key]; exists {
			continue
		}
		raw, ok := creds[key]
		if !ok {
			continue
		}
		if s, isStr := raw.(string); isStr && strings.TrimSpace(s) == "" {
			continue
		}
		out[key] = raw
	}
	return out
}

// ApplyProviderForcedCredentials 写入 credentials 中强制开启的伪装项。
//
// intercept_warmup_requests 在 credentials 而非 extra，别放错地方。
func ApplyProviderForcedCredentials(creds map[string]any) map[string]any {
	out := make(map[string]any, len(creds)+1)
	for k, v := range creds {
		out[k] = v
	}
	out[credKeyInterceptWarmupRequests] = true
	return out
}

// SanitizeProviderExtra 过滤供号商提交的 extra。
//
// 采用白名单：只保留档位相关键，其余一律丢弃。这样即便供号商构造请求塞
// persona_enabled 或任何未来新增的内部配置键，也进不了账号。
// 返回被丢弃的键便于记日志。
func SanitizeProviderExtra(in map[string]any) (kept map[string]any, dropped []string) {
	kept = make(map[string]any, len(in))
	if len(in) == 0 {
		return kept, nil
	}
	allowed := make(map[string]struct{}, len(providerTierExtraKeys))
	for _, k := range providerTierExtraKeys {
		allowed[k] = struct{}{}
	}
	for k, v := range in {
		if _, ok := allowed[k]; ok {
			kept[k] = v
			continue
		}
		dropped = append(dropped, k)
	}
	return kept, dropped
}

// ContainsPersonaKey 报告 map 中是否含任何 persona_ 前缀键，用于测试与日志。
func ContainsPersonaKey(in map[string]any) bool {
	for k := range in {
		if strings.HasPrefix(k, personaKeyPrefix) {
			return true
		}
	}
	return false
}
