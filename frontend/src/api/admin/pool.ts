import { apiClient } from '../client'
import type { Account } from '@/types'

export interface PoolAccountCounts {
  total: number
  active: number
  schedulable: number
  rate_limited: number
  error: number
}

export interface PoolRuntime {
  enabled: boolean
  owner_id: string
  persistent_runs: boolean
  run_count: number
  waiting_tools: number
  sessions: number
  pool: {
    global_limit: number
    per_account_limit: number
    queue_limit: number
    active_global: number
    active_accounts: number
    waiting_accounts: number
    waiting_global: number
  }
}

export interface PoolUsageWindow {
  utilization?: number
  resets_at?: string | null
}

export interface PoolUsage {
  five_hour?: PoolUsageWindow | null
  seven_day?: PoolUsageWindow | null
  source?: string
}

export interface PoolAccountsResponse {
  items: Account[]
  total: number
  page: number
  page_size: number
}

export interface PoolOverview {
  accounts: PoolAccountCounts
  runtime: PoolRuntime
}

export interface PoolEvent {
  time: string
  type: string
  session: string
  account_id?: number
  message?: string
}

export interface PoolRun {
  run_id: string
  session: string
  account_id: number
  account_name: string
  state: string
  started_at: string
  last_used_at: string
  tool_pending: number
}

export async function getOverview(): Promise<PoolOverview> {
  const { data } = await apiClient.get('/admin/pool/overview')
  return data
}

export async function getAccounts(params: {
  page?: number
  page_size?: number
  status?: string
  search?: string
  sort_by?: string
  sort_order?: string
}): Promise<PoolAccountsResponse> {
  const { data } = await apiClient.get('/admin/pool/accounts', { params })
  return data
}

export async function getUsageBatch(accountIDs: number[]): Promise<{ usage: Record<string, PoolUsage> }> {
  const { data } = await apiClient.post('/admin/pool/usage', { account_ids: accountIDs })
  return data
}

export async function getRuns(): Promise<{ items: PoolRun[] }> {
  const { data } = await apiClient.get('/admin/pool/runs')
  return data
}

export async function releaseRun(runID: string): Promise<void> {
  await apiClient.delete(`/admin/pool/runs/${encodeURIComponent(runID)}`)
}

export async function getEvents(): Promise<{ items: PoolEvent[] }> {
  const { data } = await apiClient.get('/admin/pool/events')
  return data
}

export async function getHealth(): Promise<{ enabled: boolean; owner_id: string }> {
  const { data } = await apiClient.get('/admin/pool/health')
  return data
}

export default { getOverview, getAccounts, getUsageBatch, getRuns, getHealth, getEvents, releaseRun }
