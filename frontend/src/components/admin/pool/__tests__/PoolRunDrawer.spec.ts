import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PoolRunDrawer from '../PoolRunDrawer.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k }) }))

describe('PoolRunDrawer', () => {
  it('renders run and emits release', async () => {
    const run = {
      run_id: 'full-session-id', session: 'full-session', account_id: 7,
      account_name: 'acct-7', state: 'waiting_tool', tool_pending: 1,
      started_at: '2026-10-25T09:00:00Z', last_used_at: '2026-10-25T09:01:00Z',
    }
    const wrapper = mount(PoolRunDrawer, { props: { show: true, run }, attachTo: document.body })
    expect(document.body.textContent).toContain('full-session-id')
    expect(document.body.textContent).toContain('waiting_tool')
    const buttons = document.body.querySelectorAll('button')
    buttons[buttons.length - 1].dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('release')?.[0]?.[0]).toMatchObject({ run_id: 'full-session-id' })
    wrapper.unmount()
  })
})
