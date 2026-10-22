import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'

import PoolHealthView from '../PoolHealthView.vue'
import type { PublicPoolHealthSnapshot } from '@/api/poolHealth'

/**
 * 公开号池监控页（匿名可访问）：
 *  - 统计的分组名由后台设置决定，页面只读接口返回的 group_name，不能再写死；
 *  - 后台关闭「公开号池监控」后接口返回 404，页面要显示「该页面未开放」，而不是报错或空白；
 *  - 只有 404 才代表「未开放」，其它错误仍按错误提示处理。
 *
 * 这个页面没有 i18n，文案直接是中文，所以断言里也直接写中文。
 */

const { getPoolHealth } = vi.hoisted(() => ({ getPoolHealth: vi.fn() }))

vi.mock('@/api/poolHealth', () => ({ getPoolHealth }))

function makeSnapshot(overrides: Partial<PublicPoolHealthSnapshot> = {}): PublicPoolHealthSnapshot {
  return {
    updated_at: new Date().toISOString(),
    platform: 'anthropic',
    group_name: 'team-a',
    account_type: 'setup-token',
    horizon_seconds: 14400,
    accounts: {
      total: 12,
      effective: 11,
      available: 9,
      blocked: 1,
      measured: 11,
      in_use: 3,
      idle: 6,
      exhausted: 2,
      unavailable: 3
    },
    capacity: {
      remaining_percent: 62.5,
      pool_load_percent: 37.5,
      waiting: 0,
      total_slots: 40,
      schedulable_slots: 32,
      busy_slots: 12,
      free_slots: 20,
      overcommitted_slots: 0
    },
    health: { level: 'healthy', label: '健康', message: '号池运行平稳' },
    unavailable_breakdown: [{ reason: 'rate_limited', label: '限流', count: 2 }],
    recovery_buckets: [],
    ...overrides
  }
}

let wrapper: VueWrapper | undefined

async function mountView() {
  wrapper = mount(PoolHealthView)
  await flushPromises()
  return wrapper
}

const NOT_OPEN = '[data-testid="pool-health-not-open"]'
const SUBTITLE = '[data-testid="pool-health-subtitle"]'

beforeEach(() => {
  getPoolHealth.mockReset()
})

afterEach(() => {
  // 页面挂载后会起 30 秒轮询定时器，卸载时才会清掉
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
})

describe('PoolHealthView 分组名', () => {
  it.each(['ccmax', 'team-a', 'vip-pool', '中文分组'])('副标题显示接口返回的 group_name：%s', async (name) => {
    getPoolHealth.mockResolvedValue(makeSnapshot({ group_name: name }))

    const w = await mountView()

    expect(w.get(SUBTITLE).text()).toBe(`${name} · Setup Token`)
  })

  it('返回的不是 ccmax 时，页面任何位置都不再出现写死的 ccmax', async () => {
    getPoolHealth.mockResolvedValue(makeSnapshot({ group_name: 'team-a' }))

    const w = await mountView()

    expect(w.text()).not.toContain('ccmax')
  })

  it('返回值首尾有空白时去掉空白再显示', async () => {
    getPoolHealth.mockResolvedValue(makeSnapshot({ group_name: '  team-a  ' }))

    const w = await mountView()

    expect(w.get(SUBTITLE).text()).toBe('team-a · Setup Token')
  })

  it('接口返回前不凭空显示分组名', async () => {
    getPoolHealth.mockReturnValue(new Promise(() => {}))

    wrapper = mount(PoolHealthView)
    await flushPromises()

    expect(wrapper.get(SUBTITLE).text()).toBe('Setup Token')
  })

  it.each([
    ['字段缺失（旧后端）', undefined],
    ['空字符串', ''],
    ['只有空白', '   '],
    ['null', null]
  ])('group_name %s：不崩，副标题只剩 Setup Token，其余数据照常显示', async (_label, groupName) => {
    const snapshot = makeSnapshot()
    if (groupName === undefined) {
      delete snapshot.group_name
    } else {
      snapshot.group_name = groupName as unknown as string
    }
    getPoolHealth.mockResolvedValue(snapshot)

    const w = await mountView()

    expect(w.get(SUBTITLE).text()).toBe('Setup Token')
    expect(w.text()).not.toContain('undefined')
    expect(w.text()).not.toContain('null')
    expect(w.text()).toContain('号池可用性监控')
    expect(w.text()).toContain('健康')
    expect(w.find(NOT_OPEN).exists()).toBe(false)
  })
})

describe('PoolHealthView 功能未开放（接口 404）', () => {
  it('显示「该页面未开放」，不显示监控面板，也不当成错误提示', async () => {
    getPoolHealth.mockRejectedValue({ status: 404, message: 'Not found' })

    const w = await mountView()

    const panel = w.find(NOT_OPEN)
    expect(panel.exists()).toBe(true)
    expect(panel.text()).toContain('该页面未开放')
    // 图标真的渲染出来（防止漏 import Icon 导致只剩一个空圆圈）
    expect(panel.find('svg').exists()).toBe(true)

    expect(w.text()).not.toContain('号池可用性监控')
    expect(w.text()).not.toContain('Not found')
    expect(w.text()).not.toContain('监控数据加载失败')
    expect(w.find(SUBTITLE).exists()).toBe(false)
  })

  it('未开放状态下不暴露分组名', async () => {
    getPoolHealth.mockRejectedValue({ status: 404, message: 'Not found' })

    const w = await mountView()

    expect(w.text()).not.toContain('ccmax')
    expect(w.text()).not.toContain('Setup Token')
  })

  it.each([500, 503, 403, 401, 0])('状态码 %s 不是「未开放」，仍显示错误提示', async (status) => {
    getPoolHealth.mockRejectedValue({ status, message: 'boom' })

    const w = await mountView()

    expect(w.find(NOT_OPEN).exists()).toBe(false)
    expect(w.text()).toContain('boom')
    expect(w.text()).toContain('号池可用性监控')
  })

  it('抛出的不是对象（异常值）也不崩，按普通错误处理', async () => {
    getPoolHealth.mockRejectedValue(undefined)

    const w = await mountView()

    expect(w.find(NOT_OPEN).exists()).toBe(false)
    expect(w.text()).toContain('监控数据加载失败')
  })

  describe('30 秒轮询', () => {
    beforeEach(() => {
      // 只替换定时器；flushPromises 依赖 setImmediate / setTimeout，不能一起换掉
      vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    })

    it('一开始未开放，管理员随后打开开关：下一次轮询自动恢复为监控面板', async () => {
      getPoolHealth
        .mockRejectedValueOnce({ status: 404, message: 'Not found' })
        .mockResolvedValue(makeSnapshot({ group_name: 'team-a' }))

      const w = await mountView()
      expect(w.find(NOT_OPEN).exists()).toBe(true)

      vi.advanceTimersByTime(30_000)
      await flushPromises()

      expect(getPoolHealth).toHaveBeenCalledTimes(2)
      expect(w.find(NOT_OPEN).exists()).toBe(false)
      expect(w.get(SUBTITLE).text()).toBe('team-a · Setup Token')
    })

    it('正在显示数据时被关闭：下一次轮询 404 后切到「未开放」，旧数据不再残留', async () => {
      getPoolHealth
        .mockResolvedValueOnce(makeSnapshot({ group_name: 'team-a' }))
        .mockRejectedValue({ status: 404, message: 'Not found' })

      const w = await mountView()
      expect(w.get(SUBTITLE).text()).toBe('team-a · Setup Token')

      vi.advanceTimersByTime(30_000)
      await flushPromises()

      expect(w.find(NOT_OPEN).exists()).toBe(true)
      expect(w.text()).not.toContain('team-a')
      expect(w.text()).not.toContain('号池可用性监控')
    })
  })
})
