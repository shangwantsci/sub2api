import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, nextTick, reactive } from 'vue'

// t 回显「key::参数 JSON」：断言里能直接看到卡片传了哪个 key、带了哪些参数
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => (params ? `${key}::${JSON.stringify(params)}` : key)
    })
  }
})

import GroupClaudePolicyCard from '../GroupClaudePolicyCard.vue'

/**
 * 卡片完全派生：三个字段只通过 v-model 读写，帽子由 describeHat 反推。
 * 这里用 Host 外壳（<form> 里放四个 v-model）驱动，期望值全部写字面量。
 * Select 是自己的组件（点开 + Teleport），这里用原生 <select> 的桩代替：
 * change 时空串 -> null、数字串 -> Number，data-testid 等 attrs 会落到根 <select> 上。
 */

interface CardState {
  claudeCodeOnly: boolean
  fallbackGroupId: number | null
  systemPromptPolicy: string
  contentReviewPolicy: string
}

const DEFAULT_STATE: CardState = {
  claudeCodeOnly: false,
  fallbackGroupId: null,
  systemPromptPolicy: 'inherit',
  contentReviewPolicy: 'inherit'
}

const OPTIONS = [
  { id: 5, name: '备用池 A' },
  { id: 6, name: '备用池 B' }
]

const ALL_HATS = ['cc_line', 'cc_fallback', 'open_follow', 'open_full', 'open_light', 'open_keep_prompt']

// 6 顶帽子各自对应的字段起点（第一项就是期望选中的帽子）
const HAT_STATES: Array<[string, Partial<CardState>]> = [
  ['cc_line', { claudeCodeOnly: true, fallbackGroupId: null, systemPromptPolicy: 'enabled' }],
  ['cc_fallback', { claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'inherit' }],
  ['open_follow', { claudeCodeOnly: false, fallbackGroupId: null, systemPromptPolicy: 'inherit' }],
  ['open_full', { claudeCodeOnly: false, fallbackGroupId: null, systemPromptPolicy: 'enabled' }],
  ['open_light', { claudeCodeOnly: false, fallbackGroupId: null, systemPromptPolicy: 'identity_only' }],
  ['open_keep_prompt', { claudeCodeOnly: false, fallbackGroupId: null, systemPromptPolicy: 'disabled' }]
]

const SelectStub = defineComponent({
  props: ['modelValue', 'options', 'placeholder', 'disabled'],
  emits: ['update:modelValue'],
  setup(_props, { emit }) {
    return {
      onChange(event: Event) {
        const raw = (event.target as HTMLSelectElement).value
        emit('update:modelValue', raw === '' ? null : Number(raw))
      }
    }
  },
  template: `
    <select :value="modelValue ?? ''" :disabled="disabled" :data-placeholder="placeholder" @change="onChange">
      <option v-for="option in options" :key="String(option.value)" :value="option.value ?? ''">{{ option.label }}</option>
    </select>
  `
})

// 必须挂到 document 上：jsdom 里没挂载的 <form> 点击没有 type 的按钮根本不会触发提交，
// 「点击不提交」就成了空转的断言（实测：未挂载 0 次，挂载后 1 次）。每个用例结束后统一卸载。
const mountedWrappers: Array<{ unmount: () => void }> = []
afterEach(() => {
  for (const wrapper of mountedWrappers.splice(0)) wrapper.unmount()
  // 真实 Select 的下拉 Teleport 到 body，关闭时带离场动画（等下一帧才移除）：清空 body，免得残留节点漏进下一个用例
  document.body.innerHTML = ''
})

function mountCard(
  initial: Partial<CardState> = {},
  cardProps: Record<string, unknown> = {},
  { realSelect = false }: { realSelect?: boolean } = {}
) {
  const state = reactive<CardState>({ ...DEFAULT_STATE, ...initial })
  const props = reactive<Record<string, unknown>>({
    fallbackOptions: OPTIONS,
    fallbackLoading: false,
    referrers: [],
    ...cardProps
  })
  const onSubmit = vi.fn()
  const Host = defineComponent({
    components: { GroupClaudePolicyCard },
    setup: () => ({ state, props, onSubmit }),
    // 创建 / 编辑弹窗里它整体放在 <form> 里：放进 form 里挂载，才能确认点击不会把表单提交掉
    template: `
      <form @submit.prevent="onSubmit">
        <GroupClaudePolicyCard
          v-model:claudeCodeOnly="state.claudeCodeOnly"
          v-model:fallbackGroupId="state.fallbackGroupId"
          v-model:systemPromptPolicy="state.systemPromptPolicy"
          v-model:contentReviewPolicy="state.contentReviewPolicy"
          v-bind="props"
        />
      </form>
    `
  })
  const wrapper = mount(Host, {
    attachTo: document.body,
    global: { stubs: realSelect ? {} : { Select: SelectStub } }
  })
  mountedWrappers.push(wrapper)
  return { wrapper, state, props, onSubmit }
}

type Mounted = ReturnType<typeof mountCard>

const radios = (m: Mounted) => m.wrapper.findAll('[role="radio"]')
const radio = (m: Mounted, id: string) => m.wrapper.get(`[data-testid="hat-option-${id}"]`)
const checkedHats = (m: Mounted) =>
  radios(m)
    .filter((item) => item.attributes('aria-checked') === 'true')
    .map((item) => item.attributes('data-testid')!.replace('hat-option-', ''))
const fallbackSelect = (m: Mounted) => m.wrapper.get('[data-testid="hat-fallback-select"]')
const optionTexts = (select: ReturnType<Mounted['wrapper']['get']>) => select.findAll('option').map((item) => item.text())
const exists = (m: Mounted, testid: string) => m.wrapper.find(`[data-testid="${testid}"]`).exists()
const text = (m: Mounted, testid: string) => m.wrapper.get(`[data-testid="${testid}"]`).text()
const snapshot = (m: Mounted): CardState => ({ ...m.state })
/** 卡片对外发出的 update:* 事件名（排序后） */
const updateEvents = (m: Mounted) =>
  Object.keys(m.wrapper.findComponent(GroupClaudePolicyCard).emitted())
    .filter((name) => name.startsWith('update:'))
    .sort()
const willWrite = (m: Mounted) => {
  const line = text(m, 'hat-will-write')
  const match = line.match(/admin\.groups\.hatCard\.willWriteLine::(\{.*\})/)
  return { line, params: (match ? JSON.parse(match[1]) : {}) as Record<string, string> }
}

describe('挂载：完全派生，绝不写任何字段', () => {
  it.each([
    ['默认', {}],
    ...HAT_STATES.map(([hat, state]): [string, Partial<CardState>] => [hat, state]),
    ['开放·完整 + 残留降级分组 9', { claudeCodeOnly: false, fallbackGroupId: 9, systemPromptPolicy: 'enabled' }],
    ['CC 降级池指向已不在候选里的分组 99', { claudeCodeOnly: true, fallbackGroupId: 99, systemPromptPolicy: 'disabled' }]
  ] as Array<[string, Partial<CardState>]>)('起点「%s」：挂载并刷新后状态不变，没有任何 update:* 事件', async (_name, initial) => {
    const m = mountCard({ ...initial, contentReviewPolicy: 'enabled' })
    await flushPromises()
    expect(snapshot(m)).toEqual({ ...DEFAULT_STATE, ...initial, contentReviewPolicy: 'enabled' })
    expect(updateEvents(m)).toEqual([])
  })
})

describe('单选语义', () => {
  it('恰好 6 个 radio，顺序固定，都在 radiogroup 里', () => {
    const m = mountCard()
    expect(radios(m).map((item) => item.attributes('data-testid'))).toEqual(ALL_HATS.map((id) => `hat-option-${id}`))
    expect(m.wrapper.findAll('[role="radiogroup"] [role="radio"]')).toHaveLength(6)
    expect(m.wrapper.get('[role="radiogroup"]').attributes('aria-label')).toBe('admin.groups.hatCard.hatTitle')
  })

  it.each(HAT_STATES)('起点是 %s：恰好一个 aria-checked=true，就是它自己', (hat, initial) => {
    const m = mountCard(initial)
    expect(checkedHats(m)).toEqual([hat])
    expect(radios(m).filter((item) => item.attributes('aria-checked') === 'false')).toHaveLength(5)
  })

  it('选中的容器用选中配色，其余用未选中配色；radio 按钮里不嵌任何交互元素', () => {
    const m = mountCard(HAT_STATES[3][1])
    for (const id of ALL_HATS) {
      const container = radio(m, id).element.parentElement!
      expect(container.classList.contains('border-primary-500'), id).toBe(id === 'open_full')
      expect(container.classList.contains('border-gray-200'), id).toBe(id !== 'open_full')
      expect(radio(m, id).find('select, button, input, a').exists(), `${id} 的 radio 按钮里嵌了交互元素`).toBe(false)
    }
  })

  it('每顶帽子的容器里有徽章（短名）、名称与一句话用途的词条', () => {
    const m = mountCard()
    for (const id of ALL_HATS) {
      const button = radio(m, id)
      expect(button.text()).toContain(`admin.groups.hats.${id}.short`)
      expect(button.text()).toContain(`admin.groups.hats.${id}.name`)
      expect(button.text()).toContain(`admin.groups.hats.${id}.purpose`)
    }
    expect(radio(m, 'open_full').find('span').classes()).toEqual(
      expect.arrayContaining(['badge', 'badge-purple', 'whitespace-nowrap'])
    )
  })

  it('标题与说明', () => {
    const m = mountCard()
    expect(m.wrapper.get('h4').text()).toBe('admin.groups.anthropicPolicies.title')
    expect(m.wrapper.text()).toContain('admin.groups.hatCard.hatHint')
  })
})

describe('点击不会提交外层表单', () => {
  it('点遍所有 radio（含被禁用的），表单都没有被提交', async () => {
    const m = mountCard({ systemPromptPolicy: 'enabled' })
    for (const item of radios(m)) await item.trigger('click')
    expect(m.onSubmit).not.toHaveBeenCalled()
  })

  it('点「清除」按钮也不会提交', async () => {
    const m = mountCard({ claudeCodeOnly: false, fallbackGroupId: 9, systemPromptPolicy: 'enabled' })
    await m.wrapper.get('[data-testid="hat-stale-fallback-clear"]').trigger('click')
    expect(m.onSubmit).not.toHaveBeenCalled()
  })

  it('卡片里每个 <button> 都是 type="button"（运行时再数一遍）', () => {
    const m = mountCard({ claudeCodeOnly: false, fallbackGroupId: 9, systemPromptPolicy: 'enabled' })
    const buttons = m.wrapper.findAll('button')
    // 6 个 radio + 1 个清除按钮
    expect(buttons).toHaveLength(7)
    for (const button of buttons) expect(button.attributes('type')).toBe('button')
  })
})

describe('选帽子：非降级池', () => {
  // 起点：[名称, 字段, 起点的注入策略（cc_line 保持它）]
  const STARTS: Array<[string, Partial<CardState>, string]> = [
    ['默认', {}, 'inherit'],
    ['CC 专线 + 隐藏策略 disabled', { claudeCodeOnly: true, fallbackGroupId: null, systemPromptPolicy: 'disabled' }, 'disabled'],
    ['开放·完整 + 残留降级分组 9', { claudeCodeOnly: false, fallbackGroupId: 9, systemPromptPolicy: 'enabled' }, 'enabled'],
    ['CC 降级池(5) + 策略 identity_only', { claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'identity_only' }, 'identity_only']
  ]

  describe.each(STARTS)('起点：%s', (_name, initial, startPolicy) => {
    // 目标帽子与写入后的三个字段，全部字面量（cc_line 的策略保持起点的值）
    const TARGETS: Array<[string, boolean, string]> = [
      ['cc_line', true, startPolicy],
      ['open_follow', false, 'inherit'],
      ['open_full', false, 'enabled'],
      ['open_light', false, 'identity_only'],
      ['open_keep_prompt', false, 'disabled']
    ]

    it.each(TARGETS)('点 %s：cc=%s、降级分组 null、策略 %s，内容审计不动', async (hat, cc, policy) => {
      const m = mountCard({ ...initial, contentReviewPolicy: 'enabled' })
      await radio(m, hat).trigger('click')
      expect(snapshot(m)).toEqual({
        claudeCodeOnly: cc,
        fallbackGroupId: null,
        systemPromptPolicy: policy,
        contentReviewPolicy: 'enabled'
      })
      // 严格 null：不是 0，也不是 undefined
      expect(m.state.fallbackGroupId).toBe(null)
      expect(checkedHats(m)).toEqual([hat])
    })
  })

  it('点击当前已选中的帽子：不会写出不同的值（残留的降级分组除外，那是显式选帽子的结果）', async () => {
    const m = mountCard({ claudeCodeOnly: false, fallbackGroupId: null, systemPromptPolicy: 'identity_only' })
    await radio(m, 'open_light').trigger('click')
    expect(snapshot(m)).toEqual({ ...DEFAULT_STATE, systemPromptPolicy: 'identity_only' })
    expect(updateEvents(m)).toEqual([])
  })
})

describe('选帽子：降级池', () => {
  const WITH_NINE = [...OPTIONS, { id: 9, name: '旧分组' }]

  it.each([
    ['开放·完整 + 残留降级分组 9', { claudeCodeOnly: false, fallbackGroupId: 9, systemPromptPolicy: 'enabled' }],
    ['默认', {}],
    ['CC 专线', { claudeCodeOnly: true, fallbackGroupId: null, systemPromptPolicy: 'disabled' }],
    // 库里的怪值（0 不是有效的降级分组 id）：点卡片也不能把它悄悄归一写回
    ['怪值：降级分组 0', { claudeCodeOnly: false, fallbackGroupId: 0, systemPromptPolicy: 'enabled' }]
  ] as Array<[string, Partial<CardState>]>)('起点「%s」：点降级池卡片本身不写任何字段', async (_name, initial) => {
    const m = mountCard(initial, { fallbackOptions: WITH_NINE })
    await radio(m, 'cc_fallback').trigger('click')
    expect(snapshot(m)).toEqual({ ...DEFAULT_STATE, ...initial })
    expect(updateEvents(m)).toEqual([])
  })

  it.each([
    // 起点, 在内嵌下拉里选的 id, 写入后的三个字段（策略保持起点的值）
    ['开放·完整 + 残留降级分组 9', { claudeCodeOnly: false, fallbackGroupId: 9, systemPromptPolicy: 'enabled' }, '5', 5, 'enabled'],
    ['默认', {}, '6', 6, 'inherit'],
    ['CC 专线 + 隐藏策略 disabled', { claudeCodeOnly: true, fallbackGroupId: null, systemPromptPolicy: 'disabled' }, '5', 5, 'disabled'],
    ['CC 降级池(5) + 策略 identity_only，改选 6', { claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'identity_only' }, '6', 6, 'identity_only']
  ] as Array<[string, Partial<CardState>, string, number, string]>)(
    '起点「%s」：下拉里选 %s -> cc=true、降级分组=%s、策略 %s，残留的 9 不被采用',
    async (_name, initial, picked, expectedId, policy) => {
      const m = mountCard(initial, { fallbackOptions: WITH_NINE })
      await fallbackSelect(m).setValue(picked)
      expect(snapshot(m)).toEqual({
        claudeCodeOnly: true,
        fallbackGroupId: expectedId,
        systemPromptPolicy: policy,
        contentReviewPolicy: 'inherit'
      })
      expect(checkedHats(m)).toEqual(['cc_fallback'])
      expect((fallbackSelect(m).element as HTMLSelectElement).value).toBe(String(expectedId))
    }
  )

  it('已是降级池时选「不降级」（空值）：cc 仍为真，降级分组 null，落到 CC 专线', async () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'enabled' })
    await fallbackSelect(m).setValue('')
    expect(snapshot(m)).toEqual({
      claudeCodeOnly: true,
      fallbackGroupId: null,
      systemPromptPolicy: 'enabled',
      contentReviewPolicy: 'inherit'
    })
    expect(m.state.fallbackGroupId).toBe(null)
    expect(checkedHats(m)).toEqual(['cc_line'])
  })

  it('不是降级池时，下拉里没有「不降级」，也不会把残留的降级分组显示成选中', () => {
    const m = mountCard({ claudeCodeOnly: false, fallbackGroupId: 9, systemPromptPolicy: 'enabled' }, { fallbackOptions: WITH_NINE })
    const select = fallbackSelect(m)
    expect(optionTexts(select)).toEqual(['备用池 A', '备用池 B', '旧分组'])
    expect(optionTexts(select)).not.toContain('admin.groups.claudeCode.noFallback')
    // 候选里有 9，但它只是残留，不是选择
    expect((select.element as HTMLSelectElement).value).toBe('')
    expect(select.attributes('data-placeholder')).toBe('admin.groups.claudeCode.fallbackGroup')
  })

  it('是降级池时，下拉第一项是「不降级」，选中的是当前降级分组', () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 6, systemPromptPolicy: 'inherit' })
    const select = fallbackSelect(m)
    expect(optionTexts(select)).toEqual(['admin.groups.claudeCode.noFallback', '备用池 A', '备用池 B'])
    expect((select.element as HTMLSelectElement).value).toBe('6')
  })

  it('降级分组的下拉与降级池的 radio 在同一个容器里，但不在 radio 按钮里', () => {
    const m = mountCard()
    const container = radio(m, 'cc_fallback').element.parentElement!
    expect(container.contains(fallbackSelect(m).element)).toBe(true)
    expect(radio(m, 'cc_fallback').element.contains(fallbackSelect(m).element)).toBe(false)
    // 其它帽子的容器里没有降级下拉
    expect(m.wrapper.findAll('[data-testid="hat-fallback-select"]')).toHaveLength(1)
  })

  it('下拉传来 0 / 负数 / NaN：不写 0，已是降级池就落到 CC 专线（降级分组 null）', async () => {
    for (const bad of [0, -1, Number.NaN]) {
      const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'enabled' })
      m.wrapper.findComponent(SelectStub).vm.$emit('update:modelValue', bad)
      await nextTick()
      expect(snapshot(m), `传入 ${String(bad)}`).toEqual({
        claudeCodeOnly: true,
        fallbackGroupId: null,
        systemPromptPolicy: 'enabled',
        contentReviewPolicy: 'inherit'
      })
      expect(m.state.fallbackGroupId).toBe(null)
    }
  })

  it('下拉传来 0 / null：不是降级池时什么都不写', async () => {
    for (const bad of [0, null]) {
      const m = mountCard({ claudeCodeOnly: false, fallbackGroupId: 9, systemPromptPolicy: 'enabled' }, { fallbackOptions: WITH_NINE })
      m.wrapper.findComponent(SelectStub).vm.$emit('update:modelValue', bad)
      await nextTick()
      expect(snapshot(m), `传入 ${String(bad)}`).toEqual({
        claudeCodeOnly: false,
        fallbackGroupId: 9,
        systemPromptPolicy: 'enabled',
        contentReviewPolicy: 'inherit'
      })
      expect(updateEvents(m)).toEqual([])
    }
  })
})

describe('与真实 Select 组件联动（不用桩）', () => {
  // 桩只是原生 <select>：这里用真实组件确认语义没有漂移（null 选项的显示、选 null 选项发出 null、disabled 不能打开）
  const dropdownOptions = () => [...document.body.querySelectorAll<HTMLElement>('.select-dropdown-portal .select-option')]
  const clickOption = async (label: string) => {
    const target = dropdownOptions().find((item) => item.textContent?.trim() === label)
    expect(target, `下拉里没有「${label}」`).toBeDefined()
    target!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
  }
  const trigger = (m: Mounted) => m.wrapper.get('button.select-trigger')

  it('不是降级池时触发器显示占位符；点开列出候选，选一个 -> cc=true、降级分组=id，触发器显示该候选的名字', async () => {
    const m = mountCard({ systemPromptPolicy: 'enabled' }, {}, { realSelect: true })
    expect(trigger(m).text()).toBe('admin.groups.claudeCode.fallbackGroup')

    await trigger(m).trigger('click')
    // 不是降级池：没有「不降级」
    expect(dropdownOptions().map((item) => item.textContent?.trim())).toEqual(['备用池 A', '备用池 B'])
    await clickOption('备用池 B')

    expect(snapshot(m)).toEqual({
      claudeCodeOnly: true,
      fallbackGroupId: 6,
      systemPromptPolicy: 'enabled',
      contentReviewPolicy: 'inherit'
    })
    expect(checkedHats(m)).toEqual(['cc_fallback'])
    expect(trigger(m).text()).toBe('备用池 B')
  })

  it('已是降级池时第一项是「不降级」，当前降级分组被标为选中；选「不降级」 -> 降级分组 null，落到 CC 专线', async () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'inherit' }, {}, { realSelect: true })
    expect(trigger(m).text()).toBe('备用池 A')

    await trigger(m).trigger('click')
    const items = dropdownOptions()
    expect(items.map((item) => item.textContent?.trim())).toEqual(['admin.groups.claudeCode.noFallback', '备用池 A', '备用池 B'])
    expect(items.map((item) => item.getAttribute('aria-selected'))).toEqual(['false', 'true', 'false'])
    await clickOption('admin.groups.claudeCode.noFallback')

    expect(snapshot(m)).toEqual({
      claudeCodeOnly: true,
      fallbackGroupId: null,
      systemPromptPolicy: 'inherit',
      contentReviewPolicy: 'inherit'
    })
    expect(m.state.fallbackGroupId).toBe(null)
    expect(checkedHats(m)).toEqual(['cc_line'])
  })

  it('没有候选且当前不是降级池：触发器被禁用，点击也打不开下拉', async () => {
    const m = mountCard({}, { fallbackOptions: [] }, { realSelect: true })
    expect(trigger(m).attributes('disabled')).toBeDefined()
    await trigger(m).trigger('click')
    expect(dropdownOptions()).toEqual([])
  })
})

describe('降级候选：无候选与加载中', () => {
  const NOT_FALLBACK = { claudeCodeOnly: false, fallbackGroupId: null, systemPromptPolicy: 'inherit' }

  it('没有候选且当前不是降级池：radio 与下拉都禁用，提示没有可用的降级分组', () => {
    const m = mountCard(NOT_FALLBACK, { fallbackOptions: [] })
    expect(radio(m, 'cc_fallback').attributes('disabled')).toBeDefined()
    expect(fallbackSelect(m).attributes('disabled')).toBeDefined()
    expect(text(m, 'hat-fallback-empty')).toBe('admin.groups.hatCard.fallbackNoCandidates')
    expect(exists(m, 'hat-fallback-loading')).toBe(false)
    expect(exists(m, 'hat-fallback-pick-hint')).toBe(false)
  })

  it('候选加载中：radio 与下拉都禁用，显示加载中，不显示「没有可用」', () => {
    for (const fallbackOptions of [OPTIONS, []]) {
      const m = mountCard(NOT_FALLBACK, { fallbackOptions, fallbackLoading: true })
      expect(radio(m, 'cc_fallback').attributes('disabled')).toBeDefined()
      expect(fallbackSelect(m).attributes('disabled')).toBeDefined()
      expect(text(m, 'hat-fallback-loading')).toBe('common.loading')
      expect(exists(m, 'hat-fallback-empty')).toBe(false)
      expect(exists(m, 'hat-fallback-pick-hint')).toBe(false)
    }
  })

  it('有候选且当前不是降级池：不禁用，下拉下面提示选择后才会生效', () => {
    const m = mountCard(NOT_FALLBACK)
    expect(radio(m, 'cc_fallback').attributes('disabled')).toBeUndefined()
    expect(fallbackSelect(m).attributes('disabled')).toBeUndefined()
    expect(text(m, 'hat-fallback-pick-hint')).toBe('admin.groups.hatCard.fallbackPickHint')
    expect(exists(m, 'hat-fallback-empty')).toBe(false)
    expect(exists(m, 'hat-fallback-loading')).toBe(false)
  })

  it('当前已是降级池时不因没有候选而禁用，也没有「没有可用」与「选择后才会生效」的提示', () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'inherit' }, { fallbackOptions: [] })
    expect(radio(m, 'cc_fallback').attributes('disabled')).toBeUndefined()
    expect(fallbackSelect(m).attributes('disabled')).toBeUndefined()
    expect(exists(m, 'hat-fallback-empty')).toBe(false)
    expect(exists(m, 'hat-fallback-pick-hint')).toBe(false)
  })

  it('已是降级池但候选还在加载：禁用，且还不能判断是否悬空（没有悬空提示与合成选项）', () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 99, systemPromptPolicy: 'inherit' }, { fallbackOptions: [], fallbackLoading: true })
    expect(radio(m, 'cc_fallback').attributes('disabled')).toBeDefined()
    expect(fallbackSelect(m).attributes('disabled')).toBeDefined()
    expect(exists(m, 'hat-dangling-fallback')).toBe(false)
    expect(optionTexts(fallbackSelect(m))).toEqual(['admin.groups.claudeCode.noFallback'])
  })

  it('加载结束后（props 变化）立刻恢复可用', async () => {
    const m = mountCard(NOT_FALLBACK, { fallbackLoading: true })
    expect(fallbackSelect(m).attributes('disabled')).toBeDefined()
    m.props.fallbackLoading = false
    await nextTick()
    expect(fallbackSelect(m).attributes('disabled')).toBeUndefined()
    expect(radio(m, 'cc_fallback').attributes('disabled')).toBeUndefined()
    expect(exists(m, 'hat-fallback-loading')).toBe(false)
  })
})

describe('降级流量的提示词策略（子项）', () => {
  it.each(HAT_STATES)('起点是 %s：策略下拉只在降级池选中时出现', (hat, initial) => {
    const m = mountCard(initial)
    expect(exists(m, 'hat-fallback-policy-select')).toBe(hat === 'cc_fallback')
  })

  it('降级池选中时：4 个选项，值与文字（词条 key）按固定顺序，并带说明', () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'enabled' })
    const select = m.wrapper.get('[data-testid="hat-fallback-policy-select"]')
    expect(select.findAll('option').map((item) => item.element.value)).toEqual(['inherit', 'enabled', 'identity_only', 'disabled'])
    expect(optionTexts(select)).toEqual([
      'admin.groups.anthropicPolicies.inherit',
      'admin.groups.anthropicPolicies.enabled',
      'admin.groups.anthropicPolicies.identityOnly',
      'admin.groups.anthropicPolicies.disabled'
    ])
    expect((select.element as HTMLSelectElement).value).toBe('enabled')
    expect(m.wrapper.text()).toContain('admin.groups.hatCard.fallbackPolicyLabel')
    expect(m.wrapper.text()).toContain('admin.groups.hatCard.fallbackPolicyHint')
  })

  it('改它只改 systemPromptPolicy：帽子、降级分组、内容审计都不动', async () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'inherit', contentReviewPolicy: 'disabled' })
    await m.wrapper.get('[data-testid="hat-fallback-policy-select"]').setValue('identity_only')
    expect(snapshot(m)).toEqual({
      claudeCodeOnly: true,
      fallbackGroupId: 5,
      systemPromptPolicy: 'identity_only',
      contentReviewPolicy: 'disabled'
    })
    expect(updateEvents(m)).toEqual(['update:systemPromptPolicy'])
    expect(checkedHats(m)).toEqual(['cc_fallback'])
  })
})

describe('残留的降级分组（只提示，不自动改写）', () => {
  const STALE: Partial<CardState> = { claudeCodeOnly: false, fallbackGroupId: 9, systemPromptPolicy: 'enabled' }

  it('cc 为假但还留着降级分组：出现提示，名字来自 resolveGroupName，帽子仍是开放帽', () => {
    const m = mountCard(STALE, { resolveGroupName: (id: number) => (id === 9 ? '旧降级组' : undefined) })
    expect(text(m, 'hat-stale-fallback')).toContain('admin.groups.hatCard.staleFallbackNotice::{"name":"旧降级组"}')
    expect(checkedHats(m)).toEqual(['open_full'])
  })

  it('查不到名字（或没传 resolveGroupName）时显示 #id', () => {
    expect(text(mountCard(STALE), 'hat-stale-fallback')).toContain('staleFallbackNotice::{"name":"#9"}')
    expect(text(mountCard(STALE, { resolveGroupName: () => undefined }), 'hat-stale-fallback')).toContain(
      'staleFallbackNotice::{"name":"#9"}'
    )
  })

  it('点「清除」：降级分组变 null，其它字段不变，提示消失，只发出 update:fallbackGroupId', async () => {
    const m = mountCard({ ...STALE, contentReviewPolicy: 'disabled' })
    await m.wrapper.get('[data-testid="hat-stale-fallback-clear"]').trigger('click')
    expect(snapshot(m)).toEqual({
      claudeCodeOnly: false,
      fallbackGroupId: null,
      systemPromptPolicy: 'enabled',
      contentReviewPolicy: 'disabled'
    })
    expect(m.state.fallbackGroupId).toBe(null)
    expect(exists(m, 'hat-stale-fallback')).toBe(false)
    expect(updateEvents(m)).toEqual(['update:fallbackGroupId'])
    expect(checkedHats(m)).toEqual(['open_full'])
  })

  it.each([
    ['cc 为真且有降级分组（降级池）', { claudeCodeOnly: true, fallbackGroupId: 9, systemPromptPolicy: 'enabled' }],
    ['cc 为真且没有降级分组', { claudeCodeOnly: true, fallbackGroupId: null, systemPromptPolicy: 'enabled' }],
    ['cc 为假且没有降级分组', { claudeCodeOnly: false, fallbackGroupId: null, systemPromptPolicy: 'enabled' }]
  ] as Array<[string, Partial<CardState>]>)('%s：没有残留提示', (_name, initial) => {
    expect(exists(mountCard(initial), 'hat-stale-fallback')).toBe(false)
  })

  it.each(['inherit', 'identity_only', 'disabled'])('开放帽（策略 %s）带残留降级分组时同样提示', (policy) => {
    expect(exists(mountCard({ claudeCodeOnly: false, fallbackGroupId: 9, systemPromptPolicy: policy }), 'hat-stale-fallback')).toBe(true)
  })

  it('选任意开放帽会把残留的降级分组写成 null（这是显式选帽子的结果，提示随之消失）', async () => {
    const m = mountCard(STALE)
    await radio(m, 'open_light').trigger('click')
    expect(m.state.fallbackGroupId).toBe(null)
    expect(exists(m, 'hat-stale-fallback')).toBe(false)
  })
})

describe('悬空的降级分组（已停用或已删除）', () => {
  const DANGLING: Partial<CardState> = { claudeCodeOnly: true, fallbackGroupId: 99, systemPromptPolicy: 'inherit' }

  it('降级池指向不在候选里的分组：下拉末尾合成一项（带 id），并出现红字提示', () => {
    const m = mountCard(DANGLING)
    expect(optionTexts(fallbackSelect(m))).toEqual([
      'admin.groups.claudeCode.noFallback',
      '备用池 A',
      '备用池 B',
      'admin.groups.hatCard.danglingFallbackLabel::{"id":99}'
    ])
    expect((fallbackSelect(m).element as HTMLSelectElement).value).toBe('99')
    expect(text(m, 'hat-dangling-fallback')).toBe('admin.groups.hatCard.danglingFallbackNotice')
    expect(checkedHats(m)).toEqual(['cc_fallback'])
  })

  it('选「不降级」：降级分组变 null，合成项与提示一起消失', async () => {
    const m = mountCard(DANGLING)
    await fallbackSelect(m).setValue('')
    expect(m.state.fallbackGroupId).toBe(null)
    expect(snapshot(m).claudeCodeOnly).toBe(true)
    expect(exists(m, 'hat-dangling-fallback')).toBe(false)
    expect(optionTexts(fallbackSelect(m))).toEqual(['备用池 A', '备用池 B'])
  })

  it('改选一个候选：降级分组换成它', async () => {
    const m = mountCard(DANGLING)
    await fallbackSelect(m).setValue('6')
    expect(snapshot(m)).toEqual({ claudeCodeOnly: true, fallbackGroupId: 6, systemPromptPolicy: 'inherit', contentReviewPolicy: 'inherit' })
    expect(exists(m, 'hat-dangling-fallback')).toBe(false)
  })

  it('降级分组在候选里：不合成选项，也没有悬空提示', () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'inherit' })
    expect(optionTexts(fallbackSelect(m))).toEqual(['admin.groups.claudeCode.noFallback', '备用池 A', '备用池 B'])
    expect(exists(m, 'hat-dangling-fallback')).toBe(false)
  })

  it('不是降级池（开放帽带残留 99）：不合成选项，也不报悬空（那是残留提示的事）', () => {
    const m = mountCard({ claudeCodeOnly: false, fallbackGroupId: 99, systemPromptPolicy: 'inherit' })
    expect(optionTexts(fallbackSelect(m))).toEqual(['备用池 A', '备用池 B'])
    expect(exists(m, 'hat-dangling-fallback')).toBe(false)
    expect(exists(m, 'hat-stale-fallback')).toBe(true)
  })
})

describe('被其它分组当作降级目标（referrers）', () => {
  const REFERRERS = [
    { id: 1, name: '分组甲' },
    { id: 2, name: '分组乙' }
  ]

  it.each(HAT_STATES)('起点是 %s：只有 CC 类帽子才出现提示，名字用 ", " 连接', (hat, initial) => {
    const m = mountCard(initial, { referrers: REFERRERS })
    const isCc = hat === 'cc_line' || hat === 'cc_fallback'
    expect(exists(m, 'hat-referrers')).toBe(isCc)
    if (isCc) expect(text(m, 'hat-referrers')).toBe('admin.groups.hatCard.referrersNotice::{"names":"分组甲, 分组乙"}')
  })

  it('referrers 为空（默认）时不出现', () => {
    expect(exists(mountCard(HAT_STATES[0][1]), 'hat-referrers')).toBe(false)
  })

  it('从 CC 帽切到开放帽后提示消失', async () => {
    const m = mountCard(HAT_STATES[0][1], { referrers: REFERRERS })
    expect(exists(m, 'hat-referrers')).toBe(true)
    await radio(m, 'open_follow').trigger('click')
    expect(exists(m, 'hat-referrers')).toBe(false)
  })
})

describe('「将写入」一行', () => {
  const NONE = 'admin.groups.hatCard.valueNone'

  it.each([
    ['默认', {}, undefined, { cc: 'false', fallback: NONE, policy: 'inherit' }],
    ['CC 专线 + 隐藏策略 enabled', { claudeCodeOnly: true, fallbackGroupId: null, systemPromptPolicy: 'enabled' }, undefined, { cc: 'true', fallback: NONE, policy: 'enabled' }],
    ['降级池(5)，名字来自候选', { claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'inherit' }, undefined, { cc: 'true', fallback: '备用池 A', policy: 'inherit' }],
    ['降级池(7)，名字来自 resolveGroupName', { claudeCodeOnly: true, fallbackGroupId: 7, systemPromptPolicy: 'disabled' }, (id: number) => (id === 7 ? '外部分组' : undefined), { cc: 'true', fallback: '外部分组', policy: 'disabled' }],
    ['降级池(99)，哪里都查不到', { claudeCodeOnly: true, fallbackGroupId: 99, systemPromptPolicy: 'identity_only' }, undefined, { cc: 'true', fallback: '#99', policy: 'identity_only' }],
    ['开放帽带残留降级分组(9)：保存时会一起写入，所以也显示', { claudeCodeOnly: false, fallbackGroupId: 9, systemPromptPolicy: 'identity_only' }, (id: number) => (id === 9 ? '旧分组' : undefined), { cc: 'false', fallback: '旧分组', policy: 'identity_only' }]
  ] as Array<[string, Partial<CardState>, ((id: number) => string | undefined) | undefined, Record<string, string>]>)(
    '%s',
    (_name, initial, resolveGroupName, expected) => {
      const m = mountCard(initial, resolveGroupName ? { resolveGroupName } : {})
      expect(willWrite(m).params).toEqual(expected)
      // 粗体的「将写入」在前，后面接这一行
      expect(willWrite(m).line.startsWith('admin.groups.hatCard.willWrite')).toBe(true)
      expect(m.wrapper.get('[data-testid="hat-will-write"]').classes()).toEqual(expect.arrayContaining(['font-mono', 'text-xs']))
    }
  )

  it('候选与 resolveGroupName 都能查到时，以候选里的名字为准', () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'inherit' }, { resolveGroupName: () => '不该用这个' })
    expect(willWrite(m).params.fallback).toBe('备用池 A')
  })

  it('随选帽子即时变化：点开放·轻 -> cc=false、无降级分组、策略 identity_only', async () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'enabled' })
    expect(willWrite(m).params).toEqual({ cc: 'true', fallback: '备用池 A', policy: 'enabled' })
    await radio(m, 'open_light').trigger('click')
    expect(willWrite(m).params).toEqual({ cc: 'false', fallback: NONE, policy: 'identity_only' })
  })
})

describe('外部重置：卡片没有本地状态', () => {
  it('Host 把字段改回默认：radio、将写入、下拉、子项立刻跟着变', async () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'enabled' })
    expect(checkedHats(m)).toEqual(['cc_fallback'])
    expect(exists(m, 'hat-fallback-policy-select')).toBe(true)

    m.state.claudeCodeOnly = false
    m.state.fallbackGroupId = null
    m.state.systemPromptPolicy = 'inherit'
    await nextTick()

    expect(checkedHats(m)).toEqual(['open_follow'])
    expect(willWrite(m).params).toEqual({ cc: 'false', fallback: 'admin.groups.hatCard.valueNone', policy: 'inherit' })
    expect(exists(m, 'hat-fallback-policy-select')).toBe(false)
    expect((fallbackSelect(m).element as HTMLSelectElement).value).toBe('')
    expect(text(m, 'hat-detail')).toBe('admin.groups.hats.open_follow.detail')
  })

  it('再次外部写入降级池：卡片立刻回到降级池，下拉选中新的降级分组', async () => {
    const m = mountCard()
    m.state.claudeCodeOnly = true
    m.state.fallbackGroupId = 6
    m.state.systemPromptPolicy = 'disabled'
    await nextTick()
    expect(checkedHats(m)).toEqual(['cc_fallback'])
    expect((fallbackSelect(m).element as HTMLSelectElement).value).toBe('6')
    expect((m.wrapper.get('[data-testid="hat-fallback-policy-select"]').element as HTMLSelectElement).value).toBe('disabled')
  })

  it('只改 cc（平台切换式的重置）：降级池瞬间变成「开放帽 + 残留降级分组」并出现残留提示', async () => {
    const m = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5, systemPromptPolicy: 'inherit' })
    m.state.claudeCodeOnly = false
    await nextTick()
    expect(checkedHats(m)).toEqual(['open_follow'])
    expect(exists(m, 'hat-stale-fallback')).toBe(true)
  })
})

describe('风控中心内容审计（与帽子无关）', () => {
  it('恰好 3 个选项：继承 / 开启 / 关闭，没有「仅必要身份」', () => {
    const select = mountCard().wrapper.get('[data-testid="content-review-select"]')
    expect(select.findAll('option').map((item) => item.element.value)).toEqual(['inherit', 'enabled', 'disabled'])
    expect(optionTexts(select)).toEqual([
      'admin.groups.anthropicPolicies.inherit',
      'admin.groups.anthropicPolicies.enabled',
      'admin.groups.anthropicPolicies.disabled'
    ])
  })

  it.each(HAT_STATES)('起点是 %s：内容审计下拉都在，并显示「与帽子无关」', (_hat, initial) => {
    const m = mountCard({ ...initial, contentReviewPolicy: 'enabled' })
    expect((m.wrapper.get('[data-testid="content-review-select"]').element as HTMLSelectElement).value).toBe('enabled')
    expect(m.wrapper.text()).toContain('admin.groups.anthropicPolicies.contentReview')
    expect(m.wrapper.text()).toContain('admin.groups.hatCard.contentReviewNote')
    expect(m.wrapper.text()).toContain('admin.groups.anthropicPolicies.contentReviewHint')
  })

  it('改它只改 contentReviewPolicy，不影响帽子', async () => {
    const m = mountCard({ claudeCodeOnly: false, fallbackGroupId: null, systemPromptPolicy: 'enabled' })
    await m.wrapper.get('[data-testid="content-review-select"]').setValue('disabled')
    expect(snapshot(m)).toEqual({
      claudeCodeOnly: false,
      fallbackGroupId: null,
      systemPromptPolicy: 'enabled',
      contentReviewPolicy: 'disabled'
    })
    expect(updateEvents(m)).toEqual(['update:contentReviewPolicy'])
    expect(checkedHats(m)).toEqual(['open_full'])
  })
})

describe('选中帽子的展开内容', () => {
  it.each(HAT_STATES)('起点是 %s：只展开它自己的 detail；暴露面提示只有开放类才有', (hat, initial) => {
    const m = mountCard(initial)
    expect(m.wrapper.findAll('[data-testid="hat-detail"]')).toHaveLength(1)
    expect(text(m, 'hat-detail')).toBe(`admin.groups.hats.${hat}.detail`)
    for (const other of ALL_HATS.filter((id) => id !== hat)) {
      expect(m.wrapper.text()).not.toContain(`admin.groups.hats.${other}.detail`)
    }
    expect(exists(m, 'hat-exposure')).toBe(hat.startsWith('open_'))
    if (hat.startsWith('open_')) expect(text(m, 'hat-exposure')).toBe('admin.groups.hatCard.exposureNotice')
  })

  it('点另一顶帽子后，展开内容跟着换', async () => {
    const m = mountCard()
    await radio(m, 'open_keep_prompt').trigger('click')
    expect(text(m, 'hat-detail')).toBe('admin.groups.hats.open_keep_prompt.detail')
    await radio(m, 'cc_line').trigger('click')
    expect(text(m, 'hat-detail')).toBe('admin.groups.hats.cc_line.detail')
    expect(exists(m, 'hat-exposure')).toBe(false)
  })
})

describe('idPrefix', () => {
  it('默认前缀 claude-policy；传了前缀就换掉，label 的 for 与 select 的 id 一一对应', () => {
    const byDefault = mountCard()
    expect(byDefault.wrapper.get('[data-testid="content-review-select"]').attributes('id')).toBe('claude-policy-content-review')

    const prefixed = mountCard({ claudeCodeOnly: true, fallbackGroupId: 5 }, { idPrefix: 'edit-group' })
    const review = prefixed.wrapper.get('[data-testid="content-review-select"]')
    expect(review.attributes('id')).toBe('edit-group-content-review')
    expect(prefixed.wrapper.find('label[for="edit-group-content-review"]').exists()).toBe(true)
    const policy = prefixed.wrapper.get('[data-testid="hat-fallback-policy-select"]')
    expect(policy.attributes('id')).toBe('edit-group-fallback-policy')
    expect(prefixed.wrapper.find('label[for="edit-group-fallback-policy"]').exists()).toBe(true)
  })
})

describe('源码守卫', () => {
  // 只检查代码，不检查注释（文件头注释里会写「没有 watch / validate」这类说明）
  const source = readFileSync(join(process.cwd(), 'src/components/admin/group/GroupClaudePolicyCard.vue'), 'utf8')
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(^|\s)\/\/.*$/gm, '$1')

  it('去掉注释之后还有内容（防止下面的断言空转通过）', () => {
    expect(source).toContain('defineModel')
    expect(source).toContain('role="radio"')
  })

  it('卡里没有第二份「策略 -> 词条」映射：4 个策略词条只能经 POLICY_LABEL_KEYS 引用', () => {
    expect(source).not.toMatch(/anthropicPolicies\.(inherit|enabled|identityOnly|disabled)/)
    expect(source).toContain('POLICY_LABEL_KEYS')
  })

  it('每个 <button> 都带 type="button"（它会被放进 <form>）', () => {
    const buttons = source.match(/<button\b[^>]*>/g) ?? []
    expect(buttons.length).toBeGreaterThan(0)
    for (const tag of buttons) expect(tag, tag).toMatch(/\btype="button"/)
  })

  it('没有 sticky top-0（弹窗里的 sticky 会盖住内容）', () => {
    expect(source).not.toContain('sticky top-0')
  })

  it('没有本地状态的痕迹：watch / onMounted / defineExpose / validate 都不能出现', () => {
    expect(source).not.toMatch(/\bwatch(Effect)?\s*\(/)
    expect(source).not.toMatch(/\bonMounted\b|\bonBeforeMount\b|\bdefineExpose\b|\bvalidate\b/)
  })

  it('不直接给降级分组赋 0', () => {
    expect(source).not.toMatch(/fallbackGroupId\.value\s*=\s*0\b/)
    expect(source).not.toMatch(/\?\?\s*0\b/)
  })
})
