/**
 * 供号商站点的类型定义。
 *
 * 这些类型对应后端 handler/provider 包的严格脱敏视图：后端刻意不下发
 * credentials、extra、proxy、oauth_client、分组策略等内部处理字段，
 * 前端类型也不要凭空加回来。
 */

/**
 * 金额的传输类型：完整精度的十进制字符串，例如 `"1.2345678901"`。
 *
 * 后端刻意序列化成字符串而不是 JSON 数字 —— JSON 数字在 JS 侧被解析成 float64，
 * decimal(20,10) 的精度当场丢失，对账时两边的合计就会对不上。
 *
 * 不要直接对这个值做算术或 toFixed，一律通过 utils/providerMoney 处理。
 */
export type MoneyString = string

/** 账号状态的压缩四态，内部调度细节不下发。 */
export type ProviderAccountStatus = 'active' | 'paused' | 'error' | 'unknown'

/**
 * 一个额度窗口的用量。
 *
 * utilization 是 Anthropic 自己给出的百分比（0-100+，可能超过 100），
 * 也就是「这个号在这个窗口里用掉了多少额度」，与平台设的任何限额无关。
 *
 * 刻意没有任何金额字段：后端 AccountUsageWindowView 就不下发，
 * 那些金额含平台倍率或是向客户收的价。
 */
export interface ProviderUsageWindow {
  utilization: number
  resets_at?: string | null
  remaining_seconds?: number
  requests?: number
  tokens?: number
}

/** 账号在 Anthropic 侧的额度用量。 */
export interface ProviderAccountUsage {
  /** passive = 请求时顺带采集的响应头（可能不是最新）；active = 刚主动查过上游。 */
  source?: 'passive' | 'active'
  updated_at?: string | null
  five_hour?: ProviderUsageWindow | null
  seven_day?: ProviderUsageWindow | null
  seven_day_sonnet?: ProviderUsageWindow | null
  seven_day_fable?: ProviderUsageWindow | null
}

export interface ProviderAccount {
  id: number
  name: string
  notes?: string | null
  /**
   * 该账号对应的 Anthropic 登录邮箱，来自上号时的换票结果。
   *
   * 供号商靠它对照「手上哪些号已经上了」。极早期上号且未重新授权过的账号可能为空。
   */
  email?: string
  status: ProviderAccountStatus
  /** 托管类型的对外文案，不是分组名。 */
  hosting_type_label: string
  /**
   * 速率档位。
   *
   * **按账号实际生效的参数反推**，不是存的那一列的直译 —— 管理员在管理端改过账号
   * 的并发/RPM 之后，存的档位标识不会跟着变，只有反推才能保证标签不撒谎。
   *
   * tier_label 为空且 tier 为 'custom' 时，表示不属于任何标准档位，
   * 前端按 i18n 渲染成「自定义」（后端不回硬编码中文）。
   */
  tier_label: string
  tier: string

  /**
   * 当前生效的四个档位参数，供编辑弹窗预填。
   *
   * 0 表示「不启用该限制」（既有约定，最高档的 5h 上限就是 0），不是「限为 0」。
   */
  concurrency: number
  max_sessions: number
  base_rpm: number
  window_cost_limit: number
  /**
   * 调度槽位此刻占用。缺省或 null 表示本次没采到（该项未启用或 Redis 失败），
   * 不要当成 0。这是平台调度占用，不是 Anthropic 额度窗口。
   */
  current_concurrency?: number | null
  current_rpm?: number | null
  active_sessions?: number | null
  expires_at?: string | null
  last_used_at?: string | null
  created_at: string
  period_requests: number
  period_tokens: number
  /** 本结算周期内该账号的 1 倍率金额。见 MoneyString。 */
  period_cost: MoneyString
}

/**
 * 账号编辑请求。
 *
 * 三项都是可选的，**不传表示不改这一项**。注意 notes 传空字符串是清空备注，
 * 与不传不是一回事。
 */
export interface ProviderAccountUpdatePayload {
  name?: string
  notes?: string | null
  tier?: string
  custom_tier?: ProviderCustomTierPayload | null
}

export interface ProviderHostingType {
  id: number
  label: string
  description: string
}

export interface ProviderCapacityTier {
  tier: string
  label: string
  concurrency: number
  max_sessions: number
  base_rpm: number
  /** 0 表示不限。 */
  window_cost_limit: number
}

export interface ProviderCustomTierCaps {
  enabled: boolean
  concurrency: number
  max_sessions: number
  base_rpm: number
  window_cost_limit: number
}

/** 站点开放的出口来源。both = 两种都给选。 */
export type ProviderProxyModePolicy = 'both' | 'auto_only' | 'manual_only'

/** 单次上号声明的出口来源。 */
export type ProviderProxyMode = 'auto' | 'manual'

export interface ProviderOnboardOptions {
  hosting_types: ProviderHostingType[]
  default_hosting_type_id: number
  tiers: ProviderCapacityTier[]
  default_tier: string
  custom_tier: ProviderCustomTierCaps
  proxy_mode_policy: ProviderProxyModePolicy
  /**
   * 平台侧当前还有没有可分配的出口。
   *
   * 只有布尔值，没有数量：可用数量会暴露平台的出口规模。
   */
  auto_proxy_available: boolean
}

/**
 * 自定义档参数。
 *
 * 四个数值都必须大于 0：运行时 0 表示「不启用该限制」，填 0 等于绕过护栏上限。
 * 不含 rpm_strategy —— 那是平台的调度策略，不开放给供号商。
 */
export interface ProviderCustomTierPayload {
  concurrency: number
  max_sessions: number
  base_rpm: number
  window_cost_limit: number
  session_idle_timeout_minutes?: number
}

export interface ProviderOnboardPayload {
  /**
   * 留空时由后端按换票拿到的邮箱自动命名。
   *
   * 批量上号（一次粘贴多行 session key）就是靠这个：逐个手填名字不现实，
   * 而邮箱正是供号商用来对照「手上哪些号已经上了」的标识。
   */
  name?: string
  notes?: string | null
  /** 只有 oauth 与 setup-token 两种。 */
  method: 'oauth' | 'setup-token'
  session_key?: string
  session_id?: string
  code?: string
  proxy_mode: ProviderProxyMode
  /**
   * 只在 manual 模式使用，一整行连接串。
   *
   * 刻意不拆成 protocol/host/port：供号商手上拿到的就是一整行，
   * 解析以后端为准，前端只做即时预览。
   */
  proxy_url?: string
  hosting_type_id: number
  tier: string
  custom_tier?: ProviderCustomTierPayload | null
}

export interface ProviderAuthURLResult {
  auth_url: string
  session_id: string
}

/**
 * 上号提交的结果。
 *
 * duplicate 为 true 时 account 是**已存在**的那条账号，本次没有新建 —— 后端按
 * Anthropic 账号 UUID 判重。这不是错误：前端请求超时后供号商重试是常态，
 * 批量上号里重复粘贴同一批 key 也很常见，界面上要和「新建成功」区分开显示。
 */
export interface ProviderOnboardResult {
  account: ProviderAccount
  duplicate: boolean
}

export interface ProviderAccountUsage {
  account_id: number
  account_name: string
  /** true 表示账号已被下线；其金额仍计入本期。 */
  offline: boolean
  requests: number
  tokens: number
  standard_cost: MoneyString
}

export interface ProviderDailyUsage {
  date: string
  requests: number
  tokens: number
  standard_cost: MoneyString
}

export interface ProviderPeriodTotals {
  requests: number
  tokens: number
  standard_cost: MoneyString
  account_count: number
}

export interface ProviderCurrentPeriod {
  provider_user_id: number
  period_start: string
  as_of: string
  totals: ProviderPeriodTotals
  accounts: ProviderAccountUsage[]
  daily?: ProviderDailyUsage[]
  /** 按日聚合所用的结算时区，界面上要标注出来。 */
  settlement_timezone: string
}

export interface ProviderSettlement {
  id: number
  provider_user_id: number
  period_start: string
  period_end: string
  standard_cost: MoneyString
  requests: number
  tokens: number
  account_count: number
  status: 'settled' | 'voided'
  settled_at: string
  settled_by?: number | null
  voided_at?: string | null
  voided_by?: number | null
  notes?: string
  created_at: string
}

export interface ProviderProfile {
  id: number
  email: string
  username: string
  created_at: string
}
