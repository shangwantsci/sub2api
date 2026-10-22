package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
//
// 速率档位的数值不在这里：它们属于统一的容量档位模板（见 capacity_presets.go），供货商设置
// 只保存「哪些模板对供货商开放」（enabled_tier_keys）与默认档位（default_tier）。

const (
	// ProviderTierCustom 是自定义档的档位标识。该档账号不参与「应用到存量」批量回填。
	ProviderTierCustom = "custom"

	providerSettingsDBTimeout = 5 * time.Second
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

// ProviderCustomTierCaps 是自定义档各项参数的上限护栏。
type ProviderCustomTierCaps struct {
	Concurrency     int     `json:"concurrency"`
	MaxSessions     int     `json:"max_sessions"`
	BaseRPM         int     `json:"base_rpm"`
	WindowCostLimit float64 `json:"window_cost_limit"`
}

// ProviderSettings 汇总供号商站点的全部设置。
type ProviderSettings struct {
	PortalEnabled  bool                  `json:"portal_enabled"`
	HostingTypes   []ProviderHostingType `json:"hosting_types"`
	DefaultGroupID int64                 `json:"default_group_id"`
	// EnabledTierKeys 是对供号商开放的容量档位模板 key；缺省 = 模板表里的全部 key。
	EnabledTierKeys []string `json:"enabled_tier_keys"`
	// DefaultTier 必须落在 EnabledTierKeys 内，且存在于模板表。
	DefaultTier        string                 `json:"default_tier"`
	CustomTierEnabled  bool                   `json:"custom_tier_enabled"`
	CustomTierCaps     ProviderCustomTierCaps `json:"custom_tier_caps"`
	SettlementTimezone string                 `json:"settlement_timezone"`
	// SettlementCooldownSeconds 封账终点相对当前时刻回退的秒数。
	SettlementCooldownSeconds int `json:"settlement_cooldown_seconds"`
	// AccountPriority 供号商账号的调度优先级。硬门槛语义，见 DefaultProviderAccountPriority。
	AccountPriority int `json:"account_priority"`
	// AutoProxyMaxAccounts 单个平台代理最多绑定多少个供号商账号。
	AutoProxyMaxAccounts int `json:"auto_proxy_max_accounts"`
	// ProxyModePolicy 供号商上号允许的代理来源：both | auto_only | manual_only。
	ProxyModePolicy string `json:"proxy_mode_policy"`

	// Presets 是容量档位模板表的只读快照，由 GetProviderSettings 填充，供 FindTier / EnabledTiers /
	// ResolveProviderAccountTier 这类纯函数查数值。它不参与 JSON：模板只归
	// GET/PUT /admin/settings/capacity-presets 管理，保存供货商设置时也不会写它
	// （SaveProviderSettings 校验前会重新读取最新模板）。
	Presets []CapacityPreset `json:"-"`
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

// 供号商上号时允许的代理来源策略。
const (
	ProviderProxyPolicyBoth       = "both"
	ProviderProxyPolicyAutoOnly   = "auto_only"
	ProviderProxyPolicyManualOnly = "manual_only"
)

// 单次上号请求声明的代理来源。
const (
	ProviderProxyModeAuto   = "auto"
	ProviderProxyModeManual = "manual"
)

// DefaultProviderProxyModePolicy 是代理来源策略的种子值：两种都开放。
const DefaultProviderProxyModePolicy = ProviderProxyPolicyBoth

// DefaultProviderAutoProxyMaxAccounts 是单个平台代理可绑定的供号商账号数种子值。
//
// 取 2 是伪装侧的保守值：同一出口 IP 上挂两个 Anthropic 账号还能解释成一台机器上
// 的两个身份，再多就开始像账号农场，直接冲撞本 fork「每个账号看起来像独立真人」
// 的核心目标。代理池扩容后可在设置页调大。
const DefaultProviderAutoProxyMaxAccounts = 2

// providerMaxAutoProxyMaxAccounts 是该设置项的上限护栏。
const providerMaxAutoProxyMaxAccounts = 50

func defaultProviderSettings() ProviderSettings {
	presets := DefaultCapacityPresets()
	return ProviderSettings{
		PortalEnabled:             false,
		HostingTypes:              []ProviderHostingType{},
		DefaultGroupID:            0,
		EnabledTierKeys:           presets.Keys(),
		DefaultTier:               presets.DefaultKey,
		Presets:                   presets.Presets,
		CustomTierEnabled:         true,
		CustomTierCaps:            DefaultProviderCustomTierCaps(),
		SettlementTimezone:        DefaultProviderSettlementTimezone,
		SettlementCooldownSeconds: int(DefaultProviderSettlementCooldown / time.Second),
		AccountPriority:           DefaultProviderAccountPriority,
		AutoProxyMaxAccounts:      DefaultProviderAutoProxyMaxAccounts,
		ProxyModePolicy:           DefaultProviderProxyModePolicy,
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
		SettingKeyCapacityPresets,
		SettingKeyProviderEnabledTierKeys,
		SettingKeyProviderDefaultTier,
		SettingKeyProviderCustomTierEnabled,
		SettingKeyProviderCustomTierCaps,
		SettingKeyProviderSettlementTimezone,
		SettingKeyProviderSettlementCooldownSeconds,
		SettingKeyProviderAccountPriority,
		SettingKeyProviderAutoProxyMaxAccounts,
		SettingKeyProviderProxyModePolicy,
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
	// 模板表：缺失或不可用时沿用 defaultProviderSettings 里的种子。
	presets := CapacityPresets{Presets: out.Presets, DefaultKey: out.DefaultTier}
	if raw, ok := values[SettingKeyCapacityPresets]; ok {
		if parsed, usable := parseCapacityPresets(raw); usable {
			presets = parsed
		} else if strings.TrimSpace(raw) != "" {
			slog.Warn("capacity_presets setting is unusable, falling back to the seed presets",
				"setting", SettingKeyCapacityPresets)
		}
	}
	out.Presets = presets.Presets

	// 开放档位：设置缺失（空串、不是合法 JSON 数组、JSON null）= 全部模板对供号商开放；
	// 显式存下的数组（含 []）按存的算，其中指向已不存在模板的 key 会被忽略，不让一条悬空引用把上号页弄坏。
	// 如果过滤后一个都不剩（例如管理员把对供号商开放的那几个模板都删了），结果就是「没有任何固定档开放」：
	// 宁可让设置页上看得见「没开放任何档位」，也不静默把新建的模板全部暴露给供号商。
	out.EnabledTierKeys = presets.Keys()
	if raw, ok := values[SettingKeyProviderEnabledTierKeys]; ok && strings.TrimSpace(raw) != "" {
		var stored []string
		// json.Unmarshal 对 null 不报错且得到 nil 切片，要和显式的 [] 区分开。
		if json.Unmarshal([]byte(raw), &stored) == nil && stored != nil {
			out.EnabledTierKeys = existingTierKeys(stored, presets)
		}
	}

	out.DefaultTier = presets.DefaultKey
	if raw, ok := values[SettingKeyProviderDefaultTier]; ok && strings.TrimSpace(raw) != "" {
		out.DefaultTier = strings.TrimSpace(raw)
	}
	// 默认档位必须是对供号商开放且存在的档位；否则落到第一个开放档位，而不是把悬空值交给上号页。
	if !containsString(out.EnabledTierKeys, out.DefaultTier) && len(out.EnabledTierKeys) > 0 {
		out.DefaultTier = out.EnabledTierKeys[0]
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
	if raw, ok := values[SettingKeyProviderAutoProxyMaxAccounts]; ok && strings.TrimSpace(raw) != "" {
		if v, convErr := strconv.Atoi(strings.TrimSpace(raw)); convErr == nil && v > 0 {
			out.AutoProxyMaxAccounts = v
		}
	}
	if raw, ok := values[SettingKeyProviderProxyModePolicy]; ok && strings.TrimSpace(raw) != "" {
		out.ProxyModePolicy = strings.TrimSpace(raw)
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
// 模板表只归 capacity-presets 接口管理：这里不信任调用方带来的 Presets，校验前重新读取最新模板，
// 并且只写「开放哪些档位」与默认档位，不写模板本身。
func (s *SettingService) SaveProviderSettings(ctx context.Context, in ProviderSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	presets, err := s.GetCapacityPresets(ctx)
	if err != nil {
		return err
	}
	in.Presets = presets.Presets
	normalized, err := ValidateProviderSettings(in)
	if err != nil {
		return err
	}

	hostingJSON, err := json.Marshal(normalized.HostingTypes)
	if err != nil {
		return fmt.Errorf("marshal hosting types: %w", err)
	}
	enabledKeysJSON, err := json.Marshal(normalized.EnabledTierKeys)
	if err != nil {
		return fmt.Errorf("marshal enabled tier keys: %w", err)
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
		SettingKeyProviderEnabledTierKeys:           string(enabledKeysJSON),
		SettingKeyProviderDefaultTier:               normalized.DefaultTier,
		SettingKeyProviderCustomTierEnabled:         strconv.FormatBool(normalized.CustomTierEnabled),
		SettingKeyProviderCustomTierCaps:            string(capsJSON),
		SettingKeyProviderSettlementTimezone:        normalized.SettlementTimezone,
		SettingKeyProviderSettlementCooldownSeconds: strconv.Itoa(normalized.SettlementCooldownSeconds),
		SettingKeyProviderAccountPriority:           strconv.Itoa(normalized.AccountPriority),
		SettingKeyProviderAutoProxyMaxAccounts:      strconv.Itoa(normalized.AutoProxyMaxAccounts),
		SettingKeyProviderProxyModePolicy:           normalized.ProxyModePolicy,
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

	// ---- 对供号商开放的速率档位 ----
	// 开放的 key 必须都存在于模板表（in.Presets）；重复的 key 静默去重，保留首次出现的顺序。
	templateKeys := make(map[string]bool, len(in.Presets))
	for _, preset := range in.Presets {
		templateKeys[preset.Key] = true
	}
	enabledTiers := make(map[string]bool, len(in.EnabledTierKeys))
	cleanKeys := make([]string, 0, len(in.EnabledTierKeys))
	for _, raw := range in.EnabledTierKeys {
		key := strings.TrimSpace(raw)
		if key == "" {
			return out, infraerrors.BadRequest("INVALID_TIER", "tier key must not be empty")
		}
		if key == ProviderTierCustom {
			return out, infraerrors.BadRequest("INVALID_TIER",
				"\"custom\" is reserved and cannot be listed in enabled_tier_keys")
		}
		if !templateKeys[key] {
			return out, infraerrors.BadRequest("UNKNOWN_TIER",
				fmt.Sprintf("tier %q does not exist in the capacity presets", key))
		}
		if enabledTiers[key] {
			continue
		}
		enabledTiers[key] = true
		cleanKeys = append(cleanKeys, key)
	}
	if len(cleanKeys) == 0 {
		return out, infraerrors.BadRequest("NO_ENABLED_TIER", "at least one capacity tier must be enabled")
	}
	out.EnabledTierKeys = cleanKeys

	// ---- 默认项必须处于开放状态（开放的 key 已确认都存在于模板表）----
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

	// ---- 自动分配代理 ----
	// 与结算冷却期一致：<=0 视为「没填」而回落种子值，不报错。
	if out.AutoProxyMaxAccounts <= 0 {
		out.AutoProxyMaxAccounts = DefaultProviderAutoProxyMaxAccounts
	}
	if out.AutoProxyMaxAccounts > providerMaxAutoProxyMaxAccounts {
		return out, infraerrors.BadRequest("INVALID_AUTO_PROXY_MAX_ACCOUNTS",
			fmt.Sprintf("auto proxy max accounts must be between 1 and %d", providerMaxAutoProxyMaxAccounts))
	}

	out.ProxyModePolicy = strings.TrimSpace(out.ProxyModePolicy)
	if out.ProxyModePolicy == "" {
		out.ProxyModePolicy = DefaultProviderProxyModePolicy
	}
	switch out.ProxyModePolicy {
	case ProviderProxyPolicyBoth, ProviderProxyPolicyAutoOnly, ProviderProxyPolicyManualOnly:
	default:
		return out, infraerrors.BadRequest("INVALID_PROXY_MODE_POLICY",
			"proxy mode policy must be one of both, auto_only, manual_only")
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

// EnabledTiers 返回对供号商开放的档位模板，按模板表内的顺序排列（与 EnabledTierKeys 的顺序无关）。
func (p ProviderSettings) EnabledTiers() []CapacityPreset {
	out := make([]CapacityPreset, 0, len(p.EnabledTierKeys))
	for _, preset := range p.Presets {
		if containsString(p.EnabledTierKeys, preset.Key) {
			out = append(out, preset)
		}
	}
	return out
}

// FindTier 按档位标识查找对供号商开放的固定档。
func (p ProviderSettings) FindTier(tier string) (CapacityPreset, bool) {
	tier = strings.TrimSpace(tier)
	if !containsString(p.EnabledTierKeys, tier) {
		return CapacityPreset{}, false
	}
	for _, preset := range p.Presets {
		if preset.Key == tier {
			return preset, true
		}
	}
	return CapacityPreset{}, false
}

// existingTierKeys 过滤出确实存在于模板表的 key，去重并保持原有顺序。
func existingTierKeys(keys []string, presets CapacityPresets) []string {
	out := make([]string, 0, len(keys))
	for _, raw := range keys {
		key := strings.TrimSpace(raw)
		if _, ok := presets.Find(key); !ok || containsString(out, key) {
			continue
		}
		out = append(out, key)
	}
	return out
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
