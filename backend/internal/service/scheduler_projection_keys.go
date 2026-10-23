package service

// SchedulerProjectedExtraKeys 列出调度快照的精简账号副本必须保留的 account.extra 键。
//
// 选号阶段（候选过滤、拿并发槽、排序）读取的任何 extra 键都必须登记在这里。
// 调度快照命中时，选号读到的是只保留这些键的精简账号；没登记的键读出来是空值，
// 而对限流类配置来说，空值等于「没配置」：RPM 门、动态降速等会静默放行，缓存未命中时
// 又回退读库恢复正常，所以现象是间歇性的，单测直接构造完整账号也抓不到。
//
// 新增选号阶段会读取的 extra 键时：
//  1. 登记到下面对应分组；
//  2. 在 repository/scheduler_projection_contract_unit_test.go 的 contractScenarios 加一行用例。
//
// 选中账号之后才发生的读取（转发、计费、用户消息限速的延迟计算等）读的是水合后的完整账号，
// 不需要登记。
func SchedulerProjectedExtraKeys() []string {
	out := make([]string, len(schedulerProjectedExtraKeys))
	copy(out, schedulerProjectedExtraKeys)
	return out
}

var schedulerProjectedExtraKeys = []string{
	// 调度模式
	"mixed_scheduling",

	// 5h 窗口费用门
	"window_cost_limit",
	"window_cost_sticky_reserve",

	// 会话数门
	"max_sessions",
	"session_idle_timeout_minutes",

	// RPM 门（三区/粘性豁免、粘性缓冲）
	"base_rpm",
	"rpm_strategy",
	"rpm_sticky_buffer",

	// 动态降速开关：并发上限、负载分母与 RPM 上限都要乘降速系数，下面的 5h/7d 利用率是它的输入。
	// 开关与利用率必须一起在投影里，否则缓存命中时降速静默失效（见 account_throttle.go）。
	"adaptive_throttle",

	// Anthropic 被动采样的 5h/7d 利用率与 Fable 新鲜度证据
	"session_window_utilization",
	"passive_usage_7d_utilization",
	"passive_usage_7d_reset",
	"passive_usage_7d_oi_utilization",
	"passive_usage_7d_oi_reset",
	"passive_usage_sampled_at",

	// API Key / Bedrock 账号自设额度（总 / 日 / 周）：Account.IsSchedulable → IsQuotaExceeded 在选号阶段读取，
	// 固定重置模式还要读重置时刻、星期与时区。键缺失时读出来是「没设额度」，
	// 额度已经用完的账号在缓存命中时照样被选中。
	"quota_limit",
	"quota_used",
	"quota_daily_limit",
	"quota_daily_used",
	"quota_daily_start",
	"quota_daily_reset_mode",
	"quota_daily_reset_hour",
	"quota_weekly_limit",
	"quota_weekly_used",
	"quota_weekly_start",
	"quota_weekly_reset_mode",
	"quota_weekly_reset_day",
	"quota_weekly_reset_hour",
	"quota_reset_timezone",

	// OpenAI 透传开关（新键 + 历史兼容键）：候选过滤的模型支持判断读它。
	// 缺失会把透传账号误判为「不支持该模型」：单独测账号能通，走网关却报 no available accounts。
	"openai_passthrough",
	"openai_oauth_passthrough",

	// OpenAI WebSocket / Responses 模式
	"openai_oauth_responses_websockets_v2_enabled",
	"openai_oauth_responses_websockets_v2_mode",
	"openai_apikey_responses_websockets_v2_enabled",
	"openai_apikey_responses_websockets_v2_mode",
	"responses_websockets_v2_enabled",
	"openai_ws_enabled",
	"openai_ws_force_http",
	"openai_responses_mode",
	"openai_responses_supported",

	// OpenAI Codex 用量与自动暂停阈值
	"codex_5h_used_percent",
	"codex_7d_used_percent",
	"codex_5h_reset_at",
	"codex_7d_reset_at",
	"codex_5h_reset_after_seconds",
	"codex_7d_reset_after_seconds",
	"codex_usage_updated_at",
	"auto_pause_5h_threshold",
	"auto_pause_7d_threshold",
	"auto_pause_5h_disabled",
	"auto_pause_7d_disabled",

	// 模型级限流与访问拒绝
	"model_rate_limits",
	"model_access_denials",
}
