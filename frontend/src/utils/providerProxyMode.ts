import type { ProviderProxyMode, ProviderProxyModePolicy } from '@/api/provider'

export interface ProviderProxyModeState {
  /** 策略是否开放平台出口。 */
  autoAllowed: boolean
  /** 策略是否开放自带代理。 */
  manualAllowed: boolean
  /** 平台出口既开放、又还有库存。 */
  autoAvailable: boolean
  /** 两种来源都用不了，任何提交都必然失败。 */
  blocked: boolean
  /** 是否需要给出来源单选（只有两种都开放时才有得选）。 */
  showChoice: boolean
  /** 打开页面时默认选中的来源。 */
  initialMode: ProviderProxyMode
}

/**
 * 由站点策略与平台出口库存推出上号页的代理来源状态。
 *
 * 初始来源必须由**策略**决定，不能只看库存。曾经写成 `autoAvailable ? 'auto' : 'manual'`，
 * 于是 auto_only 且池子为空时落到了 manual：页面一边显示「当前无法上号」，一边把自带
 * 代理的输入框露了出来。提交那步虽然被拦住，但界面自相矛盾。
 *
 * 后端 AssertProviderProxyModeAllowed 会独立地再拒一次，这里只负责别把界面渲染错。
 */
export function resolveProviderProxyModeState(
  policy: ProviderProxyModePolicy | undefined,
  autoProxyAvailable: boolean
): ProviderProxyModeState {
  const normalized: ProviderProxyModePolicy =
    policy === 'auto_only' || policy === 'manual_only' ? policy : 'both'

  const autoAllowed = normalized !== 'manual_only'
  const manualAllowed = normalized !== 'auto_only'
  const autoAvailable = autoAllowed && autoProxyAvailable

  let initialMode: ProviderProxyMode
  if (!manualAllowed) {
    // 只开放平台出口：即便当前没库存也停在 auto，页面显示「暂时无法上号」，
    // 而不是把一个策略根本不允许的输入框摆出来。
    initialMode = 'auto'
  } else if (!autoAllowed) {
    initialMode = 'manual'
  } else {
    initialMode = autoAvailable ? 'auto' : 'manual'
  }

  return {
    autoAllowed,
    manualAllowed,
    autoAvailable,
    blocked: !manualAllowed && !autoAvailable,
    showChoice: autoAllowed && manualAllowed,
    initialMode
  }
}
