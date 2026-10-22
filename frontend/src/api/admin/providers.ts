/**
 * 管理端供号商对账与供货商设置 API。
 */

import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'
import type {
  MoneyString,
  ProviderAccountUsage,
  ProviderCurrentPeriod,
  ProviderSettlement,
} from '../provider/types'

export interface ProviderSummary {
  user_id: number
  email: string
  username: string
  status: string
  created_at: string
  accounts_total: number
  accounts_active: number
  accounts_paused: number
  period_start: string
  pending_requests: number
  pending_tokens: number
  /** 待结算金额，恒为 1 倍率标准价。 */
  pending_cost: MoneyString
  last_settled_at?: string | null
  last_settled_cost?: MoneyString | null
  last_settlement_id?: number | null
}

export interface ProviderSummaryList {
  providers: ProviderSummary[]
  total_providers: number
  pending_providers: number
  total_pending_cost: MoneyString
  settlement_timezone: string
}

export interface ProviderBatchSettleResult {
  period_end: string
  settled: ProviderSettlement[]
  skipped: number[]
  failed?: Record<number, string>
  total_cost: MoneyString
  total_count: number
  settlement_timezone: string
}

// ==================== 对账面板 ====================

export async function list(): Promise<ProviderSummaryList> {
  const { data } = await apiClient.get<ProviderSummaryList>('/admin/providers')
  return data
}

export async function getCurrentPeriod(id: number): Promise<ProviderCurrentPeriod> {
  const { data } = await apiClient.get<ProviderCurrentPeriod>(`/admin/providers/${id}/current-period`)
  return data
}

export async function settle(id: number, notes?: string): Promise<ProviderSettlement> {
  const { data } = await apiClient.post<ProviderSettlement>(`/admin/providers/${id}/settle`, { notes })
  return data
}

export async function settleBatch(
  providerUserIDs: number[],
  notes?: string
): Promise<ProviderBatchSettleResult> {
  const { data } = await apiClient.post<ProviderBatchSettleResult>('/admin/providers/settle-batch', {
    provider_user_ids: providerUserIDs,
    notes,
  })
  return data
}

export async function voidSettlement(settlementID: number, reason: string) {
  const { data } = await apiClient.post<{ id: number; voided: boolean }>(
    `/admin/providers/settlements/${settlementID}/void`,
    { reason }
  )
  return data
}

export async function listSettlements(
  id: number,
  page: number = 1,
  pageSize: number = 20
): Promise<PaginatedResponse<ProviderSettlement>> {
  const { data } = await apiClient.get<PaginatedResponse<ProviderSettlement>>(
    `/admin/providers/${id}/settlements`,
    { params: { page, page_size: pageSize } }
  )
  return data
}

export async function exportSettlement(settlementID: number): Promise<Blob> {
  const { data } = await apiClient.get(`/admin/providers/settlements/${settlementID}/export`, {
    responseType: 'blob',
  })
  return data as Blob
}

// ==================== 供货商设置 ====================

export interface ProviderHostingTypeSetting {
  group_id: number
  label: string
  description: string
  enabled: boolean
  sort: number
}

export interface ProviderCustomTierCapsSetting {
  concurrency: number
  max_sessions: number
  base_rpm: number
  window_cost_limit: number
}

export interface ProviderSettings {
  portal_enabled: boolean
  hosting_types: ProviderHostingTypeSetting[]
  default_group_id: number
  /**
   * 对供货商开放的容量档位（取自容量档位模板的 key）。
   *
   * 档位的数值只存在于模板里（设置 → 常规），这里只记「哪些模板开放」，
   * 两处各写各的列，避免互相回滚。default_tier 必须落在这个列表内。
   */
  enabled_tier_keys: string[]
  /** 默认档位：模板 key，必须在 enabled_tier_keys 内。 */
  default_tier: string
  custom_tier_enabled: boolean
  custom_tier_caps: ProviderCustomTierCapsSetting
  /** 对账按日聚合强制使用该时区，与浏览器时区无关。 */
  settlement_timezone: string
  /**
   * 封账终点相对当前时刻回退的秒数。
   *
   * usage 是异步落库的，刚过去的一小段时间内还可能有记录没提交完。
   * 封账往回退这段时间，避免刚结完账就冒出漏网的用量。
   */
  settlement_cooldown_seconds: number
  /**
   * 供号商账号的调度优先级。
   *
   * 注意这是硬门槛而非权重：调度只保留分组内 priority 数值最小的那批账号，
   * 其余一个请求都拿不到。因此该值必须与同分组内其它账号一致。
   */
  account_priority: number
  /**
   * 单个平台代理最多绑定多少个供号商账号。
   *
   * 自动分配按「绑定最少优先」挑，到顶的代理不再参与。同一出口 IP 上挂太多账号
   * 会让这些账号彼此关联，所以这是个上限而不是目标值。
   */
  auto_proxy_max_accounts: number
  /** 供号商上号可用的出口来源：both | auto_only | manual_only。 */
  proxy_mode_policy: 'both' | 'auto_only' | 'manual_only'
}

/**
 * 分组的真实策略，仅管理端可见。
 *
 * 设置页把这几列以只读形式展示在托管类型表旁边，方便填对外文案时核对实际配了什么。
 * 供号商侧接口绝不返回这些字段。
 */
export interface ProviderGroupPolicyView {
  id: number
  name: string
  platform: string
  rate_multiplier: number
  content_review_policy: string
  claude_oauth_system_prompt_policy: string
  status: string
  /**
   * 该分组内非供号商账号已有的 priority 去重值。
   *
   * 用于提示优先级冲突：调度只保留分组内 priority 数值最小的那批账号，
   * 与供号商账号取值不同的话，其中一边会被完全饿死。
   */
  existing_priorities: number[]
}

export interface ProviderSettingsResponse extends ProviderSettings {
  available_groups: ProviderGroupPolicyView[]
}

export async function getSettings(): Promise<ProviderSettingsResponse> {
  const { data } = await apiClient.get<ProviderSettingsResponse>('/admin/provider-settings')
  return data
}

export async function updateSettings(payload: ProviderSettings): Promise<ProviderSettings> {
  const { data } = await apiClient.put<ProviderSettings>('/admin/provider-settings', payload)
  return data
}

/**
 * 「应用到存量」会改写的六项数值，取值与预览接口返回的 changes[].field 一一对应。
 * load_factor 是有效负载因子（账号没设负载因子时等于并发）：应用时会和并发一起对齐，所以必须在预览里可见。
 */
export type ProviderTierChangeField =
  | 'concurrency'
  | 'max_sessions'
  | 'base_rpm'
  | 'window_cost_limit'
  | 'adaptive_throttle'
  | 'load_factor'

export interface ProviderTierChange {
  field: ProviderTierChangeField
  /** 数字或布尔：adaptive_throttle 是布尔；window_cost_limit 是美元，0 表示不限。 */
  from: number | boolean
  to: number | boolean
}

export interface ProviderTierPreviewSample {
  account_id: number
  name: string
  changes: ProviderTierChange[]
}

export interface ProviderTierApplyPreview {
  tier: string
  /** 该档位下的账号总数。 */
  matched: number
  /** 按当前模板写入后确实有数值会变的账号数；0 表示没有可改的。 */
  will_change: number
  /** 将被改动的账号样例：后端最多给 20 条、只含将改动的、按 id 升序。 */
  samples: ProviderTierPreviewSample[]
}

/**
 * 预览「应用到存量」：只读，不写任何账号。
 *
 * 管理员确认时拿到的 will_change 会原样作为 applyTierToExisting 的 expectedChanged，
 * 后端据此识别「预览之后数据又变了」。
 */
export async function getTierApplyPreview(tier: string): Promise<ProviderTierApplyPreview> {
  const { data } = await apiClient.get<ProviderTierApplyPreview>(
    `/admin/provider-settings/tiers/${encodeURIComponent(tier)}/apply-preview`
  )
  // Go 的空切片可能序列化成 null，统一成空数组，页面就不用到处判空
  return { ...data, samples: data.samples ?? [] }
}

/**
 * 把档位模板写到该档位的存量账号。
 *
 * expectedChanged 必填（取自刚才预览的 will_change）：后端会重新计算，
 * 与实际将改动的账号数不一致就返回 HTTP 409（reason 为 PRESET_APPLY_CONFLICT）且不写任何账号，
 * 调用方应据此重新拉预览让管理员再确认一次。
 */
export async function applyTierToExisting(
  tier: string,
  expectedChanged: number
): Promise<{ matched: number; updated: number }> {
  const { data } = await apiClient.post<{ matched: number; updated: number }>(
    `/admin/provider-settings/tiers/${encodeURIComponent(tier)}/apply-existing`,
    { expected_changed: expectedChanged }
  )
  return data
}

export type { ProviderAccountUsage, ProviderCurrentPeriod, ProviderSettlement }

const providersAPI = {
  list,
  getCurrentPeriod,
  settle,
  settleBatch,
  voidSettlement,
  listSettlements,
  exportSettlement,
  getSettings,
  updateSettings,
  getTierApplyPreview,
  applyTierToExisting,
}

export default providersAPI
