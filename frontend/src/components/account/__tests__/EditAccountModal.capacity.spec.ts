import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  update: vi.fn(),
  checkMixedChannelRisk: vi.fn(),
  getCapacityPresets: vi.fn(),
  getSettings: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isSimpleMode: true })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      update: mocks.update,
      checkMixedChannelRisk: mocks.checkMixedChannelRisk
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
  getAntigravityDefaultModelMapping: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

import EditAccountModal from '../EditAccountModal.vue'
import { resetCapacityPresets } from '@/composables/useCapacityPresets'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
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

function buildClaudeAccount(
  extra: Record<string, unknown> = {},
  overrides: Record<string, unknown> = {}
) {
  return {
    id: 11,
    name: 'Claude OAuth',
    notes: '',
    platform: 'anthropic',
    type: 'oauth',
    credentials: { access_token: 'tok' },
    extra,
    proxy_id: null,
    concurrency: 3,
    load_factor: null,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false,
    ...overrides
  } as any
}

/** 账号上已经是「均衡」档位的完整容量 extra（含各从属项）。 */
const BALANCED_EXTRA = {
  max_sessions: 3,
  session_idle_timeout_minutes: 7,
  window_cost_limit: 60,
  window_cost_sticky_reserve: 15,
  base_rpm: 30,
  rpm_strategy: 'sticky_exempt',
  user_msg_queue_mode: 'throttle',
  adaptive_throttle: true,
  nurturing_enabled: false
}

function mountModal(account: any, props: Record<string, unknown> = {}) {
  return mount(EditAccountModal, {
    props: { show: true, account, proxies: [], groups: [], ...props },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
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

async function open(account: any): Promise<Wrapper> {
  const wrapper = mountModal(account)
  await flushPromises()
  return wrapper
}

const presetSelect = (wrapper: Wrapper) => wrapper.get<HTMLSelectElement>('[data-testid="capacity-preset-select"]')
const numberInput = (wrapper: Wrapper, testId: string) =>
  wrapper.get<HTMLInputElement>(`[data-testid="${testId}"]`)

async function save(wrapper: Wrapper) {
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(mocks.update).toHaveBeenCalledTimes(1)
  return mocks.update.mock.calls[0][1] as Record<string, any>
}

describe('EditAccountModal 容量与节流', () => {
  beforeEach(() => {
    resetCapacityPresets()
    Object.values(mocks).forEach((mock) => mock.mockReset())
    mocks.update.mockImplementation(async (_id: number, payload: unknown) => payload)
    mocks.checkMixedChannelRisk.mockResolvedValue({ has_risk: false })
    mocks.getCapacityPresets.mockResolvedValue(structuredClone(SEED))
    mocks.getSettings.mockResolvedValue({ account_nurturing_enabled: true })
  })

  describe('载入账号', () => {
    it('并发读账号列，其余读 extra 里的显式值；下拉由 4 个数值反推出档位', async () => {
      const wrapper = await open(
        buildClaudeAccount(
          {
            max_sessions: 5,
            session_idle_timeout_minutes: 9,
            window_cost_limit: 100,
            window_cost_sticky_reserve: 12,
            base_rpm: 50,
            rpm_strategy: 'tiered',
            rpm_sticky_buffer: 8,
            user_msg_queue_mode: 'serialize',
            adaptive_throttle: true,
            nurturing_enabled: true
          },
          { concurrency: 5 }
        )
      )

      expect(presetSelect(wrapper).element.value).toBe('4')
      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('5')
      expect(numberInput(wrapper, 'capacity-max-sessions').element.value).toBe('5')
      expect(numberInput(wrapper, 'capacity-base-rpm').element.value).toBe('50')
      expect(numberInput(wrapper, 'capacity-window-cost').element.value).toBe('100')
      expect(wrapper.get('[data-testid="capacity-adaptive-toggle"]').attributes('aria-checked')).toBe('true')
      expect(wrapper.get('[data-testid="capacity-nurturing-toggle"]').attributes('aria-checked')).toBe('true')
      expect(wrapper.get('[data-testid="capacity-nurturing-toggle"]').attributes('disabled')).toBeUndefined()
    })

    it('数值对不上任何档位：显示「自定义」', async () => {
      const wrapper = await open(buildClaudeAccount({ ...BALANCED_EXTRA, base_rpm: 33 }))
      expect(presetSelect(wrapper).element.value).toBe('custom')
    })

    it('只用 DTO 提升出来的字段而 extra 里没有时，不会被当成已配置（避免把后端算过的默认值写回）', async () => {
      const wrapper = await open(buildClaudeAccount({}, { base_rpm: 99, max_sessions: 9, window_cost_limit: 9 }))
      expect(numberInput(wrapper, 'capacity-base-rpm').element.value).toBe('0')
      expect(numberInput(wrapper, 'capacity-max-sessions').element.value).toBe('0')
      expect(numberInput(wrapper, 'capacity-window-cost').element.value).toBe('0')
    })

    it('Claude OAuth 的并发只在容量卡里，通用并发输入框被隐藏', async () => {
      const wrapper = await open(buildClaudeAccount(BALANCED_EXTRA))
      expect(wrapper.find('[data-testid="capacity-concurrency"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="account-concurrency"]').exists()).toBe(false)
    })

    it('模板加载失败：不显示档位下拉也不显示「自定义」，仍能正常编辑并保存', async () => {
      mocks.getCapacityPresets.mockRejectedValue(new Error('boom'))
      const wrapper = await open(buildClaudeAccount(BALANCED_EXTRA))

      expect(wrapper.find('[data-testid="capacity-preset-select"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="capacity-presets-unavailable"]').exists()).toBe(true)
      expect(wrapper.text()).not.toContain('admin.accounts.capacityFields.presetCustom')

      const payload = await save(wrapper)
      expect(payload.extra.base_rpm).toBe(30)
    })

    it('全局养号开关关闭：账号上的养号开关置灰', async () => {
      mocks.getSettings.mockResolvedValue({ account_nurturing_enabled: false })
      const wrapper = await open(buildClaudeAccount({ ...BALANCED_EXTRA, nurturing_enabled: true }))

      expect(wrapper.get('[data-testid="capacity-nurturing-toggle"]').attributes('disabled')).toBeDefined()
      // 置灰不等于清除：账号上原有的值照常保留
      const payload = await save(wrapper)
      expect(payload.extra.nurturing_enabled).toBe(true)
    })
  })

  describe('保存（PUT 整体替换 extra）', () => {
    it('不改任何东西：容量键原样回写，无关键保留，开关写显式 bool', async () => {
      const wrapper = await open(
        buildClaudeAccount({ ...BALANCED_EXTRA, org_uuid: 'org-1', custom_flag: 7 })
      )

      const payload = await save(wrapper)

      expect(payload.concurrency).toBe(3)
      expect(payload.extra).toEqual({
        org_uuid: 'org-1',
        custom_flag: 7,
        max_sessions: 3,
        session_idle_timeout_minutes: 7,
        window_cost_limit: 60,
        window_cost_sticky_reserve: 15,
        base_rpm: 30,
        rpm_strategy: 'sticky_exempt',
        user_msg_queue_mode: 'throttle',
        adaptive_throttle: true,
        nurturing_enabled: false
      })
    })

    it('选「激进」：只覆盖 5 项，策略 / 缓冲 / 预留 / 空闲超时保持原样', async () => {
      const wrapper = await open(
        buildClaudeAccount({
          ...BALANCED_EXTRA,
          rpm_strategy: 'tiered',
          rpm_sticky_buffer: 9,
          adaptive_throttle: false
        })
      )

      await presetSelect(wrapper).setValue('4')
      await flushPromises()
      const payload = await save(wrapper)

      expect(payload.concurrency).toBe(5)
      expect(payload.extra).toEqual({
        max_sessions: 5,
        session_idle_timeout_minutes: 7,
        window_cost_limit: 100,
        window_cost_sticky_reserve: 15,
        base_rpm: 50,
        rpm_strategy: 'tiered',
        rpm_sticky_buffer: 9,
        user_msg_queue_mode: 'throttle',
        adaptive_throttle: true,
        nurturing_enabled: false
      })
    })

    it('选「旗舰」（5h 额度 0 = 不限）：额度与预留一起从 extra 里删掉', async () => {
      const wrapper = await open(buildClaudeAccount(BALANCED_EXTRA))

      await presetSelect(wrapper).setValue('5')
      await flushPromises()
      const payload = await save(wrapper)

      expect(payload.concurrency).toBe(8)
      expect(payload.extra.max_sessions).toBe(8)
      expect(payload.extra.base_rpm).toBe(80)
      expect('window_cost_limit' in payload.extra).toBe(false)
      expect('window_cost_sticky_reserve' in payload.extra).toBe(false)
    })

    it('把限制改成 0 = 关闭：对应键被删除（不写 0、不写默认 15），开关仍是显式 bool', async () => {
      const wrapper = await open(buildClaudeAccount({ ...BALANCED_EXTRA, org_uuid: 'org-1' }))

      await numberInput(wrapper, 'capacity-max-sessions').setValue('0')
      await numberInput(wrapper, 'capacity-base-rpm').setValue('')
      await numberInput(wrapper, 'capacity-window-cost').setValue('0')
      await wrapper.get('[data-testid="capacity-adaptive-toggle"]').trigger('click')
      await flushPromises()
      const payload = await save(wrapper)

      expect(payload.extra).toEqual({
        org_uuid: 'org-1',
        user_msg_queue_mode: 'throttle',
        adaptive_throttle: false,
        nurturing_enabled: false
      })
    })

    it('用户消息限速改成「关闭」会删键，并顺手清掉旧版 user_msg_queue_enabled', async () => {
      const wrapper = await open(
        buildClaudeAccount({ ...BALANCED_EXTRA, user_msg_queue_mode: 'serialize', user_msg_queue_enabled: true })
      )

      const off = wrapper
        .findAll('button')
        .find((button) => button.text() === 'admin.accounts.quotaControl.rpmLimit.umqModeOff')
      await off!.trigger('click')
      const payload = await save(wrapper)

      expect('user_msg_queue_mode' in payload.extra).toBe(false)
      expect('user_msg_queue_enabled' in payload.extra).toBe(false)
    })

    it('改并发：写账号列，不进 extra', async () => {
      const wrapper = await open(buildClaudeAccount(BALANCED_EXTRA))

      await numberInput(wrapper, 'capacity-concurrency').setValue('6')
      const payload = await save(wrapper)

      expect(payload.concurrency).toBe(6)
      expect('concurrency' in payload.extra).toBe(false)
    })

    it('从不写任何 persona 开头的键', async () => {
      const wrapper = await open(buildClaudeAccount(BALANCED_EXTRA))
      await presetSelect(wrapper).setValue('2')
      await flushPromises()
      const payload = await save(wrapper)

      expect(Object.keys(payload.extra).filter((key) => key.startsWith('persona'))).toEqual([])
    })
  })

  describe('TLS 指纹三态', () => {
    const tlsOf = (payload: Record<string, any>) => payload.extra.enable_tls_fingerprint

    it('账号上没有显式值：开关显示为关并提示未显式设置；不拨动就不写不删', async () => {
      const wrapper = await open(buildClaudeAccount(BALANCED_EXTRA))

      expect(wrapper.get('[data-testid="tls-fingerprint-toggle"]').element.className).toContain('bg-gray-200')
      expect(wrapper.find('[data-testid="tls-fingerprint-unset-hint"]').exists()).toBe(true)

      const payload = await save(wrapper)
      expect('enable_tls_fingerprint' in payload.extra).toBe(false)
    })

    it('原值 true 且不拨动：保持 true；原值 false 且不拨动：保持 false（不被改成缺省）', async () => {
      const on = await open(buildClaudeAccount({ ...BALANCED_EXTRA, enable_tls_fingerprint: true }))
      expect(on.find('[data-testid="tls-fingerprint-unset-hint"]').exists()).toBe(false)
      expect(tlsOf(await save(on))).toBe(true)

      mocks.update.mockClear()
      const off = await open(buildClaudeAccount({ ...BALANCED_EXTRA, enable_tls_fingerprint: false }))
      expect(off.find('[data-testid="tls-fingerprint-unset-hint"]').exists()).toBe(false)
      expect(tlsOf(await save(off))).toBe(false)
    })

    it('缺省 → 拨开：写显式 true', async () => {
      const wrapper = await open(buildClaudeAccount(BALANCED_EXTRA))
      await wrapper.get('[data-testid="tls-fingerprint-toggle"]').trigger('click')

      expect(tlsOf(await save(wrapper))).toBe(true)
    })

    it('缺省 → 拨开再拨回：写显式 false（动过开关就写显式布尔）', async () => {
      const wrapper = await open(buildClaudeAccount(BALANCED_EXTRA))
      await wrapper.get('[data-testid="tls-fingerprint-toggle"]').trigger('click')
      await wrapper.get('[data-testid="tls-fingerprint-toggle"]').trigger('click')

      expect(tlsOf(await save(wrapper))).toBe(false)
    })

    it('原值 true → 拨关：写显式 false 并删掉指纹模板 id', async () => {
      const wrapper = await open(
        buildClaudeAccount(
          { ...BALANCED_EXTRA, enable_tls_fingerprint: true, tls_fingerprint_profile_id: 5 },
          { tls_fingerprint_profile_id: 5 }
        )
      )
      await wrapper.get('[data-testid="tls-fingerprint-toggle"]').trigger('click')

      const payload = await save(wrapper)
      expect(tlsOf(payload)).toBe(false)
      expect('tls_fingerprint_profile_id' in payload.extra).toBe(false)
    })

    it('原值 true、带模板 id、不拨动：两者都原样保留', async () => {
      const wrapper = await open(
        buildClaudeAccount(
          { ...BALANCED_EXTRA, enable_tls_fingerprint: true, tls_fingerprint_profile_id: 5 },
          { tls_fingerprint_profile_id: 5 }
        )
      )

      const payload = await save(wrapper)
      expect(tlsOf(payload)).toBe(true)
      expect(payload.extra.tls_fingerprint_profile_id).toBe(5)
    })

    it('原值 false、不拨动：不碰指纹模板 id', async () => {
      const wrapper = await open(
        buildClaudeAccount({ ...BALANCED_EXTRA, enable_tls_fingerprint: false, tls_fingerprint_profile_id: 5 })
      )

      const payload = await save(wrapper)
      expect(tlsOf(payload)).toBe(false)
      expect(payload.extra.tls_fingerprint_profile_id).toBe(5)
    })
  })

  describe('不属于容量模型的账号', () => {
    it('OpenAI 账号：没有容量卡，用通用并发输入框，extra 里不会冒出容量键', async () => {
      const account = buildClaudeAccount({}, { platform: 'openai', type: 'apikey', credentials: { api_key: 'sk', base_url: 'https://api.openai.com' } })
      const wrapper = await open(account)

      expect(wrapper.find('[data-testid="account-capacity-card"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="account-concurrency"]').exists()).toBe(true)

      const payload = await save(wrapper)
      for (const key of ['max_sessions', 'base_rpm', 'window_cost_limit', 'adaptive_throttle', 'nurturing_enabled']) {
        expect(payload.extra ?? {}, `OpenAI 账号不应写入 ${key}`).not.toHaveProperty(key)
      }
    })

    it('Anthropic API Key 账号：没有容量卡，extra 里不会冒出容量键', async () => {
      const account = buildClaudeAccount(
        {},
        { type: 'apikey', credentials: { api_key: 'sk', base_url: 'https://api.anthropic.com' }, credentials_status: { has_api_key: true } }
      )
      const wrapper = await open(account)

      expect(wrapper.find('[data-testid="account-capacity-card"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="account-concurrency"]').exists()).toBe(true)

      const payload = await save(wrapper)
      for (const key of ['max_sessions', 'base_rpm', 'window_cost_limit', 'adaptive_throttle', 'nurturing_enabled', 'enable_tls_fingerprint']) {
        expect(payload.extra ?? {}, `API Key 账号不应写入 ${key}`).not.toHaveProperty(key)
      }
    })
  })

  describe('重新打开', () => {
    it('改了数值但没保存就关闭，再次打开同一账号会从账号重新载入', async () => {
      const account = buildClaudeAccount(BALANCED_EXTRA)
      const wrapper = await open(account)
      await numberInput(wrapper, 'capacity-base-rpm').setValue('99')
      expect(presetSelect(wrapper).element.value).toBe('custom')

      await wrapper.setProps({ show: false })
      await wrapper.setProps({ show: true })
      await flushPromises()

      expect(numberInput(wrapper, 'capacity-base-rpm').element.value).toBe('30')
      expect(presetSelect(wrapper).element.value).toBe('3')
    })

    it('切换到另一个账号：容量卡显示新账号的值，不残留上一个账号的状态', async () => {
      const wrapper = await open(buildClaudeAccount({ ...BALANCED_EXTRA, base_rpm: 99 }, { concurrency: 4 }))
      await wrapper.setProps({
        account: buildClaudeAccount({}, { id: 12, concurrency: 2 })
      })
      await flushPromises()

      expect(numberInput(wrapper, 'capacity-concurrency').element.value).toBe('2')
      expect(numberInput(wrapper, 'capacity-base-rpm').element.value).toBe('0')
      expect(numberInput(wrapper, 'capacity-max-sessions').element.value).toBe('0')
      expect(wrapper.get('[data-testid="capacity-adaptive-toggle"]').attributes('aria-checked')).toBe('false')
    })
  })
})
