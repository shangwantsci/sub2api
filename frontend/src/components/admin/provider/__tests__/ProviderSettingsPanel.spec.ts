import { defineComponent } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
  getTierApplyPreview: vi.fn(),
  applyTierToExisting: vi.fn(),
  getCapacityPresets: vi.fn()
}))

// 默认 t 只回显 key（配合下面大部分断言）；「应用到存量」那组用例要核对管理员真正看到的中文，
// 把这个开关打开后 t 改用真实的 zh 词条。
const i18nMode = vi.hoisted(() => ({ real: false }))

vi.mock('@/api', () => ({
  adminAPI: {
    providers: {
      getSettings: mocks.getSettings,
      updateSettings: mocks.updateSettings,
      getTierApplyPreview: mocks.getTierApplyPreview,
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
  const { default: zh } = await import('@/i18n/locales/zh')
  const realI18n = actual.createI18n({ legacy: false, locale: 'zh', messages: { zh } })
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (i18nMode.real) return realI18n.global.t(key, params ?? {})
        return params ? `${key}::${JSON.stringify(params)}` : key
      }
    })
  }
})

import ProviderSettingsPanel from '../ProviderSettingsPanel.vue'
import { resetCapacityPresets } from '@/composables/useCapacityPresets'

// 真实 BaseDialog 会 Teleport 到 body，这里换成原地渲染的桩，并带上标题和一个会触发 close 的 X 按钮
const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false }, title: { type: String, default: '' } },
  emits: ['close'],
  template: `
    <div v-if="show" data-testid="apply-dialog">
      <h3 data-testid="apply-dialog-title">{{ title }}</h3>
      <button type="button" data-testid="apply-dialog-x" @click="$emit('close')">x</button>
      <slot />
      <slot name="footer" />
    </div>`
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

// 契约里的预览响应形状：matched 是该档位账号总数，will_change 是确实会变的账号数，
// samples 只含将改动的账号。will_change 故意大于样例条数，用来证明确认时带的是 will_change 而不是样例条数。
function previewResponse(overrides: Record<string, unknown> = {}) {
  return {
    tier: '3',
    matched: 12,
    will_change: 5,
    samples: [
      {
        account_id: 101,
        name: 'alice@example.com',
        changes: [
          { field: 'concurrency', from: 10, to: 3 },
          { field: 'adaptive_throttle', from: false, to: true }
        ]
      },
      {
        account_id: 102,
        name: 'bob@example.com',
        changes: [
          { field: 'window_cost_limit', from: 0, to: 60 },
          { field: 'max_sessions', from: 5, to: 3 },
          { field: 'base_rpm', from: 50, to: 30 }
        ]
      }
    ],
    ...overrides
  }
}

function resetMocks() {
  resetCapacityPresets()
  Object.values(mocks).forEach((mock) => mock.mockReset())
  mocks.getSettings.mockResolvedValue(providerSettings())
  mocks.updateSettings.mockImplementation(async (payload: unknown) => payload)
  mocks.getCapacityPresets.mockResolvedValue(structuredClone(PRESETS))
  mocks.getTierApplyPreview.mockResolvedValue(previewResponse())
  mocks.applyTierToExisting.mockResolvedValue({ matched: 12, updated: 5 })
}

describe('ProviderSettingsPanel 速率档位（只读数值 + 对供货商开放）', () => {
  beforeEach(resetMocks)

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

  it('未对供货商开放的档位：「应用到存量」禁用并说明原因；刚勾选但还没保存的也不行，保存后才可用', async () => {
    const wrapper = await mountPanel()
    const applyBtn = (rowIndex: number) => tierRows(wrapper)[rowIndex].get('[data-testid="provider-tier-apply"]')

    // 夹具里档位 2 / 3 已开放，档位 1 / 5 没开放
    expect(applyBtn(0).attributes('disabled')).toBeDefined()
    expect(applyBtn(3).attributes('disabled')).toBeDefined()
    expect(applyBtn(0).attributes('title')).toBe('admin.providerSettings.applyClosedTierHint')
    expect(applyBtn(1).attributes('disabled')).toBeUndefined()
    expect(applyBtn(2).attributes('disabled')).toBeUndefined()
    expect(applyBtn(2).attributes('title')).toBeUndefined()

    // 刚勾选还没保存：后端按已保存的设置判断开放范围，此时点了仍会得到 400 INVALID_TIER，所以保持禁用
    await toggleOpen(wrapper, 0, true)
    expect(applyBtn(0).attributes('disabled')).toBeDefined()

    await saveButton(wrapper).trigger('click')
    await flushPromises()
    expect(applyBtn(0).attributes('disabled')).toBeUndefined()
    expect(applyBtn(0).attributes('title')).toBeUndefined()
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

  it('自定义档护栏仍在，上限与档位模板卡共用同一份常量', async () => {
    const wrapper = await mountPanel()

    expect(wrapper.get('#ps-cap-conc').attributes('max')).toBe('50')
    expect(wrapper.get('#ps-cap-sessions').attributes('max')).toBe('50')
    expect(wrapper.get('#ps-cap-rpm').attributes('max')).toBe('500')
    expect(wrapper.get('#ps-cap-window').attributes('max')).toBe('1000')
  })
})

// ===== 「应用到存量」：先预览、再确认 =====
//
// 这组用例用真实的 zh 词条（i18nMode.real），断言的是管理员真正看到的文字。

const applyButtons = (wrapper: Wrapper) =>
  tierRows(wrapper).map((row) => row.get('[data-testid="provider-tier-apply"]'))
const applyButton = (wrapper: Wrapper, rowIndex: number) => applyButtons(wrapper)[rowIndex]
/**
 * 已对供货商开放的档位（本文件夹具里是档位 2 与 3，即第 1、2 行）的「应用到存量」按钮。
 * 未开放档位的按钮恒禁用（见上面的「未对供货商开放的档位」用例），所以「请求结束后按钮恢复」
 * 只能看已开放档位的按钮。
 */
const openApplyButtons = (wrapper: Wrapper) => [applyButton(wrapper, 1), applyButton(wrapper, 2)]
const isDisabled = (button: { attributes: (name: string) => string | undefined }) =>
  button.attributes('disabled') !== undefined
const dialogExists = (wrapper: Wrapper) => wrapper.find('[data-testid="apply-dialog"]').exists()
const confirmButton = (wrapper: Wrapper) => wrapper.get('[data-testid="apply-confirm"]')
const cancelButton = (wrapper: Wrapper) => wrapper.get('[data-testid="apply-cancel"]')
const dialogText = (wrapper: Wrapper) => wrapper.get('[data-testid="apply-dialog"]').text()

/** 点第 rowIndex 行（默认档位 3）的「应用到存量」，等预览回来，确认框已弹出。 */
async function openApplyDialog(wrapper: Wrapper, rowIndex = 2) {
  await applyButton(wrapper, rowIndex).trigger('click')
  await flushPromises()
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

/** 与 apiClient 响应拦截器对 HTTP 409 产出的错误形状一致（见 api/__tests__/admin.providers.spec.ts）。 */
const conflictError = {
  status: 409,
  code: 409,
  reason: 'PRESET_APPLY_CONFLICT',
  message: 'preset apply conflict'
}

describe('ProviderSettingsPanel 应用到存量（先预览、再确认）', () => {
  beforeEach(() => {
    resetMocks()
    i18nMode.real = true
  })
  afterEach(() => {
    i18nMode.real = false
  })

  it('点「应用到存量」先调预览接口（带模板 key），此时还没有写任何账号；确认框显示改动数、覆盖提示、样例与人话改动', async () => {
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)

    expect(mocks.getTierApplyPreview).toHaveBeenCalledTimes(1)
    expect(mocks.getTierApplyPreview).toHaveBeenCalledWith('3')
    expect(mocks.applyTierToExisting).not.toHaveBeenCalled()

    expect(wrapper.get('[data-testid="apply-dialog-title"]').text()).toBe('应用到存量账号：档位 3 (均衡)')
    expect(wrapper.get('[data-testid="apply-summary"]').text()).toBe('将改动 5 个账号（该档位共 12 个账号）。')
    expect(wrapper.get('[data-testid="apply-warning"]').text()).toContain(
      '这会用模板数值覆盖这些账号上已手工调整过的并发、会话数、RPM、5h 额度、动态降速和负载因子'
    )

    const samples = wrapper.findAll('[data-testid="apply-sample"]')
    expect(samples).toHaveLength(2)
    expect(samples[0].text()).toContain('alice@example.com')
    expect(samples[0].text()).toContain('#101')
    expect(samples[0].findAll('[data-testid="apply-change"]').map((li) => li.text())).toEqual([
      '并发 10 → 3',
      '动态降速 关 → 开'
    ])
    expect(samples[1].text()).toContain('bob@example.com')
    expect(samples[1].findAll('[data-testid="apply-change"]').map((li) => li.text())).toEqual([
      '5h 额度 不限 → $60',
      '会话数 5 → 3',
      'RPM 50 → 30'
    ])

    // 样例只有 2 条而将改动 5 个：要说明还有多少没列出来
    expect(wrapper.get('[data-testid="apply-samples-more"]').text()).toBe(
      '仅列出其中 2 个，另有 3 个账号也会被改动。'
    )
    expect(confirmButton(wrapper).text()).toBe('确认应用到 5 个账号')
    expect(isDisabled(confirmButton(wrapper))).toBe(false)
  })

  it.each([
    ['并发', { field: 'concurrency', from: 10, to: 3 }, '并发 10 → 3'],
    ['会话数', { field: 'max_sessions', from: 5, to: 3 }, '会话数 5 → 3'],
    ['会话数改为不限', { field: 'max_sessions', from: 5, to: 0 }, '会话数 5 → 不限'],
    ['RPM', { field: 'base_rpm', from: 50, to: 30 }, 'RPM 50 → 30'],
    ['RPM 从不限改为有上限', { field: 'base_rpm', from: 0, to: 30 }, 'RPM 不限 → 30'],
    ['额度从不限改为美元数', { field: 'window_cost_limit', from: 0, to: 60 }, '5h 额度 不限 → $60'],
    ['额度从美元数改为不限', { field: 'window_cost_limit', from: 60, to: 0 }, '5h 额度 $60 → 不限'],
    ['动态降速由关到开', { field: 'adaptive_throttle', from: false, to: true }, '动态降速 关 → 开'],
    ['动态降速由开到关', { field: 'adaptive_throttle', from: true, to: false }, '动态降速 开 → 关'],
    // 负载因子随并发一起对齐：手调过负载因子的账号会被覆盖，必须在预览里看得见
    ['负载因子被手调过', { field: 'load_factor', from: 9, to: 3 }, '负载因子 9 → 3']
  ])('改动用人话显示：%s', async (_name, change, expected) => {
    mocks.getTierApplyPreview.mockResolvedValue(
      previewResponse({
        will_change: 1,
        samples: [{ account_id: 7, name: 'solo@example.com', changes: [change] }]
      })
    )
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)

    expect(wrapper.findAll('[data-testid="apply-change"]').map((li) => li.text())).toEqual([expected])
    // 样例就是全部将改动的账号时，不出现「还有多少没列出」
    expect(wrapper.find('[data-testid="apply-samples-more"]').exists()).toBe(false)
  })

  it('样例按后端给的条数全部列出（最多 20 条），将改动数超出样例时给出未列出的数量', async () => {
    const samples = Array.from({ length: 20 }, (_, i) => ({
      account_id: 100 + i,
      name: `acct-${i}@example.com`,
      changes: [{ field: 'concurrency', from: 10, to: 3 }]
    }))
    mocks.getTierApplyPreview.mockResolvedValue(previewResponse({ matched: 40, will_change: 25, samples }))
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)

    expect(wrapper.findAll('[data-testid="apply-sample"]')).toHaveLength(20)
    expect(wrapper.get('[data-testid="apply-summary"]').text()).toBe('将改动 25 个账号（该档位共 40 个账号）。')
    expect(wrapper.get('[data-testid="apply-samples-more"]').text()).toBe(
      '仅列出其中 20 个，另有 5 个账号也会被改动。'
    )
  })

  it('确认：应用接口带上预览返回的 will_change 作为 expected_changed，成功后提示已更新 N 个账号并关闭确认框', async () => {
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)
    await confirmButton(wrapper).trigger('click')
    await flushPromises()

    expect(mocks.applyTierToExisting).toHaveBeenCalledTimes(1)
    // 第二个参数是 will_change（5），不是样例条数（2），也不是 matched（12）
    expect(mocks.applyTierToExisting).toHaveBeenCalledWith('3', 5)
    expect(mocks.getTierApplyPreview).toHaveBeenCalledTimes(1)
    expect(dialogExists(wrapper)).toBe(false)
    expect(wrapper.get('[data-testid="apply-notice"]').text()).toBe('已更新 5 个账号（该档位共 12 个）')
    expect(wrapper.find('[data-testid="apply-error"]').exists()).toBe(false)
  })

  it('页面有未保存的修改时，「已更新 N 个账号」仍然看得到（不能被「有未保存的修改」顶掉）', async () => {
    const wrapper = await mountPanel()
    await toggleOpen(wrapper, 3, true)
    expect(wrapper.text()).toContain('有未保存的修改')

    await openApplyDialog(wrapper)
    await confirmButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="apply-notice"]').text()).toContain('已更新 5 个账号')
  })

  it('将改动 0 个账号：提示没有需要改动的账号，确认按钮禁用，点了也不会调用应用接口', async () => {
    mocks.getTierApplyPreview.mockResolvedValue(previewResponse({ will_change: 0, samples: [] }))
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)

    expect(wrapper.get('[data-testid="apply-no-change"]').text()).toBe(
      '没有需要改动的账号：该档位下的 12 个账号已经与当前模板一致。'
    )
    expect(wrapper.find('[data-testid="apply-warning"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="apply-samples"]').exists()).toBe(false)
    expect(isDisabled(confirmButton(wrapper))).toBe(true)
    // 没有可应用的对象时，禁用的按钮不应该写着「应用到 0 个账号」
    expect(confirmButton(wrapper).text()).toBe('确认')

    await confirmButton(wrapper).trigger('click')
    await flushPromises()
    expect(mocks.applyTierToExisting).not.toHaveBeenCalled()
    // 仍可关闭
    await cancelButton(wrapper).trigger('click')
    expect(dialogExists(wrapper)).toBe(false)
  })

  it('该档位下没有账号（matched 为 0）：给出明确提示，确认按钮禁用', async () => {
    mocks.getTierApplyPreview.mockResolvedValue(previewResponse({ matched: 0, will_change: 0, samples: [] }))
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)

    expect(wrapper.get('[data-testid="apply-no-match"]').text()).toBe('该档位下还没有账号，没有可应用的对象。')
    expect(wrapper.find('[data-testid="apply-no-change"]').exists()).toBe(false)
    expect(isDisabled(confirmButton(wrapper))).toBe(true)
    await confirmButton(wrapper).trigger('click')
    expect(mocks.applyTierToExisting).not.toHaveBeenCalled()
  })

  it('取消：不调用应用接口；再点一次会重新拉一份新的预览', async () => {
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)
    await cancelButton(wrapper).trigger('click')

    expect(dialogExists(wrapper)).toBe(false)
    expect(mocks.applyTierToExisting).not.toHaveBeenCalled()

    await openApplyDialog(wrapper)
    expect(dialogExists(wrapper)).toBe(true)
    expect(mocks.getTierApplyPreview).toHaveBeenCalledTimes(2)
    expect(mocks.applyTierToExisting).not.toHaveBeenCalled()
  })

  it('应用接口返回 409：提示数据变了，自动重新拉预览并刷新确认框，用新的 will_change 再确认', async () => {
    mocks.applyTierToExisting.mockRejectedValueOnce(conflictError)
    mocks.getTierApplyPreview
      .mockResolvedValueOnce(previewResponse())
      .mockResolvedValueOnce(
        previewResponse({
          will_change: 4,
          samples: [{ account_id: 103, name: 'carol@example.com', changes: [{ field: 'base_rpm', from: 60, to: 30 }] }]
        })
      )
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)
    await confirmButton(wrapper).trigger('click')
    await flushPromises()

    expect(mocks.applyTierToExisting).toHaveBeenCalledTimes(1)
    expect(mocks.applyTierToExisting).toHaveBeenLastCalledWith('3', 5)
    // 重新预览了一次，仍然是同一个档位
    expect(mocks.getTierApplyPreview).toHaveBeenCalledTimes(2)
    expect(mocks.getTierApplyPreview).toHaveBeenLastCalledWith('3')

    // 确认框还开着，内容换成了新的预览；冲突提示出现，不当作普通错误显示
    expect(dialogExists(wrapper)).toBe(true)
    expect(wrapper.get('[data-testid="apply-conflict"]').text()).toBe(
      '账号数据在你确认期间发生了变化，请重新确认。'
    )
    expect(wrapper.find('[data-testid="apply-error"]').exists()).toBe(false)
    expect(dialogText(wrapper)).not.toContain('preset apply conflict')
    expect(wrapper.find('[data-testid="apply-notice"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="apply-summary"]').text()).toBe('将改动 4 个账号（该档位共 12 个账号）。')
    expect(wrapper.findAll('[data-testid="apply-sample"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="apply-sample"]').text()).toContain('carol@example.com')

    // 管理员对新的数字再确认：这次带的是新的 will_change（4）
    expect(isDisabled(confirmButton(wrapper))).toBe(false)
    await confirmButton(wrapper).trigger('click')
    await flushPromises()
    expect(mocks.applyTierToExisting).toHaveBeenCalledTimes(2)
    expect(mocks.applyTierToExisting).toHaveBeenLastCalledWith('3', 4)
    expect(dialogExists(wrapper)).toBe(false)
    expect(wrapper.get('[data-testid="apply-notice"]').text()).toContain('已更新 5 个账号')
  })

  it('409 之后重新预览发现已经没有要改的账号：确认框改为「没有需要改动的账号」，确认按钮禁用', async () => {
    mocks.applyTierToExisting.mockRejectedValueOnce(conflictError)
    mocks.getTierApplyPreview
      .mockResolvedValueOnce(previewResponse())
      .mockResolvedValueOnce(previewResponse({ will_change: 0, samples: [] }))
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)
    await confirmButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="apply-conflict"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="apply-no-change"]').exists()).toBe(true)
    expect(isDisabled(confirmButton(wrapper))).toBe(true)
  })

  it('409 之后重新预览也失败：显示失败原因，确认按钮保持禁用（不能拿过期的数字去确认），可以关闭', async () => {
    mocks.applyTierToExisting.mockRejectedValueOnce(conflictError)
    mocks.getTierApplyPreview
      .mockResolvedValueOnce(previewResponse())
      .mockRejectedValueOnce({ status: 500, message: 'preview boom' })
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)
    await confirmButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="apply-conflict"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="apply-error"]').text()).toBe('preview boom')
    expect(wrapper.find('[data-testid="apply-summary"]').exists()).toBe(false)
    expect(isDisabled(confirmButton(wrapper))).toBe(true)
    expect(isDisabled(cancelButton(wrapper))).toBe(false)
    expect(wrapper.find('[data-testid="apply-preview-loading"]').exists()).toBe(false)

    await cancelButton(wrapper).trigger('click')
    expect(dialogExists(wrapper)).toBe(false)
  })

  it('应用接口返回其它错误：在确认框里显示错误，不重新预览，按钮恢复可点（不会卡在加载中）', async () => {
    mocks.applyTierToExisting.mockRejectedValueOnce({ status: 500, message: 'db down' })
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)
    await confirmButton(wrapper).trigger('click')
    await flushPromises()

    expect(dialogExists(wrapper)).toBe(true)
    expect(wrapper.get('[data-testid="apply-error"]').text()).toBe('db down')
    expect(wrapper.find('[data-testid="apply-conflict"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="apply-notice"]').exists()).toBe(false)
    expect(mocks.getTierApplyPreview).toHaveBeenCalledTimes(1)
    expect(isDisabled(confirmButton(wrapper))).toBe(false)
    expect(confirmButton(wrapper).text()).toBe('确认应用到 5 个账号')
    expect(isDisabled(cancelButton(wrapper))).toBe(false)
    expect(openApplyButtons(wrapper).some(isDisabled)).toBe(false)

    // 重试：成功后错误消失
    await confirmButton(wrapper).trigger('click')
    await flushPromises()
    expect(mocks.applyTierToExisting).toHaveBeenCalledTimes(2)
    expect(dialogExists(wrapper)).toBe(false)
    expect(wrapper.get('[data-testid="apply-notice"]').text()).toContain('已更新 5 个账号')
  })

  it('应用失败且错误里没有 message：显示兜底文案「应用失败」', async () => {
    mocks.applyTierToExisting.mockRejectedValueOnce(new Error(''))
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)
    await confirmButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="apply-error"]').text()).toBe('应用失败')
  })

  it('预览请求失败：不弹确认框，错误显示在页面上，入口按钮恢复可点（不会卡在加载中）', async () => {
    mocks.getTierApplyPreview.mockRejectedValueOnce({ status: 500, message: 'preview boom' })
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)

    expect(dialogExists(wrapper)).toBe(false)
    expect(wrapper.text()).toContain('preview boom')
    expect(mocks.applyTierToExisting).not.toHaveBeenCalled()
    expect(openApplyButtons(wrapper).some(isDisabled)).toBe(false)
    expect(applyButton(wrapper, 2).text()).toBe('应用到存量')

    // 再点一次即可重试
    await openApplyDialog(wrapper)
    expect(dialogExists(wrapper)).toBe(true)
    expect(wrapper.text()).not.toContain('preview boom')
  })

  it('预览失败且错误里没有 message：显示兜底文案「获取预览失败」', async () => {
    mocks.getTierApplyPreview.mockRejectedValueOnce(new Error(''))
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)

    expect(dialogExists(wrapper)).toBe(false)
    expect(wrapper.text()).toContain('获取预览失败')
  })

  it('预览请求在途时：所有「应用到存量」入口禁用，当前行显示加载中，重复点击不会重复请求', async () => {
    const pending = deferred<ReturnType<typeof previewResponse>>()
    mocks.getTierApplyPreview.mockReturnValue(pending.promise)
    const wrapper = await mountPanel()

    await applyButton(wrapper, 2).trigger('click')
    expect(applyButtons(wrapper).every(isDisabled)).toBe(true)
    expect(applyButton(wrapper, 2).text()).toBe('加载中...')
    expect(applyButton(wrapper, 0).text()).toBe('应用到存量')

    await applyButton(wrapper, 2).trigger('click')
    await applyButton(wrapper, 0).trigger('click')
    expect(mocks.getTierApplyPreview).toHaveBeenCalledTimes(1)
    expect(dialogExists(wrapper)).toBe(false)

    pending.resolve(previewResponse())
    await flushPromises()
    expect(dialogExists(wrapper)).toBe(true)
    expect(openApplyButtons(wrapper).some(isDisabled)).toBe(false)
  })

  it('应用请求在途时：确认与取消按钮都禁用并显示加载中，重复点击和关闭都不生效；返回后正常收尾', async () => {
    const pending = deferred<{ matched: number; updated: number }>()
    mocks.applyTierToExisting.mockReturnValue(pending.promise)
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)

    await confirmButton(wrapper).trigger('click')
    expect(isDisabled(confirmButton(wrapper))).toBe(true)
    expect(isDisabled(cancelButton(wrapper))).toBe(true)
    expect(confirmButton(wrapper).text()).toBe('加载中...')
    expect(applyButtons(wrapper).every(isDisabled)).toBe(true)

    await confirmButton(wrapper).trigger('click')
    await cancelButton(wrapper).trigger('click')
    // Esc / 右上角 X 发出的 close 事件也不能把在途请求的确认框关掉
    await wrapper.get('[data-testid="apply-dialog-x"]').trigger('click')
    expect(mocks.applyTierToExisting).toHaveBeenCalledTimes(1)
    expect(dialogExists(wrapper)).toBe(true)

    pending.resolve({ matched: 12, updated: 5 })
    await flushPromises()
    expect(dialogExists(wrapper)).toBe(false)
    expect(wrapper.get('[data-testid="apply-notice"]').text()).toContain('已更新 5 个账号')
    expect(openApplyButtons(wrapper).some(isDisabled)).toBe(false)
  })

  it('应用请求失败后同样不会卡住：按钮恢复，可以取消', async () => {
    const pending = deferred<{ matched: number; updated: number }>()
    mocks.applyTierToExisting.mockReturnValue(pending.promise)
    const wrapper = await mountPanel()
    await openApplyDialog(wrapper)
    await confirmButton(wrapper).trigger('click')

    pending.reject({ status: 0, message: 'Network error. Please check your connection.' })
    await flushPromises()

    expect(wrapper.get('[data-testid="apply-error"]').text()).toBe('Network error. Please check your connection.')
    expect(isDisabled(confirmButton(wrapper))).toBe(false)
    expect(isDisabled(cancelButton(wrapper))).toBe(false)
    await cancelButton(wrapper).trigger('click')
    expect(dialogExists(wrapper)).toBe(false)
  })
})
