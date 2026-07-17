package service

import (
	"fmt"
	"time"
)

// 人格信封（persona envelope）：把一个池账号约束成“一个真人在本机 Claude Code 里使用”
// 的行为 + 本地化维度。配置全部落在 account.Extra(JSONB)，无需 schema 迁移；相关键缺失
// 或未启用时 persona 视为关闭，调度/转发行为与改造前完全一致（fail-open），可随时回滚。
//
// 与既有能力的分工（本结构只补“行为 + 本地化”，不重复造轮子）：
//   - 设备指纹（User-Agent / x-stainless-* / TLS）由 identityService 与 TLS profile 提供；
//   - 出口 IP 由 account.Proxy 提供；
//   - 并发槽、会话数、RPM、窗口费用已有各自的 Extra 键与调度检查；
//   - 本结构新增：时区、locale、作息窗口、（messages 维度）并发上限、日请求上限。
//
// 设计动机见死亡取证：账号中位寿命 ~5 天，主因是被复用成“24 小时长亮、跨时区”的机器画像；
// 而全池峰值并发仅 ~10，账号数远超需求——因此把每个账号整形成“单人作息”几乎零吞吐代价。
const (
	extraPersonaEnabled        = "persona_enabled"
	extraPersonaTimezone       = "persona_timezone"          // IANA 名称，如 "America/New_York"
	extraPersonaLocale         = "persona_locale"            // BCP-47，如 "en-US"
	extraPersonaActiveStart    = "persona_active_start_hour" // 0..23（含）
	extraPersonaActiveEnd      = "persona_active_end_hour"   // 1..24（不含；24 表示到当地午夜）
	extraPersonaMaxConcurrency = "persona_max_concurrency"   // 每账号 messages 真实并发上限
	extraPersonaDailyCap       = "persona_daily_request_cap" // 每日请求上限（0 = 不限）
)

// PersonaEnvelope 是账号人格的行为/本地化配置快照。
type PersonaEnvelope struct {
	Enabled        bool
	Timezone       string // 空 = 未指定（作息判定退化为不门控）
	Locale         string
	ActiveStart    int // 0..23，含
	ActiveEnd      int // 1..24，不含；ActiveEnd <= ActiveStart 时表示跨夜窗口
	MaxConcurrency int // 0 = 未指定（回落到 account.Concurrency）
	DailyCap       int // 0 = 不限
}

// personaBool 采用与 intercept_warmup_requests 一致的严格布尔语义：仅真正的 bool true
// 视为开启，字符串 "true"/1 等一律不识别，避免误开导致线上行为意外改变。
func personaBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

// GetPersonaEnvelope 从 account.Extra 读取人格配置。未配置时返回 Enabled=false 的零值信封。
func (a *Account) GetPersonaEnvelope() PersonaEnvelope {
	if a == nil || a.Extra == nil {
		return PersonaEnvelope{}
	}
	env := PersonaEnvelope{
		Enabled:  personaBool(a.Extra[extraPersonaEnabled]),
		Timezone: a.GetExtraString(extraPersonaTimezone),
		Locale:   a.GetExtraString(extraPersonaLocale),
	}
	if v, ok := a.Extra[extraPersonaActiveStart]; ok {
		env.ActiveStart = parseExtraInt(v)
	}
	if v, ok := a.Extra[extraPersonaActiveEnd]; ok {
		env.ActiveEnd = parseExtraInt(v)
	}
	if v, ok := a.Extra[extraPersonaMaxConcurrency]; ok {
		env.MaxConcurrency = parseExtraInt(v)
	}
	if v, ok := a.Extra[extraPersonaDailyCap]; ok {
		env.DailyCap = parseExtraInt(v)
	}
	return env
}

// IsPersonaEnabled 报告该账号是否启用了人格信封。
func (a *Account) IsPersonaEnabled() bool {
	return a != nil && a.Extra != nil && personaBool(a.Extra[extraPersonaEnabled])
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

// hasActiveWindow 报告是否配置了有效的作息窗口。start==end==0 视为“未配置”，
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
//   - 未配置作息窗口 → true
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

// EffectiveMaxConcurrency 返回人格约束下的 messages 并发上限：
// 显式配置优先，否则回落到账号自身 concurrency。<=0 表示不额外限制。
func (a *Account) EffectivePersonaMaxConcurrency() int {
	env := a.GetPersonaEnvelope()
	if env.Enabled && env.MaxConcurrency > 0 {
		return env.MaxConcurrency
	}
	return a.Concurrency
}

// validatePersonaFieldValues 校验 map 中出现的每个 persona_* 字段取值是否合法
// （时区可解析、作息小时在范围、上限非负）。不关心 persona_enabled 是否存在。
func validatePersonaFieldValues(m map[string]any) error {
	if tz, ok := m[extraPersonaTimezone].(string); ok && tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("persona_timezone %q is not a valid IANA timezone", tz)
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
	return nil
}

// ValidatePersonaConfig 校验完整 Extra 中的人格配置（仅当出现 persona_enabled 键时才校验）。
// 供账号创建/整体更新（覆盖式写入 account.Extra）调用。
func ValidatePersonaConfig(extra map[string]any) error {
	if extra == nil {
		return nil
	}
	if _, ok := extra[extraPersonaEnabled]; !ok {
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
