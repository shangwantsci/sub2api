import { describe, expect, it } from 'vitest'
import { vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupBadge from '../GroupBadge.vue'
import GroupOptionItem from '../GroupOptionItem.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

describe('group rate display', () => {
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
})
