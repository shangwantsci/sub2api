import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'

const { getOnboardOptionsMock, generateAuthURLMock, onboardMock } = vi.hoisted(() => ({
  getOnboardOptionsMock: vi.fn(),
  generateAuthURLMock: vi.fn(),
  onboardMock: vi.fn()
}))

vi.mock('@/api/provider', () => ({
  getOnboardOptions: getOnboardOptionsMock,
  generateAuthURL: generateAuthURLMock,
  onboard: onboardMock
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

vi.mock('@/components/layout/ProviderLayout.vue', () => ({
  default: { name: 'ProviderLayout', template: '<div><slot /></div>' }
}))

import ProviderOnboard from '@/views/provider/ProviderOnboard.vue'

/** 生产 GET /provider/onboard/options 的真实形态。 */
function options(overrides: Record<string, unknown> = {}) {
  return {
    hosting_types: [{ id: 11, label: '稳健型', description: '寿命最长' }],
    default_hosting_type_id: 11,
    tiers: [
      { tier: '3', label: '3 档', concurrency: 3, max_sessions: 3, base_rpm: 30, window_cost_limit: 60 }
    ],
    default_tier: '3',
    custom_tier: { enabled: true, concurrency: 10, max_sessions: 10, base_rpm: 100, window_cost_limit: 200 },
    proxy_mode_policy: 'both',
    auto_proxy_available: false,
    ...overrides
  }
}

function mountPage() {
  const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh } })
  return mount(ProviderOnboard, { global: { plugins: [i18n] } })
}

describe('ProviderOnboard 挂载', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    generateAuthURLMock.mockResolvedValue({ auth_url: 'https://x', session_id: 's' })
  })

  // 这个页面在生产上白屏过一次。挂载测试是唯一能抓到渲染期异常的手段 ——
  // 源码文本断言看不到运行时，i18n 断言也只覆盖文案本身。
  it.each([
    ['两者皆可 + 无库存', options()],
    ['两者皆可 + 有库存', options({ auto_proxy_available: true })],
    ['仅平台 IP + 有库存', options({ proxy_mode_policy: 'auto_only', auto_proxy_available: true })],
    ['仅平台 IP + 无库存', options({ proxy_mode_policy: 'auto_only', auto_proxy_available: false })],
    ['仅自带代理', options({ proxy_mode_policy: 'manual_only', auto_proxy_available: false })],
    ['未知策略', options({ proxy_mode_policy: 'platform_only' })],
    ['策略字段缺失（旧后端）', (() => { const o = options() as Record<string, unknown>; delete o.proxy_mode_policy; delete o.auto_proxy_available; return o })()]
  ])('%s 能正常渲染', async (_name, payload) => {
    getOnboardOptionsMock.mockResolvedValue(payload)
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.find('form').exists()).toBe(true)
  })

  it('options 请求失败时不白屏，显示错误而不是崩溃', async () => {
    getOnboardOptionsMock.mockRejectedValue(new Error('boom'))
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.text()).toBeTruthy()
  })

  // auto_only 且池子为空时，页面只能显示「无法上号」，不能同时露出自带代理输入框。
  it('仅平台 IP 且无库存时不露出自带代理输入框', async () => {
    getOnboardOptionsMock.mockResolvedValue(
      options({ proxy_mode_policy: 'auto_only', auto_proxy_available: false })
    )
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.find('#proxy-url').exists()).toBe(false)
  })

  it('两者皆可且无库存时给出自带代理输入框', async () => {
    getOnboardOptionsMock.mockResolvedValue(options())
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.find('#proxy-url').exists()).toBe(true)
  })
})
