import { apiClient } from './client'

export interface PublicPoolHealthAccounts {
  total: number
  effective: number
  available: number
  blocked: number
  measured: number
  in_use: number
  idle: number
  exhausted: number
  unavailable: number
}

export interface PublicPoolHealthCapacity {
  remaining_percent: number
  pool_load_percent: number
  waiting: number
  total_slots: number
  schedulable_slots: number
  busy_slots: number
  free_slots: number
  overcommitted_slots: number
}

export interface PublicPoolHealthStatus {
  level: 'healthy' | 'watch' | 'tight' | 'empty' | string
  label: string
  message: string
  next_recovery_seconds?: number
}

export interface PublicPoolHealthBreakdown {
  reason: string
  label: string
  count: number
}

export interface PublicPoolHealthRecoverySegment {
  unit: string
  count: number
}

export interface PublicPoolHealthRecoveryBucket {
  after_seconds: number
  label: string
  count: number
  segments: PublicPoolHealthRecoverySegment[]
}

export interface PublicPoolHealthSnapshot {
  updated_at: string
  platform: string
  /** 实际统计的分组名（取自后台设置）；过旧的后端可能不返回，页面需自行回落 */
  group_name?: string
  account_type: string
  horizon_seconds: number
  accounts: PublicPoolHealthAccounts
  capacity: PublicPoolHealthCapacity
  health: PublicPoolHealthStatus
  unavailable_breakdown: PublicPoolHealthBreakdown[]
  recovery_buckets: PublicPoolHealthRecoveryBucket[]
}

export interface PublicPoolHealthQuery {
  horizon?: string
}

/**
 * 后台关闭「公开号池监控」后，该接口返回 HTTP 404（刻意不用 403，避免暴露入口存在），
 * 此时 Promise 以 `{ status: 404, ... }` 被拒绝，调用方据此显示「未开放」。
 */
export async function getPoolHealth(query: PublicPoolHealthQuery = {}): Promise<PublicPoolHealthSnapshot> {
  const { data } = await apiClient.get<PublicPoolHealthSnapshot>('/public/pool-health', {
    params: {
      horizon: query.horizon || '4h',
    },
  })
  return data
}

export const poolHealthAPI = {
  get: getPoolHealth,
}

export default poolHealthAPI
