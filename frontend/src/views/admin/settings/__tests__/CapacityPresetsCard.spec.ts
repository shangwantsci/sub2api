import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  getCapacityPresets: vi.fn(),
  updateCapacityPresets: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api', () => ({
  adminAPI: {
    settings: {
      getCapacityPresets: mocks.getCapacityPresets,
      updateCapacityPresets: mocks.updateCapacityPresets
    }
  }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess })
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (err: unknown, fallback: string) =>
    (err as { message?: string })?.message || fallback
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => (params ? `${key}::${JSON.stringify(params)}` : key)
    })
  }
})

import CapacityPresetsCard from '../CapacityPresetsCard.vue'
import { resetCapacityPresets, useCapacityPresets } from '@/composables/useCapacityPresets'

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

async function mountCard() {
  // 设置页整体是一个 <form>：放进 form 里挂载，才能确认回车与按钮不会把它提交掉
  const submitSpy = vi.fn((event: Event) => event.preventDefault())
  const Host = {
    components: { CapacityPresetsCard },
    setup: () => ({ submitSpy }),
    template: '<form @submit="submitSpy"><CapacityPresetsCard /></form>'
  }
  const wrapper = mount(Host)
  await flushPromises()
  return { wrapper, submitSpy }
}

type Mounted = Awaited<ReturnType<typeof mountCard>>

const rows = (m: Mounted) => m.wrapper.findAll('[data-testid="capacity-presets-row"]')
const field = (row: ReturnType<Mounted['wrapper']['findAll']>[number], name: string) =>
  row.get<HTMLInputElement>(`[data-field="${name}"]`)
const errorsText = (m: Mounted) => m.wrapper.find('[data-testid="capacity-presets-errors"]').text()

async function save(m: Mounted) {
  await m.wrapper.get('[data-testid="capacity-presets-save"]').trigger('click')
  await flushPromises()
}

async function addRow(m: Mounted, values: Partial<Record<'key' | 'label' | 'concurrency' | 'max_sessions' | 'base_rpm' | 'window_cost_limit', string>>) {
  await m.wrapper.get('[data-testid="capacity-presets-add"]').trigger('click')
  const row = rows(m)[rows(m).length - 1]
  for (const [name, value] of Object.entries(values)) {
    await field(row, name).setValue(value)
  }
  return row
}

describe('CapacityPresetsCard', () => {
  beforeEach(() => {
    resetCapacityPresets()
    Object.values(mocks).forEach((mock) => mock.mockReset())
    mocks.getCapacityPresets.mockResolvedValue(structuredClone(SEED))
    mocks.updateCapacityPresets.mockImplementation(async (payload: unknown) => structuredClone(payload))
  })

  describe('展示', () => {
    it('载入模板：每行一个模板，数值与名称都在，默认档位被选中', async () => {
      const m = await mountCard()

      expect(rows(m)).toHaveLength(5)
      const balanced = rows(m)[2]
      expect(field(balanced, 'key').element.value).toBe('3')
      expect(field(balanced, 'label').element.value).toBe('均衡')
      expect(field(balanced, 'concurrency').element.value).toBe('3')
      expect(field(balanced, 'max_sessions').element.value).toBe('3')
      expect(field(balanced, 'base_rpm').element.value).toBe('30')
      expect(field(balanced, 'window_cost_limit').element.value).toBe('60')
      expect(rows(m).map((row) => field(row, 'default').element.checked)).toEqual([false, false, true, false, false])
    })

    it('卡上写明：改模板只影响之后套用的账号，且本卡片单独保存', async () => {
      const m = await mountCard()
      const text = m.wrapper.text()
      expect(text).toContain('admin.settings.capacityPresets.onlyAffectsFuture')
      expect(text).toContain('admin.settings.capacityPresets.savedSeparately')
    })

    it('已保存模板的 key 只读；新增行的 key 可以填', async () => {
      const m = await mountCard()
      expect(field(rows(m)[0], 'key').attributes('readonly')).toBeDefined()

      await m.wrapper.get('[data-testid="capacity-presets-add"]').trigger('click')
      const added = rows(m)[5]
      expect(field(added, 'key').attributes('readonly')).toBeUndefined()
    })

    it('加载失败：显示失败提示与重试，保存按钮置灰；重试成功后恢复', async () => {
      mocks.getCapacityPresets.mockReset().mockRejectedValueOnce(new Error('boom')).mockResolvedValueOnce(structuredClone(SEED))
      const m = await mountCard()

      expect(m.wrapper.find('[data-testid="capacity-presets-load-failed"]').exists()).toBe(true)
      expect(m.wrapper.get('[data-testid="capacity-presets-save"]').attributes('disabled')).toBeDefined()

      await m.wrapper.get('[data-testid="capacity-presets-retry"]').trigger('click')
      await flushPromises()
      expect(rows(m)).toHaveLength(5)
      expect(m.wrapper.find('[data-testid="capacity-presets-load-failed"]').exists()).toBe(false)
    })
  })

  describe('设置页是一个 <form>：回车与按钮都不能把它提交掉', () => {
    it('所有输入都拦截回车（keydown.enter.prevent）', async () => {
      const m = await mountCard()
      const inputs = m.wrapper.findAll('input')
      expect(inputs.length).toBeGreaterThan(5 * 6)

      for (const input of inputs) {
        const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
        input.element.dispatchEvent(event)
        expect(event.defaultPrevented, `回车没有被拦截：${input.html().slice(0, 80)}`).toBe(true)
      }
    })

    it('所有按钮都是 type="button"，点击不会触发表单提交', async () => {
      const m = await mountCard()
      const buttons = m.wrapper.findAll('button')
      expect(buttons.length).toBeGreaterThan(0)
      for (const button of buttons) {
        expect(button.attributes('type')).toBe('button')
      }

      await m.wrapper.get('[data-testid="capacity-presets-add"]').trigger('click')
      await save(m)
      expect(m.submitSpy).not.toHaveBeenCalled()
    })
  })

  describe('校验（前端提前反馈，后端会再校验一遍）', () => {
    it('key 不能为空', async () => {
      const m = await mountCard()
      await addRow(m, { label: '新档', concurrency: '9', max_sessions: '9', base_rpm: '90', window_cost_limit: '200' })
      await save(m)

      expect(errorsText(m)).toContain('admin.settings.capacityPresets.errKeyRequired')
      expect(mocks.updateCapacityPresets).not.toHaveBeenCalled()
    })

    it.each(['custom', 'Custom', 'CUSTOM'])('key 不能是保留字 %s', async (key) => {
      const m = await mountCard()
      await addRow(m, { key, label: '新档', concurrency: '9', max_sessions: '9', base_rpm: '90', window_cost_limit: '200' })
      await save(m)

      expect(errorsText(m)).toContain('admin.settings.capacityPresets.errKeyReserved')
      expect(mocks.updateCapacityPresets).not.toHaveBeenCalled()
    })

    it('key 不能与已有模板重复', async () => {
      const m = await mountCard()
      await addRow(m, { key: '3', label: '新档', concurrency: '9', max_sessions: '9', base_rpm: '90', window_cost_limit: '200' })
      await save(m)

      expect(errorsText(m)).toContain('admin.settings.capacityPresets.errKeyDuplicate')
      expect(mocks.updateCapacityPresets).not.toHaveBeenCalled()
    })

    it('显示名不能为空', async () => {
      const m = await mountCard()
      await addRow(m, { key: '9', label: '', concurrency: '9', max_sessions: '9', base_rpm: '90', window_cost_limit: '200' })
      await save(m)

      expect(errorsText(m)).toContain('admin.settings.capacityPresets.errLabelRequired')
    })

    it('4 个数值（并发、会话数、RPM、5h 额度）组成的元组必须唯一，两行都被标红', async () => {
      const m = await mountCard()
      await addRow(m, { key: '9', label: '撞车', concurrency: '3', max_sessions: '3', base_rpm: '30', window_cost_limit: '60' })
      await save(m)

      expect(errorsText(m)).toContain('admin.settings.capacityPresets.errTupleDuplicate')
      expect(field(rows(m)[2], 'concurrency').classes()).toContain('border-red-500')
      expect(field(rows(m)[5], 'concurrency').classes()).toContain('border-red-500')
      expect(mocks.updateCapacityPresets).not.toHaveBeenCalled()
    })

    it('元组只差 adaptive_throttle 不算不同（反推标签不含动态降速）', async () => {
      const m = await mountCard()
      const row = await addRow(m, { key: '9', label: '只差开关', concurrency: '3', max_sessions: '3', base_rpm: '30', window_cost_limit: '60' })
      await row.get('[data-field="adaptive_throttle"]').trigger('click') // 关掉动态降速
      await save(m)

      expect(errorsText(m)).toContain('admin.settings.capacityPresets.errTupleDuplicate')
    })

    it.each([
      ['并发 0', { concurrency: '0' }, 'errConcurrency'],
      ['并发超上限', { concurrency: '51' }, 'errConcurrency'],
      ['会话数超上限', { max_sessions: '51' }, 'errMaxSessions'],
      ['RPM 超上限', { base_rpm: '501' }, 'errBaseRpm'],
      ['5h 额度超上限', { window_cost_limit: '1001' }, 'errWindowCost'],
      ['并发不是整数', { concurrency: '2.5' }, 'errConcurrency'],
      ['会话数为负', { max_sessions: '-1' }, 'errMaxSessions']
    ])('数值越界：%s', async (_name, override, errKey) => {
      const m = await mountCard()
      await addRow(m, {
        key: '9',
        label: '越界',
        concurrency: '9',
        max_sessions: '9',
        base_rpm: '90',
        window_cost_limit: '200',
        ...override
      })
      await save(m)

      expect(errorsText(m)).toContain(`admin.settings.capacityPresets.${errKey}`)
      expect(mocks.updateCapacityPresets).not.toHaveBeenCalled()
    })

    it('数值输入框被清空（得到空串）按非法处理，不会提交空串', async () => {
      const m = await mountCard()
      await field(rows(m)[0], 'base_rpm').setValue('')
      await save(m)

      expect(errorsText(m)).toContain('admin.settings.capacityPresets.errBaseRpm')
      expect(mocks.updateCapacityPresets).not.toHaveBeenCalled()
    })

    it('旗舰档 5h 额度 0、会话数 0 是合法的「不限」', async () => {
      const m = await mountCard()
      await field(rows(m)[4], 'max_sessions').setValue('0')
      await save(m)

      expect(mocks.updateCapacityPresets).toHaveBeenCalledTimes(1)
    })
  })

  describe('保存', () => {
    it('提交整张表：{presets:[...], default_key}，key 与名称去掉首尾空白', async () => {
      const m = await mountCard()
      await addRow(m, { key: ' 9 ', label: ' 新档 ', concurrency: '9', max_sessions: '9', base_rpm: '90', window_cost_limit: '200' })
      await save(m)

      expect(mocks.updateCapacityPresets).toHaveBeenCalledTimes(1)
      expect(mocks.updateCapacityPresets).toHaveBeenCalledWith({
        presets: [
          ...SEED.presets,
          { key: '9', label: '新档', concurrency: 9, max_sessions: 9, base_rpm: 90, window_cost_limit: 200, adaptive_throttle: true }
        ],
        default_key: '3'
      })
      expect(mocks.showSuccess).toHaveBeenCalledWith('admin.settings.capacityPresets.saved')
    })

    it('保存成功后：新增行的 key 从此只读，并通过 setPresets 让其它处自动刷新', async () => {
      const shared = useCapacityPresets()
      const m = await mountCard()
      await addRow(m, { key: '9', label: '新档', concurrency: '9', max_sessions: '9', base_rpm: '90', window_cost_limit: '200' })
      await field(rows(m)[0], 'label').setValue('入门改名')
      await save(m)

      expect(field(rows(m)[5], 'key').attributes('readonly')).toBeDefined()
      expect(shared.ready.value).toBe(true)
      expect(shared.presets.value.map((preset) => preset.key)).toEqual(['1', '2', '3', '4', '5', '9'])
      expect(shared.presets.value[0].label).toBe('入门改名')
      expect(shared.defaultKey.value).toBe('3')
    })

    it('改默认档位：default_key 跟着走', async () => {
      const m = await mountCard()
      await field(rows(m)[3], 'default').setValue(true)
      await save(m)

      expect(mocks.updateCapacityPresets.mock.calls[0][0].default_key).toBe('4')
    })

    it('先选新增行为默认、再改它的 key：default_key 跟着新 key 走', async () => {
      const m = await mountCard()
      const row = await addRow(m, { key: 'tmp', label: '新档', concurrency: '9', max_sessions: '9', base_rpm: '90', window_cost_limit: '200' })
      await field(row, 'default').setValue(true)
      await field(row, 'key').setValue('final')
      await save(m)

      expect(mocks.updateCapacityPresets.mock.calls[0][0].default_key).toBe('final')
    })

    it('保存失败：弹出错误，表格保持用户正在编辑的内容，新增行的 key 仍可改', async () => {
      mocks.updateCapacityPresets.mockRejectedValue(new Error('模板被供货商账号引用，不能删除'))
      const m = await mountCard()
      await addRow(m, { key: '9', label: '新档', concurrency: '9', max_sessions: '9', base_rpm: '90', window_cost_limit: '200' })
      await save(m)

      expect(mocks.showError).toHaveBeenCalledWith('模板被供货商账号引用，不能删除')
      expect(mocks.showSuccess).not.toHaveBeenCalled()
      expect(rows(m)).toHaveLength(6)
      expect(field(rows(m)[5], 'key').attributes('readonly')).toBeUndefined()
    })
  })

  describe('删除', () => {
    it('删除普通行', async () => {
      const m = await mountCard()
      await rows(m)[0].get('[data-testid="capacity-presets-delete"]').trigger('click')

      expect(rows(m)).toHaveLength(4)
      await save(m)
      expect(mocks.updateCapacityPresets.mock.calls[0][0].presets.map((p: { key: string }) => p.key)).toEqual(['2', '3', '4', '5'])
    })

    it('删除的是默认档位：默认改指第一个剩余模板', async () => {
      const m = await mountCard()
      await rows(m)[2].get('[data-testid="capacity-presets-delete"]').trigger('click')
      await save(m)

      expect(mocks.updateCapacityPresets.mock.calls[0][0].default_key).toBe('1')
    })

    it('只剩最后一行时不能再删', async () => {
      mocks.getCapacityPresets.mockResolvedValue({ presets: [SEED.presets[0]], default_key: '1' })
      const m = await mountCard()

      expect(rows(m)[0].get('[data-testid="capacity-presets-delete"]').attributes('disabled')).toBeDefined()
    })
  })
})
