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

const mountCookieFlow = (addMethod: 'oauth' | 'setup-token') =>
  mount(OAuthAuthorizationFlow, {
    props: {
      addMethod,
      platform: 'anthropic',
      showCookieOption: true,
      allowMultiple: true,
      initialInputMethod: 'cookie'
    },
    global: {
      stubs: {
        Icon: true
      }
    }
  })

describe('OAuthAuthorizationFlow', () => {
  it('renders the standard cookie auto-auth copy for setup-token accounts', () => {
    const wrapper = mountCookieFlow('setup-token')

    expect(wrapper.text()).toContain('admin.accounts.oauth.cookieAutoAuth')
    expect(wrapper.text()).toContain('admin.accounts.oauth.startAutoAuth')
    expect(wrapper.find('textarea').attributes('rows')).toBe('3')
  })

  it('emits plain cookie auth without any Chrome OAuth variant', async () => {
    const wrapper = mountCookieFlow('oauth')

    expect(wrapper.text()).not.toContain('admin.accounts.oauth.chromeCookieAuth')

    await wrapper.find('textarea').setValue('sk-ant-sid02-test')
    const submit = wrapper.findAll('button').find((button) =>
      button.text().includes('admin.accounts.oauth.startAutoAuth')
    )
    expect(submit).toBeDefined()
    await submit!.trigger('click')

    expect(wrapper.emitted('cookie-auth')).toEqual([['sk-ant-sid02-test']])
    expect(wrapper.emitted('chrome-cookie-auth')).toBeUndefined()
  })
})
