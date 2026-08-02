/**
 * 供号商站点 API。
 *
 * 后端路由挂在 /api/v1/provider/*，除注册外全部需要 JWT + 供号商身份。
 * 登录复用普通用户的 /auth/login。
 */

import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'
import type {
  ProviderAccount,
  ProviderAccountUpdatePayload,
  ProviderAccountUsage,
  ProviderAuthURLResult,
  ProviderCurrentPeriod,
  ProviderHostingType,
  ProviderOnboardOptions,
  ProviderOnboardPayload,
  ProviderOnboardResult,
  ProviderPeriodTotals,
  ProviderProfile,
  ProviderSettlement,
} from './types'

export * from './types'

// ==================== 注册 ====================

export interface ProviderRegisterPayload {
  email: string
  password: string
  verify_code?: string
  /** 邀请码强制必填，供号商站点不开放自由注册。 */
  invite_code: string
}

export async function register(payload: ProviderRegisterPayload) {
  const { data } = await apiClient.post<{
    token: string
    user: { id: number; email: string; username: string; role: string; is_provider: boolean }
  }>('/provider/auth/register', payload)
  return data
}

/**
 * 发送供号商注册验证码。
 *
 * 走 provider 专用入口而不是 /auth/send-verify-code：后者受 registration_enabled 控制，
 * 而供号商站点的典型配置是关闭公开注册、只放邀请码注册。
 */
export async function sendVerifyCode(email: string): Promise<{ countdown: number }> {
  const { data } = await apiClient.post<{ countdown: number }>(
    '/provider/auth/send-verify-code',
    { email }
  )
  return data
}

// ==================== 基本信息 ====================

export async function getProfile(): Promise<ProviderProfile> {
  const { data } = await apiClient.get<ProviderProfile>('/provider/profile')
  return data
}

export async function getHostingTypes(): Promise<{
  hosting_types: ProviderHostingType[]
  default_hosting_type_id: number
}> {
  const { data } = await apiClient.get<{
    hosting_types: ProviderHostingType[]
    default_hosting_type_id: number
  }>('/provider/hosting-types')
  return data
}

// ==================== 上号 ====================

export async function getOnboardOptions(): Promise<ProviderOnboardOptions> {
  const { data } = await apiClient.get<ProviderOnboardOptions>('/provider/onboard/options')
  return data
}

export async function generateAuthURL(method: 'oauth' | 'setup-token'): Promise<ProviderAuthURLResult> {
  const { data } = await apiClient.post<ProviderAuthURLResult>('/provider/onboard/auth-url', { method })
  return data
}

/**
 * 单次上号。批量上号由调用方逐条串行调用本函数，见 ProviderOnboard.vue。
 *
 * timeout 单独放宽：换取凭据要串行打三次 claude.ai，最坏能到 180 秒，
 * 而 apiClient 的默认超时是 30 秒。用默认值的话慢链路上会「前端已报错、
 * 后端仍把号建成了」，供号商看到失败去重试，就多出一个重复号
 * （后端按 account_uuid 判重能兜住，但界面上仍然是一次莫名其妙的失败）。
 */
export async function onboard(payload: ProviderOnboardPayload): Promise<ProviderOnboardResult> {
  const { data } = await apiClient.post<ProviderOnboardResult>('/provider/onboard/submit', payload, {
    timeout: ONBOARD_TIMEOUT_MS,
  })
  return data
}

/** 换取凭据最坏耗时（3 次 claude.ai 往返 × 60s）再留一点余量。 */
const ONBOARD_TIMEOUT_MS = 200_000

// ==================== 账号 ====================

export async function listAccounts(
  page: number = 1,
  pageSize: number = 20
): Promise<PaginatedResponse<ProviderAccount>> {
  const { data } = await apiClient.get<PaginatedResponse<ProviderAccount>>('/provider/accounts', {
    params: { page, page_size: pageSize },
  })
  return data
}

export async function getAccountStats(id: number): Promise<{
  account: ProviderAccount
  period_start: string
  totals: ProviderPeriodTotals
}> {
  const { data } = await apiClient.get<{
    account: ProviderAccount
    period_start: string
    totals: ProviderPeriodTotals
  }>(`/provider/accounts/${id}/stats`)
  return data
}

/**
 * 取账号在 Anthropic 侧的额度用量。
 *
 * 默认 passive：只读后端账号 extra 里的被动采样值（网关每次请求顺带采回来的响应头），
 * 零外部调用，列表页逐行拉也不会打爆什么。
 * active 会真的去问一次 Anthropic，只在供号商手动点刷新时用。
 */
export async function getAccountUsage(
  id: number,
  source: 'passive' | 'active' = 'passive'
): Promise<ProviderAccountUsage> {
  const { data } = await apiClient.get<ProviderAccountUsage>(`/provider/accounts/${id}/usage`, {
    params: { source },
  })
  return data
}

/**
 * 编辑账号：名称、备注、速率档位。
 *
 * 只传要改的字段。notes 传空字符串是清空备注，不传才是「不改」。
 */
export async function updateAccount(
  id: number,
  payload: ProviderAccountUpdatePayload
): Promise<ProviderAccount> {
  const { data } = await apiClient.patch<ProviderAccount>(`/provider/accounts/${id}`, payload)
  return data
}

export async function pauseAccount(id: number): Promise<ProviderAccount> {
  const { data } = await apiClient.post<ProviderAccount>(`/provider/accounts/${id}/pause`)
  return data
}

export async function resumeAccount(id: number): Promise<ProviderAccount> {
  const { data } = await apiClient.post<ProviderAccount>(`/provider/accounts/${id}/resume`)
  return data
}

/**
 * 为已有账号生成重新授权链接。
 *
 * 与上号时的 generateAuthURL 分开：链接类型必须跟账号原有类型一致，而账号是
 * OAuth 还是 Setup Token 属于内部处理方式、不下发给供号商，所以由后端按账号自己定，
 * 这里不传 method。
 */
export async function generateReauthURL(id: number): Promise<ProviderAuthURLResult> {
  const { data } = await apiClient.post<ProviderAuthURLResult>(`/provider/accounts/${id}/auth-url`)
  return data
}

export async function reauthAccount(
  id: number,
  payload: { session_key?: string; session_id?: string; code?: string }
): Promise<ProviderAccount> {
  const { data } = await apiClient.post<ProviderAccount>(`/provider/accounts/${id}/reauth`, payload)
  return data
}

/** 下线账号（软删除）。已产生的金额仍计入本期结算。 */
export async function offlineAccount(id: number): Promise<{ id: number; offline: boolean }> {
  const { data } = await apiClient.delete<{ id: number; offline: boolean }>(`/provider/accounts/${id}`)
  return data
}

// ==================== 对账 ====================

export async function getCurrentBilling(): Promise<ProviderCurrentPeriod> {
  const { data } = await apiClient.get<ProviderCurrentPeriod>('/provider/billing/current')
  return data
}

/**
 * 只取当前周期的元信息，不触发用量聚合。
 * 账号列表这类只需要「本期从哪天开始」的页面用它，别去调 getCurrentBilling。
 */
export async function getCurrentPeriodInfo(): Promise<{
  period_start: string
  settlement_timezone: string
}> {
  const { data } = await apiClient.get<{ period_start: string; settlement_timezone: string }>(
    '/provider/billing/period'
  )
  return data
}

/**
 * 分页拉取历史结算单。
 *
 * 结算时区不在这个响应里，由当期对账接口（getCurrentBilling）提供 —— 它是页面级
 * 信息，跟着分页数据走没有意义。
 */
export async function listSettlements(
  page: number = 1,
  pageSize: number = 20
): Promise<PaginatedResponse<ProviderSettlement>> {
  const { data } = await apiClient.get<PaginatedResponse<ProviderSettlement>>(
    '/provider/billing/settlements',
    { params: { page, page_size: pageSize } }
  )
  return data
}

/** 导出当期明细 CSV，返回 blob 供浏览器下载。 */
export async function exportCurrentBilling(): Promise<Blob> {
  const { data } = await apiClient.get('/provider/billing/export', { responseType: 'blob' })
  return data as Blob
}
