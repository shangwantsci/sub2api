// Package provider 实现供号商站点（/api/v1/provider）的 HTTP 接口。
//
// 这个包与 handler/admin 刻意保持完全独立，不复用 dto.Account：
// dto.AccountFromServiceShallow 会把 Extra（含 persona_*、tls、rpm、window 等
// 全部内部处理配置）和 ProxyID 原样序列化出去，那是给管理员看的。
// 供号商侧必须走本文件里的独立结构体，字段是显式白名单而非「排除法」，
// 这样将来给 service.Account 加新字段时不会意外泄露。
package provider

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

// AccountView 是供号商能看到的账号视图。
//
// 刻意不含（这些都是平台对账号做的内部处理，供号商不该感知）：
// credentials / credentials_status、type、platform、extra、proxy / proxy_id、
// priority / pool_weight / load_factor / rate_multiplier、oauth_client、
// content_review_policy、claude_oauth_system_prompt_policy、
// scheduler score、group_ids。
//
// 档位参数（concurrency / max_sessions / base_rpm / window_cost_limit）是**有意
// 下发**的例外：上号页选档时这四个值一直就是明着给供号商看的，见 CapacityTierView。
//
// 账号自己在 Anthropic 那边的额度用量不在这个视图里，走单独的
// GET /provider/accounts/:id/usage，见 AccountUsageView。
type AccountView struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Notes *string `json:"notes,omitempty"`
	// Email 是这个账号对应的 Anthropic 登录邮箱，来自换票结果。
	//
	// 供号商手上通常有一批号，名称可以随便填也可以重复，只有邮箱能让他对上
	// 「哪些已经上了、哪些还没上」。这是他自己的账号信息，不是平台的内部处理。
	Email string `json:"email,omitempty"`
	// Status 是压缩过的四态：active / paused / error / unknown。
	// 内部的 rate limit、overload、temp unschedulable 等调度细节一律不下发。
	Status string `json:"status"`
	// HostingTypeLabel 是托管类型的对外文案，不是分组名，更不是底层策略。
	HostingTypeLabel string `json:"hosting_type_label"`
	// TierLabel 是速率档位的对外文案。
	//
	// **按账号实际生效的参数反推，不是 provider_tier 列的直译**，见
	// service.ResolveProviderAccountTier。为空表示不属于任何标准档位，
	// 由前端按 i18n 渲染成「自定义」。
	TierLabel  string     `json:"tier_label"`
	Tier       string     `json:"tier"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`

	// 当前生效的四个档位参数。
	//
	// 与上号页 OnboardOptionsView.Tiers 下发的四项完全一致 —— 那里每个档位的这四个
	// 值一直是明着展示给供号商看的（他就是照着这个选档的），所以下发账号当前的取值
	// 不构成新的信息泄露。不给这四个值的话，用自定义档的供号商在编辑时只能盲填，
	// 一保存就把自己原先设的参数覆盖成默认值。
	//
	// 0 的语义：max_sessions / base_rpm / window_cost_limit 的 0 表示「不启用该限制」
	// （既有约定，最高档的 5h 上限就是 0）；并发的 0 同样是不限。
	Concurrency     int     `json:"concurrency"`
	MaxSessions     int     `json:"max_sessions"`
	BaseRPM         int     `json:"base_rpm"`
	WindowCostLimit float64 `json:"window_cost_limit"`

	// 调度槽位此刻占用。只由列表 enrichment 填，AccountViewFromService 故意不碰。
	//
	// 三项都是指针：nil 表示本次没采到（该项未启用，或 Redis 失败），不要当成 0。
	// 并发上限为 0 时调度器不写 Redis 槽位，current 永远是 0，因此那种号不下发。
	// 不下发 rpm_strategy / rpm_sticky_buffer / session_idle_timeout。
	//
	// 若管理员事后开了 persona gating，真实并发分母可能是
	// EffectivePersonaMaxConcurrency；供号商自己上号写不进 persona_*，
	// 这里仍用档位 concurrency。
	CurrentConcurrency *int `json:"current_concurrency,omitempty"`
	CurrentRPM         *int `json:"current_rpm,omitempty"`
	ActiveSessions     *int `json:"active_sessions,omitempty"`

	// 本结算周期内该账号的 1 倍率用量。
	// 金额用 decimal 并序列化成字符串，避免 JSON 数字在 JS 侧退化为二进制浮点。
	PeriodRequests int64           `json:"period_requests"`
	PeriodTokens   int64           `json:"period_tokens"`
	PeriodCost     decimal.Decimal `json:"period_cost"`
}

// AccountUsageWindowView 是一个额度窗口的对外视图。
//
// Utilization 是 Anthropic 自己给出的用量百分比（0-100+），也就是「这个号在这个窗口
// 里用掉了多少额度」，与平台设的任何限额无关。
//
// **刻意不含任何金额**：UsageInfo.WindowStats 里的 Cost 含账号倍率、UserCost 是向
// 客户收的价，两者都会泄露平台定价；StandardCost 虽然与结算同口径，但账号列表里
// 已经有「本期金额」，再放一个区间不同的金额只会让人以为对账对不上。
type AccountUsageWindowView struct {
	Utilization      float64    `json:"utilization"`
	ResetsAt         *time.Time `json:"resets_at,omitempty"`
	RemainingSeconds int        `json:"remaining_seconds,omitempty"`
	// 窗口内的本地请求数与 token 数，供号商用来对照「这段时间跑了多少量」。
	Requests int64 `json:"requests,omitempty"`
	Tokens   int64 `json:"tokens,omitempty"`
}

// AccountUsageView 是账号在 Anthropic 侧的额度用量。
//
// 与 AccountView 一样是显式白名单：service.UsageInfo 上挂着 Grok/Gemini/Antigravity
// 的一大堆字段和各种金额，直接透传出去迟早出事。供号商账号强制 PlatformAnthropic，
// 这里只取四个窗口。
type AccountUsageView struct {
	// Source 为 passive 表示数据来自请求时顺带采集的响应头（可能不是最新的），
	// active 表示刚从 Anthropic 主动查过。
	Source    string     `json:"source,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`

	FiveHour       *AccountUsageWindowView `json:"five_hour,omitempty"`
	SevenDay       *AccountUsageWindowView `json:"seven_day,omitempty"`
	SevenDaySonnet *AccountUsageWindowView `json:"seven_day_sonnet,omitempty"`
	SevenDayFable  *AccountUsageWindowView `json:"seven_day_fable,omitempty"`
}

// AccountUsageViewFromService 把内部用量结构收敛成供号商视图。
func AccountUsageViewFromService(info *service.UsageInfo) AccountUsageView {
	if info == nil {
		return AccountUsageView{}
	}
	return AccountUsageView{
		Source:         info.Source,
		UpdatedAt:      info.UpdatedAt,
		FiveHour:       usageWindowView(info.FiveHour),
		SevenDay:       usageWindowView(info.SevenDay),
		SevenDaySonnet: usageWindowView(info.SevenDaySonnet),
		SevenDayFable:  usageWindowView(info.SevenDayFable),
	}
}

func usageWindowView(p *service.UsageProgress) *AccountUsageWindowView {
	if p == nil {
		return nil
	}
	out := &AccountUsageWindowView{
		Utilization:      p.Utilization,
		ResetsAt:         p.ResetsAt,
		RemainingSeconds: p.RemainingSeconds,
	}
	// 只搬请求数与 token 数。WindowStats 里的三个金额一个都不能带出去。
	if p.WindowStats != nil {
		out.Requests = p.WindowStats.Requests
		out.Tokens = p.WindowStats.Tokens
	}
	return out
}

// OnboardResultView 是上号提交的结果。
//
// 比起直接回 AccountView 多一个 Duplicate：批量上号时供号商需要分清
// 「这条新建成功」和「这个号之前就上过、这次没有重复建」，
// 两者都不是错误，但前端要显示成不同的结果。
type OnboardResultView struct {
	Account AccountView `json:"account"`
	// Duplicate 为 true 时 Account 是**已存在**的那条账号，本次没有新建。
	Duplicate bool `json:"duplicate"`
}

// AccountViewFromService 构造供号商账号视图。
//
// 只读取显式列出的字段。usage 为 nil 时本期用量归零。
func AccountViewFromService(
	a *service.Account,
	settings service.ProviderSettings,
	usage *service.ProviderPeriodTotals,
) AccountView {
	if a == nil {
		return AccountView{}
	}
	// 档位按账号实际参数反推，不读 provider_tier 列 —— 管理员改过参数后那一列不会跟着变。
	tier, tierLabel := service.ResolveProviderAccountTier(settings, a)
	out := AccountView{
		ID:               a.ID,
		Name:             a.Name,
		Notes:            a.Notes,
		Email:            providerAccountEmail(a),
		Status:           service.ProviderAccountDisplayStatus(a),
		HostingTypeLabel: service.ProviderHostingLabel(settings, a.GroupIDs),
		Tier:             tier,
		TierLabel:        tierLabel,
		ExpiresAt:        a.ExpiresAt,
		LastUsedAt:       a.LastUsedAt,
		CreatedAt:        a.CreatedAt,
		PeriodCost:       decimal.Zero,
		Concurrency:      a.Concurrency,
		MaxSessions:      a.GetMaxSessions(),
		BaseRPM:          a.GetBaseRPM(),
		WindowCostLimit:  a.GetWindowCostLimit(),
	}
	if usage != nil {
		out.PeriodRequests = usage.Requests
		out.PeriodTokens = usage.Tokens
		out.PeriodCost = usage.StandardCost
	}
	return out
}

// providerAccountEmail 取账号对应的 Anthropic 登录邮箱。
//
// 先读 extra：上号时 MirrorProviderIdentityToExtra 会把身份字段从 credentials 复刻过去，
// 网关也只认 extra 里的那份。回落 credentials 是为了照顾该机制上线之前建的存量账号 ——
// 它们 extra 里没有这个键，但 credentials 里一直有。
func providerAccountEmail(a *service.Account) string {
	if a == nil {
		return ""
	}
	if v := strings.TrimSpace(a.GetExtraString("email_address")); v != "" {
		return v
	}
	return credentialString(a.Credentials, "email_address")
}

// providerAccountUUID 取账号对应的 Anthropic 账号 UUID，用于判定是否为同一个号。
func providerAccountUUID(a *service.Account) string {
	if a == nil {
		return ""
	}
	if v := strings.TrimSpace(a.GetExtraString("account_uuid")); v != "" {
		return v
	}
	return credentialString(a.Credentials, "account_uuid")
}

func credentialString(creds map[string]any, key string) string {
	if creds == nil {
		return ""
	}
	if s, ok := creds[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// HostingTypeView 是供号商可选的托管类型。
//
// 只有对外文案，绝不含 content_review_policy / claude_oauth_system_prompt_policy /
// rate_multiplier —— 那些正是「我对账号做了什么处理」，是必须隐藏的部分。
type HostingTypeView struct {
	ID          int64  `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// CapacityTierView 是供号商可选的速率档位。
type CapacityTierView struct {
	Tier            string  `json:"tier"`
	Label           string  `json:"label"`
	Concurrency     int     `json:"concurrency"`
	MaxSessions     int     `json:"max_sessions"`
	BaseRPM         int     `json:"base_rpm"`
	WindowCostLimit float64 `json:"window_cost_limit"`
}

// CustomTierCapsView 是自定义档的上限，供前端做输入约束提示。
type CustomTierCapsView struct {
	Enabled         bool    `json:"enabled"`
	Concurrency     int     `json:"concurrency"`
	MaxSessions     int     `json:"max_sessions"`
	BaseRPM         int     `json:"base_rpm"`
	WindowCostLimit float64 `json:"window_cost_limit"`
}

// OnboardOptionsView 是上号页需要的全部选项。
type OnboardOptionsView struct {
	HostingTypes     []HostingTypeView  `json:"hosting_types"`
	DefaultHostingID int64              `json:"default_hosting_type_id"`
	Tiers            []CapacityTierView `json:"tiers"`
	DefaultTier      string             `json:"default_tier"`
	CustomTier       CustomTierCapsView `json:"custom_tier"`
	// ProxyModePolicy 决定上号页给出哪些代理来源选项：both | auto_only | manual_only。
	ProxyModePolicy string `json:"proxy_mode_policy"`
	// AutoProxyAvailable 报告平台侧当前还有没有可分配的出口。
	//
	// 只回布尔值。可用数量属于平台内部信息，下发出去等于把代理池规模告诉供号商。
	AutoProxyAvailable bool `json:"auto_proxy_available"`
}

// OnboardOptionsFromSettings 把设置翻译成上号页选项，只暴露对外字段。
func OnboardOptionsFromSettings(settings service.ProviderSettings) OnboardOptionsView {
	out := OnboardOptionsView{
		HostingTypes:     make([]HostingTypeView, 0, len(settings.HostingTypes)),
		DefaultHostingID: settings.DefaultGroupID,
		Tiers:            make([]CapacityTierView, 0, len(settings.CapacityTiers)),
		DefaultTier:      settings.DefaultTier,
		CustomTier: CustomTierCapsView{
			Enabled:         settings.CustomTierEnabled,
			Concurrency:     settings.CustomTierCaps.Concurrency,
			MaxSessions:     settings.CustomTierCaps.MaxSessions,
			BaseRPM:         settings.CustomTierCaps.BaseRPM,
			WindowCostLimit: settings.CustomTierCaps.WindowCostLimit,
		},
		ProxyModePolicy: service.NormalizeProviderProxyPolicy(settings.ProxyModePolicy),
	}
	for _, ht := range settings.EnabledHostingTypes() {
		out.HostingTypes = append(out.HostingTypes, HostingTypeView{
			ID:          ht.GroupID,
			Label:       ht.Label,
			Description: ht.Description,
		})
	}
	for _, t := range settings.EnabledTiers() {
		out.Tiers = append(out.Tiers, CapacityTierView{
			Tier:            t.Tier,
			Label:           t.Label,
			Concurrency:     t.Concurrency,
			MaxSessions:     t.MaxSessions,
			BaseRPM:         t.BaseRPM,
			WindowCostLimit: t.WindowCostLimit,
		})
	}
	return out
}
