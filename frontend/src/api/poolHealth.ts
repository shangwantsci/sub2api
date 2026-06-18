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
  group_name: string
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
