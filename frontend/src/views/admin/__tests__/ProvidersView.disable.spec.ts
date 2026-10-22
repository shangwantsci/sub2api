import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'

import ProvidersView from '../ProvidersView.vue'

const { listProviders, updateUser } = vi.hoisted(() => ({
  listProviders: vi.fn(),
  updateUser: vi.fn()
}))

vi.mock('@/api', () => ({
  adminAPI: {
    providers: { list: listProviders },
    users: { update: updateUser }
  }
}))

// t 把参数一起回显，才能断言确认框里的账号数；其余沿用「回显 key」的写法。
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key} ${JSON.stringify(params)}` : key
    })
  }
})

const SUMMARY = {
  providers: [
    {
      user_id: 11,
      email: 'alice@example.com',
      username: 'alice',
      status: 'active',
      created_at: '2026-10-01T00:00:00Z',
      accounts_total: 4,
      accounts_active: 3,
      accounts_paused: 1,
      period_start: '2026-10-01T00:00:00Z',
      pending_requests: 0,
      pending_tokens: 0,
      pending_cost: '0'
    },
    {
      user_id: 12,
      email: 'bob@example.com',
      username: 'bob',
      status: 'disabled',
      created_at: '2026-10-02T00:00:00Z',
      accounts_total: 2,
      accounts_active: 0,
      accounts_paused: 2,
      period_start: '2026-10-01T00:00:00Z',
      pending_requests: 0,
      pending_tokens: 0,
      pending_cost: '0'
    }
  ],
  total_providers: 2,
  pending_providers: 0,
  total_pending_cost: '0',
  settlement_timezone: 'Asia/Shanghai'
}

// BaseDialog 的真实实现带 Teleport 等细节，这里只需要「show 为真时渲染默认插槽与页脚」。
const BaseDialogStub = {
  props: ['show', 'title'],
  template: '<div v-if="show" class="dialog"><slot /><slot name="footer" /></div>'
}

function mountView(): VueWrapper {
  return mount(ProvidersView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        RouterLink: { template: '<a><slot /></a>' },
        Pagination: true,
        BaseDialog: BaseDialogStub
      }
    }
  })
}

async function openManageTab(wrapper: VueWrapper) {
  const tab = wrapper.findAll('nav button').find((b) => b.text() === 'admin.providers.tabManage')
  expect(tab, '应有「供号商管理」页签').toBeTruthy()
  await tab!.trigger('click')
  await flushPromises()
}

function rowActionButton(wrapper: VueWrapper, email: string, label: 'disable' | 'enable') {
  const row = wrapper.findAll('tbody tr').find((tr) => tr.text().includes(email))
  expect(row, `应有 ${email} 这一行`).toBeTruthy()
  const button = row!.findAll('button').find((b) => b.text() === `admin.providers.${label}`)
  expect(button, `${email} 应有「${label}」按钮`).toBeTruthy()
  return button!
}

function confirmDialogText(wrapper: VueWrapper): string {
  return wrapper.find('.dialog').exists() ? wrapper.get('.dialog').text() : ''
}

describe('ProvidersView 停用 / 启用供号商', () => {
  beforeEach(() => {
    listProviders.mockReset().mockResolvedValue(structuredClone(SUMMARY))
    updateUser.mockReset().mockResolvedValue({})
  })

  it('点「停用」先弹确认框，说清影响多少账号；还没点确认之前不调用后端', async () => {
    const wrapper = mountView()
    await flushPromises()
    await openManageTab(wrapper)

    expect(confirmDialogText(wrapper)).toBe('')
    await rowActionButton(wrapper, 'alice@example.com', 'disable').trigger('click')
    await flushPromises()

    const text = confirmDialogText(wrapper)
    expect(text).toContain('admin.providers.disableConfirmMessage')
    expect(text).toContain('"provider":"alice@example.com"')
    expect(text).toContain('"total":4')
    expect(text).toContain('"active":3')
    expect(updateUser).not.toHaveBeenCalled()
  })

  it('确认后才发 status=disabled，并重新加载列表', async () => {
    const wrapper = mountView()
    await flushPromises()
    await openManageTab(wrapper)
    await rowActionButton(wrapper, 'alice@example.com', 'disable').trigger('click')
    await flushPromises()

    const confirm = wrapper.findAll('.dialog button').find((b) => b.text() === 'admin.providers.disable')
    expect(confirm, '确认框里应有确认按钮').toBeTruthy()
    await confirm!.trigger('click')
    await flushPromises()

    expect(updateUser).toHaveBeenCalledTimes(1)
    expect(updateUser).toHaveBeenCalledWith(11, { status: 'disabled' })
    expect(listProviders).toHaveBeenCalledTimes(2)
    expect(confirmDialogText(wrapper)).toBe('')
  })

  it('取消不调用后端，也不会留着旧目标', async () => {
    const wrapper = mountView()
    await flushPromises()
    await openManageTab(wrapper)
    await rowActionButton(wrapper, 'alice@example.com', 'disable').trigger('click')
    await flushPromises()

    const cancel = wrapper.findAll('.dialog button').find((b) => b.text() === 'common.cancel')
    expect(cancel, '确认框里应有取消按钮').toBeTruthy()
    await cancel!.trigger('click')
    await flushPromises()

    expect(updateUser).not.toHaveBeenCalled()
    expect(confirmDialogText(wrapper)).toBe('')
  })

  it('「启用」只是恢复，不弹确认框，直接发 status=active', async () => {
    const wrapper = mountView()
    await flushPromises()
    await openManageTab(wrapper)

    await rowActionButton(wrapper, 'bob@example.com', 'enable').trigger('click')
    await flushPromises()

    expect(confirmDialogText(wrapper)).toBe('')
    expect(updateUser).toHaveBeenCalledTimes(1)
    expect(updateUser).toHaveBeenCalledWith(12, { status: 'active' })
  })

  it('确认后后端失败：错误显示在页面上，不会卡住', async () => {
    updateUser.mockRejectedValue({ message: '暂停账号失败' })
    const wrapper = mountView()
    await flushPromises()
    await openManageTab(wrapper)
    await rowActionButton(wrapper, 'alice@example.com', 'disable').trigger('click')
    await flushPromises()
    await wrapper.findAll('.dialog button').find((b) => b.text() === 'admin.providers.disable')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('暂停账号失败')
    expect(confirmDialogText(wrapper)).toBe('')
  })
})
