import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
  getTierAffectedCount: vi.fn(),
  applyTierToExisting: vi.fn(),
  getCapacityPresets: vi.fn()
}))

vi.mock('@/api', () => ({
  adminAPI: {
    providers: {
      getSettings: mocks.getSettings,
      updateSettings: mocks.updateSettings,
      getTierAffectedCount: mocks.getTierAffectedCount,
      applyTierToExisting: mocks.applyTierToExisting
    }
  }
}))

// 档位模板共享缓存走的是 @/api/admin
vi.mock('@/api/admin', () => ({
  adminAPI: { settings: { getCapacityPresets: mocks.getCapacityPresets } }
}))

vi.mock('vue-router', () => ({ onBeforeRouteLeave: vi.fn() }))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => (params ? `${key}::${JSON.stringify(params)}` : key)
    })
  }
})

import ProviderSettingsPanel from '../ProviderSettingsPanel.vue'
import { resetCapacityPresets } from '@/composables/useCapacityPresets'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show" data-testid="apply-dialog"><slot /><slot name="footer" /></div>'
})

const PRESETS = {
  presets: [
    { key: '1', label: '入门', concurrency: 1, max_sessions: 1, base_rpm: 10, window_cost_limit: 20, adaptive_throttle: true },
    { key: '2', label: '保守', concurrency: 2, max_sessions: 2, base_rpm: 20, window_cost_limit: 40, adaptive_throttle: true },
    { key: '3', label: '均衡', concurrency: 3, max_sessions: 3, base_rpm: 30, window_cost_limit: 60, adaptive_throttle: false },
    { key: '5', label: '旗舰', concurrency: 8, max_sessions: 8, base_rpm: 80, window_cost_limit: 0, adaptive_throttle: true }
  ],
  default_key: '3'
}

function providerSettings(overrides: Record<string, unknown> = {}) {
  return {
    portal_enabled: true,
    hosting_types: [{ group_id: 1, label: 'Claude', description: '', enabled: true, sort: 1 }],
    default_group_id: 1,
    enabled_tier_keys: ['2', '3'],
    default_tier: '3',
    custom_tier_enabled: true,
    custom_tier_caps: { concurrency: 8, max_sessions: 8, base_rpm: 80, window_cost_limit: 0 },
    settlement_timezone: 'Asia/Shanghai',
    settlement_cooldown_seconds: 600,
    account_priority: 1,
    auto_proxy_max_accounts: 2,
    proxy_mode_policy: 'both',
    available_groups: [
      {
        id: 1,
        name: 'ccmax',
        platform: 'anthropic',
        rate_multiplier: 1,
        content_review_policy: 'inherit',
        claude_oauth_system_prompt_policy: 'inherit',
        status: 'active',
        existing_priorities: [1]
      }
    ],
    ...overrides
  }
}

async function mountPanel() {
  const wrapper = mount(ProviderSettingsPanel, { global: { stubs: { BaseDialog: BaseDialogStub } } })
  await flushPromises()
  return wrapper
}

type Wrapper = Awaited<ReturnType<typeof mountPanel>>

const tierRows = (wrapper: Wrapper) => wrapper.findAll('[data-testid="provider-tier-row"]')
const openBoxes = (wrapper: Wrapper) =>
  tierRows(wrapper).map((row) => (row.get('[data-testid="provider-tier-open"]').element as HTMLInputElement).checked)
const defaultTierOptions = (wrapper: Wrapper) =>
  wrapper
    .get('#ps-default-tier')
    .findAll('option')
    .map((option) => option.text())
const saveButton = (wrapper: Wrapper) =>
  wrapper.findAll('button').find((button) => button.text() === 'admin.providerSettings.save')!

async function toggleOpen(wrapper: Wrapper, rowIndex: number, checked: boolean) {
  await tierRows(wrapper)[rowIndex].get('[data-testid="provider-tier-open"]').setValue(checked)
}

describe('ProviderSettingsPanel 速率档位（只读数值 + 对供货商开放）', () => {
  beforeEach(() => {
    resetCapacityPresets()
    Object.values(mocks).forEach((mock) => mock.mockReset())
    mocks.getSettings.mockResolvedValue(providerSettings())
    mocks.updateSettings.mockImplementation(async (payload: unknown) => payload)
    mocks.getCapacityPresets.mockResolvedValue(structuredClone(PRESETS))
    mocks.getTierAffectedCount.mockResolvedValue({ tier: '3', affected: 7 })
    mocks.applyTierToExisting.mockResolvedValue({ matched: 7, updated: 5 })
  })

  it('每个模板一行：数值只读（行内没有任何输入框），开放状态来自 enabled_tier_keys', async () => {
    const wrapper = await mountPanel()

    expect(tierRows(wrapper)).toHaveLength(4)
    expect(openBoxes(wrapper)).toEqual([false, true, true, false])
    for (const row of tierRows(wrapper)) {
      // 唯一的输入控件就是「对供货商开放」勾选框：数值列不能编辑
      expect(row.findAll('input')).toHaveLength(1)
    }
    const balanced = tierRows(wrapper)[2].text()
    expect(balanced).toContain('3')
    expect(balanced).toContain('均衡')
    expect(balanced).toContain('30')
    expect(balanced).toContain('60')
    expect(balanced).toContain('admin.providerSettings.adaptiveOff')
    expect(tierRows(wrapper)[1].text()).toContain('admin.providerSettings.adaptiveOn')
  })

  it('不再有防封档位（persona）列或下拉', async () => {
    const wrapper = await mountPanel()
    expect(wrapper.html()).not.toContain('PersonaPreset')
    expect(wrapper.html()).not.toContain('personaPreset')
  })

  it('默认档位下拉只列已开放的模板', async () => {
    const wrapper = await mountPanel()
    expect(defaultTierOptions(wrapper)).toEqual(['保守', '均衡'])
  })

  it('勾选开放：默认档位下拉随之变化，出现「有未保存的修改」', async () => {
    const wrapper = await mountPanel()
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()

    await toggleOpen(wrapper, 3, true)

    expect(openBoxes(wrapper)).toEqual([false, true, true, true])
    expect(defaultTierOptions(wrapper)).toEqual(['保守', '均衡', '旗舰'])
    expect(wrapper.text()).toContain('admin.providerSettings.unsavedChanges')
    expect(saveButton(wrapper).attributes('disabled')).toBeUndefined()
  })

  it('开放列表始终按模板表的顺序排列，勾选再取消不会无端变成「有未保存的修改」', async () => {
    const wrapper = await mountPanel()

    await toggleOpen(wrapper, 0, true)
    await toggleOpen(wrapper, 0, false)

    expect(wrapper.text()).not.toContain('admin.providerSettings.unsavedChanges')
  })

  it('保存：提交 enabled_tier_keys，不再带 capacity_tiers，其它字段原样提交', async () => {
    const wrapper = await mountPanel()
    await toggleOpen(wrapper, 3, true)
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(mocks.updateSettings).toHaveBeenCalledTimes(1)
    const payload = mocks.updateSettings.mock.calls[0][0]
    expect(payload.enabled_tier_keys).toEqual(['2', '3', '5'])
    expect(payload.default_tier).toBe('3')
    expect(payload).not.toHaveProperty('capacity_tiers')
    expect(payload).not.toHaveProperty('available_groups')
    expect(payload.custom_tier_enabled).toBe(true)
    expect(payload.custom_tier_caps).toEqual({ concurrency: 8, max_sessions: 8, base_rpm: 80, window_cost_limit: 0 })
  })

  it('至少要开放一个档位', async () => {
    const wrapper = await mountPanel()
    await toggleOpen(wrapper, 1, false)
    await toggleOpen(wrapper, 2, false)
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(mocks.updateSettings).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.providerSettings.tiersHint')
  })

  it('默认档位必须是已开放的档位：把默认档位取消开放就不能保存', async () => {
    const wrapper = await mountPanel()
    await toggleOpen(wrapper, 2, false) // 取消开放默认档位 3，仍剩 2 号
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(mocks.updateSettings).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('common.required')

    // 把默认档位改成仍开放的 2 号后即可保存
    await wrapper.get('#ps-default-tier').setValue('2')
    await saveButton(wrapper).trigger('click')
    await flushPromises()
    expect(mocks.updateSettings).toHaveBeenCalledTimes(1)
    expect(mocks.updateSettings.mock.calls[0][0].default_tier).toBe('2')
  })

  it('模板没加载出来：只给提示、不渲染档位表；其它设置仍可保存，开放列表原样提交，不做档位校验', async () => {
    mocks.getCapacityPresets.mockRejectedValue(new Error('boom'))
    const wrapper = await mountPanel()

    expect(wrapper.find('[data-testid="provider-tiers-unavailable"]').exists()).toBe(true)
    expect(tierRows(wrapper)).toHaveLength(0)

    await wrapper.get('#ps-timezone').setValue('UTC')
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(mocks.updateSettings).toHaveBeenCalledTimes(1)
    const payload = mocks.updateSettings.mock.calls[0][0]
    expect(payload.settlement_timezone).toBe('UTC')
    expect(payload.enabled_tier_keys).toEqual(['2', '3'])
    expect(payload.default_tier).toBe('3')
  })

  it('「应用到存量」按钮把模板 key 传给接口，确认框里带上受影响的账号数', async () => {
    const wrapper = await mountPanel()
    const applyButtons = tierRows(wrapper).map((row) =>
      row.findAll('button').find((button) => button.text() === 'admin.providerSettings.applyToExisting')!
    )
    await applyButtons[2].trigger('click')
    await flushPromises()

    expect(mocks.getTierAffectedCount).toHaveBeenCalledWith('3')
    expect(wrapper.get('[data-testid="apply-dialog"]').text()).toContain('"count":7')
  })

  it('自定义档护栏仍在，上限与档位模板卡共用同一份常量', async () => {
    const wrapper = await mountPanel()

    expect(wrapper.get('#ps-cap-conc').attributes('max')).toBe('50')
    expect(wrapper.get('#ps-cap-sessions').attributes('max')).toBe('50')
    expect(wrapper.get('#ps-cap-rpm').attributes('max')).toBe('500')
    expect(wrapper.get('#ps-cap-window').attributes('max')).toBe('1000')
  })
})
