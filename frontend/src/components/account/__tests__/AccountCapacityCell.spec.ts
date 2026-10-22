import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({ getCapacityPresets: vi.fn() }))

vi.mock('@/api/admin', () => ({
  adminAPI: { settings: { getCapacityPresets: mocks.getCapacityPresets } }
}))

// 带上参数，方便断言「降速中」提示里到底写了什么数
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}::${JSON.stringify(params)}` : key
    })
  }
})

import AccountCapacityCell from '../AccountCapacityCell.vue'
import { resetCapacityPresets } from '@/composables/useCapacityPresets'

const SEED = {
  presets: [
    { key: '1', label: '入门', concurrency: 1, max_sessions: 1, base_rpm: 10, window_cost_limit: 20, adaptive_throttle: true },
    { key: '3', label: '均衡', concurrency: 3, max_sessions: 3, base_rpm: 30, window_cost_limit: 60, adaptive_throttle: true },
    { key: '5', label: '旗舰', concurrency: 8, max_sessions: 8, base_rpm: 80, window_cost_limit: 0, adaptive_throttle: true }
  ],
  default_key: '3'
}

function claudeAccount(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    name: 'claude',
    platform: 'anthropic',
    type: 'oauth',
    concurrency: 3,
    current_concurrency: 1,
    extra: {
      max_sessions: 3,
      base_rpm: 30,
      window_cost_limit: 60,
      adaptive_throttle: true
    },
    window_cost_limit: 60,
    max_sessions: 3,
    base_rpm: 30,
    rpm_strategy: 'tiered',
    rpm_sticky_buffer: 6,
    current_rpm: 0,
    ...overrides
  } as any
}

async function mountCell(account: any) {
  const wrapper = mount(AccountCapacityCell, { props: { account } })
  await flushPromises()
  return wrapper
}

// 容量徽标（CapacityBadge）按 DOM 顺序：并发、5h 窗口费用、会话数、RPM；没有 tooltip 的徽标也要算上
const badges = (wrapper: Awaited<ReturnType<typeof mountCell>>) =>
  wrapper.findAll('span').filter((node) => node.classes().includes('rounded-md') && node.classes().includes('inline-flex'))

describe('AccountCapacityCell', () => {
  beforeEach(() => {
    resetCapacityPresets()
    mocks.getCapacityPresets.mockReset().mockResolvedValue(structuredClone(SEED))
  })

  describe('档位标签（由数值反推）', () => {
    it('Claude OAuth 账号数值与某个模板一致：显示该模板的名称', async () => {
      const wrapper = await mountCell(claudeAccount())

      const badge = wrapper.get('[data-testid="capacity-preset-badge"]')
      expect(badge.text()).toBe('均衡')
      expect(badge.attributes('title')).toContain('admin.accounts.capacity.presetBadgeTitle')
    })

    it('旗舰档 5h 额度为 0（不限）也能认出来', async () => {
      const wrapper = await mountCell(
        claudeAccount({
          concurrency: 8,
          extra: { max_sessions: 8, base_rpm: 80, adaptive_throttle: true }
        })
      )
      expect(wrapper.get('[data-testid="capacity-preset-badge"]').text()).toBe('旗舰')
    })

    it('只切动态降速开关不影响标签（反推不含 adaptive）', async () => {
      const wrapper = await mountCell(
        claudeAccount({ extra: { max_sessions: 3, base_rpm: 30, window_cost_limit: 60, adaptive_throttle: false } })
      )
      expect(wrapper.get('[data-testid="capacity-preset-badge"]').text()).toBe('均衡')
    })

    it('数值对不上任何模板：不显示标签，也绝不显示「自定义」', async () => {
      const wrapper = await mountCell(
        claudeAccount({ extra: { max_sessions: 3, base_rpm: 31, window_cost_limit: 60 } })
      )
      expect(wrapper.find('[data-testid="capacity-preset-badge"]').exists()).toBe(false)
      expect(wrapper.text()).not.toContain('custom')
      expect(wrapper.text()).not.toContain('自定义')
    })

    it('模板没加载出来：不显示标签（不能把「没加载」误判成「自定义」）', async () => {
      mocks.getCapacityPresets.mockReset().mockRejectedValue(new Error('boom'))
      const wrapper = await mountCell(claudeAccount())

      expect(wrapper.find('[data-testid="capacity-preset-badge"]').exists()).toBe(false)
      expect(wrapper.text()).not.toContain('自定义')
    })

    it('标签读 extra 里的显式值，不读 DTO 提升出来的字段', async () => {
      // extra 里没有任何容量配置，只有 DTO 提升字段恰好等于均衡档
      const wrapper = await mountCell(claudeAccount({ extra: {}, concurrency: 3 }))
      expect(wrapper.find('[data-testid="capacity-preset-badge"]').exists()).toBe(false)
    })

    it.each([
      ['Anthropic API Key', { type: 'apikey' }],
      ['OpenAI OAuth', { platform: 'openai' }],
      ['Anthropic Bedrock', { type: 'bedrock' }]
    ])('%s 账号不是托管账号：不显示标签，也不会去请求档位模板', async (_name, overrides) => {
      const wrapper = await mountCell(claudeAccount(overrides))

      expect(wrapper.find('[data-testid="capacity-preset-badge"]').exists()).toBe(false)
      expect(mocks.getCapacityPresets).not.toHaveBeenCalled()
    })

    it('多行同时渲染只发一次模板请求', async () => {
      await Promise.all([mountCell(claudeAccount()), mountCell(claudeAccount({ id: 2 })), mountCell(claudeAccount({ id: 3 }))])
      expect(mocks.getCapacityPresets).toHaveBeenCalledTimes(1)
    })
  })

  describe('动态降速：展示后端折算后的有效值', () => {
    it('并发徽标的上限是有效并发，系数 < 1 时 tooltip 提示降速中', async () => {
      const wrapper = await mountCell(
        claudeAccount({
          concurrency: 5,
          current_concurrency: 1,
          effective_concurrency: 2,
          throttle_factor: 0.4
        })
      )

      const concurrency = badges(wrapper)[0]
      expect(concurrency.text()).toContain('1')
      expect(concurrency.text()).toContain('2')
      expect(concurrency.text()).not.toContain('5')
      expect(concurrency.attributes('title')).toContain('admin.accounts.capacity.throttled')
      expect(concurrency.attributes('title')).toContain('"effective":2')
      expect(concurrency.attributes('title')).toContain('"configured":5')
      expect(concurrency.attributes('title')).toContain('"percent":40')
    })

    it('系数为 1（全速）：不提示降速', async () => {
      const wrapper = await mountCell(
        claudeAccount({ effective_concurrency: 3, effective_base_rpm: 30, throttle_factor: 1 })
      )
      expect(wrapper.html()).not.toContain('admin.accounts.capacity.throttled')
    })

    it('后端没返回有效值（非托管账号 / 旧后端）：回退显示配置值，不自己重算', async () => {
      const wrapper = await mountCell(claudeAccount({ concurrency: 5, throttle_factor: undefined }))
      expect(badges(wrapper)[0].text()).toContain('5')
      expect(wrapper.html()).not.toContain('admin.accounts.capacity.throttled')
    })

    it('RPM 徽标的上限与三区判定都按有效 RPM，tooltip 带降速提示', async () => {
      const wrapper = await mountCell(
        claudeAccount({
          base_rpm: 50,
          effective_base_rpm: 25,
          throttle_factor: 0.5,
          rpm_sticky_buffer: 8,
          current_rpm: 30
        })
      )

      const rpm = badges(wrapper).find((node) => node.text().includes('[T]'))
      expect(rpm, '应有 RPM 徽标').toBeDefined()
      expect(rpm!.text()).toContain('25')
      expect(rpm!.text()).not.toContain('50')
      // 30 >= 有效 RPM 25 且 < 25 + 缓冲 8：粘性专用区（橙色），而不是按配置值 50 判成正常
      expect(rpm!.classes().join(' ')).toContain('orange')
      expect(rpm!.attributes('title')).toContain('admin.accounts.capacity.throttled')
      expect(rpm!.attributes('title')).toContain('admin.accounts.capacity.rpm.tieredStickyOnly')
      expect(rpm!.attributes('title')).toContain('"buffer":8')
    })

    it('粘性缓冲直接用后端给的值，不再自己按 base/5 重算', async () => {
      const wrapper = await mountCell(
        claudeAccount({ base_rpm: 50, effective_base_rpm: 50, rpm_sticky_buffer: 3, current_rpm: 52 })
      )
      const rpm = badges(wrapper).find((node) => node.text().includes('[T]'))
      expect(rpm!.attributes('title')).toContain('"buffer":3')
    })
  })

  describe('保留的既有徽标', () => {
    it('5h 窗口费用与会话数徽标照常显示', async () => {
      const wrapper = await mountCell(
        claudeAccount({ current_window_cost: 12.5, window_cost_limit: 60, active_sessions: 2, max_sessions: 3 })
      )
      const text = wrapper.text()
      expect(text).toContain('$12.50')
      expect(text).toContain('$60.00')
    })

    it('旧的人格徽标没有了：即使账号 extra 里还残留旧键也不显示', async () => {
      const wrapper = await mountCell(
        claudeAccount({ extra: { persona_preset: 'balanced', persona_enabled: true } })
      )
      expect(wrapper.html()).not.toContain('admin.accounts.persona')
    })
  })
})
