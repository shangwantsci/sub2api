import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import type { AdminGroup } from '@/types'
import GroupsView from '../GroupsView.vue'

/**
 * GroupsView 挂载级集成测试：真实的 GroupClaudePolicyCard 接在创建 / 编辑弹窗里，
 * 每条用例都断言 adminAPI 收到的 payload，而不是只看界面。卡片自己的行为由 GroupClaudePolicyCard.spec.ts 负责，
 * 这里只证明「接线」：四个 v-model 都接对了字段、候选来自 getAll('anthropic') 并正确过滤、
 * 平台切走时重置、编辑时的残留与 referrers、加载与失败回落。
 *
 * 挂到 document.body：jsdom 里没挂载的 <form> 不会真的提交，「点提交按钮」就成了空转。
 */

const mocks = vi.hoisted(() => ({
  listGroups: vi.fn(),
  getAllGroups: vi.fn(),
  getModelsListCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  createGroup: vi.fn(),
  updateGroup: vi.fn(),
  listAccounts: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: mocks.listGroups,
      getAll: mocks.getAllGroups,
      getModelsListCandidates: mocks.getModelsListCandidates,
      getUsageSummary: mocks.getUsageSummary,
      getCapacitySummary: mocks.getCapacitySummary,
      create: mocks.createGroup,
      update: mocks.updateGroup,
      delete: vi.fn(),
      updateSortOrder: vi.fn()
    },
    accounts: { list: mocks.listAccounts }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess })
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({ isCurrentStep: mocks.isCurrentStep, nextStep: mocks.nextStep })
}))

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

function makeGroup(overrides: Partial<AdminGroup> = {}): AdminGroup {
  return {
    id: 1,
    name: 'Core Anthropic',
    description: null,
    platform: 'anthropic',
    rate_multiplier: 1,
    rpm_limit: 0,
    is_exclusive: false,
    status: 'active',
    subscription_type: 'standard',
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    allow_image_generation: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
    claude_code_only: false,
    fallback_group_id: null,
    fallback_group_id_on_invalid_request: null,
    allow_messages_dispatch: false,
    default_mapped_model: '',
    messages_dispatch_model_config: undefined,
    require_oauth_only: false,
    require_privacy_set: false,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
    model_routing: null,
    model_routing_enabled: false,
    content_review_policy: 'inherit',
    claude_oauth_system_prompt_policy: 'inherit',
    mcp_xml_inject: true,
    supported_model_scopes: [],
    account_count: 0,
    active_account_count: 0,
    rate_limited_account_count: 0,
    models_list_config: undefined,
    sort_order: 10,
    ...overrides
  } as AdminGroup
}

const listPage = (items: AdminGroup[]) => ({ items, total: items.length, page: 1, page_size: 20, pages: 1 })

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

// ---- 桩：DataTable 渲染每行的「操作」列（点「编辑」要用）；Select 是原生 <select>，data-* 等 attrs 落在根上 ----
const DataTableStub = defineComponent({
  props: ['columns', 'data'],
  emits: ['sort'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id" :data-test="'row-' + row.id">
        <slot name="cell-actions" :row="row" :value="undefined" :expanded="true" />
      </div>
    </div>
  `
})

const SelectStub = defineComponent({
  props: ['modelValue', 'options', 'placeholder', 'disabled'],
  emits: ['update:modelValue', 'change'],
  setup(_props, { emit }) {
    return {
      onChange(event: Event) {
        const raw = (event.target as HTMLSelectElement).value
        const value = raw === '' ? null : /^\d+$/.test(raw) ? Number(raw) : raw
        emit('update:modelValue', value)
        emit('change', value)
      }
    }
  },
  template: `
    <select :value="modelValue ?? ''" :disabled="disabled" @change="onChange">
      <option v-for="option in options" :key="String(option.value)" :value="option.value ?? ''">{{ option.label }}</option>
    </select>
  `
})

const stubs = {
  AppLayout: { template: '<div><slot /></div>' },
  TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
  DataTable: DataTableStub,
  Pagination: true,
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  ConfirmDialog: true,
  EmptyState: true,
  Select: SelectStub,
  PlatformIcon: true,
  Icon: { props: ['name'], template: '<span data-test="icon">{{ name }}</span>' },
  GroupCapacityBadge: true,
  GroupHatBadge: true,
  GroupRateMultipliersModal: true,
  GroupRPMOverridesModal: true,
  VueDraggable: { template: '<div><slot /></div>' }
}

const mountedWrappers: Array<{ unmount: () => void }> = []

async function mountView() {
  const wrapper = mount(GroupsView, { attachTo: document.body, global: { stubs } })
  mountedWrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

type Mounted = Awaited<ReturnType<typeof mountView>>

const CARD = '[data-testid="group-claude-policy-card"]'
const cardExists = (w: Mounted) => w.find(CARD).exists()
const testId = (w: Mounted, id: string) => w.get(`[data-testid="${id}"]`)
const exists = (w: Mounted, id: string) => w.find(`[data-testid="${id}"]`).exists()
const radio = (w: Mounted, hat: string) => testId(w, `hat-option-${hat}`)
const checkedHats = (w: Mounted) =>
  w
    .findAll('[role="radio"]')
    .filter((item) => item.attributes('aria-checked') === 'true')
    .map((item) => item.attributes('data-testid')!.replace('hat-option-', ''))
const fallbackSelect = (w: Mounted) => testId(w, 'hat-fallback-select')
const selectValue = (el: { element: Element }) => (el.element as HTMLSelectElement).value
const optionTexts = (el: { findAll: (selector: string) => Array<{ text: () => string }> }) =>
  el.findAll('option').map((item) => item.text())

async function openCreate(w: Mounted) {
  await w.get('[data-tour="groups-create-btn"]').trigger('click')
  await flushPromises()
}

async function cancelDialog(w: Mounted) {
  const cancel = w.findAll('button').find((item) => item.text() === 'common.cancel')
  expect(cancel, '找不到取消按钮').toBeDefined()
  await cancel!.trigger('click')
  await flushPromises()
}

async function openEdit(w: Mounted, id: number) {
  const edit = w
    .get(`[data-test="row-${id}"]`)
    .findAll('button')
    .find((item) => item.text().includes('common.edit'))
  expect(edit, `找不到分组 ${id} 的编辑按钮`).toBeDefined()
  await edit!.trigger('click')
  await flushPromises()
}

async function submit(w: Mounted) {
  await w.get('button[data-tour="group-form-submit"]').trigger('click')
  await flushPromises()
}

async function fillName(w: Mounted, name = '集成测试分组') {
  await w.get('[data-tour="group-form-name"]').setValue(name)
}

/** getAll 被以 'anthropic' 调用的次数（排序弹窗会不带参数调用 getAll，不算）。 */
const anthropicLoads = () => mocks.getAllGroups.mock.calls.filter(([platform]) => platform === 'anthropic')

const createdPayload = () => mocks.createGroup.mock.calls[0][0] as Record<string, unknown>
const updatedCall = () => mocks.updateGroup.mock.calls[0] as [number, Record<string, unknown>]

// Claude 分组的 getAll 返回：混着 CC-only、已停用与非 anthropic 的分组，卡片的候选必须只留 active、非 CC-only 的 anthropic 分组
const MIXED_ALL = [
  makeGroup({ id: 10, name: 'Claude 主池' }),
  makeGroup({ id: 11, name: 'Claude 备用池' }),
  makeGroup({ id: 12, name: 'CC 专线组', claude_code_only: true }),
  makeGroup({ id: 13, name: '停用的 Claude 组', status: 'inactive' }),
  makeGroup({ id: 14, name: 'OpenAI 组', platform: 'openai' })
]

describe('GroupsView 接线：Claude 策略卡', () => {
  beforeEach(() => {
    localStorage.clear()
    for (const mock of Object.values(mocks)) mock.mockReset()
    mocks.listGroups.mockResolvedValue(listPage([makeGroup()]))
    mocks.getAllGroups.mockResolvedValue([])
    mocks.getModelsListCandidates.mockResolvedValue([])
    mocks.getUsageSummary.mockResolvedValue([])
    mocks.getCapacitySummary.mockResolvedValue([])
    mocks.listAccounts.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    mocks.createGroup.mockResolvedValue({})
    mocks.updateGroup.mockResolvedValue({})
    mocks.isCurrentStep.mockReturnValue(false)
  })

  afterEach(() => {
    for (const wrapper of mountedWrappers.splice(0)) wrapper.unmount()
    document.body.innerHTML = ''
    localStorage.clear()
    vi.restoreAllMocks()
  })

  describe('创建', () => {
    it('(1) 默认打开选中「开放·跟随全局」；点开放·轻后提交：cc=false、降级分组严格为 null、策略 identity_only', async () => {
      const w = await mountView()
      await openCreate(w)
      expect(checkedHats(w)).toEqual(['open_follow'])

      await radio(w, 'open_light').trigger('click')
      expect(checkedHats(w)).toEqual(['open_light'])
      await fillName(w)
      await submit(w)

      expect(mocks.showError).not.toHaveBeenCalled()
      expect(mocks.createGroup).toHaveBeenCalledTimes(1)
      const payload = createdPayload()
      expect(payload.claude_code_only).toBe(false)
      // 严格 null：创建接口对 0 会校验失败
      expect(payload.fallback_group_id).toBe(null)
      expect(payload.claude_oauth_system_prompt_policy).toBe('identity_only')
      expect(payload.content_review_policy).toBe('inherit')
      expect(payload.platform).toBe('anthropic')
    })

    it('(1b) 内容审计下拉写进创建请求，且不影响帽子相关的三个字段', async () => {
      const w = await mountView()
      await openCreate(w)
      await testId(w, 'content-review-select').setValue('enabled')
      await fillName(w)
      await submit(w)

      const payload = createdPayload()
      expect(payload.content_review_policy).toBe('enabled')
      expect(payload.claude_code_only).toBe(false)
      expect(payload.fallback_group_id).toBe(null)
      expect(payload.claude_oauth_system_prompt_policy).toBe('inherit')
    })

    it('(2a) 点 CC 专线：cc=true、降级分组 null，策略保持', async () => {
      const w = await mountView()
      await openCreate(w)
      await radio(w, 'open_full').trigger('click')
      await radio(w, 'cc_line').trigger('click')
      expect(checkedHats(w)).toEqual(['cc_line'])
      await fillName(w)
      await submit(w)

      const payload = createdPayload()
      expect(payload.claude_code_only).toBe(true)
      expect(payload.fallback_group_id).toBe(null)
      // CC 类帽子不改动已存的注入策略
      expect(payload.claude_oauth_system_prompt_policy).toBe('enabled')
    })

    it('(2b) 在降级下拉里选一个候选：cc=true、降级分组=该正整数、策略保持原值', async () => {
      mocks.getAllGroups.mockResolvedValue(MIXED_ALL)
      const w = await mountView()
      await openCreate(w)
      await radio(w, 'open_full').trigger('click')

      await fallbackSelect(w).setValue('11')
      expect(checkedHats(w)).toEqual(['cc_fallback'])
      await fillName(w)
      await submit(w)

      const payload = createdPayload()
      expect(payload.claude_code_only).toBe(true)
      expect(payload.fallback_group_id).toBe(11)
      expect(payload.claude_oauth_system_prompt_policy).toBe('enabled')
    })

    it('(2c) 候选来自 getAll("anthropic")，只留 active、非 CC-only 的 anthropic 分组；每次打开创建弹窗调用一次', async () => {
      mocks.getAllGroups.mockResolvedValue(MIXED_ALL)
      const w = await mountView()
      expect(anthropicLoads()).toHaveLength(0)

      await openCreate(w)
      expect(anthropicLoads()).toHaveLength(1)
      expect(optionTexts(fallbackSelect(w))).toEqual(['Claude 主池', 'Claude 备用池'])

      await cancelDialog(w)
      await openCreate(w)
      expect(anthropicLoads()).toHaveLength(2)
      expect(optionTexts(fallbackSelect(w))).toEqual(['Claude 主池', 'Claude 备用池'])
    })

    it('(2d) 候选不依赖当前页：当前页没有任何 Claude 分组时，全量列表里的分组照样可选', async () => {
      mocks.listGroups.mockResolvedValue(listPage([makeGroup({ id: 99, name: '只有 OpenAI', platform: 'openai' })]))
      mocks.getAllGroups.mockResolvedValue(MIXED_ALL)
      const w = await mountView()
      await openCreate(w)
      expect(optionTexts(fallbackSelect(w))).toEqual(['Claude 主池', 'Claude 备用池'])
    })

    it('(3) 先点 CC 专线再把平台切到 openai：卡片消失，创建请求不带 Claude 专属字段', async () => {
      const w = await mountView()
      await openCreate(w)
      await radio(w, 'open_light').trigger('click')
      await radio(w, 'cc_line').trigger('click')
      expect(cardExists(w)).toBe(true)

      await w.get('[data-tour="group-form-platform"]').setValue('openai')
      expect(cardExists(w)).toBe(false)
      await fillName(w)
      await submit(w)

      expect(mocks.createGroup).toHaveBeenCalledTimes(1)
      const payload = createdPayload()
      expect(payload.platform).toBe('openai')
      expect(payload.claude_code_only).toBe(false)
      expect(payload.fallback_group_id).toBe(null)
      expect(payload.claude_oauth_system_prompt_policy).toBe('inherit')
      expect(payload.content_review_policy).toBe('inherit')
    })

    it('(3b) 选了降级分组再切到 gemini：降级分组与 cc 一起被重置', async () => {
      mocks.getAllGroups.mockResolvedValue(MIXED_ALL)
      const w = await mountView()
      await openCreate(w)
      await fallbackSelect(w).setValue('10')
      expect(checkedHats(w)).toEqual(['cc_fallback'])

      await w.get('[data-tour="group-form-platform"]').setValue('gemini')
      await fillName(w)
      await submit(w)

      const payload = createdPayload()
      expect(payload.platform).toBe('gemini')
      expect(payload.claude_code_only).toBe(false)
      expect(payload.fallback_group_id).toBe(null)
    })

    it('(3c) 切走后再切回 anthropic：卡片回到默认「开放·跟随全局」', async () => {
      const w = await mountView()
      await openCreate(w)
      await radio(w, 'cc_line').trigger('click')
      await w.get('[data-tour="group-form-platform"]').setValue('openai')
      await w.get('[data-tour="group-form-platform"]').setValue('anthropic')
      expect(cardExists(w)).toBe(true)
      expect(checkedHats(w)).toEqual(['open_follow'])
    })

    it('(12) 创建成功后再次打开：卡片回到默认，降级下拉没有残留选择', async () => {
      mocks.getAllGroups.mockResolvedValue(MIXED_ALL)
      const w = await mountView()
      await openCreate(w)
      await fallbackSelect(w).setValue('11')
      await fillName(w)
      await submit(w)
      expect(mocks.createGroup).toHaveBeenCalledTimes(1)
      expect(w.find('form#create-group-form').exists()).toBe(false)

      await openCreate(w)
      expect(checkedHats(w)).toEqual(['open_follow'])
      expect(selectValue(fallbackSelect(w))).toBe('')
    })

    it('(9) 候选加载期间：降级池卡片与下拉被禁用并显示加载中，加载完成后恢复', async () => {
      const pending = deferred<AdminGroup[]>()
      mocks.getAllGroups.mockReturnValue(pending.promise)
      const w = await mountView()
      await openCreate(w)

      expect(radio(w, 'cc_fallback').attributes('disabled')).toBeDefined()
      expect(fallbackSelect(w).attributes('disabled')).toBeDefined()
      expect(testId(w, 'hat-fallback-loading').text()).toBe('common.loading')
      // 弹窗已经打开，其它帽子照常可用
      await radio(w, 'cc_line').trigger('click')
      expect(checkedHats(w)).toEqual(['cc_line'])

      pending.resolve(MIXED_ALL)
      await flushPromises()
      expect(radio(w, 'cc_fallback').attributes('disabled')).toBeUndefined()
      expect(fallbackSelect(w).attributes('disabled')).toBeUndefined()
      expect(exists(w, 'hat-fallback-loading')).toBe(false)
      expect(optionTexts(fallbackSelect(w))).toContain('Claude 主池')
    })

    it('(10) getAll 失败：回落到当前页里的 Claude 分组，弹窗仍可用，不弹 toast，只打 console.error', async () => {
      const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
      mocks.getAllGroups.mockRejectedValue(new Error('boom'))
      mocks.listGroups.mockResolvedValue(
        listPage([
          makeGroup({ id: 1, name: '当前页的 Claude 分组' }),
          makeGroup({ id: 2, name: '当前页的 CC 专线', claude_code_only: true }),
          makeGroup({ id: 3, name: '当前页的 OpenAI', platform: 'openai' })
        ])
      )
      const w = await mountView()
      await openCreate(w)

      expect(mocks.showError).not.toHaveBeenCalled()
      expect(consoleError).toHaveBeenCalled()
      expect(consoleError.mock.calls.some(([message]) => String(message).includes('fallback picker'))).toBe(true)
      expect(optionTexts(fallbackSelect(w))).toEqual(['当前页的 Claude 分组'])
      expect(fallbackSelect(w).attributes('disabled')).toBeUndefined()
      expect(exists(w, 'hat-fallback-loading')).toBe(false)
      // 弹窗照常可用
      await fallbackSelect(w).setValue('1')
      await fillName(w)
      await submit(w)
      expect(createdPayload().fallback_group_id).toBe(1)
    })

    it('(11a) 并发加载：先发的请求后返回时被忽略，以最后一次为准', async () => {
      const first = deferred<AdminGroup[]>()
      const second = deferred<AdminGroup[]>()
      mocks.getAllGroups.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
      const w = await mountView()
      await openCreate(w)
      await cancelDialog(w)
      await openCreate(w)
      expect(anthropicLoads()).toHaveLength(2)

      second.resolve([makeGroup({ id: 21, name: '第二次的结果' })])
      await flushPromises()
      expect(optionTexts(fallbackSelect(w))).toEqual(['第二次的结果'])
      expect(exists(w, 'hat-fallback-loading')).toBe(false)

      // 第一次的请求姗姗来迟：不能覆盖第二次的结果，也不能把 loading 再打开
      first.resolve([makeGroup({ id: 20, name: '第一次的结果' })])
      await flushPromises()
      expect(optionTexts(fallbackSelect(w))).toEqual(['第二次的结果'])
      expect(exists(w, 'hat-fallback-loading')).toBe(false)
      expect(fallbackSelect(w).attributes('disabled')).toBeUndefined()
    })

    it('(11b) 并发加载：先发的请求先返回时被忽略，loading 要等最后一次结束才关', async () => {
      const first = deferred<AdminGroup[]>()
      const second = deferred<AdminGroup[]>()
      mocks.getAllGroups.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
      const w = await mountView()
      await openCreate(w)
      await cancelDialog(w)
      await openCreate(w)

      first.resolve([makeGroup({ id: 20, name: '第一次的结果' })])
      await flushPromises()
      expect(exists(w, 'hat-fallback-loading')).toBe(true)
      expect(fallbackSelect(w).attributes('disabled')).toBeDefined()
      expect(optionTexts(fallbackSelect(w))).toEqual([])

      second.resolve([makeGroup({ id: 21, name: '第二次的结果' })])
      await flushPromises()
      expect(exists(w, 'hat-fallback-loading')).toBe(false)
      expect(optionTexts(fallbackSelect(w))).toEqual(['第二次的结果'])
    })
  })

  describe('编辑', () => {
    // 两个策略取不同的非默认值：任何一个 v-model 接错字段，payload 里的值都会对不上
    const CC_FALLBACK_GROUP = makeGroup({
      id: 20,
      name: 'CC 优先分组',
      claude_code_only: true,
      fallback_group_id: 5,
      claude_oauth_system_prompt_policy: 'enabled',
      content_review_policy: 'disabled'
    })
    const CANDIDATES = [
      makeGroup({ id: 5, name: '降级 A' }),
      makeGroup({ id: 6, name: '降级 B' }),
      CC_FALLBACK_GROUP
    ]

    it('(4a) CC 优先分组：打开后选中降级池、下拉选中 5，四个字段都按分组原值回填', async () => {
      mocks.listGroups.mockResolvedValue(listPage([CC_FALLBACK_GROUP]))
      mocks.getAllGroups.mockResolvedValue(CANDIDATES)
      const w = await mountView()
      await openEdit(w, 20)

      expect(anthropicLoads()).toHaveLength(1)
      expect(checkedHats(w)).toEqual(['cc_fallback'])
      expect(selectValue(fallbackSelect(w))).toBe('5')
      expect(optionTexts(fallbackSelect(w))).toEqual(['admin.groups.claudeCode.noFallback', '降级 A', '降级 B'])
      expect(exists(w, 'hat-dangling-fallback')).toBe(false)
      expect(selectValue(testId(w, 'hat-fallback-policy-select'))).toBe('enabled')
      expect(selectValue(testId(w, 'content-review-select'))).toBe('disabled')
    })

    it('(4b) 改选「不降级」后提交：payload 的 fallback_group_id 是 0（null->0 映射），cc 仍为 true，两个策略原样', async () => {
      mocks.listGroups.mockResolvedValue(listPage([CC_FALLBACK_GROUP]))
      mocks.getAllGroups.mockResolvedValue(CANDIDATES)
      const w = await mountView()
      await openEdit(w, 20)

      await fallbackSelect(w).setValue('')
      expect(checkedHats(w)).toEqual(['cc_line'])
      await submit(w)

      expect(mocks.showError).not.toHaveBeenCalled()
      expect(mocks.updateGroup).toHaveBeenCalledTimes(1)
      const [id, payload] = updatedCall()
      expect(id).toBe(20)
      expect(payload.fallback_group_id).toBe(0)
      expect(payload.claude_code_only).toBe(true)
      expect(payload.claude_oauth_system_prompt_policy).toBe('enabled')
      expect(payload.content_review_policy).toBe('disabled')
    })

    it('(4c) 编辑里四个绑定都写得进更新请求：改选降级 B、改策略、改内容审计', async () => {
      mocks.listGroups.mockResolvedValue(listPage([CC_FALLBACK_GROUP]))
      mocks.getAllGroups.mockResolvedValue(CANDIDATES)
      const w = await mountView()
      await openEdit(w, 20)

      await fallbackSelect(w).setValue('6')
      await testId(w, 'hat-fallback-policy-select').setValue('identity_only')
      await testId(w, 'content-review-select').setValue('enabled')
      await submit(w)

      const [, payload] = updatedCall()
      expect(payload.claude_code_only).toBe(true)
      expect(payload.fallback_group_id).toBe(6)
      expect(payload.claude_oauth_system_prompt_policy).toBe('identity_only')
      expect(payload.content_review_policy).toBe('enabled')
    })

    it('(4d) 编辑里从 CC 优先改选开放·完整：cc=false、降级分组 0、策略 enabled', async () => {
      mocks.listGroups.mockResolvedValue(listPage([CC_FALLBACK_GROUP]))
      mocks.getAllGroups.mockResolvedValue(CANDIDATES)
      const w = await mountView()
      await openEdit(w, 20)

      await radio(w, 'open_full').trigger('click')
      await submit(w)

      const [, payload] = updatedCall()
      expect(payload.claude_code_only).toBe(false)
      expect(payload.fallback_group_id).toBe(0)
      expect(payload.claude_oauth_system_prompt_policy).toBe('enabled')
    })

    it('(5a) 残留降级分组：出现残留提示（名字来自全量列表，即使它不在当前页）；不动直接提交，payload 仍是 9，界面不静默清除', async () => {
      const stale = makeGroup({ id: 21, name: '开放分组', fallback_group_id: 9, claude_oauth_system_prompt_policy: 'enabled' })
      mocks.listGroups.mockResolvedValue(listPage([stale]))
      mocks.getAllGroups.mockResolvedValue([makeGroup({ id: 9, name: '旧降级组' }), stale])
      const w = await mountView()
      await openEdit(w, 21)

      expect(checkedHats(w)).toEqual(['open_full'])
      expect(testId(w, 'hat-stale-fallback').text()).toContain('admin.groups.hatCard.staleFallbackNotice::{"name":"旧降级组"}')
      await submit(w)

      const [id, payload] = updatedCall()
      expect(id).toBe(21)
      expect(payload.fallback_group_id).toBe(9)
      expect(payload.claude_code_only).toBe(false)
      expect(payload.claude_oauth_system_prompt_policy).toBe('enabled')
    })

    it('(5b) 点「清除」后提交：payload 的 fallback_group_id 是 0，其它字段不变', async () => {
      const stale = makeGroup({ id: 21, name: '开放分组', fallback_group_id: 9, claude_oauth_system_prompt_policy: 'enabled' })
      mocks.listGroups.mockResolvedValue(listPage([stale]))
      mocks.getAllGroups.mockResolvedValue([makeGroup({ id: 9, name: '旧降级组' }), stale])
      const w = await mountView()
      await openEdit(w, 21)

      await testId(w, 'hat-stale-fallback-clear').trigger('click')
      expect(exists(w, 'hat-stale-fallback')).toBe(false)
      await submit(w)

      const [, payload] = updatedCall()
      expect(payload.fallback_group_id).toBe(0)
      expect(payload.claude_code_only).toBe(false)
      expect(payload.claude_oauth_system_prompt_policy).toBe('enabled')
    })

    it('(5c) 残留的降级分组既不在全量列表也不在当前页时显示 #id', async () => {
      const stale = makeGroup({ id: 21, name: '开放分组', fallback_group_id: 99 })
      mocks.listGroups.mockResolvedValue(listPage([stale]))
      mocks.getAllGroups.mockResolvedValue([stale])
      const w = await mountView()
      await openEdit(w, 21)
      expect(testId(w, 'hat-stale-fallback').text()).toContain('staleFallbackNotice::{"name":"#99"}')
    })

    it('(5e) 残留的降级分组是 CC-only 分组（不是候选）且不在当前页：名字仍来自全量列表，不显示 #id', async () => {
      const stale = makeGroup({ id: 21, name: '开放分组', fallback_group_id: 9 })
      mocks.listGroups.mockResolvedValue(listPage([stale]))
      mocks.getAllGroups.mockResolvedValue([makeGroup({ id: 9, name: 'CC 旧降级组', claude_code_only: true }), stale])
      const w = await mountView()
      await openEdit(w, 21)

      // 它是 CC-only，不在候选里；当前页也没有它，只有全量列表里查得到
      expect(optionTexts(fallbackSelect(w))).not.toContain('CC 旧降级组')
      expect(testId(w, 'hat-stale-fallback').text()).toContain('staleFallbackNotice::{"name":"CC 旧降级组"}')
      expect(testId(w, 'hat-will-write').text()).toContain('"fallback":"CC 旧降级组"')
    })

    it('(5f) 降级池指向后来变成 CC-only 的分组（悬空）：下拉合成悬空项，「将写入」里仍显示它的名字', async () => {
      const group = makeGroup({ id: 22, name: '降级池分组', claude_code_only: true, fallback_group_id: 9 })
      mocks.listGroups.mockResolvedValue(listPage([group]))
      mocks.getAllGroups.mockResolvedValue([makeGroup({ id: 9, name: 'CC 旧降级组', claude_code_only: true }), group])
      const w = await mountView()
      await openEdit(w, 22)

      expect(optionTexts(fallbackSelect(w))).toContain('admin.groups.hatCard.danglingFallbackLabel::{"id":9}')
      expect(exists(w, 'hat-dangling-fallback')).toBe(true)
      expect(testId(w, 'hat-will-write').text()).toContain('"fallback":"CC 旧降级组"')
    })

    it('(5d) 降级池指向已停用或已删除的分组：下拉里合成悬空项并提示；不动直接提交，payload 仍是该 id', async () => {
      const dangling = makeGroup({ id: 22, name: '悬空分组', claude_code_only: true, fallback_group_id: 77 })
      mocks.listGroups.mockResolvedValue(listPage([dangling]))
      mocks.getAllGroups.mockResolvedValue([makeGroup({ id: 5, name: '降级 A' }), dangling])
      const w = await mountView()
      await openEdit(w, 22)

      expect(checkedHats(w)).toEqual(['cc_fallback'])
      expect(optionTexts(fallbackSelect(w))).toEqual([
        'admin.groups.claudeCode.noFallback',
        '降级 A',
        'admin.groups.hatCard.danglingFallbackLabel::{"id":77}'
      ])
      expect(exists(w, 'hat-dangling-fallback')).toBe(true)
      await submit(w)
      expect(updatedCall()[1].fallback_group_id).toBe(77)
    })

    it('(6) referrers：别的分组把本分组当降级目标；当前是开放帽时不提示，点 CC 专线后出现并点名该分组', async () => {
      const current = makeGroup({ id: 30, name: '被引用的分组' })
      const referrer = makeGroup({ id: 31, name: 'B 组', claude_code_only: true, fallback_group_id: 30 })
      const bystander = makeGroup({ id: 32, name: 'C 组' })
      mocks.listGroups.mockResolvedValue(listPage([current]))
      mocks.getAllGroups.mockResolvedValue([current, referrer, bystander])
      const w = await mountView()
      await openEdit(w, 30)

      expect(checkedHats(w)).toEqual(['open_follow'])
      expect(exists(w, 'hat-referrers')).toBe(false)

      await radio(w, 'cc_line').trigger('click')
      const notice = testId(w, 'hat-referrers').text()
      expect(notice).toBe('admin.groups.hatCard.referrersNotice::{"names":"B 组"}')
      expect(notice).not.toContain('C 组')
    })

    it('(6c) referrers 只点名降级真的在起作用的分组：自己没开仅限 Claude Code、降级分组只是残留值的不算', async () => {
      const current = makeGroup({ id: 30, name: '被引用的分组' })
      const live = makeGroup({ id: 31, name: '降级在起作用', claude_code_only: true, fallback_group_id: 30 })
      const staleReferrer = makeGroup({ id: 34, name: '只是残留', claude_code_only: false, fallback_group_id: 30 })
      mocks.listGroups.mockResolvedValue(listPage([current]))
      mocks.getAllGroups.mockResolvedValue([current, live, staleReferrer])
      const w = await mountView()
      await openEdit(w, 30)

      await radio(w, 'cc_line').trigger('click')
      const notice = testId(w, 'hat-referrers').text()
      expect(notice).toBe('admin.groups.hatCard.referrersNotice::{"names":"降级在起作用"}')
      expect(notice).not.toContain('只是残留')
    })

    it('(6d) 引用本分组的分组都只是残留值时，改成 CC 专线也不提示', async () => {
      const current = makeGroup({ id: 30, name: '被引用的分组' })
      const staleReferrer = makeGroup({ id: 34, name: '只是残留', claude_code_only: false, fallback_group_id: 30 })
      mocks.listGroups.mockResolvedValue(listPage([current]))
      mocks.getAllGroups.mockResolvedValue([current, staleReferrer])
      const w = await mountView()
      await openEdit(w, 30)

      await radio(w, 'cc_line').trigger('click')
      expect(exists(w, 'hat-referrers')).toBe(false)
    })

    it('(6b) referrers 只算引用了本分组的，不含引用别人的分组', async () => {
      const current = makeGroup({ id: 30, name: '被引用的分组', claude_code_only: true })
      const other = makeGroup({ id: 33, name: '引用别人的分组', claude_code_only: true, fallback_group_id: 10 })
      mocks.listGroups.mockResolvedValue(listPage([current]))
      mocks.getAllGroups.mockResolvedValue([current, other, makeGroup({ id: 10, name: '别人' })])
      const w = await mountView()
      await openEdit(w, 30)
      expect(checkedHats(w)).toEqual(['cc_line'])
      expect(exists(w, 'hat-referrers')).toBe(false)
    })

    it('(7) 编辑 openai 分组：不请求 getAll("anthropic")，也没有 Claude 策略卡，弹窗照常打开', async () => {
      const openai = makeGroup({ id: 50, name: 'OpenAI 分组', platform: 'openai' })
      mocks.listGroups.mockResolvedValue(listPage([openai]))
      const w = await mountView()
      await openEdit(w, 50)

      expect(anthropicLoads()).toHaveLength(0)
      expect(w.find('form#edit-group-form').exists()).toBe(true)
      expect(cardExists(w)).toBe(false)
    })

    it('(8) 编辑 anthropic 分组：候选排除自身，且只留 active、非 CC-only 的 anthropic 分组', async () => {
      const current = makeGroup({ id: 40, name: '正在编辑的分组' })
      mocks.listGroups.mockResolvedValue(listPage([current]))
      mocks.getAllGroups.mockResolvedValue([current, makeGroup({ id: 41, name: '别的 Claude 分组' }), ...MIXED_ALL.slice(2)])
      const w = await mountView()
      await openEdit(w, 40)

      expect(optionTexts(fallbackSelect(w))).toEqual(['别的 Claude 分组'])
    })

    it('(8b) 编辑与创建各自用自己的候选：创建弹窗的候选包含这个分组，编辑弹窗里排除', async () => {
      const current = makeGroup({ id: 40, name: '正在编辑的分组' })
      mocks.listGroups.mockResolvedValue(listPage([current]))
      mocks.getAllGroups.mockResolvedValue([current, makeGroup({ id: 41, name: '别的 Claude 分组' })])
      const w = await mountView()
      await openEdit(w, 40)
      expect(optionTexts(fallbackSelect(w))).toEqual(['别的 Claude 分组'])
      await cancelDialog(w)

      await openCreate(w)
      expect(optionTexts(fallbackSelect(w))).toEqual(['正在编辑的分组', '别的 Claude 分组'])
    })

    it('(10b) 编辑时 getAll 失败：弹窗仍然打开，候选回落到当前页，不弹 toast', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      const current = makeGroup({ id: 40, name: '正在编辑的分组', claude_code_only: true, fallback_group_id: 41 })
      mocks.listGroups.mockResolvedValue(listPage([current, makeGroup({ id: 41, name: '当前页的降级分组' })]))
      mocks.getAllGroups.mockRejectedValue(new Error('boom'))
      const w = await mountView()
      await openEdit(w, 40)

      expect(mocks.showError).not.toHaveBeenCalled()
      expect(w.find('form#edit-group-form').exists()).toBe(true)
      expect(checkedHats(w)).toEqual(['cc_fallback'])
      expect(optionTexts(fallbackSelect(w))).toEqual(['admin.groups.claudeCode.noFallback', '当前页的降级分组'])
      expect(exists(w, 'hat-dangling-fallback')).toBe(false)
    })

    it('(13) 编辑完一个分组、取消后再编辑另一个：卡片按各自的分组回填，没有串值', async () => {
      const cc = makeGroup({ id: 20, name: 'CC 优先分组', claude_code_only: true, fallback_group_id: 5, claude_oauth_system_prompt_policy: 'enabled' })
      const open = makeGroup({ id: 23, name: '开放·轻', claude_oauth_system_prompt_policy: 'identity_only', content_review_policy: 'enabled' })
      mocks.listGroups.mockResolvedValue(listPage([cc, open]))
      mocks.getAllGroups.mockResolvedValue([makeGroup({ id: 5, name: '降级 A' }), cc, open])
      const w = await mountView()

      await openEdit(w, 20)
      expect(checkedHats(w)).toEqual(['cc_fallback'])
      await cancelDialog(w)

      await openEdit(w, 23)
      expect(checkedHats(w)).toEqual(['open_light'])
      expect(selectValue(fallbackSelect(w))).toBe('')
      expect(selectValue(testId(w, 'content-review-select'))).toBe('enabled')
      expect(exists(w, 'hat-fallback-policy-select')).toBe(false)
    })
  })
})
