package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTimezoneForCountry(t *testing.T) {
	// 单时区国家：直接返回代表时区
	require.Equal(t, "Asia/Tokyo", TimezoneForCountry("JP", ""))
	require.Equal(t, "Asia/Singapore", TimezoneForCountry("SG", "anything"))
	require.Equal(t, "Asia/Shanghai", TimezoneForCountry("cn", "")) // 大小写不敏感

	// 美国：按 Region 关键字挑；取不到用列表首项（America/New_York）
	require.Equal(t, "America/Los_Angeles", TimezoneForCountry("US", "California"))
	require.Equal(t, "America/Chicago", TimezoneForCountry("US", "Texas"))
	require.Equal(t, "America/New_York", TimezoneForCountry("US", "")) // 未知 region 回落首项
	require.Equal(t, "America/New_York", TimezoneForCountry("US", "Narnia"))

	// 未知国家：返回空（调用方据此不 prefill）
	require.Equal(t, "", TimezoneForCountry("ZZ", ""))
	require.Equal(t, "", TimezoneForCountry("", ""))
}

func TestLocaleForCountry(t *testing.T) {
	require.Equal(t, "zh-CN", LocaleForCountry("cn"))
	require.Equal(t, "en-US", LocaleForCountry("US"))
	require.Equal(t, "en-SG", LocaleForCountry("SG"))
	require.Equal(t, "ja-JP", LocaleForCountry("JP"))
	require.Equal(t, "", LocaleForCountry("ZZ"))
	require.Equal(t, "", LocaleForCountry(""))
}

func TestPersonaTimezoneMatchesCountry(t *testing.T) {
	// 一致
	require.True(t, PersonaTimezoneMatchesCountry("Asia/Tokyo", "JP"))
	require.True(t, PersonaTimezoneMatchesCountry("America/Los_Angeles", "US"))
	require.True(t, PersonaTimezoneMatchesCountry("America/New_York", "us")) // 大小写不敏感

	// 破绽：美国 IP 却声称东八区 -> 不一致
	require.False(t, PersonaTimezoneMatchesCountry("Asia/Shanghai", "US"))
	require.False(t, PersonaTimezoneMatchesCountry("Asia/Tokyo", "US"))

	// 信息不足 / 未知国家 -> fail-open 放行
	require.True(t, PersonaTimezoneMatchesCountry("", "US"))
	require.True(t, PersonaTimezoneMatchesCountry("Asia/Tokyo", ""))
	require.True(t, PersonaTimezoneMatchesCountry("Asia/Tokyo", "ZZ"))
}

func TestValidatePersonaExtraPatch(t *testing.T) {
	require.NoError(t, ValidatePersonaExtraPatch(nil))
	require.NoError(t, ValidatePersonaExtraPatch(map[string]any{}))

	// 只改时区（无 persona_enabled）也应校验
	require.Error(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaTimezone: "Not/AZone"}))
	require.NoError(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaTimezone: "Asia/Tokyo"}))

	// 作息小时越界
	require.Error(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaActiveStart: 24}))
	require.Error(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaActiveEnd: 0}))
	require.NoError(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaActiveStart: 9, extraPersonaActiveEnd: 24}))

	// 上限非负
	require.Error(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaMaxConcurrency: -1}))
	require.NoError(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaMaxConcurrency: 2, extraPersonaDailyCap: 500}))
}
