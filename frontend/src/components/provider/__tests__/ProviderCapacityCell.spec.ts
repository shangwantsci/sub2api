import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import ProviderCapacityCell from '../ProviderCapacityCell.vue'
import type { ProviderAccount } from '@/api/provider'

function account(overrides: Partial<ProviderAccount> = {}): ProviderAccount {
  return {
    id: 1,
    name: 'supplier@example.com',
    status: 'active',
    hosting_type_label: '稳健型',
    tier_label: '3 档',
    tier: '3',
    concurrency: 3,
    max_sessions: 3,
    base_rpm: 30,
    window_cost_limit: 60,
    created_at: '2026-08-01T00:00:00Z',
    period_requests: 0,
    period_tokens: 0,
    period_cost: '0',
    ...overrides,
  }
}

function mountCell(overrides: Partial<ProviderAccount> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh } })
  return mount(ProviderCapacityCell, {
    props: { account: account(overrides) },
    global: { plugins: [i18n] },
  })
}

describe('ProviderCapacityCell', () => {
  it('上限大于 0 且采到占用时渲染三枚徽章', () => {
    const wrapper = mountCell({
      current_concurrency: 1,
      active_sessions: 2,
      current_rpm: 5,
    })

    expect(wrapper.find('[data-test="occupancy-concurrency"]').text()).toMatch(/1\s*\/\s*3/)
    expect(wrapper.find('[data-test="occupancy-sessions"]').text()).toMatch(/2\s*\/\s*3/)
    expect(wrapper.find('[data-test="occupancy-rpm"]').text()).toMatch(/5\s*\/\s*30/)
  })

  it('并发上限为 0 时不渲染并发徽章，即使带了 current', () => {
    const wrapper = mountCell({
      concurrency: 0,
      current_concurrency: 5,
      active_sessions: 1,
      current_rpm: 4,
    })

    expect(wrapper.find('[data-test="occupancy-concurrency"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="occupancy-sessions"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="occupancy-rpm"]').exists()).toBe(true)
  })

  it('占用字段为 null 时不渲染对应徽章', () => {
    const wrapper = mountCell({
      current_concurrency: null,
      active_sessions: null,
      current_rpm: null,
    })

    expect(wrapper.find('[data-test="occupancy-concurrency"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="occupancy-sessions"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="occupancy-rpm"]').exists()).toBe(false)
    expect(wrapper.text()).toBe('-')
  })

  it('RPM 达到档位上限时标橙而不是红', () => {
    const wrapper = mountCell({
      current_concurrency: 0,
      active_sessions: 0,
      current_rpm: 30,
    })

    const rpm = wrapper.find('[data-test="occupancy-rpm"]')
    expect(rpm.classes()).toContain('bg-orange-100')
    expect(rpm.classes()).not.toContain('bg-red-100')
    expect(rpm.attributes('title')).toContain('已达档位每分钟请求数')
  })

  it('会话已满时标红', () => {
    const wrapper = mountCell({
      current_concurrency: 0,
      active_sessions: 3,
      current_rpm: 0,
    })

    expect(wrapper.find('[data-test="occupancy-sessions"]').classes()).toContain('bg-red-100')
  })

  it('并发已满时标红，空闲时标灰', () => {
    const full = mountCell({ current_concurrency: 3, active_sessions: 0, current_rpm: 0 })
    expect(full.find('[data-test="occupancy-concurrency"]').classes()).toContain('bg-red-100')

    const idle = mountCell({ current_concurrency: 0, active_sessions: 0, current_rpm: 0 })
    expect(idle.find('[data-test="occupancy-concurrency"]').classes()).toContain('bg-gray-100')
  })
})
