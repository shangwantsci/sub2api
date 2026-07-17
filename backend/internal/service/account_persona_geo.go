package service

import "strings"

// 账号人格时区的地理判定：把代理出口 IP 的国家/地区映射到一个代表性 IANA 时区，
// 用于在创建/编辑账号时按所绑代理"自动 prefill 时区"，并做"代理国家 vs 选定时区"
// 一致性校验（挡住"美国 IP 却声称 +08"这类破绽）。
//
// 设计取舍：
//   - 只需覆盖常见住宅代理国家；未知国家返回空（不 prefill、不阻断，交由人工填）。
//   - 多时区国家（US/CA/AU）用 Region/City 关键字挑代表时区，取不到就用列表首项。
//   - 一致性校验用"国家 -> 允许时区集合"的成员判断，允许一国多时区。

// countryTimezones 是"国家码 -> 该国允许的 IANA 时区集合"。集合首项为代表时区（用于 prefill）。
var countryTimezones = map[string][]string{
	"JP": {"Asia/Tokyo"},
	"SG": {"Asia/Singapore"},
	"CN": {"Asia/Shanghai", "Asia/Urumqi"},
	"HK": {"Asia/Hong_Kong"},
	"TW": {"Asia/Taipei"},
	"KR": {"Asia/Seoul"},
	"IN": {"Asia/Kolkata"},
	"ID": {"Asia/Jakarta", "Asia/Makassar", "Asia/Jayapura"},
	"MY": {"Asia/Kuala_Lumpur"},
	"TH": {"Asia/Bangkok"},
	"VN": {"Asia/Ho_Chi_Minh"},
	"PH": {"Asia/Manila"},
	"AE": {"Asia/Dubai"},
	"GB": {"Europe/London"},
	"IE": {"Europe/Dublin"},
	"DE": {"Europe/Berlin"},
	"FR": {"Europe/Paris"},
	"NL": {"Europe/Amsterdam"},
	"ES": {"Europe/Madrid"},
	"IT": {"Europe/Rome"},
	"SE": {"Europe/Stockholm"},
	"CH": {"Europe/Zurich"},
	"PL": {"Europe/Warsaw"},
	"US": {"America/New_York", "America/Chicago", "America/Denver", "America/Los_Angeles", "America/Phoenix", "America/Anchorage", "Pacific/Honolulu"},
	"CA": {"America/Toronto", "America/Winnipeg", "America/Edmonton", "America/Vancouver", "America/Halifax"},
	"AU": {"Australia/Sydney", "Australia/Melbourne", "Australia/Brisbane", "Australia/Adelaide", "Australia/Perth"},
	"BR": {"America/Sao_Paulo"},
	"MX": {"America/Mexico_City"},
}

// countryLocales 是 persona_locale 的组织元数据默认值。它不会写入出站请求头；
// 仅用于在管理员选择“按代理自动识别”时免去手填。
var countryLocales = map[string]string{
	"JP": "ja-JP",
	"SG": "en-SG",
	"CN": "zh-CN",
	"HK": "zh-HK",
	"TW": "zh-TW",
	"KR": "ko-KR",
	"US": "en-US",
	"CA": "en-CA",
	"GB": "en-GB",
	"AU": "en-AU",
}

// usRegionTimezones 把美国常见州/地区名映射到时区（Region 文本来自代理探测 exitInfo.Region）。
var usRegionTimezones = map[string]string{
	"california": "America/Los_Angeles", "washington": "America/Los_Angeles", "oregon": "America/Los_Angeles", "nevada": "America/Los_Angeles",
	"new york": "America/New_York", "new jersey": "America/New_York", "florida": "America/New_York", "georgia": "America/New_York",
	"virginia": "America/New_York", "massachusetts": "America/New_York", "pennsylvania": "America/New_York", "ohio": "America/New_York",
	"north carolina": "America/New_York", "michigan": "America/New_York",
	"illinois": "America/Chicago", "texas": "America/Chicago", "missouri": "America/Chicago", "minnesota": "America/Chicago",
	"wisconsin": "America/Chicago", "louisiana": "America/Chicago",
	"colorado": "America/Denver", "utah": "America/Denver", "new mexico": "America/Denver",
	"arizona": "America/Phoenix", "hawaii": "Pacific/Honolulu", "alaska": "America/Anchorage",
}

// TimezoneForCountry 返回该国家/地区的代表性 IANA 时区，用于自动 prefill。
// 未知国家返回空字符串（调用方据此不 prefill）。
func TimezoneForCountry(countryCode, region string) string {
	cc := strings.ToUpper(strings.TrimSpace(countryCode))
	zones, ok := countryTimezones[cc]
	if !ok || len(zones) == 0 {
		return ""
	}
	// 多时区国家：优先按 Region 关键字挑（目前覆盖 US）。
	if cc == "US" {
		if tz := usRegionTimezones[strings.ToLower(strings.TrimSpace(region))]; tz != "" {
			return tz
		}
	}
	return zones[0]
}

// LocaleForCountry 返回代理出口国家对应的 BCP-47 persona locale 组织默认值。
// 未知国家返回空；locale 仍然只作管理元数据，不注入 Accept-Language。
func LocaleForCountry(countryCode string) string {
	return countryLocales[strings.ToUpper(strings.TrimSpace(countryCode))]
}

// PersonaTimezoneMatchesCountry 校验人格时区与代理出口国家是否自洽。
// 返回 true 表示一致或无法判断（fail-open，不误伤）：
//   - tz 或 countryCode 为空 -> true（信息不足，不阻断）
//   - 未知国家 -> true（映射表未覆盖，不阻断）
//   - 已知国家 -> tz 必须落在该国允许集合内
func PersonaTimezoneMatchesCountry(tz, countryCode string) bool {
	tz = strings.TrimSpace(tz)
	cc := strings.ToUpper(strings.TrimSpace(countryCode))
	if tz == "" || cc == "" {
		return true
	}
	zones, ok := countryTimezones[cc]
	if !ok {
		return true
	}
	for _, z := range zones {
		if z == tz {
			return true
		}
	}
	return false
}
