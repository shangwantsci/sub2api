package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 供号商站点设置。
//
// 全部存在 settings 表里，管理端「供货商」设置页可视化编辑，代码只提供首次部署的种子值。
// 档位数值与托管类型对外文案都不硬编码，上线后调整无需改代码。

const (
	// ProviderTierCustom 是自定义档的档位标识。该档账号不参与「应用到存量」批量回填。
	ProviderTierCustom = "custom"

	providerSettingsDBTimeout = 5 * time.Second

	// 固定档数量。档位标识为 "1".."5"。
	providerFixedTierCount = 5
)

// 设置项数值区间护栏。前后端都要校验，这里是后端的权威边界。
const (
	providerMaxConcurrency     = 50
	providerMaxSessions        = 50
	providerMaxBaseRPM         = 500
	providerMaxWindowCostLimit = 1000.0
	providerMaxIdleTimeoutMin  = 120
	// providerMaxAccountPriority 与账号表 priority 的常见取值域一致（0-100）。
	providerMaxAccountPriority = 100
)

// ProviderHostingType 是一个可供供号商选择的托管类型。
//
// Label 与 Description 是对外文案，供号商只看得到这两项；GroupID 背后真实的
// content_review_policy / claude_oauth_system_prompt_policy / rate_multiplier
// 属于内部处理方式，绝不下发到供号商侧。
type ProviderHostingType struct {
	GroupID     int64  `json:"group_id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Sort        int    `json:"sort"`
}

// ProviderCapacityTier 是一个固定速率档位的定义。
type ProviderCapacityTier struct {
	Tier            string  `json:"tier"`
	Label           string  `json:"label"`
	Concurrency     int     `json:"concurrency"`
	MaxSessions     int     `json:"max_sessions"`
	BaseRPM         int     `json:"base_rpm"`
	WindowCostLimit float64 `json:"window_cost_limit"` // 0 = 不限
	Enabled         bool    `json:"enabled"`
}

// ProviderCustomTierCaps 是自定义档各项参数的上限护栏。
type ProviderCustomTierCaps struct {
	Concurrency     int     `json:"concurrency"`
	MaxSessions     int     `json:"max_sessions"`
	BaseRPM         int     `json:"base_rpm"`
	WindowCostLimit float64 `json:"window_cost_limit"`
}

// ProviderSettings 汇总供号商站点的全部设置。
type ProviderSettings struct {
	PortalEnabled      bool                   `json:"portal_enabled"`
	HostingTypes       []ProviderHostingType  `json:"hosting_types"`
	DefaultGroupID     int64                  `json:"default_group_id"`
	CapacityTiers      []ProviderCapacityTier `json:"capacity_tiers"`
	DefaultTier        string                 `json:"default_tier"`
	CustomTierEnabled  bool                   `json:"custom_tier_enabled"`
	CustomTierCaps     ProviderCustomTierCaps `json:"custom_tier_caps"`
	SettlementTimezone string                 `json:"settlement_timezone"`
	// SettlementCooldownSeconds 封账终点相对当前时刻回退的秒数。
	SettlementCooldownSeconds int `json:"settlement_cooldown_seconds"`
	// AccountPriority 供号商账号的调度优先级。硬门槛语义，见 DefaultProviderAccountPriority。
	AccountPriority int `json:"account_priority"`
}

// DefaultProviderCapacityTiers 返回 1-5 档的种子值。
//
// 设计依据：1 档最接近单个真人使用 Claude Code CLI 的节奏（真人含工具调用大约
// 5-15 RPM，单 CLI 会话并发为 1），逐档放开到 5 档的最大吞吐。这些只是首次部署
// 的初值，上线后在设置页按实际账号承载能力调整。
func DefaultProviderCapacityTiers() []ProviderCapacityTier {
	return []ProviderCapacityTier{
		{Tier: "1", Label: "1 档", Concurrency: 1, MaxSessions: 1, BaseRPM: 10, WindowCostLimit: 20, Enabled: true},
		{Tier: "2", Label: "2 档", Concurrency: 2, MaxSessions: 2, BaseRPM: 20, WindowCostLimit: 40, Enabled: true},
		{Tier: "3", Label: "3 档", Concurrency: 3, MaxSessions: 3, BaseRPM: 30, WindowCostLimit: 60, Enabled: true},
		{Tier: "4", Label: "4 档", Concurrency: 5, MaxSessions: 5, BaseRPM: 50, WindowCostLimit: 100, Enabled: true},
		{Tier: "5", Label: "5 档", Concurrency: 8, MaxSessions: 8, BaseRPM: 80, WindowCostLimit: 0, Enabled: true},
	}
}

// DefaultProviderCustomTierCaps 返回自定义档护栏的种子值。
func DefaultProviderCustomTierCaps() ProviderCustomTierCaps {
	return ProviderCustomTierCaps{
		Concurrency:     10,
		MaxSessions:     10,
		BaseRPM:         100,
		WindowCostLimit: 200,
	}
}

// DefaultProviderSettlementTimezone 是结算时区的种子值。
const DefaultProviderSettlementTimezone = "Asia/Shanghai"

// DefaultProviderSettlementCooldown 是结算冷却期的种子值。
//
// usage_logs 由异步 worker 写入（worker 任务超时 5s，批处理窗口 20ms，
// 调用方 detached 超时 15s），理论最坏延迟在 30 秒量级。10 分钟留了足够余量，
// 同时不会让管理员等太久才能结算。
const DefaultProviderSettlementCooldown = 10 * time.Minute

// providerSettlementCooldownBounds 限制冷却期的可配置范围。
// 下限 1 分钟：低于写入链最坏延迟就失去意义；上限 24 小时：再长会让结算无法及时进行。
const (
	minProviderSettlementCooldown = time.Minute
	maxProviderSettlementCooldown = 24 * time.Hour
)

// DefaultProviderAccountPriority 是供号商账号调度优先级的种子值。
//
// 取 1 而不是 Ent schema 的 50，是为了与管理端创建账号表单的默认值（priority: 1）对齐：
// 调度里 priority 是硬门槛，filterByMinPriority 只保留数值最小的一批账号，
// 其余一个请求都拿不到。若供号商账号与自有账号同处一个分组却取不同值，
// 数值大的那一边会被永久饿死且没有任何报错。
const DefaultProviderAccountPriority = 1

func defaultProviderSettings() ProviderSettings {
	return ProviderSettings{
		PortalEnabled:             false,
		HostingTypes:              []ProviderHostingType{},
		DefaultGroupID:            0,
		CapacityTiers:             DefaultProviderCapacityTiers(),
		DefaultTier:               "3",
		CustomTierEnabled:         true,
		CustomTierCaps:            DefaultProviderCustomTierCaps(),
		SettlementTimezone:        DefaultProviderSettlementTimezone,
		SettlementCooldownSeconds: int(DefaultProviderSettlementCooldown / time.Second),
		AccountPriority:           DefaultProviderAccountPriority,
	}
}

// GetProviderSettings 读取供号商站点设置，缺失项回落种子值。
func (s *SettingService) GetProviderSettings(ctx context.Context) (ProviderSettings, error) {
	out := defaultProviderSettings()
	if s == nil || s.settingRepo == nil {
		return out, fmt.Errorf("setting service unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerSettingsDBTimeout)
	defer cancel()

	values, err := s.settingRepo.GetMultiple(dbCtx, []string{
		SettingKeyProviderPortalEnabled,
		SettingKeyProviderSelectableGroups,
		SettingKeyProviderDefaultGroupID,
		SettingKeyProviderCapacityTiers,
		SettingKeyProviderDefaultTier,
		SettingKeyProviderCustomTierEnabled,
		SettingKeyProviderCustomTierCaps,
		SettingKeyProviderSettlementTimezone,
		SettingKeyProviderSettlementCooldownSeconds,
		SettingKeyProviderAccountPriority,
	})
	if err != nil {
		return out, err
	}

	if raw, ok := values[SettingKeyProviderPortalEnabled]; ok && strings.TrimSpace(raw) != "" {
		out.PortalEnabled = strings.EqualFold(strings.TrimSpace(raw), "true")
	}
	if raw, ok := values[SettingKeyProviderSelectableGroups]; ok && strings.TrimSpace(raw) != "" {
		var parsed []ProviderHostingType
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			out.HostingTypes = parsed
		}
	}
	if raw, ok := values[SettingKeyProviderDefaultGroupID]; ok && strings.TrimSpace(raw) != "" {
		if v, convErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); convErr == nil {
			out.DefaultGroupID = v
		}
	}
	if raw, ok := values[SettingKeyProviderCapacityTiers]; ok && strings.TrimSpace(raw) != "" {
		var parsed []ProviderCapacityTier
		if json.Unmarshal([]byte(raw), &parsed) == nil && len(parsed) > 0 {
			out.CapacityTiers = parsed
		}
	}
	if raw, ok := values[SettingKeyProviderDefaultTier]; ok && strings.TrimSpace(raw) != "" {
		out.DefaultTier = strings.TrimSpace(raw)
	}
	if raw, ok := values[SettingKeyProviderCustomTierEnabled]; ok && strings.TrimSpace(raw) != "" {
		out.CustomTierEnabled = strings.EqualFold(strings.TrimSpace(raw), "true")
	}
	if raw, ok := values[SettingKeyProviderCustomTierCaps]; ok && strings.TrimSpace(raw) != "" {
		var parsed ProviderCustomTierCaps
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			out.CustomTierCaps = parsed
		}
	}
	if raw, ok := values[SettingKeyProviderSettlementTimezone]; ok && strings.TrimSpace(raw) != "" {
		out.SettlementTimezone = strings.TrimSpace(raw)
	}
	if raw, ok := values[SettingKeyProviderSettlementCooldownSeconds]; ok && strings.TrimSpace(raw) != "" {
		if v, convErr := strconv.Atoi(strings.TrimSpace(raw)); convErr == nil && v > 0 {
			out.SettlementCooldownSeconds = v
		}
	}
	if raw, ok := values[SettingKeyProviderAccountPriority]; ok && strings.TrimSpace(raw) != "" {
		if v, convErr := strconv.Atoi(strings.TrimSpace(raw)); convErr == nil {
			out.AccountPriority = v
		}
	}

	sort.SliceStable(out.HostingTypes, func(i, j int) bool {
		return out.HostingTypes[i].Sort < out.HostingTypes[j].Sort
	})
	return out, nil
}

// IsProviderPortalEnabled 是 provider 守卫的快速判定。读取失败时 fail-closed，
// 因为供号商站点面向外部用户，宁可暂时不可用也不能在设置异常时敞开。
func (s *SettingService) IsProviderPortalEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerSettingsDBTimeout)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyProviderPortalEnabled)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(raw), "true")
}

// GetProviderSettlementTimezone 返回结算时区，非法或缺失时回落种子值。
//
// 对账按日聚合必须用它，不能用请求参数里的浏览器时区，否则管理员与供号商看到的
// 每日明细会按各自时区切分，总额一致但逐日对不上。
func (s *SettingService) GetProviderSettlementTimezone(ctx context.Context) string {
	if s == nil || s.settingRepo == nil {
		return DefaultProviderSettlementTimezone
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerSettingsDBTimeout)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyProviderSettlementTimezone)
	if err != nil {
		return DefaultProviderSettlementTimezone
	}
	tz := strings.TrimSpace(raw)
	if tz == "" {
		return DefaultProviderSettlementTimezone
	}
	if _, loadErr := time.LoadLocation(tz); loadErr != nil {
		return DefaultProviderSettlementTimezone
	}
	return tz
}

// SaveProviderSettings 校验并整体保存供号商设置。
//
// 校验在后端做而不只靠前端：设置错了会直接影响上号与调度。
func (s *SettingService) SaveProviderSettings(ctx context.Context, in ProviderSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	normalized, err := ValidateProviderSettings(in)
	if err != nil {
		return err
	}

	hostingJSON, err := json.Marshal(normalized.HostingTypes)
	if err != nil {
		return fmt.Errorf("marshal hosting types: %w", err)
	}
	tiersJSON, err := json.Marshal(normalized.CapacityTiers)
	if err != nil {
		return fmt.Errorf("marshal capacity tiers: %w", err)
	}
	capsJSON, err := json.Marshal(normalized.CustomTierCaps)
	if err != nil {
		return fmt.Errorf("marshal custom tier caps: %w", err)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerSettingsDBTimeout)
	defer cancel()

	return s.settingRepo.SetMultiple(dbCtx, map[string]string{
		SettingKeyProviderPortalEnabled:             strconv.FormatBool(normalized.PortalEnabled),
		SettingKeyProviderSelectableGroups:          string(hostingJSON),
		SettingKeyProviderDefaultGroupID:            strconv.FormatInt(normalized.DefaultGroupID, 10),
		SettingKeyProviderCapacityTiers:             string(tiersJSON),
		SettingKeyProviderDefaultTier:               normalized.DefaultTier,
		SettingKeyProviderCustomTierEnabled:         strconv.FormatBool(normalized.CustomTierEnabled),
		SettingKeyProviderCustomTierCaps:            string(capsJSON),
		SettingKeyProviderSettlementTimezone:        normalized.SettlementTimezone,
		SettingKeyProviderSettlementCooldownSeconds: strconv.Itoa(normalized.SettlementCooldownSeconds),
		SettingKeyProviderAccountPriority:           strconv.Itoa(normalized.AccountPriority),
	})
}

// GetProviderSettlementCooldown 返回结算冷却期，非法值回落种子值并夹在合理区间内。
func (s *SettingService) GetProviderSettlementCooldown(ctx context.Context) time.Duration {
	if s == nil || s.settingRepo == nil {
		return DefaultProviderSettlementCooldown
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerSettingsDBTimeout)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyProviderSettlementCooldownSeconds)
	if err != nil {
		return DefaultProviderSettlementCooldown
	}
	seconds, convErr := strconv.Atoi(strings.TrimSpace(raw))
	if convErr != nil {
		return DefaultProviderSettlementCooldown
	}
	d := time.Duration(seconds) * time.Second
	if d < minProviderSettlementCooldown || d > maxProviderSettlementCooldown {
		return DefaultProviderSettlementCooldown
	}
	return d
}

// GetProviderAccountPriority 返回供号商账号的调度优先级。
func (s *SettingService) GetProviderAccountPriority(ctx context.Context) int {
	if s == nil || s.settingRepo == nil {
		return DefaultProviderAccountPriority
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerSettingsDBTimeout)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyProviderAccountPriority)
	if err != nil {
		return DefaultProviderAccountPriority
	}
	v, convErr := strconv.Atoi(strings.TrimSpace(raw))
	if convErr != nil || v < 0 || v > providerMaxAccountPriority {
		return DefaultProviderAccountPriority
	}
	return v
}

// ValidateProviderSettings 校验并归一化设置，返回可直接持久化的副本。
func ValidateProviderSettings(in ProviderSettings) (ProviderSettings, error) {
	out := in

	// ---- 托管类型 ----
	enabledGroups := make(map[int64]bool)
	seenGroups := make(map[int64]bool)
	cleanHosting := make([]ProviderHostingType, 0, len(in.HostingTypes))
	for _, ht := range in.HostingTypes {
		if ht.GroupID <= 0 {
			return out, infraerrors.BadRequest("INVALID_HOSTING_TYPE", "hosting type group_id must be positive")
		}
		if seenGroups[ht.GroupID] {
			return out, infraerrors.BadRequest("DUPLICATE_HOSTING_TYPE",
				fmt.Sprintf("hosting type for group %d is listed more than once", ht.GroupID))
		}
		seenGroups[ht.GroupID] = true

		ht.Label = strings.TrimSpace(ht.Label)
		ht.Description = strings.TrimSpace(ht.Description)
		if ht.Enabled && ht.Label == "" {
			return out, infraerrors.BadRequest("MISSING_HOSTING_TYPE_LABEL",
				fmt.Sprintf("enabled hosting type for group %d needs a display label", ht.GroupID))
		}
		if ht.Enabled {
			enabledGroups[ht.GroupID] = true
		}
		cleanHosting = append(cleanHosting, ht)
	}
	sort.SliceStable(cleanHosting, func(i, j int) bool { return cleanHosting[i].Sort < cleanHosting[j].Sort })
	out.HostingTypes = cleanHosting

	// ---- 速率档位 ----
	seenTiers := make(map[string]bool)
	enabledTiers := make(map[string]bool)
	cleanTiers := make([]ProviderCapacityTier, 0, len(in.CapacityTiers))
	for _, t := range in.CapacityTiers {
		t.Tier = strings.TrimSpace(t.Tier)
		if t.Tier == "" {
			return out, infraerrors.BadRequest("INVALID_TIER", "tier id must not be empty")
		}
		if t.Tier == ProviderTierCustom {
			return out, infraerrors.BadRequest("INVALID_TIER",
				"\"custom\" is reserved and cannot be used as a fixed tier id")
		}
		if seenTiers[t.Tier] {
			return out, infraerrors.BadRequest("DUPLICATE_TIER",
				fmt.Sprintf("tier %q is listed more than once", t.Tier))
		}
		seenTiers[t.Tier] = true

		t.Label = strings.TrimSpace(t.Label)
		if t.Label == "" {
			t.Label = t.Tier
		}
		if err := validateTierNumbers(t.Tier, t.Concurrency, t.MaxSessions, t.BaseRPM, t.WindowCostLimit); err != nil {
			return out, err
		}
		if t.Enabled {
			enabledTiers[t.Tier] = true
		}
		cleanTiers = append(cleanTiers, t)
	}
	if len(enabledTiers) == 0 {
		return out, infraerrors.BadRequest("NO_ENABLED_TIER", "at least one capacity tier must be enabled")
	}
	out.CapacityTiers = cleanTiers

	// ---- 默认项必须处于启用状态 ----
	out.DefaultTier = strings.TrimSpace(out.DefaultTier)
	if out.DefaultTier == "" {
		return out, infraerrors.BadRequest("MISSING_DEFAULT_TIER", "default tier must be set")
	}
	if !enabledTiers[out.DefaultTier] {
		return out, infraerrors.BadRequest("INVALID_DEFAULT_TIER",
			fmt.Sprintf("default tier %q must reference an enabled tier", out.DefaultTier))
	}

	// 站点开启时才强制要求有可用托管类型，关闭状态允许留空慢慢配。
	if out.PortalEnabled {
		if len(enabledGroups) == 0 {
			return out, infraerrors.BadRequest("NO_ENABLED_HOSTING_TYPE",
				"at least one hosting type must be enabled before opening the provider portal")
		}
		if out.DefaultGroupID <= 0 {
			return out, infraerrors.BadRequest("MISSING_DEFAULT_HOSTING_TYPE", "default hosting type must be set")
		}
		if !enabledGroups[out.DefaultGroupID] {
			return out, infraerrors.BadRequest("INVALID_DEFAULT_HOSTING_TYPE",
				"default hosting type must reference an enabled hosting type")
		}
	}

	// ---- 自定义档护栏 ----
	if out.CustomTierEnabled {
		caps := out.CustomTierCaps
		if err := validateTierNumbers("custom caps", caps.Concurrency, caps.MaxSessions, caps.BaseRPM, caps.WindowCostLimit); err != nil {
			return out, err
		}
		if caps.Concurrency <= 0 || caps.MaxSessions <= 0 {
			return out, infraerrors.BadRequest("INVALID_CUSTOM_TIER_CAPS",
				"custom tier concurrency and session caps must be positive")
		}
	}

	// ---- 结算时区 ----
	out.SettlementTimezone = strings.TrimSpace(out.SettlementTimezone)
	if out.SettlementTimezone == "" {
		out.SettlementTimezone = DefaultProviderSettlementTimezone
	}
	if _, err := time.LoadLocation(out.SettlementTimezone); err != nil {
		return out, infraerrors.BadRequest("INVALID_SETTLEMENT_TIMEZONE",
			fmt.Sprintf("unknown timezone %q", out.SettlementTimezone))
	}

	// ---- 结算冷却期 ----
	if out.SettlementCooldownSeconds <= 0 {
		out.SettlementCooldownSeconds = int(DefaultProviderSettlementCooldown / time.Second)
	}
	cooldown := time.Duration(out.SettlementCooldownSeconds) * time.Second
	if cooldown < minProviderSettlementCooldown || cooldown > maxProviderSettlementCooldown {
		return out, infraerrors.BadRequest("INVALID_SETTLEMENT_COOLDOWN",
			fmt.Sprintf("settlement cooldown must be between %d and %d seconds",
				int(minProviderSettlementCooldown/time.Second),
				int(maxProviderSettlementCooldown/time.Second)))
	}

	// ---- 调度优先级 ----
	if out.AccountPriority < 0 || out.AccountPriority > providerMaxAccountPriority {
		return out, infraerrors.BadRequest("INVALID_ACCOUNT_PRIORITY",
			fmt.Sprintf("account priority must be between 0 and %d", providerMaxAccountPriority))
	}

	return out, nil
}

func validateTierNumbers(label string, concurrency, maxSessions, baseRPM int, windowCostLimit float64) error {
	// 并发下限是 1，不是 0。ConcurrencyService.AcquireAccountSlot 对
	// maxConcurrency <= 0 直接返回「无限制」，所以填 0 的效果不是「没有容量」
	// 而是「不限并发」——与管理员的直觉完全相反，还会让本该最小的档位变成最大。
	//
	// max_sessions / base_rpm / window_cost_limit 的 0 表示「不启用该限制」，
	// 那是既有约定，与此处不同，不要一起改。
	if concurrency < 1 || concurrency > providerMaxConcurrency {
		return infraerrors.BadRequest("INVALID_TIER_CONCURRENCY",
			fmt.Sprintf("%s: concurrency must be between 1 and %d (0 would mean unlimited, not zero capacity)",
				label, providerMaxConcurrency))
	}
	if maxSessions < 0 || maxSessions > providerMaxSessions {
		return infraerrors.BadRequest("INVALID_TIER_SESSIONS",
			fmt.Sprintf("%s: max sessions must be between 0 and %d", label, providerMaxSessions))
	}
	if baseRPM < 0 || baseRPM > providerMaxBaseRPM {
		return infraerrors.BadRequest("INVALID_TIER_RPM",
			fmt.Sprintf("%s: base RPM must be between 0 and %d", label, providerMaxBaseRPM))
	}
	if windowCostLimit < 0 || windowCostLimit > providerMaxWindowCostLimit {
		return infraerrors.BadRequest("INVALID_TIER_WINDOW_COST",
			fmt.Sprintf("%s: window cost limit must be between 0 and %.0f", label, providerMaxWindowCostLimit))
	}
	return nil
}

// EnabledHostingTypes 返回按 sort 排序的已启用托管类型。
func (p ProviderSettings) EnabledHostingTypes() []ProviderHostingType {
	out := make([]ProviderHostingType, 0, len(p.HostingTypes))
	for _, ht := range p.HostingTypes {
		if ht.Enabled {
			out = append(out, ht)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort < out[j].Sort })
	return out
}

// FindHostingType 按 group id 查找已启用的托管类型。
func (p ProviderSettings) FindHostingType(groupID int64) (ProviderHostingType, bool) {
	for _, ht := range p.HostingTypes {
		if ht.GroupID == groupID && ht.Enabled {
			return ht, true
		}
	}
	return ProviderHostingType{}, false
}

// EnabledTiers 返回已启用的固定档位。
func (p ProviderSettings) EnabledTiers() []ProviderCapacityTier {
	out := make([]ProviderCapacityTier, 0, len(p.CapacityTiers))
	for _, t := range p.CapacityTiers {
		if t.Enabled {
			out = append(out, t)
		}
	}
	return out
}

// FindTier 按档位标识查找已启用的固定档。
func (p ProviderSettings) FindTier(tier string) (ProviderCapacityTier, bool) {
	tier = strings.TrimSpace(tier)
	for _, t := range p.CapacityTiers {
		if t.Tier == tier && t.Enabled {
			return t, true
		}
	}
	return ProviderCapacityTier{}, false
}
