import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'

const {
  listAccountsMock,
  getCurrentPeriodInfoMock,
  getOnboardOptionsMock,
  getAccountUsageMock,
  updateAccountMock,
  reauthAccountMock,
  generateReauthURLMock,
  pauseAccountMock,
  resumeAccountMock,
  offlineAccountMock
} = vi.hoisted(() => ({
  listAccountsMock: vi.fn(),
  getCurrentPeriodInfoMock: vi.fn(),
  getOnboardOptionsMock: vi.fn(),
  getAccountUsageMock: vi.fn(),
  updateAccountMock: vi.fn(),
  reauthAccountMock: vi.fn(),
  generateReauthURLMock: vi.fn(),
  pauseAccountMock: vi.fn(),
  resumeAccountMock: vi.fn(),
  offlineAccountMock: vi.fn()
}))

vi.mock('@/api/provider', () => ({
  listAccounts: listAccountsMock,
  getCurrentPeriodInfo: getCurrentPeriodInfoMock,
  getOnboardOptions: getOnboardOptionsMock,
  getAccountUsage: getAccountUsageMock,
  updateAccount: updateAccountMock,
  reauthAccount: reauthAccountMock,
  generateReauthURL: generateReauthURLMock,
  pauseAccount: pauseAccountMock,
  resumeAccount: resumeAccountMock,
  offlineAccount: offlineAccountMock
}))

vi.mock('@/components/layout/ProviderLayout.vue', () => ({
  default: { name: 'ProviderLayout', template: '<div><slot /></div>' }
}))

// BaseDialog 用 Teleport 挂到 body，测试里换成就地渲染，断言才看得到内容。
vi.mock('@/components/common/BaseDialog.vue', () => ({
  default: {
    name: 'BaseDialog',
    props: ['show', 'title', 'width'],
    template: '<div v-if="show" class="dialog"><slot /><slot name="footer" /></div>'
  }
}))

vi.mock('@/components/common/ConfirmDialog.vue', () => ({
  default: { name: 'ConfirmDialog', props: ['show'], template: '<div />' }
}))

vi.mock('@/components/common/Pagination.vue', () => ({
  default: { name: 'Pagination', props: ['page', 'total', 'pageSize'], template: '<div />' }
}))

vi.mock('vue-router', () => ({
  RouterLink: { name: 'RouterLink', props: ['to'], template: '<a><slot /></a>' }
}))

import ProviderAccounts from '@/views/provider/ProviderAccounts.vue'

/** 生产 GET /provider/accounts 单条记录的真实形态。 */
function account(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    name: 'supplier@example.com',
    email: 'supplier@example.com',
    notes: null,
    status: 'active',
    hosting_type_label: '稳健型',
    tier_label: '3 档',
    tier: '3',
    created_at: '2026-08-01T00:00:00Z',
    period_requests: 120,
    period_tokens: 5000,
    period_cost: '1.2345678901',
    ...overrides
  }
}

/** 生产 GET /provider/accounts/:id/usage 的真实形态。 */
function usage(overrides: Record<string, unknown> = {}) {
  return {
    source: 'passive',
    updated_at: '2026-08-02T05:00:00Z',
    five_hour: { utilization: 42.5, resets_at: '2026-08-02T10:00:00Z', requests: 120, tokens: 5000 },
    seven_day: { utilization: 18, resets_at: '2026-08-08T00:00:00Z' },
    ...overrides
  }
}

function mountPage() {
  const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh } })
  return mount(ProviderAccounts, { global: { plugins: [i18n] } })
}

describe('ProviderAccounts 挂载', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getAccountUsageMock.mockResolvedValue(usage())
    getCurrentPeriodInfoMock.mockResolvedValue({
      period_start: '2026-07-01T00:00:00Z',
      settlement_timezone: 'Asia/Shanghai'
    })
    getOnboardOptionsMock.mockResolvedValue({
      hosting_types: [{ id: 11, label: '稳健型', description: '寿命最长' }],
      default_hosting_type_id: 11,
      tiers: [
        { tier: '3', label: '3 档', concurrency: 3, max_sessions: 3, base_rpm: 30, window_cost_limit: 60 },
        { tier: '5', label: '5 档', concurrency: 8, max_sessions: 8, base_rpm: 80, window_cost_limit: 0 }
      ],
      default_tier: '3',
      custom_tier: { enabled: true, concurrency: 10, max_sessions: 10, base_rpm: 100, window_cost_limit: 200 },
      proxy_mode_policy: 'both',
      auto_proxy_available: true
    })
  })

  // 供号商手上通常有一批号，名称可以重复也可以乱填，只有邮箱能让他对上
  // 「哪些已经上了、哪些还没上」—— 这正是加这一栏的全部理由。
  it('把账号邮箱显示出来', async () => {
    listAccountsMock.mockResolvedValue({ items: [account()], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountPage()
    await flushPromises()

    expect(wrapper.text()).toContain('supplier@example.com')
  })

  // 展示的是账号自己在 Anthropic 那边用掉了多少额度，不是平台设的费用上限。
  it('按窗口渲染额度用量百分比', async () => {
    listAccountsMock.mockResolvedValue({ items: [account()], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountPage()
    await flushPromises()

    expect(getAccountUsageMock).toHaveBeenCalledWith(1)
    const text = wrapper.text()
    expect(text).toContain('5h')
    expect(text).toContain('43%')
    expect(text).toContain('7d')
    expect(text).toContain('18%')
  })

  /**
   * 金额一个都不能出现在这一栏里。
   *
   * 后端 AccountUsageWindowView 就不下发金额（含账号倍率的 cost、向客户收的 user_cost），
   * 前端也不能从别处捡一个补上 —— 复用的 UsageProgressBar 只要拿到 windowStats
   * 就会渲染 `A $x.xx`。
   */
  it('额度栏不出现任何金额', async () => {
    listAccountsMock.mockResolvedValue({ items: [account()], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountPage()
    await flushPromises()

    const usageCell = wrapper.findAll('td')[4]
    expect(usageCell.text()).not.toContain('$')
  })

  // 上游没给某个窗口就整条不渲染：补一个 0% 会被读成「这个号完全没用过」。
  it('上游只给了 5h 时不补出一条 0% 的 7d', async () => {
    getAccountUsageMock.mockResolvedValue(usage({ seven_day: null }))
    listAccountsMock.mockResolvedValue({ items: [account()], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountPage()
    await flushPromises()

    const usageCell = wrapper.findAll('td')[4]
    expect(usageCell.text()).toContain('5h')
    expect(usageCell.text()).not.toContain('7d')
  })

  // 用量拿不到不该把整张表打成错误状态 —— 账号本身的信息还是好的。
  it('用量查询失败时表格照常显示', async () => {
    getAccountUsageMock.mockRejectedValue(new Error('boom'))
    listAccountsMock.mockResolvedValue({ items: [account()], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountPage()
    await flushPromises()

    expect(wrapper.text()).toContain('supplier@example.com')
    expect(wrapper.text()).not.toContain('boom')
  })

  // 被动采样可能是很久以前那次请求留下的，要能主动查一次上游。
  it('刷新按钮走主动查询', async () => {
    listAccountsMock.mockResolvedValue({ items: [account()], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountPage()
    await flushPromises()

    const refresh = wrapper
      .findAll('button')
      .find((b) => b.text() === zh.provider.accounts.usageRefresh)
    expect(refresh, '有用量数据时要给出刷新入口').toBeTruthy()
    await refresh!.trigger('click')
    await flushPromises()

    expect(getAccountUsageMock).toHaveBeenLastCalledWith(1, 'active')
  })
})

describe('ProviderAccounts 编辑', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getAccountUsageMock.mockResolvedValue(usage())
    getCurrentPeriodInfoMock.mockResolvedValue({
      period_start: '2026-07-01T00:00:00Z',
      settlement_timezone: 'Asia/Shanghai'
    })
    getOnboardOptionsMock.mockResolvedValue({
      hosting_types: [{ id: 11, label: '稳健型', description: '寿命最长' }],
      default_hosting_type_id: 11,
      tiers: [
        { tier: '3', label: '3 档', concurrency: 3, max_sessions: 3, base_rpm: 30, window_cost_limit: 60 }
      ],
      default_tier: '3',
      custom_tier: { enabled: false, concurrency: 10, max_sessions: 10, base_rpm: 100, window_cost_limit: 200 },
      proxy_mode_policy: 'both',
      auto_proxy_available: true
    })
    listAccountsMock.mockResolvedValue({ items: [account()], total: 1, page: 1, page_size: 20, pages: 1 })
    updateAccountMock.mockResolvedValue(account())
  })

  async function openEditDialog() {
    const wrapper = mountPage()
    await flushPromises()
    const editButton = wrapper
      .findAll('button')
      .find((b) => b.text() === zh.provider.accounts.edit)
    expect(editButton, '账号行上必须有编辑入口').toBeTruthy()
    await editButton!.trigger('click')
    await flushPromises()
    return wrapper
  }

  it('编辑弹窗用账号当前值预填', async () => {
    const wrapper = await openEditDialog()
    const nameInput = wrapper.find('#edit-name')
    expect((nameInput.element as HTMLInputElement).value).toBe('supplier@example.com')
  })

  // 换档会重写 extra 并重新入队调度快照。只改了名字却顺手把档位也发一遍，
  // 等于每次编辑都白跑一次换档。
  it('档位没变时不发 tier 字段', async () => {
    const wrapper = await openEditDialog()
    await wrapper.find('#edit-name').setValue('新名字')
    const saveButton = wrapper.findAll('button').find((b) => b.text() === zh.common.save)
    await saveButton!.trigger('click')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const [, payload] = updateAccountMock.mock.calls[0]
    expect(payload.name).toBe('新名字')
    expect(payload.tier).toBeUndefined()
  })

  /**
   * 编辑里绝不能出现自定义档。
   *
   * 自定义档的并发/会话数/RPM 不下发到供号商侧，弹窗只能把输入框预填成默认值 ——
   * 供号商本来只想改个名字，一保存就把自己原先填的参数静默重置了，而且没有任何提示。
   * 站点设置里开着自定义档（custom_tier.enabled = true）也不例外。
   */
  it('即使站点开着自定义档，编辑里也不给这个选项', async () => {
    getOnboardOptionsMock.mockResolvedValue({
      hosting_types: [{ id: 11, label: '稳健型', description: '寿命最长' }],
      default_hosting_type_id: 11,
      tiers: [
        { tier: '3', label: '3 档', concurrency: 3, max_sessions: 3, base_rpm: 30, window_cost_limit: 60 }
      ],
      default_tier: '3',
      custom_tier: { enabled: true, concurrency: 10, max_sessions: 10, base_rpm: 100, window_cost_limit: 200 },
      proxy_mode_policy: 'both',
      auto_proxy_available: true
    })
    const wrapper = await openEditDialog()

    expect(wrapper.find('input[value="custom"]').exists()).toBe(false)
    expect(wrapper.find('#edit-concurrency').exists()).toBe(false)
    expect(wrapper.find('#edit-rpm').exists()).toBe(false)
  })

  // 自定义档的号不动档位直接保存时，不能把它顶到某个固定档上去。
  it('自定义档账号不改档位时不发 tier', async () => {
    listAccountsMock.mockResolvedValue({
      items: [account({ tier: 'custom', tier_label: '自定义' })],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    const wrapper = await openEditDialog()
    // 弹窗里要说清楚为什么没有自定义档可选，否则供号商只会以为档位列表少了一项。
    expect(wrapper.text()).toContain(zh.provider.accounts.customTierLocked)

    await wrapper.find('#edit-name').setValue('只改名字')
    const saveButton = wrapper.findAll('button').find((b) => b.text() === zh.common.save)
    await saveButton!.trigger('click')
    await flushPromises()

    const [, payload] = updateAccountMock.mock.calls[0]
    expect(payload.tier).toBeUndefined()
  })

  it('名称清空时挡下来而不是发一个空名字', async () => {
    const wrapper = await openEditDialog()
    await wrapper.find('#edit-name').setValue('   ')
    const saveButton = wrapper.findAll('button').find((b) => b.text() === zh.common.save)
    await saveButton!.trigger('click')
    await flushPromises()

    expect(updateAccountMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain(zh.provider.accounts.nameRequired)
  })
})
