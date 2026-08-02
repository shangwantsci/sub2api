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
  useRouter: () => ({ push: vi.fn() }),
  // 批量跑完后页面会给一个「查看我的账号」的链接。
  RouterLink: { name: 'RouterLink', props: ['to'], template: '<a><slot /></a>' }
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

/**
 * 批量上号。
 *
 * 之前 session key 只能整段当成一个 key 提交，供号商一行一个粘贴几十条必然报错。
 * 现在按行拆分逐条走单账号接口，这里守住拆分、串行、以及「重复」不算失败三件事。
 */
describe('ProviderOnboard 批量上号', () => {
  const KEYS = ['sk-ant-sid01-aaaaaaaa', 'sk-ant-sid01-bbbbbbbb', 'sk-ant-sid01-cccccccc']

  beforeEach(() => {
    vi.clearAllMocks()
    getOnboardOptionsMock.mockResolvedValue(options({ auto_proxy_available: true }))
  })

  function result(name: string, duplicate = false) {
    return { account: { id: 1, name, email: name }, duplicate }
  }

  async function pasteKeys(keys: string[]) {
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.find('#session-key').setValue(keys.join('\n'))
    return wrapper
  }

  it('按行拆分，多于一行时禁用名称输入并提示自动命名', async () => {
    const wrapper = await pasteKeys(KEYS)

    expect((wrapper.find('#onboard-name').element as HTMLInputElement).disabled).toBe(true)
    expect(wrapper.text()).toContain('3')
  })

  it('单行时仍走原来的单账号流程，名称可填', async () => {
    const wrapper = await pasteKeys([KEYS[0]])
    expect((wrapper.find('#onboard-name').element as HTMLInputElement).disabled).toBe(false)
  })

  // 空行与首尾空白是粘贴时的常态，不能因此发出一条空 key 换票。
  it('忽略空行与首尾空白', async () => {
    onboardMock.mockResolvedValue(result('a@example.com'))
    const wrapper = await pasteKeys([])
    await wrapper.find('#session-key').setValue(`  ${KEYS[0]}  \n\n   \n${KEYS[1]}\n`)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(onboardMock).toHaveBeenCalledTimes(2)
    expect(onboardMock.mock.calls[0][0].session_key).toBe(KEYS[0])
    expect(onboardMock.mock.calls[1][0].session_key).toBe(KEYS[1])
  })

  // 批量时不发 name：后端按各自换票拿到的邮箱命名，前端塞一个共用的名字
  // 会让几十个号重名，供号商反而分不清哪个是哪个。
  it('批量提交不带账号名', async () => {
    onboardMock.mockResolvedValue(result('a@example.com'))
    const wrapper = await pasteKeys(KEYS)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(onboardMock).toHaveBeenCalledTimes(3)
    for (const call of onboardMock.mock.calls) {
      expect(call[0].name).toBeUndefined()
    }
  })

  /**
   * 串行是必需的：auto 代理模式下平台按「当前绑定最少」选出口，只有等上一条落库、
   * 绑定数 +1，下一条才会挑到别的 IP。并发发出去整批号会全挤在同一个出口上。
   */
  it('逐条串行而不是一次全发出去', async () => {
    let inFlight = 0
    let maxInFlight = 0
    onboardMock.mockImplementation(async () => {
      inFlight += 1
      maxInFlight = Math.max(maxInFlight, inFlight)
      await Promise.resolve()
      inFlight -= 1
      return result('a@example.com')
    })

    const wrapper = await pasteKeys(KEYS)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(maxInFlight).toBe(1)
  })

  // 重复不是失败：这个号之前就上过，后端按 account_uuid 判重后没有重复建。
  // 混进失败里会让供号商以为有几条没上成，跑去重试。
  it('把重复与失败分开统计', async () => {
    onboardMock
      .mockResolvedValueOnce(result('a@example.com'))
      .mockResolvedValueOnce(result('b@example.com', true))
      .mockRejectedValueOnce(new Error('换票失败'))

    const wrapper = await pasteKeys(KEYS)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain(zhBatchStatus.created)
    expect(text).toContain(zhBatchStatus.duplicate)
    expect(text).toContain(zhBatchStatus.failed)
    expect(text).toContain('换票失败')
  })

  it('只重跑失败项，不动已经建好的号', async () => {
    onboardMock
      .mockResolvedValueOnce(result('a@example.com'))
      .mockResolvedValueOnce(result('b@example.com', true))
      .mockRejectedValueOnce(new Error('换票失败'))

    const wrapper = await pasteKeys(KEYS)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(onboardMock).toHaveBeenCalledTimes(3)

    onboardMock.mockResolvedValue(result('c@example.com'))
    const retryButton = wrapper
      .findAll('button')
      .find((b) => b.text().includes('重试失败'))
    expect(retryButton, '有失败项时必须给出重试入口').toBeTruthy()
    await retryButton!.trigger('click')
    await flushPromises()

    // 只多发一条：成功与重复的两条不再重跑。
    expect(onboardMock).toHaveBeenCalledTimes(4)
    expect(onboardMock.mock.calls[3][0].session_key).toBe(KEYS[2])
  })

  // 整串 session key 贴在界面上，一次截图就把凭据带出去了。
  it('进度列表里的 key 是脱敏的', async () => {
    onboardMock.mockResolvedValue(result('a@example.com'))
    const wrapper = await pasteKeys(KEYS)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const progress = wrapper.find('ul').text()
    expect(progress).not.toContain(KEYS[0])
    expect(progress).toContain('…')
  })
})

const zhBatchStatus = zh.provider.onboard.batchStatus
