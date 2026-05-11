import { beforeEach, describe, expect, it } from 'vitest'
import { vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const getStatus = vi.hoisted(() => vi.fn())

vi.mock('@/api/claudePool', () => ({
  claudePoolAPI: {
    getStatus,
  },
}))

import GroupBadge from '../GroupBadge.vue'
import GroupOptionItem from '../GroupOptionItem.vue'
import { __resetClaudePoolDynamicRateForTests } from '@/composables/useClaudePoolDynamicRate'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

describe('group rate display', () => {
  beforeEach(() => {
    __resetClaudePoolDynamicRateForTests()
    getStatus.mockReset()
    getStatus.mockResolvedValue({
      data: {
        status: 'fresh',
        updated_at: '2026-05-09T12:00:00Z',
        age_seconds: 12,
        stale: false,
        coefficient: 0.8,
        load_percent: 4,
        idle_percent: 96,
        state_label: '空闲充足',
        models: [],
      },
    })
  })

  it('shows only the effective user rate in the group badge when a custom rate exists', () => {
    const wrapper = mount(GroupBadge, {
      props: {
        name: 'claude满血默认',
        platform: 'anthropic',
        rateMultiplier: 1.6,
        userRateMultiplier: 2.0,
      },
      global: {
        stubs: {
          PlatformIcon: true,
        },
      },
    })

    expect(wrapper.text()).toContain('2x')
    expect(wrapper.text()).not.toContain('1.6x')
    expect(wrapper.find('.line-through').exists()).toBe(false)
  })

  it('shows only the effective user rate in group dropdown options when a custom rate exists', () => {
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'claude满血默认',
        platform: 'anthropic',
        rateMultiplier: 1.6,
        userRateMultiplier: 2.0,
      },
      global: {
        stubs: {
          PlatformIcon: true,
        },
      },
    })

    expect(wrapper.text()).toContain('2x 倍率')
    expect(wrapper.text()).not.toContain('1.6x')
    expect(wrapper.find('.line-through').exists()).toBe(false)
  })

  it('shows the dynamic raised group rate in the group badge', async () => {
    getStatus.mockResolvedValue({
      data: {
        status: 'fresh',
        updated_at: '2026-05-09T12:00:00Z',
        age_seconds: 12,
        stale: false,
        coefficient: 1.2,
        load_percent: 68,
        idle_percent: 32,
        state_label: '高峰',
        models: [],
      },
    })

    const wrapper = mount(GroupBadge, {
      props: {
        name: 'claude满血默认',
        platform: 'anthropic',
        rateMultiplier: 1.6,
      },
      global: {
        stubs: {
          PlatformIcon: true,
        },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('1.6x')
    expect(wrapper.text()).toContain('2.4x')
    expect(wrapper.text()).toContain('当前')
    expect(wrapper.find('.line-through').exists()).toBe(true)
  })

  it('applies dynamic display to the effective custom user rate in dropdown options', async () => {
    getStatus.mockResolvedValue({
      data: {
        status: 'fresh',
        updated_at: '2026-05-09T12:00:00Z',
        age_seconds: 12,
        stale: false,
        coefficient: 1.2,
        load_percent: 68,
        idle_percent: 32,
        state_label: '高峰',
        models: [],
      },
    })

    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'claude满血默认',
        platform: 'anthropic',
        rateMultiplier: 1.6,
        userRateMultiplier: 1.4,
      },
      global: {
        stubs: {
          PlatformIcon: true,
        },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('1.4x')
    expect(wrapper.text()).toContain('2.1x')
    expect(wrapper.text()).toContain('当前')
    expect(wrapper.text()).toContain('倍率')
    expect(wrapper.find('.line-through').exists()).toBe(true)
  })
})
