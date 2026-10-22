import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  bulkUpdate: vi.fn(),
  checkMixedChannelRisk: vi.fn(),
  getCapacityPresets: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      bulkUpdate: mocks.bulkUpdate,
      checkMixedChannelRisk: mocks.checkMixedChannelRisk
    },
    settings: {
      getCapacityPresets: mocks.getCapacityPresets
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

import BulkEditAccountModal from '../BulkEditAccountModal.vue'
import { resetCapacityPresets } from '@/composables/useCapacityPresets'

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

function mountModal(extraProps: Record<string, unknown> = {}) {
  return mount(BulkEditAccountModal, {
    props: {
      show: true,
      accountIds: [1, 2],
      selectedPlatforms: ['anthropic'],
      selectedTypes: ['oauth'],
      proxies: [],
      groups: [],
      ...extraProps
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        ConfirmDialog: true,
        Select: {
          props: ['modelValue', 'options'],
          emits: ['update:modelValue'],
          template: `
            <select v-bind="$attrs" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
              <option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          `
        },
        ProxySelector: true,
        GroupSelector: true,
        Icon: true
      }
    }
  })
}

type Wrapper = ReturnType<typeof mountModal>

async function open(extraProps: Record<string, unknown> = {}): Promise<Wrapper> {
  const wrapper = mountModal(extraProps)
  await flushPromises()
  return wrapper
}

const presetCheckbox = (wrapper: Wrapper) => wrapper.get('#bulk-edit-capacity-preset-enabled')
const presetSelect = (wrapper: Wrapper) =>
  wrapper.get<HTMLSelectElement>('[data-testid="bulk-edit-capacity-preset-select"]')

async function submit(wrapper: Wrapper) {
  await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
  await flushPromises()
}

async function clickByText(wrapper: Wrapper, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text() === text)
  expect(button, `找不到按钮：${text}`).toBeDefined()
  await button!.trigger('click')
}

describe('BulkEditAccountModal 容量档位 / RPM / 用户消息限速', () => {
  beforeEach(() => {
    resetCapacityPresets()
    Object.values(mocks).forEach((mock) => mock.mockReset())
    mocks.bulkUpdate.mockResolvedValue({ success: 2, failed: 0, results: [] })
    mocks.checkMixedChannelRisk.mockResolvedValue({ has_risk: false })
    mocks.getCapacityPresets.mockResolvedValue(structuredClone(SEED))
  })

  describe('套用档位', () => {
    it('勾选并选「激进」：提交 {concurrency, extra:{max_sessions, base_rpm, window_cost_limit, adaptive_throttle}}', async () => {
      const wrapper = await open()
      await presetCheckbox(wrapper).setValue(true)
      await presetSelect(wrapper).setValue('4')
      await submit(wrapper)

      expect(mocks.bulkUpdate).toHaveBeenCalledTimes(1)
      expect(mocks.bulkUpdate).toHaveBeenCalledWith([1, 2], {
        concurrency: 5,
        extra: { max_sessions: 5, base_rpm: 50, window_cost_limit: 100, adaptive_throttle: true }
      })
    })

    it('选「旗舰」：5h 额度 0 = 不限，必须显式写 0（批量合并删不了键）', async () => {
      const wrapper = await open()
      await presetCheckbox(wrapper).setValue(true)
      await presetSelect(wrapper).setValue('5')
      await submit(wrapper)

      expect(mocks.bulkUpdate).toHaveBeenCalledWith([1, 2], {
        concurrency: 8,
        extra: { max_sessions: 8, base_rpm: 80, window_cost_limit: 0, adaptive_throttle: true }
      })
    })

    it('下拉默认选中设置里的默认档位，但不勾选就不会提交', async () => {
      mocks.getCapacityPresets.mockResolvedValue({ ...structuredClone(SEED), default_key: '2' })
      const wrapper = await open()

      expect(presetSelect(wrapper).element.value).toBe('2')
      await submit(wrapper)
      expect(mocks.bulkUpdate).not.toHaveBeenCalled()
    })

    it('下拉只列模板，没有「自定义」', async () => {
      const wrapper = await open()
      const values = presetSelect(wrapper)
        .findAll('option')
        .map((option) => option.element.value)
      expect(values).toEqual(['1', '2', '3', '4', '5'])
    })

    it('同时勾选「并发」：先写档位，再用并发细项覆盖档位里的并发', async () => {
      const wrapper = await open()
      await presetCheckbox(wrapper).setValue(true)
      await presetSelect(wrapper).setValue('4')
      await wrapper.get('#bulk-edit-concurrency-enabled').setValue(true)
      await wrapper.get('#bulk-edit-concurrency').setValue('7')
      await submit(wrapper)

      expect(mocks.bulkUpdate).toHaveBeenCalledWith([1, 2], {
        concurrency: 7,
        extra: { max_sessions: 5, base_rpm: 50, window_cost_limit: 100, adaptive_throttle: true }
      })
    })

    it('同时勾选「RPM」：先写档位，再用 RPM 细项覆盖档位里的 RPM', async () => {
      const wrapper = await open()
      await presetCheckbox(wrapper).setValue(true)
      await presetSelect(wrapper).setValue('4')
      await wrapper.get('#bulk-edit-rpm-limit-enabled').setValue(true)
      await wrapper.get('[data-testid="bulk-edit-base-rpm"]').setValue('40')
      await clickByText(wrapper, 'admin.accounts.quotaControl.rpmLimit.strategyStickyExempt')
      await submit(wrapper)

      expect(mocks.bulkUpdate).toHaveBeenCalledWith([1, 2], {
        concurrency: 5,
        extra: {
          max_sessions: 5,
          base_rpm: 40,
          window_cost_limit: 100,
          adaptive_throttle: true,
          rpm_strategy: 'sticky_exempt'
        }
      })
    })

    it('档位模板没加载出来：档位块给出提示、勾选框置灰，其它批量项照常可用', async () => {
      mocks.getCapacityPresets.mockRejectedValue(new Error('boom'))
      const wrapper = await open()

      expect(wrapper.find('[data-testid="bulk-edit-capacity-preset-select"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="bulk-edit-capacity-presets-unavailable"]').exists()).toBe(true)
      expect(presetCheckbox(wrapper).attributes('disabled')).toBeDefined()

      await wrapper.get('#bulk-edit-status-enabled').setValue(true)
      await submit(wrapper)
      expect(mocks.bulkUpdate).toHaveBeenCalledWith([1, 2], { status: 'active' })
    })

    it('关闭再打开弹窗：勾选状态被重置', async () => {
      const wrapper = await open()
      await presetCheckbox(wrapper).setValue(true)

      await wrapper.setProps({ show: false })
      await wrapper.setProps({ show: true })
      await flushPromises()

      expect((presetCheckbox(wrapper).element as HTMLInputElement).checked).toBe(false)
    })
  })

  describe('RPM 块', () => {
    it('勾选并填 RPM / 策略 / 缓冲：提交 base_rpm、rpm_strategy、rpm_sticky_buffer', async () => {
      const wrapper = await open()
      await wrapper.get('#bulk-edit-rpm-limit-enabled').setValue(true)
      await wrapper.get('[data-testid="bulk-edit-base-rpm"]').setValue('40')
      const buffer = wrapper
        .findAll('input[type="number"]')
        .find((input) => (input.element as HTMLInputElement).min === '1' && (input.element as HTMLInputElement).step === '1')
      await buffer!.setValue('6')
      await submit(wrapper)

      expect(mocks.bulkUpdate).toHaveBeenCalledWith([1, 2], {
        extra: { base_rpm: 40, rpm_strategy: 'tiered', rpm_sticky_buffer: 6 }
      })
    })

    it('RPM 填 0 或留空 = 关闭：显式写空值重置（合并语义删不了键）', async () => {
      const wrapper = await open()
      await wrapper.get('#bulk-edit-rpm-limit-enabled').setValue(true)
      await submit(wrapper)

      expect(mocks.bulkUpdate).toHaveBeenCalledWith([1, 2], {
        extra: { base_rpm: 0, rpm_strategy: '', rpm_sticky_buffer: 0 }
      })
    })
  })

  describe('用户消息限速', () => {
    it('选「串行队列」：写 user_msg_queue_mode 并清理旧开关', async () => {
      const wrapper = await open()
      await clickByText(wrapper, 'admin.accounts.quotaControl.rpmLimit.umqModeSerialize')
      await submit(wrapper)

      expect(mocks.bulkUpdate).toHaveBeenCalledWith([1, 2], {
        extra: { user_msg_queue_mode: 'serialize', user_msg_queue_enabled: false }
      })
    })

    it('选「关闭」：写空串清除账号级覆盖', async () => {
      const wrapper = await open()
      await clickByText(wrapper, 'admin.accounts.quotaControl.rpmLimit.umqModeOff')
      await submit(wrapper)

      expect(mocks.bulkUpdate).toHaveBeenCalledWith([1, 2], {
        extra: { user_msg_queue_mode: '', user_msg_queue_enabled: false }
      })
    })

    it('同一个选项点两次 = 取消选择，本次不改', async () => {
      const wrapper = await open()
      await clickByText(wrapper, 'admin.accounts.quotaControl.rpmLimit.umqModeThrottle')
      await clickByText(wrapper, 'admin.accounts.quotaControl.rpmLimit.umqModeThrottle')
      await submit(wrapper)

      expect(mocks.bulkUpdate).not.toHaveBeenCalled()
    })
  })

  describe('资格：只对全部是 Claude OAuth / SetupToken 的目标开放', () => {
    const blockVisible = (wrapper: Wrapper) =>
      wrapper.find('#bulk-edit-capacity-preset-enabled').exists() ||
      wrapper.find('#bulk-edit-rpm-limit-enabled').exists()

    it.each([
      ['选中账号：oauth', { selectedPlatforms: ['anthropic'], selectedTypes: ['oauth'] }, true],
      ['选中账号：setup-token', { selectedPlatforms: ['anthropic'], selectedTypes: ['setup-token'] }, true],
      ['选中账号：oauth + setup-token', { selectedPlatforms: ['anthropic'], selectedTypes: ['oauth', 'setup-token'] }, true],
      // types 为空时 every() 恒为 true，必须被显式挡掉
      ['选中账号：types 为空', { selectedPlatforms: ['anthropic'], selectedTypes: [] }, false],
      ['选中账号：混了 apikey', { selectedPlatforms: ['anthropic'], selectedTypes: ['oauth', 'apikey'] }, false],
      ['选中账号：只有 apikey', { selectedPlatforms: ['anthropic'], selectedTypes: ['apikey'] }, false],
      ['选中账号：跨平台', { selectedPlatforms: ['anthropic', 'openai'], selectedTypes: ['oauth'] }, false],
      ['选中账号：OpenAI oauth', { selectedPlatforms: ['openai'], selectedTypes: ['oauth'] }, false]
    ])('%s → 容量块可见 = %s', async (_name, props, visible) => {
      const wrapper = await open(props)
      expect(blockVisible(wrapper)).toBe(visible)
    })

    it('筛选结果模式：筛选条件没限定类型时，即使前 100 条预览全是 oauth 也不开放', async () => {
      const wrapper = await open({
        accountIds: [],
        selectedPlatforms: [],
        selectedTypes: [],
        target: {
          mode: 'filtered',
          filters: { platform: 'anthropic', type: '' },
          previewCount: 250,
          selectedPlatforms: ['anthropic'],
          selectedTypes: ['oauth']
        }
      })
      expect(blockVisible(wrapper)).toBe(false)
    })

    it('筛选结果模式：没限定平台同样不开放', async () => {
      const wrapper = await open({
        accountIds: [],
        selectedPlatforms: [],
        selectedTypes: [],
        target: {
          mode: 'filtered',
          filters: { platform: '', type: 'oauth' },
          previewCount: 250,
          selectedPlatforms: ['anthropic'],
          selectedTypes: ['oauth']
        }
      })
      expect(blockVisible(wrapper)).toBe(false)
    })

    it.each(['oauth', 'setup-token'])('筛选结果模式：platform=anthropic 且 type=%s 才开放，并提交 filters', async (type) => {
      const filters = { platform: 'anthropic', type, status: 'active' }
      const wrapper = await open({
        accountIds: [],
        selectedPlatforms: [],
        selectedTypes: [],
        target: {
          mode: 'filtered',
          filters,
          previewCount: 250,
          selectedPlatforms: ['anthropic'],
          selectedTypes: [type]
        }
      })
      expect(blockVisible(wrapper)).toBe(true)

      await presetCheckbox(wrapper).setValue(true)
      await presetSelect(wrapper).setValue('4')
      await submit(wrapper)

      expect(mocks.bulkUpdate).toHaveBeenCalledWith({
        filters,
        concurrency: 5,
        extra: { max_sessions: 5, base_rpm: 50, window_cost_limit: 100, adaptive_throttle: true }
      })
    })

    it('不合格目标不会发出任何容量键，即使其它批量项照常提交', async () => {
      const wrapper = await open({ selectedPlatforms: ['openai'], selectedTypes: ['oauth'] })
      await wrapper.get('#bulk-edit-status-enabled').setValue(true)
      await submit(wrapper)

      expect(mocks.bulkUpdate).toHaveBeenCalledWith([1, 2], { status: 'active' })
    })
  })

  it('不再有旧的防封档位（persona）入口', async () => {
    const wrapper = await open()
    expect(wrapper.find('#bulk-edit-persona-enabled').exists()).toBe(false)
  })
})
