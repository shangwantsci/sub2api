import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PoolView from '../PoolView.vue'

const getOverview = vi.fn()
const getRuns = vi.fn()
const getEvents = vi.fn()
const getUsageBatch = vi.fn()
const getPoolAccounts = vi.fn()
const listGroups = vi.fn()

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (k: string) => k }) }
})
vi.mock('@/api/admin', () => ({
  adminAPI: {
    pool: { getOverview: (...a: any[]) => getOverview(...a), getAccounts: (...a: any[]) => getPoolAccounts(...a), getRuns: (...a: any[]) => getRuns(...a), getEvents: (...a: any[]) => getEvents(...a), getUsageBatch: (...a: any[]) => getUsageBatch(...a) },
    accounts: { list: (...a: any[]) => listAccounts(...a) },
    groups: { list: (...a: any[]) => listGroups(...a) },
  },
}))

const account = (id: number, type: string) => ({
  id, name: `acct-${id}`, platform: 'anthropic', type, status: 'active',
  schedulable: true, priority: 1, concurrency: 4, current_concurrency: 1,
  session_window_end: '2026-10-25T10:00:00Z', extra: { passive_usage_7d_utilization: 0.42 },
}) as any

describe('PoolView', () => {
  beforeEach(() => {
    getOverview.mockReset().mockResolvedValue({
      accounts: { total: 2, active: 2, schedulable: 1, rate_limited: 1, error: 0 },
      runtime: { enabled: true, owner_id: 'cc-1', persistent_runs: true, run_count: 1, waiting_tools: 0, sessions: 1, pool: { global_limit: 20, per_account_limit: 4, queue_limit: 100, active_global: 1, active_accounts: 1, waiting_accounts: 0, waiting_global: 0 } },
    })
    getUsageBatch.mockReset().mockResolvedValue({ usage: { '1': { five_hour: { utilization: 33 }, seven_day: { utilization: 42 } } } })
    getEvents.mockReset().mockResolvedValue({ items: [{ time: '2026-10-25T09:00:00Z', type: 'run_started', session: 'abc123' }] })
    getRuns.mockReset().mockResolvedValue({ items: [{ session: 'abc123', account_id: 1, account_name: 'acct-1', state: 'waiting_tool', started_at: '2026-10-25T09:00:00Z', last_used_at: '2026-10-25T09:01:00Z', tool_pending: 1 }] })
    getPoolAccounts.mockReset().mockImplementation((params: any) => {
      const items = [account(1, 'oauth'), account(3, 'setup-token')]
      const q = String(params?.search ?? '').toLowerCase()
      return Promise.resolve({ items: q ? items.filter((a: any) => a.name.includes(q)) : items, total: 2, page: 1, page_size: 100 })
    })
    listGroups.mockReset().mockResolvedValue({ items: [{ id: 7, name: 'Customer A', platform: 'anthropic', status: 'active', account_count: 2 }], total: 1 })
  })

  it('filters subscription accounts by server search', async () => {
    vi.useFakeTimers()
    const wrapper = mount(PoolView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true, LoadingSpinner: true, GroupAccountsModal: true, PoolAccountDrawer: true, PoolRunDrawer: true, RouterLink: { template: '<a><slot /></a>' } } },
    })
    await flushPromises()
    const search = wrapper.get('input[placeholder="admin.pool.searchAccounts"]')
    await search.setValue('acct-3')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(getPoolAccounts).toHaveBeenLastCalledWith(expect.objectContaining({ search: 'acct-3' }))
    expect(wrapper.text()).toContain('acct-3')
    expect(wrapper.text()).not.toContain('acct-1')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('renders overview and filters subscription accounts', async () => {
    const wrapper = mount(PoolView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: true,
          LoadingSpinner: true,
          GroupAccountsModal: true,
          RouterLink: { template: '<a><slot /></a>' },
        },
      },
    })
    await flushPromises()
    expect(getOverview).toHaveBeenCalled()
    expect(wrapper.text()).toContain('acct-1')
    expect(wrapper.text()).toContain('acct-3')
    expect(wrapper.text()).not.toContain('acct-2')
    expect(wrapper.text()).toContain('42%')
    await wrapper.findAll('button').find((b) => b.text().includes('admin.pool.runs'))!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('waiting_tool')
    wrapper.unmount()
  })
})
