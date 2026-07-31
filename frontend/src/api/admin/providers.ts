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

export interface ProviderCapacityTierSetting {
  tier: string
  label: string
  concurrency: number
  max_sessions: number
  base_rpm: number
  /** 0 表示不限。 */
  window_cost_limit: number
  enabled: boolean
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
  capacity_tiers: ProviderCapacityTierSetting[]
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

export async function getTierAffectedCount(tier: string): Promise<{ tier: string; affected: number }> {
  const { data } = await apiClient.get<{ tier: string; affected: number }>(
    `/admin/provider-settings/tiers/${encodeURIComponent(tier)}/affected-count`
  )
  return data
}

export async function applyTierToExisting(
  tier: string
): Promise<{ matched: number; updated: number }> {
  const { data } = await apiClient.post<{ matched: number; updated: number }>(
    `/admin/provider-settings/tiers/${encodeURIComponent(tier)}/apply-existing`
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
  getTierAffectedCount,
  applyTierToExisting,
}

export default providersAPI
