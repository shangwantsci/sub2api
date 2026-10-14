package service

import (
	"fmt"
	"math"
	"time"
)

// 人格信封（persona envelope）：把一个池账号约束成"一个真人在本机 Claude Code 里使用"
// 的行为 + 本地化维度。配置全部落在 account.Extra(JSONB)，无需 schema 迁移；相关键缺失
// 或未启用时 persona 视为关闭，调度/转发行为与改造前完全一致（fail-open），可随时回滚。
//
// 与既有能力的分工（本结构只补"行为 + 本地化"，不重复造轮子）：
//   - 设备指纹（User-Agent / x-stainless-* / TLS）由 identityService 与 TLS profile 提供；
//   - 出口 IP 由 account.Proxy 提供；
//   - 并发槽、会话数、RPM、窗口费用已有各自的 Extra 键与调度检查；
//   - 本结构新增：时区、locale、防封预设档位、动态并发（基于利用率）、日请求上限。
//
// v2 简化设计：用户只需设置 persona_preset（aggressive/balanced/conservative），
// 系统自动推导并发、日上限、养号策略。动态并发根据 Anthropic 5h/7d 利用率实时调节，
// 使用模式自然有波峰波谷。旧的逐字段配置仍向后兼容。
const (
	extraPersonaEnabled            = "persona_enabled"
	extraPersonaTimezone           = "persona_timezone"             // IANA 名称，如 "America/New_York"
	extraPersonaLocale             = "persona_locale"               // BCP-47，如 "en-US"
	extraPersonaActiveStart        = "persona_active_start_hour"    // 0..23（含）
	extraPersonaActiveEnd          = "persona_active_end_hour"      // 1..24（不含；24 表示到当地午夜）
	extraPersonaMaxConcurrency     = "persona_max_concurrency"      // 每账号 messages 真实并发上限
	extraPersonaDailyCap           = "persona_daily_request_cap"    // 每日请求上限（0 = 不限）
	extraPersonaOffPeakConcurrency = "persona_off_peak_concurrency" // 非活跃时段并发上限（0 = 自动计算）
	extraPersonaPreset             = "persona_preset"               // 防封预设档位：off/aggressive/balanced/conservative

	// 利用率数据键（由 ratelimit_service 被动采样写入，此处只读）
	extraSessionWindowUtilization = "session_window_utilization"   // 5h 利用率，0-1
	extraPassiveUsage7d           = "passive_usage_7d_utilization" // 7d 利用率，0-1
)

// PersonaPreset 防封预设档位。
type PersonaPreset string

const (
	PersonaPresetOff          PersonaPreset = "off"
	PersonaPresetAggressive   PersonaPreset = "aggressive"
	PersonaPresetBalanced     PersonaPreset = "balanced"
	PersonaPresetConservative PersonaPreset = "conservative"
)

// presetConfig 每个档位的默认参数。
// DailyCap=0 表示不限日请求数——依靠动态并发（基于利用率）自动控制节奏，
// 不需要人为猜一个日上限值。利用率高→并发降→请求自然变慢→充分利用但不爆。
type presetConfig struct {
	BaseConcurrency int
	BaseRPM         int // 账号未单独配置 base_rpm 时使用的推荐上限
	DailyCap        int // 0 = 不限，由动态并发和 RPM 自动调节
}

// 推荐 RPM 对齐供号商固定档：保守≈真人上限，均衡=3 档，激进=4 档。
// 利用率低于 50% 时用满这个值；更高时与并发一起按同一条曲线下降。
var presetConfigMap = map[PersonaPreset]presetConfig{
	PersonaPresetAggressive:   {BaseConcurrency: 5, BaseRPM: 50, DailyCap: 0},
	PersonaPresetBalanced:     {BaseConcurrency: 3, BaseRPM: 30, DailyCap: 0},
	PersonaPresetConservative: {BaseConcurrency: 2, BaseRPM: 15, DailyCap: 0},
}

// IsValidPreset 检查预设名是否合法。
func IsValidPreset(s string) bool {
	switch PersonaPreset(s) {
	case PersonaPresetOff, PersonaPresetAggressive, PersonaPresetBalanced, PersonaPresetConservative:
		return true
	}
	return false
}

// PersonaEnvelope 是账号人格的行为/本地化配置快照。
type PersonaEnvelope struct {
	Enabled            bool
	Preset             PersonaPreset // 预设档位，非空时自动推导 MaxConcurrency/DailyCap
	Timezone           string        // 空 = 未指定；预设模式下默认 America/New_York
	Locale             string        // 空 = 未指定；预设模式下默认 en-US
	ActiveStart        int           // 0..23，含（预设模式下不使用作息限制）
	ActiveEnd          int           // 1..24，不含
	MaxConcurrency     int           // 基础并发上限（预设模式从档位推导）
	DailyCap           int           // 日请求上限（预设模式从档位推导）
	OffPeakConcurrency int           // 非活跃时段并发上限（0 = 自动）
	// 利用率快照（从 Extra 读取，用于动态并发计算）
	Utilization5h float64 // 5h 窗口利用率，0-1，<=0 表示无数据
	Utilization7d float64 // 7d 窗口利用率，0-1，<=0 表示无数据
}

// personaBool 采用与 intercept_warmup_requests 一致的严格布尔语义：仅真正的 bool true
// 视为开启，字符串 "true"/1 等一律不识别，避免误开导致线上行为意外改变。
func personaBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

// GetPersonaEnvelope 从 account.Extra 读取人格配置。未配置时返回 Enabled=false 的零值信封。
// 当 persona_preset 存在且非 "off" 时，自动推导 Enabled/MaxConcurrency/DailyCap/Timezone/Locale。
func (a *Account) GetPersonaEnvelope() PersonaEnvelope {
	if a == nil || a.Extra == nil {
		return PersonaEnvelope{}
	}

	// 先读预设档位
	preset := PersonaPreset(a.GetExtraString(extraPersonaPreset))

	env := PersonaEnvelope{
		Enabled:  personaBool(a.Extra[extraPersonaEnabled]),
		Preset:   preset,
		Timezone: a.GetExtraString(extraPersonaTimezone),
		Locale:   a.GetExtraString(extraPersonaLocale),
	}

	// 预设模式：非 off 的有效预设自动启用 persona 并填充默认值
	if cfg, ok := presetConfigMap[preset]; ok {
		env.Enabled = true
		if env.MaxConcurrency <= 0 {
			env.MaxConcurrency = cfg.BaseConcurrency
		}
		if env.DailyCap <= 0 {
			env.DailyCap = cfg.DailyCap
		}
		// 预设模式下默认美东时区和 en-US locale
		if env.Timezone == "" {
			env.Timezone = "America/New_York"
		}
		if env.Locale == "" {
			env.Locale = "en-US"
		}
	}
	// preset == "off" 时显式禁用
	if preset == PersonaPresetOff {
		env.Enabled = false
	}

	// 手动字段覆盖预设默认值
	if v, ok := a.Extra[extraPersonaActiveStart]; ok {
		env.ActiveStart = parseExtraInt(v)
	}
	if v, ok := a.Extra[extraPersonaActiveEnd]; ok {
		env.ActiveEnd = parseExtraInt(v)
	}
	if v, ok := a.Extra[extraPersonaMaxConcurrency]; ok {
		if mc := parseExtraInt(v); mc > 0 {
			env.MaxConcurrency = mc
		}
	}
	if v, ok := a.Extra[extraPersonaDailyCap]; ok {
		if dc := parseExtraInt(v); dc > 0 {
			env.DailyCap = dc
		}
	}
	if v, ok := a.Extra[extraPersonaOffPeakConcurrency]; ok {
		env.OffPeakConcurrency = parseExtraInt(v)
	}

	// 读取利用率快照
	if v, ok := a.Extra[extraSessionWindowUtilization]; ok {
		env.Utilization5h = parseExtraFloat64(v)
	}
	if v, ok := a.Extra[extraPassiveUsage7d]; ok {
		env.Utilization7d = parseExtraFloat64(v)
	}

	return env
}

// IsPersonaEnabled 报告该账号是否启用了人格信封（含预设模式）。
func (a *Account) IsPersonaEnabled() bool {
	if a == nil || a.Extra == nil {
		return false
	}
	// 预设模式优先
	preset := PersonaPreset(a.GetExtraString(extraPersonaPreset))
	if _, ok := presetConfigMap[preset]; ok {
		return true
	}
	if preset == PersonaPresetOff {
		return false
	}
	return personaBool(a.Extra[extraPersonaEnabled])
}

// GetPersonaPreset 返回账号的防封预设档位。空字符串表示未设置预设（使用旧模式）。
func (a *Account) GetPersonaPreset() PersonaPreset {
	if a == nil || a.Extra == nil {
		return ""
	}
	return PersonaPreset(a.GetExtraString(extraPersonaPreset))
}

// location 解析人格时区；无效或未设置时回落到 UTC（调用方据此判定是否门控）。
func (p PersonaEnvelope) location() *time.Location {
	if p.Timezone == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(p.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

// hasActiveWindow 报告是否配置了有效的作息窗口。start==end==0 视为"未配置"，
// 以便启用 persona 但暂未设定作息时不会意外拦截任何流量。
func (p PersonaEnvelope) hasActiveWindow() bool {
	if p.ActiveStart == 0 && p.ActiveEnd == 0 {
		return false
	}
	return true
}

// IsWithinActiveHours 判定给定时刻是否落在该人格（其时区下）的作息窗口内。
// 语义（全部 fail-open，倾向放行以免误伤线上流量）：
//   - persona 未启用 → true
//   - 未配置作息窗口 → true（预设模式下不使用作息，因此恒 true）
//   - ActiveStart < ActiveEnd → [start, end) 当日窗口
//   - ActiveStart >= ActiveEnd → 跨夜窗口，如 22..6 表示 [22,24) ∪ [0,6)
func (p PersonaEnvelope) IsWithinActiveHours(now time.Time) bool {
	if !p.Enabled || !p.hasActiveWindow() {
		return true
	}
	h := now.In(p.location()).Hour() // 0..23
	start, end := p.ActiveStart, p.ActiveEnd
	if start < end {
		return h >= start && h < end
	}
	return h >= start || h < end
}

// DayKey 返回该人格时区下的当日日期键（YYYYMMDD），供日请求上限按人格本地日历跨日
// 重置。时区无效/未设置时按 UTC。
func (p PersonaEnvelope) DayKey(now time.Time) string {
	return now.In(p.location()).Format("20060102")
}

// throttleFactor 根据利用率返回动态并发缩减因子（0.1 - 1.0）。
// 曲线设计：
//   - u < 50%:  1.0  （全速，充分利用）
//   - u = 80%:  0.5  （开始明显减速）
//   - u = 95%:  0.2  （接近上限，大幅减速）
//   - u >= 95%: 0.1  （几乎暂停，保留最低并发防止完全浪费）
//
// 利用率 <= 0 视为无数据，返回 1.0（fail-open）。
func throttleFactor(utilization float64) float64 {
	if utilization <= 0 {
		return 1.0
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

// DynamicConcurrency 计算利用率驱动的动态并发值。
// effective = max(1, floor(baseConcurrency × min(factor5h, factor7d)))
// 无利用率数据时返回 baseConcurrency（fail-open）。
func (p PersonaEnvelope) DynamicConcurrency(baseConcurrency int) int {
	if baseConcurrency <= 0 {
		return 1
	}
	factor5h := throttleFactor(p.Utilization5h)
	factor7d := throttleFactor(p.Utilization7d)
	factor := math.Min(factor5h, factor7d)
	effective := int(math.Floor(float64(baseConcurrency) * factor))
	if effective < 1 {
		effective = 1
	}
	return effective
}

// DynamicRPM 计算利用率驱动的动态 RPM 值。
// effective = max(1, floor(baseRPM × min(factor5h, factor7d)))
// 无利用率数据时返回 baseRPM（fail-open）。
func (p PersonaEnvelope) DynamicRPM(baseRPM int) int {
	if baseRPM <= 0 {
		return baseRPM // 0 = 未配置，不动
	}
	factor5h := throttleFactor(p.Utilization5h)
	factor7d := throttleFactor(p.Utilization7d)
	factor := math.Min(factor5h, factor7d)
	effective := int(math.Floor(float64(baseRPM) * factor))
	if effective < 1 {
		effective = 1
	}
	return effective
}

// EffectivePersonaRPM 返回动态 RPM 上限（基于利用率调节）。
// 账号已配置 base_rpm 时以它为天花板；未配置且启用了预设时，用档位推荐值。
// 未启用 persona 时返回原始 baseRPM，不做调节。
func (a *Account) EffectivePersonaRPM() int {
	if a == nil {
		return 0
	}
	baseRPM := a.GetBaseRPM()
	if !a.IsPersonaEnabled() {
		return baseRPM
	}
	if baseRPM <= 0 {
		if cfg, ok := presetConfigMap[a.GetPersonaPreset()]; ok {
			baseRPM = cfg.BaseRPM
		}
	}
	if baseRPM <= 0 {
		return 0
	}
	return a.GetPersonaEnvelope().DynamicRPM(baseRPM)
}

// RPMCeiling 返回调度应执行的 RPM 上限。0 表示不限制。
func (a *Account) RPMCeiling() int {
	if a == nil {
		return 0
	}
	if a.IsPersonaEnabled() {
		return a.EffectivePersonaRPM()
	}
	rpm := a.GetBaseRPM()
	if rpm < 0 {
		return 0
	}
	return rpm
}

// EffectivePersonaConcurrency 返回动态并发上限（基于利用率调节）。
// 预设模式：baseConcurrency 来自档位，动态调节。
// 旧模式：使用 MaxConcurrency 作为 base，动态调节。
// 未启用或未配置：回落 account.Concurrency。
func (a *Account) EffectivePersonaConcurrency() int {
	env := a.GetPersonaEnvelope()
	if !env.Enabled {
		return a.Concurrency
	}
	base := env.MaxConcurrency
	if base <= 0 {
		return a.Concurrency
	}
	return env.DynamicConcurrency(base)
}

// EffectivePersonaMaxConcurrency 返回人格约束下的 messages 并发上限（旧接口，向后兼容）。
// 如果启用了预设或 persona，使用动态并发（基于利用率）。
// 活跃时段返回 MaxConcurrency；非活跃时段返回 OffPeakConcurrency（或自动
// max(MaxConcurrency/2, 1)），实现软限流而非硬拦截。未启用/未配置时回落 account.Concurrency。
func (a *Account) EffectivePersonaMaxConcurrency() int {
	env := a.GetPersonaEnvelope()
	if !env.Enabled || env.MaxConcurrency <= 0 {
		return a.Concurrency
	}

	// 预设模式：使用动态并发，不区分活跃/非活跃时段
	if env.Preset != "" && env.Preset != PersonaPresetOff {
		return env.DynamicConcurrency(env.MaxConcurrency)
	}

	// 旧模式：保持原有活跃/非活跃逻辑 + 叠加动态调节
	if env.IsWithinActiveHours(time.Now()) {
		return env.DynamicConcurrency(env.MaxConcurrency)
	}
	// 非活跃时段
	offPeak := env.OffPeakConcurrency
	if offPeak <= 0 {
		offPeak = env.MaxConcurrency / 2
		if offPeak < 1 {
			offPeak = 1
		}
	}
	return env.DynamicConcurrency(offPeak)
}

// validatePersonaFieldValues 校验 map 中出现的每个 persona_* 字段取值是否合法
// （时区可解析、作息小时在范围、上限非负、预设名合法）。不关心 persona_enabled 是否存在。
func validatePersonaFieldValues(m map[string]any) error {
	if tz, ok := m[extraPersonaTimezone].(string); ok && tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("persona_timezone %q is not a valid IANA timezone", tz)
		}
	}
	if v, ok := m[extraPersonaPreset].(string); ok && v != "" {
		if !IsValidPreset(v) {
			return fmt.Errorf("persona_preset must be one of: off, aggressive, balanced, conservative; got %q", v)
		}
	}
	if v, ok := m[extraPersonaActiveStart]; ok {
		if h := parseExtraInt(v); h < 0 || h > 23 {
			return fmt.Errorf("persona_active_start_hour must be 0..23, got %d", h)
		}
	}
	if v, ok := m[extraPersonaActiveEnd]; ok {
		if h := parseExtraInt(v); h < 1 || h > 24 {
			return fmt.Errorf("persona_active_end_hour must be 1..24, got %d", h)
		}
	}
	// 拒绝零长度窗口：start==end（此处 end 已保证 >=1，故必为 1..23 的等值）语义歧义——
	// 旧实现会落到"恒在班/全天"，反直觉。全天请用 0..24，禁用请不传作息键。
	if sv, sok := m[extraPersonaActiveStart]; sok {
		if ev, eok := m[extraPersonaActiveEnd]; eok {
			if parseExtraInt(sv) == parseExtraInt(ev) {
				return fmt.Errorf("persona_active_start_hour and persona_active_end_hour must differ (got %d); use 0..24 for all-day or omit both to disable", parseExtraInt(sv))
			}
		}
	}
	if v, ok := m[extraPersonaMaxConcurrency]; ok {
		if c := parseExtraInt(v); c < 0 {
			return fmt.Errorf("persona_max_concurrency must be >= 0, got %d", c)
		}
	}
	if v, ok := m[extraPersonaDailyCap]; ok {
		if c := parseExtraInt(v); c < 0 {
			return fmt.Errorf("persona_daily_request_cap must be >= 0, got %d", c)
		}
	}
	if v, ok := m[extraPersonaOffPeakConcurrency]; ok {
		if c := parseExtraInt(v); c < 0 {
			return fmt.Errorf("persona_off_peak_concurrency must be >= 0, got %d", c)
		}
	}
	return nil
}

// ValidatePersonaConfig 校验完整 Extra 中的人格配置（仅当出现 persona_enabled 或 persona_preset 键时才校验）。
// 供账号创建/整体更新（覆盖式写入 account.Extra）调用。
func ValidatePersonaConfig(extra map[string]any) error {
	if extra == nil {
		return nil
	}
	_, hasEnabled := extra[extraPersonaEnabled]
	_, hasPreset := extra[extraPersonaPreset]
	if !hasEnabled && !hasPreset {
		return nil
	}
	return validatePersonaFieldValues(extra)
}

// ValidatePersonaExtraPatch 校验一次"部分合并"更新里出现的人格字段是否合法。
// 与 ValidatePersonaConfig 不同：不要求 patch 中出现 persona_enabled，只要出现任一
// persona_* 键就逐个校验取值。供 UpdateAccountExtra / 批量 extra 合并路径调用，
// 闭合"部分更新绕过校验写入非法 persona 配置"的口子。
func ValidatePersonaExtraPatch(updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	return validatePersonaFieldValues(updates)
}
