import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  checkMixedChannelRisk: vi.fn(),
  generateAuthUrl: vi.fn(),
  exchangeCode: vi.fn(),
  startAnthropicSessionImport: vi.fn(),
  getAnthropicSessionImport: vi.fn(),
  getCapacityPresets: vi.fn(),
  getSettings: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: mocks.showError,
    showSuccess: mocks.showSuccess,
    showWarning: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isSimpleMode: true })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: mocks.create,
      checkMixedChannelRisk: mocks.checkMixedChannelRisk,
      generateAuthUrl: mocks.generateAuthUrl,
      exchangeCode: mocks.exchangeCode,
      startAnthropicSessionImport: mocks.startAnthropicSessionImport,
      getAnthropicSessionImport: mocks.getAnthropicSessionImport
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: mocks.getSettings,
      getCapacityPresets: mocks.getCapacityPresets
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([])
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

import CreateAccountModal from '../CreateAccountModal.vue'
import { resetCapacityPresets } from '@/composables/useCapacityPresets'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

// 第二步的授权流程：暴露 authCode / inputMethod，并用按钮触发三类授权事件
const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: {
    showManualOption: Boolean,
    showCodexSessionImportOption: Boolean,
    showAgentIdentityOption: Boolean,
    showCodexPatOption: Boolean,
    initialInputMethod: String
  },
  data: () => ({
    inputMethod: 'manual',
    authCode: 'auth-code-123',
    oauthState: '',
    projectId: ''
  }),
  emits: ['generate-url', 'cookie-auth', 'chrome-cookie-auth'],
  methods: {
    reset() {
      /* 不需要：stub 没有内部状态 */
    }
  },
  template: `
    <div>
      <button type="button" data-testid="flow-generate-url" @click="$emit('generate-url')">url</button>
      <button type="button" data-testid="flow-cookie" @click="$emit('cookie-auth', 'sk-ant-sid01-AAA')">cookie</button>
      <button type="button" data-testid="flow-chrome-cookie" @click="$emit('chrome-cookie-auth', 'sk-ant-sid01-AAA')">chrome</button>
      <button type="button" data-testid="flow-session-import" @click="$emit('cookie-auth', 'sk-ant-sid01-AAA\\nsk-ant-sid01-BBB')">import</button>
    </div>
  `
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: { type: [String, Number, Boolean, null], default: '' },
    options: { type: Array, default: () => [] }
  },
  emits: ['update:modelValue'],
  template: `
    <select v-bind="$attrs" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
      <option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option>
    </select>
  `
})

// 5 个种子模板，期望值全部写字面量，不引用 builder
const SEED = {
  presets: [
    { key: '1', label: '入门', concurrency: 1, max_sessions: 1, base_rpm: 10, window_cost_limit: 20, adaptive_throttle: true },
    { key: '2', label: '保守', concurrency: 2, max_sessions: 2, base_rpm: 20, window_cost_limit: 40, adaptive_throttle: true },
    { key: '3', label: '均衡', concurrency: 3, max_sessions: 3, base_rpm: 30, window_cost_limit: 60, adaptive_throttle: true },
    { key: '4', label: '激进', concurrency: 5, max_sessions: 5, base_rpm: 50, window_cost_limit: 100, adaptive_throttle: true },
    { key: '5', label: '旗舰', concurrency: 8, max_sessions: 8, base_rpm: 80, window_cost_limit: 0, adaptive_throttle: true }
  ],
  default_key: '3'
}

const TOKEN_INFO = { access_token: 'at', refresh_token: 'rt', org_uuid: 'org-1' }

const SNAPSHOT = {
  id: 'job-1',
  status: 'completed',
  error: '',
  result: { total: 2, processed: 2, created: 2, updated: 0, duplicate: 0, failed: 0, items: [] }
}

function mountModal(props: Record<string, unknown> = {}) {
  return mount(CreateAccountModal, {
    props: { show: false, proxies: [], groups: [], ...props },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub,
        ConfirmDialog: true,
        Select: SelectStub,
        Icon: true,
        PlatformIcon: true,
        ProxySelector: true,
        ProxyAdBanner: true,
        GroupSelector: true,
        ModelWhitelistSelector: true,
        QuotaLimitCard: true
      }
    }
  })
}

type Wrapper = ReturnType<typeof mountModal>

async function openModal(props: Record<string, unknown> = {}): Promise<Wrapper> {
  const wrapper = mountModal(props)
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

async function clickButtonByText(wrapper: Wrapper, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text().includes(text))
  expect(button, `找不到按钮：${text}`).toBeDefined()
  await button?.trigger('click')
}

const presetSelect = (wrapper: Wrapper) => wrapper.get<HTMLSelectElement>('[data-testid="capacity-preset-select"]')
const numberInput = (wrapper: Wrapper, testId: string) =>
  wrapper.get<HTMLInputElement>(`[data-testid="${testId}"]`)

async function pickPreset(wrapper: Wrapper, key: string) {
  await presetSelect(wrapper).setValue(key)
  await flushPromises()
}

async function goToAuthStep(wrapper: Wrapper, name = 'claude account') {
  await wrapper.get('form#create-account-form input[type="text"]').setValue(name)
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
}

/** 三条（外加 Chrome）Anthropic 授权路径各自的驱动：走到真正提交为止，返回 { concurrency, extra }。 */
interface PathDriver {
  name: string
  mountProps: Record<string, unknown>
  /** extra 里是否带有授权结果合并进来的基础键 */
  hasBaseExtra: boolean
  run: (wrapper: Wrapper) => Promise<{ concurrency: unknown; extra: Record<string, unknown> }>
}

const PATHS: PathDriver[] = [
  {
    name: 'OAuth 授权码兑换',
    mountProps: {},
    hasBaseExtra: true,
    async run(wrapper) {
      await goToAuthStep(wrapper)
      await wrapper.get('[data-testid="flow-generate-url"]').trigger('click')
      await flushPromises()
      await clickButtonByText(wrapper, 'admin.accounts.oauth.completeAuth')
      await flushPromises()
      expect(mocks.exchangeCode).toHaveBeenCalledTimes(1)
      expect(mocks.create).toHaveBeenCalledTimes(1)
      const payload = mocks.create.mock.calls[0][0]
      return { concurrency: payload.concurrency, extra: payload.extra }
    }
  },
  {
    name: 'Cookie 授权',
    mountProps: {},
    hasBaseExtra: true,
    async run(wrapper) {
      await goToAuthStep(wrapper)
      await wrapper.get('[data-testid="flow-cookie"]').trigger('click')
      await flushPromises()
      expect(mocks.create).toHaveBeenCalledTimes(1)
      const payload = mocks.create.mock.calls[0][0]
      return { concurrency: payload.concurrency, extra: payload.extra }
    }
  },
  {
    name: 'Chrome Cookie 授权（与 Cookie 授权共用同一个漏斗）',
    mountProps: {},
    hasBaseExtra: true,
    async run(wrapper) {
      await goToAuthStep(wrapper)
      await wrapper.get('[data-testid="flow-chrome-cookie"]').trigger('click')
      await flushPromises()
      expect(mocks.exchangeCode.mock.calls[0][0]).toBe('/admin/accounts/chrome-cookie-auth')
      expect(mocks.create).toHaveBeenCalledTimes(1)
      const payload = mocks.create.mock.calls[0][0]
      return { concurrency: payload.concurrency, extra: payload.extra }
    }
  },
  {
    name: 'SessionKey 批量导入',
    mountProps: { initialMode: 'anthropic-session-import' },
    hasBaseExtra: false,
    async run(wrapper) {
      await goToAuthStep(wrapper)
      await wrapper.get('[data-testid="flow-session-import"]').trigger('click')
      await flushPromises()
      expect(mocks.startAnthropicSessionImport).toHaveBeenCalledTimes(1)
      const payload = mocks.startAnthropicSessionImport.mock.calls[0][0]
      return { concurrency: payload.account_concurrency, extra: payload.extra }
    }
  }
]

/** 档位无关的伪装类 extra：新建默认开启 TLS 指纹并显式写 true。 */
const DISGUISE_DEFAULT = { enable_tls_fingerprint: true }

describe('CreateAccountModal 容量与节流（Claude OAuth / SetupToken）', () => {
  beforeEach(() => {
    resetCapacityPresets()
    Object.values(mocks).forEach((mock) => mock.mockReset())
    mocks.create.mockResolvedValue({})
    mocks.checkMixedChannelRisk.mockResolvedValue({ has_risk: false })
    mocks.generateAuthUrl.mockResolvedValue({ auth_url: 'https://auth.example.com', session_id: 'sess-1' })
    mocks.exchangeCode.mockResolvedValue({ ...TOKEN_INFO })
    mocks.startAnthropicSessionImport.mockResolvedValue({ ...SNAPSHOT })
    mocks.getAnthropicSessionImport.mockResolvedValue({ ...SNAPSHOT })
    mocks.getCapacityPresets.mockResolvedValue(structuredClone(SEED))
    mocks.getSettings.mockResolvedValue({ account_nurturing_enabled: false })
  })

  describe('默认预选与档位下拉', () => {
    it('打开弹窗后按设置里的 default_key 预选（而不是写死的档位）', async () => {
      mocks.getCapacityPresets.mockResolvedValue({ ...structuredClone(SEED), default_key: '2' })
      const wrapper = await openModal()

      expect(presetSelect(wrapper).element.value).toBe('2')
      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('2')
      expect(numberInput(wrapper, 'capacity-max-sessions').element.value).toBe('2')
      expect(numberInput(wrapper, 'capacity-base-rpm').element.value).toBe('20')
      expect(numberInput(wrapper, 'capacity-window-cost').element.value).toBe('40')
      expect(wrapper.get('[data-testid="capacity-adaptive-toggle"]').attributes('aria-checked')).toBe('true')
    })

    it('弹窗创建时就已经是打开状态（挂载即 show=true）也会预选默认档位', async () => {
      const wrapper = mountModal({ show: true })
      await flushPromises()

      expect(presetSelect(wrapper).element.value).toBe('3')
      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('3')
    })

    it('选「激进」把 5 项数值复制进表单，下拉显示激进', async () => {
      const wrapper = await openModal()
      await pickPreset(wrapper, '4')

      expect(presetSelect(wrapper).element.value).toBe('4')
      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('5')
      expect(numberInput(wrapper, 'capacity-max-sessions').element.value).toBe('5')
      expect(numberInput(wrapper, 'capacity-base-rpm').element.value).toBe('50')
      expect(numberInput(wrapper, 'capacity-window-cost').element.value).toBe('100')
    })

    it('选「旗舰」：5h 额度 0 = 不限，仍能反推出旗舰', async () => {
      const wrapper = await openModal()
      await pickPreset(wrapper, '5')

      expect(numberInput(wrapper, 'capacity-window-cost').element.value).toBe('0')
      expect(presetSelect(wrapper).element.value).toBe('5')
    })

    it('手改 RPM 后下拉自动回到「自定义」，改回去又认出原档位', async () => {
      const wrapper = await openModal()
      await pickPreset(wrapper, '3')
      expect(presetSelect(wrapper).element.value).toBe('3')

      await numberInput(wrapper, 'capacity-base-rpm').setValue('31')
      await flushPromises()
      expect(presetSelect(wrapper).element.value).toBe('custom')
      expect(presetSelect(wrapper).text()).toContain('admin.accounts.capacityFields.presetCustom')

      await numberInput(wrapper, 'capacity-base-rpm').setValue('30')
      await flushPromises()
      expect(presetSelect(wrapper).element.value).toBe('3')
      expect(presetSelect(wrapper).text()).not.toContain('admin.accounts.capacityFields.presetCustom')
    })

    it('只切动态降速开关不会让标签变成自定义', async () => {
      const wrapper = await openModal()
      await pickPreset(wrapper, '3')

      await wrapper.get('[data-testid="capacity-adaptive-toggle"]').trigger('click')
      await flushPromises()

      expect(wrapper.get('[data-testid="capacity-adaptive-toggle"]').attributes('aria-checked')).toBe('false')
      expect(presetSelect(wrapper).element.value).toBe('3')
    })

    it('选档位只写 5 项：不动 RPM 策略、粘性缓冲、预留、空闲超时', async () => {
      const wrapper = await openModal()
      await numberInput(wrapper, 'capacity-base-rpm').setValue('40')
      await flushPromises()
      // 先填了粘性缓冲，再选档位：缓冲应保留
      const buffer = wrapper.findAll('input[type="number"]').find((input) => input.attributes('id')?.endsWith('sticky-buffer'))
      expect(buffer).toBeDefined()
      await buffer!.setValue('9')

      await pickPreset(wrapper, '4')

      const bufferAfter = wrapper.findAll('input[type="number"]').find((input) => input.attributes('id')?.endsWith('sticky-buffer'))
      expect((bufferAfter!.element as HTMLInputElement).value).toBe('9')
    })
  })

  describe('三条 Anthropic 授权路径的提交内容', () => {
    it.each(PATHS)('$name：选「激进」后 payload 带完整容量键，且没有 persona_*', async (path) => {
      const wrapper = await openModal(path.mountProps)
      await pickPreset(wrapper, '4')

      const { concurrency, extra } = await path.run(wrapper)

      expect(concurrency).toBe(5)
      expect(extra).toEqual({
        ...(path.hasBaseExtra ? { org_uuid: 'org-1' } : {}),
        max_sessions: 5,
        session_idle_timeout_minutes: 5,
        window_cost_limit: 100,
        window_cost_sticky_reserve: 10,
        base_rpm: 50,
        rpm_strategy: 'tiered',
        adaptive_throttle: true,
        nurturing_enabled: false,
        ...DISGUISE_DEFAULT
      })
      expect(Object.keys(extra).filter((key) => key.startsWith('persona'))).toEqual([])
    })

    it.each(PATHS)('$name：什么都不动，payload 就是默认档位（均衡）的数值', async (path) => {
      const wrapper = await openModal(path.mountProps)

      const { concurrency, extra } = await path.run(wrapper)

      expect(concurrency).toBe(3)
      expect(extra).toMatchObject({
        max_sessions: 3,
        window_cost_limit: 60,
        base_rpm: 30,
        adaptive_throttle: true,
        nurturing_enabled: false
      })
    })

    it.each(PATHS)('$name：TLS 指纹关闭时显式写 false（不是缺省）', async (path) => {
      const wrapper = await openModal(path.mountProps)
      await wrapper.get('[data-testid="tls-fingerprint-toggle"]').trigger('click')

      const { extra } = await path.run(wrapper)

      expect(extra.enable_tls_fingerprint).toBe(false)
      expect('tls_fingerprint_profile_id' in extra).toBe(false)
    })

    it.each(PATHS)('$name：默认开启 TLS 指纹并显式写 true', async (path) => {
      const wrapper = await openModal(path.mountProps)

      const { extra } = await path.run(wrapper)

      expect(extra.enable_tls_fingerprint).toBe(true)
    })

    it.each(PATHS)('$name：全部关闭（并发之外都填 0）时只剩显式的动态降速 / 养号 bool', async (path) => {
      const wrapper = await openModal(path.mountProps)
      await numberInput(wrapper, 'capacity-max-sessions').setValue('0')
      await numberInput(wrapper, 'capacity-base-rpm').setValue('0')
      await numberInput(wrapper, 'capacity-window-cost').setValue('0')
      await wrapper.get('[data-testid="capacity-adaptive-toggle"]').trigger('click')

      const { extra } = await path.run(wrapper)

      expect(extra).toEqual({
        ...(path.hasBaseExtra ? { org_uuid: 'org-1' } : {}),
        adaptive_throttle: false,
        nurturing_enabled: false,
        ...DISGUISE_DEFAULT
      })
    })
  })

  describe('API Key 路径不带容量键', () => {
    it('Anthropic API Key 账号：extra 里没有任何容量键', async () => {
      const wrapper = await openModal()
      await pickPreset(wrapper, '4')
      await clickButtonByText(wrapper, 'admin.accounts.claudeConsole')
      await wrapper.get('form#create-account-form input[type="text"]').setValue('key account')
      await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-test')
      await wrapper.get('form#create-account-form').trigger('submit.prevent')
      await flushPromises()

      expect(mocks.create).toHaveBeenCalledTimes(1)
      const extra = (mocks.create.mock.calls[0][0].extra ?? {}) as Record<string, unknown>
      for (const key of [
        'max_sessions',
        'session_idle_timeout_minutes',
        'window_cost_limit',
        'window_cost_sticky_reserve',
        'base_rpm',
        'rpm_strategy',
        'rpm_sticky_buffer',
        'user_msg_queue_mode',
        'adaptive_throttle',
        'nurturing_enabled',
        'enable_tls_fingerprint'
      ]) {
        expect(extra, `API Key 路径不应写入 ${key}`).not.toHaveProperty(key)
      }
    })

    it('Claude OAuth 的并发只在容量卡里出现；API Key 才用通用并发输入框', async () => {
      const wrapper = await openModal()
      expect(wrapper.find('[data-testid="capacity-concurrency"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="account-concurrency"]').exists()).toBe(false)

      await clickButtonByText(wrapper, 'admin.accounts.claudeConsole')
      expect(wrapper.find('[data-testid="account-concurrency"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="account-capacity-card"]').exists()).toBe(false)
    })
  })

  describe('状态必须放在弹窗 setup 里', () => {
    it('第一步 → 第二步：容量卡被销毁，但状态保留并写进 payload；返回第一步后卡里数值还在', async () => {
      const wrapper = await openModal()
      await pickPreset(wrapper, '4')
      await numberInput(wrapper, 'capacity-base-rpm').setValue('55')

      await goToAuthStep(wrapper)
      // 第二步没有容量卡（v-if="step === 1" 的整块被销毁了）
      expect(wrapper.find('[data-testid="account-capacity-card"]').exists()).toBe(false)

      await clickButtonByText(wrapper, 'common.back')
      await flushPromises()
      expect(numberInput(wrapper, 'capacity-base-rpm').element.value).toBe('55')
      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('5')
      expect(presetSelect(wrapper).element.value).toBe('custom')

      await goToAuthStep(wrapper)
      await wrapper.get('[data-testid="flow-cookie"]').trigger('click')
      await flushPromises()
      const payload = mocks.create.mock.calls[0][0]
      expect(payload.concurrency).toBe(5)
      expect(payload.extra.base_rpm).toBe(55)
      expect(payload.extra.max_sessions).toBe(5)
    })
  })

  describe('养号开关', () => {
    it('全局养号总开关关闭时，账号上的养号开关置灰并给出提示', async () => {
      mocks.getSettings.mockResolvedValue({ account_nurturing_enabled: false })
      const wrapper = await openModal()

      expect(wrapper.get('[data-testid="capacity-nurturing-toggle"]').attributes('disabled')).toBeDefined()
      expect(wrapper.find('[data-testid="capacity-nurturing-global-off"]').exists()).toBe(true)

      await wrapper.get('[data-testid="capacity-nurturing-toggle"]').trigger('click')
      expect(wrapper.get('[data-testid="capacity-nurturing-toggle"]').attributes('aria-checked')).toBe('false')
    })

    it('全局开启后可以勾选，payload 里写显式 true', async () => {
      mocks.getSettings.mockResolvedValue({ account_nurturing_enabled: true })
      const wrapper = await openModal()

      const toggle = wrapper.get('[data-testid="capacity-nurturing-toggle"]')
      expect(toggle.attributes('disabled')).toBeUndefined()
      expect(wrapper.find('[data-testid="capacity-nurturing-global-off"]').exists()).toBe(false)
      await toggle.trigger('click')

      await goToAuthStep(wrapper)
      await wrapper.get('[data-testid="flow-cookie"]').trigger('click')
      await flushPromises()
      expect(mocks.create.mock.calls[0][0].extra.nurturing_enabled).toBe(true)
    })

    it('读取全局设置失败时按「关闭」处理（置灰）', async () => {
      mocks.getSettings.mockRejectedValue(new Error('boom'))
      const wrapper = await openModal()

      expect(wrapper.get('[data-testid="capacity-nurturing-toggle"]').attributes('disabled')).toBeDefined()
    })
  })

  describe('档位模板加载失败（降级，不发明第二套默认值）', () => {
    it('不显示档位下拉也绝不显示「自定义」，只给提示；数值保持初始值', async () => {
      mocks.getCapacityPresets.mockRejectedValue(new Error('boom'))
      const wrapper = await openModal()

      expect(wrapper.find('[data-testid="capacity-preset-select"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="capacity-presets-unavailable"]').exists()).toBe(true)
      expect(wrapper.text()).not.toContain('admin.accounts.capacityFields.presetCustom')
      // 没有预选任何档位：并发还是表单自己的初始值，其余限制都是 0
      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('10')
      expect(numberInput(wrapper, 'capacity-max-sessions').element.value).toBe('0')
      expect(numberInput(wrapper, 'capacity-base-rpm').element.value).toBe('0')
      expect(numberInput(wrapper, 'capacity-window-cost').element.value).toBe('0')
    })

    it('降级后仍可手填并提交：只写显式 bool 与手填的数值', async () => {
      mocks.getCapacityPresets.mockRejectedValue(new Error('boom'))
      const wrapper = await openModal()
      await numberInput(wrapper, 'capacity-base-rpm').setValue('25')

      await goToAuthStep(wrapper)
      await wrapper.get('[data-testid="flow-cookie"]').trigger('click')
      await flushPromises()

      const payload = mocks.create.mock.calls[0][0]
      expect(payload.concurrency).toBe(10)
      expect(payload.extra).toEqual({
        org_uuid: 'org-1',
        base_rpm: 25,
        rpm_strategy: 'tiered',
        adaptive_throttle: false,
        nurturing_enabled: false,
        enable_tls_fingerprint: true
      })
    })

    it('接口方法不存在（其它 spec 的 mock 形态）同样降级，不报错', async () => {
      mocks.getCapacityPresets.mockImplementation(() => {
        throw new TypeError('getCapacityPresets is not a function')
      })
      const wrapper = await openModal()

      expect(wrapper.find('[data-testid="capacity-presets-unavailable"]').exists()).toBe(true)
    })
  })

  describe('「只在用户没碰过时才预选默认档位」', () => {
    it('模板还没回来时用户已经手改了数值：模板到达后不覆盖用户的输入', async () => {
      let resolvePresets: (value: unknown) => void = () => {}
      mocks.getCapacityPresets.mockReturnValue(new Promise((resolve) => { resolvePresets = resolve }))
      const wrapper = await openModal()

      await numberInput(wrapper, 'capacity-base-rpm').setValue('77')
      resolvePresets(structuredClone(SEED))
      await flushPromises()

      expect(numberInput(wrapper, 'capacity-base-rpm').element.value).toBe('77')
      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('10')
      expect(presetSelect(wrapper).element.value).toBe('custom')
    })

    it('切到其它平台再切回：没碰过就重新预选默认档位', async () => {
      const wrapper = await openModal()
      await clickButtonByText(wrapper, 'OpenAI')
      // 其它平台改了共用的并发
      await wrapper.get('[data-testid="account-concurrency"]').setValue('7')
      expect(wrapper.find('[data-testid="account-capacity-card"]').exists()).toBe(false)

      await clickButtonByText(wrapper, 'Anthropic')
      await flushPromises()

      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('3')
      expect(presetSelect(wrapper).element.value).toBe('3')
    })

    it('切到其它平台且没碰过容量卡：撤销默认预选，通用并发恢复成默认 10（不把 Claude 档位的并发带过去）', async () => {
      const wrapper = await openModal()
      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('3')

      await clickButtonByText(wrapper, 'OpenAI')

      const generic = wrapper.get('[data-testid="account-concurrency"]').element as HTMLInputElement
      expect(generic.value).toBe('10')
    })

    it('碰过容量卡再切到其它平台：不撤销用户的选择，并发保持所选档位的值', async () => {
      const wrapper = await openModal()
      await pickPreset(wrapper, '4')

      await clickButtonByText(wrapper, 'OpenAI')

      const generic = wrapper.get('[data-testid="account-concurrency"]').element as HTMLInputElement
      expect(generic.value).toBe('5')
    })

    it('用户碰过容量卡之后切走再切回：保留用户的选择', async () => {
      const wrapper = await openModal()
      await pickPreset(wrapper, '4')
      await clickButtonByText(wrapper, 'OpenAI')
      await clickButtonByText(wrapper, 'Anthropic')
      await flushPromises()

      expect(presetSelect(wrapper).element.value).toBe('4')
      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('5')
    })

    it('关闭再打开弹窗：恢复初始并重新预选默认档位', async () => {
      const wrapper = await openModal()
      await pickPreset(wrapper, '4')
      await wrapper.setProps({ show: false })
      await wrapper.setProps({ show: true })
      await flushPromises()

      expect(presetSelect(wrapper).element.value).toBe('3')
      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('3')
    })
  })

  describe('并发输入的边界', () => {
    it('并发最少为 1：清空或填 0 立刻纠回 1', async () => {
      const wrapper = await openModal()
      const input = numberInput(wrapper, 'capacity-concurrency')

      await input.setValue('0')
      await flushPromises()
      expect(input.element.value).toBe('1')

      await input.setValue('')
      await flushPromises()
      expect(input.element.value).toBe('1')
    })
  })
})
