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

export interface ProviderAccount {
  id: number
  name: string
  notes?: string | null
  status: ProviderAccountStatus
  /** 托管类型的对外文案，不是分组名。 */
  hosting_type_label: string
  tier_label: string
  tier: string
  expires_at?: string | null
  last_used_at?: string | null
  created_at: string
  period_requests: number
  period_tokens: number
  /** 本结算周期内该账号的 1 倍率金额。见 MoneyString。 */
  period_cost: MoneyString
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
  name: string
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
