import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: false,
    copyToClipboard: vi.fn()
  })
}))

import OAuthAuthorizationFlow from '../OAuthAuthorizationFlow.vue'

const mountCookieFlow = (anthropicSessionBulkImport: boolean) =>
  mount(OAuthAuthorizationFlow, {
    props: {
      addMethod: 'setup-token',
      platform: 'anthropic',
      showCookieOption: true,
      allowMultiple: true,
      defaultInputMethod: 'cookie',
      anthropicSessionBulkImport
    },
    global: {
      stubs: {
        Icon: true
      }
    }
  })

describe('OAuthAuthorizationFlow', () => {
  it('keeps normal setup-token cookie auth out of Anthropic session bulk import mode', () => {
    const wrapper = mountCookieFlow(false)

    expect(wrapper.text()).toContain('admin.accounts.oauth.cookieAutoAuth')
    expect(wrapper.text()).toContain('admin.accounts.oauth.startAutoAuth')
    expect(wrapper.text()).not.toContain('admin.accounts.oauth.anthropicSessionBulkImport')
    expect(wrapper.find('textarea').attributes('rows')).toBe('3')
  })

  it('uses Anthropic session bulk import copy only when explicitly enabled', () => {
    const wrapper = mountCookieFlow(true)

    expect(wrapper.text()).toContain('admin.accounts.oauth.anthropicSessionBulkImport')
    expect(wrapper.text()).toContain('admin.accounts.oauth.startBatchImport')
    expect(wrapper.find('textarea').attributes('rows')).toBe('8')
  })
})
