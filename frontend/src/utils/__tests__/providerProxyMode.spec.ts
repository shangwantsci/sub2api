import { describe, it, expect } from 'vitest'
import { resolveProviderProxyModeState } from '@/utils/providerProxyMode'

describe('resolveProviderProxyModeState - 两种来源都开放', () => {
  it('有库存时默认走平台出口', () => {
    const state = resolveProviderProxyModeState('both', true)
    expect(state.initialMode).toBe('auto')
    expect(state.showChoice).toBe(true)
    expect(state.blocked).toBe(false)
  })

  it('没库存时落到自带代理，不算被挡住', () => {
    const state = resolveProviderProxyModeState('both', false)
    expect(state.initialMode).toBe('manual')
    expect(state.autoAvailable).toBe(false)
    expect(state.manualAllowed).toBe(true)
    expect(state.blocked).toBe(false)
  })
})

describe('resolveProviderProxyModeState - 仅平台出口', () => {
  it('有库存时走平台出口且不给选项', () => {
    const state = resolveProviderProxyModeState('auto_only', true)
    expect(state.initialMode).toBe('auto')
    expect(state.manualAllowed).toBe(false)
    expect(state.showChoice).toBe(false)
    expect(state.blocked).toBe(false)
  })

  // 这条是回归：初始来源曾经按库存决定，池子空时落到 manual，
  // 页面于是一边显示「当前无法上号」，一边把自带代理的输入框露了出来。
  it('池子空时仍停在平台出口，不能落到自带', () => {
    const state = resolveProviderProxyModeState('auto_only', false)
    expect(state.initialMode).toBe('auto')
    expect(state.manualAllowed).toBe(false)
    expect(state.blocked).toBe(true)
  })
})

describe('resolveProviderProxyModeState - 仅自带代理', () => {
  it('始终走自带，且平台出口不可用', () => {
    for (const hasStock of [true, false]) {
      const state = resolveProviderProxyModeState('manual_only', hasStock)
      expect(state.initialMode).toBe('manual')
      expect(state.autoAllowed).toBe(false)
      // 策略关掉了平台出口，就算池子里有货也不该当成可用。
      expect(state.autoAvailable).toBe(false)
      expect(state.showChoice).toBe(false)
      // 还能自带，就不是无路可走。
      expect(state.blocked).toBe(false)
    }
  })
})

describe('resolveProviderProxyModeState - 异常策略值', () => {
  // settings 表被手改成未知值时按 both 处理，不能把两种来源全锁死。
  it.each([undefined, 'platform_only', '', 'AUTO_ONLY'])('%s 按 both 处理', (policy) => {
    const state = resolveProviderProxyModeState(policy as never, true)
    expect(state.autoAllowed).toBe(true)
    expect(state.manualAllowed).toBe(true)
    expect(state.showChoice).toBe(true)
    expect(state.blocked).toBe(false)
  })
})

describe('resolveProviderProxyModeState - 不变量', () => {
  // 被挡住时不该同时还给出一个可用的来源，否则界面必然自相矛盾。
  it('blocked 时两种来源都不可选', () => {
    const policies = ['both', 'auto_only', 'manual_only'] as const
    for (const policy of policies) {
      for (const hasStock of [true, false]) {
        const state = resolveProviderProxyModeState(policy, hasStock)
        if (!state.blocked) continue
        expect(state.autoAvailable).toBe(false)
        expect(state.manualAllowed).toBe(false)
      }
    }
  })

  it('初始来源永远是策略允许的那一种', () => {
    const policies = ['both', 'auto_only', 'manual_only'] as const
    for (const policy of policies) {
      for (const hasStock of [true, false]) {
        const state = resolveProviderProxyModeState(policy, hasStock)
        if (state.initialMode === 'auto') {
          expect(state.autoAllowed).toBe(true)
        } else {
          expect(state.manualAllowed).toBe(true)
        }
      }
    }
  })
})
