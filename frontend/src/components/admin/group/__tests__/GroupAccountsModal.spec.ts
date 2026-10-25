import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import GroupAccountsModal from '../GroupAccountsModal.vue'

const getGroupAccounts = vi.fn()
const setGroupAccounts = vi.fn()
const listAccounts = vi.fn()

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      getGroupAccounts: (...args: any[]) => getGroupAccounts(...args),
      setGroupAccounts: (...args: any[]) => setGroupAccounts(...args),
    },
    accounts: {
      list: (...args: any[]) => listAccounts(...args),
    },
  },
}))

const group = { id: 7, name: 'Customer A', platform: 'anthropic' } as any
const account = (id: number, name: string) => ({ id, name, platform: 'anthropic', type: 'oauth', status: 'active' }) as any

describe('GroupAccountsModal', () => {
  beforeEach(() => {
    getGroupAccounts.mockReset().mockResolvedValue([account(1, 'A')])
    setGroupAccounts.mockReset().mockResolvedValue(undefined)
    listAccounts.mockReset().mockResolvedValue({ items: [account(1, 'A'), account(2, 'B')], total: 2 })
  })

  it('loads bound accounts and saves selection', async () => {
    const wrapper = mount(GroupAccountsModal, { props: { show: true, group } })
    await flushPromises()
    expect(getGroupAccounts).toHaveBeenCalledWith(7)
    const boxes = wrapper.findAll('input[type="checkbox"]')
    expect(boxes).toHaveLength(2)
    expect((boxes[0].element as HTMLInputElement).checked).toBe(true)

    await boxes[1].setValue(true)
    await wrapper.get('[data-testid="save-accounts"]').trigger('click')
    await flushPromises()
    expect(setGroupAccounts).toHaveBeenCalledWith(7, [1, 2])
    expect(wrapper.emitted('saved')).toBeTruthy()
  })

  it('shows save failure inside the modal', async () => {
    setGroupAccounts.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mount(GroupAccountsModal, { props: { show: true, group } })
    await flushPromises()
    await wrapper.get('[data-testid="save-accounts"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('boom')
  })

  it('debounces server-side search', async () => {
    vi.useFakeTimers()
    const wrapper = mount(GroupAccountsModal, { props: { show: true, group } })
    await flushPromises()
    listAccounts.mockClear()
    await wrapper.get('input.input').setValue('B')
    await vi.advanceTimersByTimeAsync(299)
    expect(listAccounts).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(listAccounts).toHaveBeenCalledWith(1, 100, expect.objectContaining({ search: 'B' }))
    vi.useRealTimers()
  })
})
