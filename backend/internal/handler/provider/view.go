// Package provider 实现供号商站点（/api/v1/provider）的 HTTP 接口。
//
// 这个包与 handler/admin 刻意保持完全独立，不复用 dto.Account：
// dto.AccountFromServiceShallow 会把 Extra（含 persona_*、tls、rpm、window 等
// 全部内部处理配置）和 ProxyID 原样序列化出去，那是给管理员看的。
// 供号商侧必须走本文件里的独立结构体，字段是显式白名单而非「排除法」，
// 这样将来给 service.Account 加新字段时不会意外泄露。
package provider

import (
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
// session 窗口内部状态、scheduler score、group_ids。
type AccountView struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Notes *string `json:"notes,omitempty"`
	// Status 是压缩过的四态：active / paused / error / unknown。
	// 内部的 rate limit、overload、temp unschedulable 等调度细节一律不下发。
	Status string `json:"status"`
	// HostingTypeLabel 是托管类型的对外文案，不是分组名，更不是底层策略。
	HostingTypeLabel string `json:"hosting_type_label"`
	// TierLabel 是速率档位的对外文案。
	TierLabel  string     `json:"tier_label"`
	Tier       string     `json:"tier"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`

	// 本结算周期内该账号的 1 倍率用量。
	// 金额用 decimal 并序列化成字符串，避免 JSON 数字在 JS 侧退化为二进制浮点。
	PeriodRequests int64           `json:"period_requests"`
	PeriodTokens   int64           `json:"period_tokens"`
	PeriodCost     decimal.Decimal `json:"period_cost"`
}

// AccountViewFromService 构造供号商账号视图。
//
// 只读取显式列出的字段。usage 为 nil 时用量归零。
func AccountViewFromService(
	a *service.Account,
	settings service.ProviderSettings,
	usage *service.ProviderPeriodTotals,
) AccountView {
	if a == nil {
		return AccountView{}
	}
	out := AccountView{
		ID:               a.ID,
		Name:             a.Name,
		Notes:            a.Notes,
		Status:           service.ProviderAccountDisplayStatus(a),
		HostingTypeLabel: service.ProviderHostingLabel(settings, a.GroupIDs),
		TierLabel:        service.ProviderTierLabel(settings, a.ProviderTier),
		ExpiresAt:        a.ExpiresAt,
		LastUsedAt:       a.LastUsedAt,
		CreatedAt:        a.CreatedAt,
		PeriodCost:       decimal.Zero,
	}
	if a.ProviderTier != nil {
		out.Tier = *a.ProviderTier
	}
	if usage != nil {
		out.PeriodRequests = usage.Requests
		out.PeriodTokens = usage.Tokens
		out.PeriodCost = usage.StandardCost
	}
	return out
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
