import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { useAppStore } from '@/stores'

const getStatus = vi.hoisted(() => vi.fn())

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    RouterLink: {
      template: '<a><slot /></a>',
    },
  }
})

vi.mock('@/api/claudePool', () => ({
  claudePoolAPI: {
    getStatus,
  },
}))

import ClaudePoolView from '../ClaudePoolView.vue'

describe('ClaudePoolView', () => {
  beforeEach(() => {
    getStatus.mockReset()
  })

  it('renders the public pool status summary without requiring auth', async () => {
    const pinia = createPinia()
    const appStore = useAppStore(pinia)
    appStore.siteName = 'Alyuxin'

    getStatus.mockResolvedValue({
      data: {
        status: 'fresh',
        updated_at: '2026-05-09T12:00:00Z',
        age_seconds: 12,
        stale: false,
        coefficient: 0.8,
        load_percent: 4,
        idle_percent: 96,
        selected_model: 'Claude Sonnet 4.6',
        state_label: '空闲充足',
        models: [
          {
            name: 'Claude Sonnet 4.6',
            load_percent: 4,
            idle_percent: 96,
            coefficient: 0.8,
          },
        ],
      },
    })

    const wrapper = mount(ClaudePoolView, {
      global: {
        plugins: [pinia],
        stubs: {
          RouterLink: {
            template: '<a><slot /></a>',
          },
        },
      },
    })
    await flushPromises()

    expect(getStatus).toHaveBeenCalled()
    expect(wrapper.text()).toContain('Alyuxin')
    expect(wrapper.text()).toContain('网络负载')
    expect(wrapper.text()).toContain('96%')
    expect(wrapper.text()).toContain('claude-sonnet-4-6')
    expect(wrapper.text()).not.toContain('0.80x')
    expect(wrapper.text()).not.toContain('?')
    expect(wrapper.text()).not.toContain('Derouter')
    expect(wrapper.text()).not.toContain('$')
  })
})
