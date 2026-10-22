/**
 * 账号「容量与节流」的唯一读写入口（纯 TS，无 Vue 依赖，仿同目录 credentialsBuilder.ts）。
 *
 * 规则：
 * - 所有容量相关 extra 键的字面量只允许出现在本文件。新建 / 编辑 / 批量三个弹窗只能调用
 *   这里的函数读写，不得自己拼键；accountCapacity.guard.spec.ts 会扫源码确认这一点。
 * - 数值 0 = 对应限制关闭（不限）。并发是账号列（accounts.concurrency），不在 extra 里。
 * - adaptive_throttle / nurturing_enabled 必须是严格 bool，始终写显式 true/false
 *   （后端对字符串 "true" 直接拒绝，因为按 bool 读取会把它静默当成 false）。
 * - 单账号保存是 PUT，extra 整体替换，所以「关闭」可以直接删键；
 *   批量更新是 JSONB 顶层合并、删不了键，所以关闭一律写显式 0 / false（见 *BulkPatch）。
 * - 档位是「模板，复制写入」：账号上不存档位名，标签由 4 个数值反推（matchPreset）。
 */
import type { CapacityPreset } from '@/api/admin/settings'

/** 与后端 account.go 的默认值保持一致：窗口费用粘性预留 10 美元，会话空闲超时 5 分钟。 */
export const DEFAULT_WINDOW_COST_STICKY_RESERVE = 10
export const DEFAULT_SESSION_IDLE_TIMEOUT_MINUTES = 5

/** write 与 bulk 函数涉及的全部 extra 键（capacityBuilder.spec.ts 断言它等于这些函数涉及键的并集）。 */
export const CAPACITY_EXTRA_KEYS = [
  'max_sessions',
  'session_idle_timeout_minutes',
  'window_cost_limit',
  'window_cost_sticky_reserve',
  'base_rpm',
  'rpm_strategy',
  'rpm_sticky_buffer',
  'user_msg_queue_mode',
  'user_msg_queue_enabled',
  'adaptive_throttle',
  'nurturing_enabled'
] as const

export type CapacityExtraKey = (typeof CAPACITY_EXTRA_KEYS)[number]

export type RpmStrategy = 'tiered' | 'sticky_exempt'

/** v-model.number 清空输入框得到 ''，所以数值输入的运行时类型比 number 宽，统一经 toPositive 归一。 */
export type NumberInput = number | string | null | undefined

/** 弹窗里完整的容量表单态。 */
export interface CapacityFormValues {
  concurrency: NumberInput
  maxSessions: NumberInput
  sessionIdleTimeout: NumberInput
  windowCostLimit: NumberInput
  windowCostStickyReserve: NumberInput
  baseRpm: NumberInput
  rpmStrategy: RpmStrategy
  rpmStickyBuffer: NumberInput
  userMsgQueueMode: string
  adaptiveThrottle: boolean
  nurturing: boolean
}

/** 档位套用与反推只关心的 5 项（缺省 / null 一律归一成 0 / false）。 */
export interface StoredCapacityView {
  concurrency: number
  maxSessions: number
  baseRpm: number
  windowCostLimit: number
  adaptiveThrottle: boolean
}

interface AccountLike {
  platform?: string | null
  type?: string | null
  concurrency?: number | null
  extra?: Record<string, unknown> | null
}

const MANAGED_TYPES: readonly string[] = ['oauth', 'setup-token']

// ========== 数值归一 ==========

/** 有限且大于 0 的数原样返回，其余（含 ''、NaN、负数、非数字字符串）一律为 0。 */
export function toPositive(value: unknown): number {
  let n = Number.NaN
  if (typeof value === 'number') {
    n = value
  } else if (typeof value === 'string' && value.trim() !== '') {
    n = Number(value)
  }
  return Number.isFinite(n) && n > 0 ? n : 0
}

function toPositiveInt(value: unknown): number {
  return Math.floor(toPositive(value))
}

// ========== 资格谓词 ==========

/** 容量与节流只对 Anthropic OAuth / SetupToken 账号生效（与后端 IsAnthropicOAuthOrSetupToken 一致）。 */
export function isCapacityManagedAccount(account: AccountLike | null | undefined): boolean {
  return !!account && account.platform === 'anthropic' && MANAGED_TYPES.includes(account.type ?? '')
}

/**
 * 批量编辑选中的账号是否全部受容量模型管理。
 * 注意 types 必须非空：空数组的 every() 恒为 true，会把「什么都没选」误判成合格。
 */
export function isCapacityManagedSelection(
  platforms: readonly string[],
  types: readonly string[]
): boolean {
  return (
    platforms.length === 1 &&
    platforms[0] === 'anthropic' &&
    types.length > 0 &&
    types.every((type) => MANAGED_TYPES.includes(type))
  )
}

/**
 * 「按筛选结果批量编辑」时，必须由筛选条件本身限定 platform=anthropic 且 type 为 oauth / setup-token。
 * 不能靠前 100 条预览推断：预览之外的账号可能是别的平台或类型。
 */
export function isCapacityManagedFilter(
  filters: { platform?: unknown; type?: unknown } | null | undefined
): boolean {
  return (
    !!filters &&
    filters.platform === 'anthropic' &&
    typeof filters.type === 'string' &&
    MANAGED_TYPES.includes(filters.type)
  )
}

// ========== 展示 ==========

/**
 * 档位下拉里的一行文案：名称 + 并发 / 会话数 / RPM / 5h 额度，0 = 不限。
 * 翻译函数由调用方传入，这里不依赖 vue-i18n；同时把模板行的键名挡在组件之外。
 */
export function presetOptionLabel(
  preset: CapacityPreset,
  t: (key: string, params?: Record<string, unknown>) => string
): string {
  const unlimited = t('admin.accounts.capacityFields.unlimited')
  const limit = (value: number) => (value > 0 ? String(value) : unlimited)
  const view = presetToValues(preset)
  return t('admin.accounts.capacityFields.presetOption', {
    label: preset.label,
    concurrency: view.concurrency,
    sessions: limit(view.maxSessions),
    rpm: limit(view.baseRpm),
    cost: view.windowCostLimit > 0 ? `$${view.windowCostLimit}` : unlimited
  })
}

// ========== 读取 ==========

/** 把任意输入归一成档位关心的 5 项。 */
export function storedViewFromValues(values: {
  concurrency?: NumberInput
  maxSessions?: NumberInput
  baseRpm?: NumberInput
  windowCostLimit?: NumberInput
  adaptiveThrottle?: unknown
}): StoredCapacityView {
  return {
    concurrency: toPositiveInt(values.concurrency),
    maxSessions: toPositiveInt(values.maxSessions),
    baseRpm: toPositiveInt(values.baseRpm),
    windowCostLimit: toPositive(values.windowCostLimit),
    adaptiveThrottle: values.adaptiveThrottle === true
  }
}

/** 服务端返回的模板行 → 规整后的模板行（缺字段 / 非数字按 0，label 缺省用 key）。 */
export function normalizePreset(raw: CapacityPreset): CapacityPreset {
  const key = String(raw.key)
  return {
    key,
    label: typeof raw.label === 'string' && raw.label !== '' ? raw.label : key,
    concurrency: Number(raw.concurrency) || 0,
    max_sessions: Number(raw.max_sessions) || 0,
    base_rpm: Number(raw.base_rpm) || 0,
    window_cost_limit: Number(raw.window_cost_limit) || 0,
    adaptive_throttle: raw.adaptive_throttle === true
  }
}

/** 模板行 → 5 项数值。 */
export function presetToValues(preset: CapacityPreset): StoredCapacityView {
  return storedViewFromValues({
    concurrency: preset.concurrency,
    maxSessions: preset.max_sessions,
    baseRpm: preset.base_rpm,
    windowCostLimit: preset.window_cost_limit,
    adaptiveThrottle: preset.adaptive_throttle
  })
}

/**
 * 账号 → 完整表单态。并发读账号列，其余读 extra 里的显式值
 * （不读 DTO 提升出来的字段：那些是后端算过的有效值，写回会把默认值固化进账号）。
 */
export function readCapacityFromAccount(account: AccountLike | null | undefined): CapacityFormValues {
  const extra = (account?.extra ?? {}) as Record<string, unknown>
  const mode = extra.user_msg_queue_mode
  const legacyEnabled = extra.user_msg_queue_enabled === true
  return {
    concurrency: account?.concurrency ?? 0,
    maxSessions: toPositiveInt(extra.max_sessions),
    sessionIdleTimeout: toPositiveInt(extra.session_idle_timeout_minutes) || null,
    windowCostLimit: toPositive(extra.window_cost_limit),
    windowCostStickyReserve: toPositive(extra.window_cost_sticky_reserve) || null,
    baseRpm: toPositiveInt(extra.base_rpm),
    rpmStrategy: extra.rpm_strategy === 'sticky_exempt' ? 'sticky_exempt' : 'tiered',
    rpmStickyBuffer: toPositiveInt(extra.rpm_sticky_buffer) || null,
    userMsgQueueMode:
      mode === 'throttle' || mode === 'serialize' ? mode : legacyEnabled ? 'serialize' : '',
    adaptiveThrottle: extra.adaptive_throttle === true,
    nurturing: extra.nurturing_enabled === true
  }
}

/** 账号 → 档位关心的 5 项（供列表标签反推使用）。 */
export function storedViewFromAccount(account: AccountLike | null | undefined): StoredCapacityView {
  return storedViewFromValues(readCapacityFromAccount(account))
}

/**
 * 用 4 个数值（并发、会话数、RPM、5h 额度）反推档位 key，不含 adaptive_throttle，
 * 否则单独切一下动态降速开关标签就会变成「自定义」。重复元组取第一个；没有匹配返回 null。
 * 必须与后端 providerTierMatchesAccount 同口径。
 */
export function matchPreset(
  values: {
    concurrency?: NumberInput
    maxSessions?: NumberInput
    baseRpm?: NumberInput
    windowCostLimit?: NumberInput
  },
  presets: readonly CapacityPreset[]
): string | null {
  const view = storedViewFromValues(values)
  for (const preset of presets) {
    const p = presetToValues(preset)
    if (
      p.concurrency === view.concurrency &&
      p.maxSessions === view.maxSessions &&
      p.baseRpm === view.baseRpm &&
      p.windowCostLimit === view.windowCostLimit
    ) {
      return preset.key
    }
  }
  return null
}

// ========== 写入（单账号：新建 POST / 编辑 PUT） ==========

/**
 * 把容量表单态写进 extra（原地修改并返回）。
 * 数值 > 0 才写入，否则删键（PUT 整体替换，删键就是关闭；新建时是空操作），
 * 不属于容量的键原样保留。动态降速与养号始终写显式 bool。
 */
export function writeCapacityToExtra(
  extra: Record<string, unknown>,
  values: CapacityFormValues
): Record<string, unknown> {
  const maxSessions = toPositiveInt(values.maxSessions)
  if (maxSessions > 0) {
    extra.max_sessions = maxSessions
    extra.session_idle_timeout_minutes =
      toPositiveInt(values.sessionIdleTimeout) || DEFAULT_SESSION_IDLE_TIMEOUT_MINUTES
  } else {
    delete extra.max_sessions
    delete extra.session_idle_timeout_minutes
  }

  const windowCostLimit = toPositive(values.windowCostLimit)
  if (windowCostLimit > 0) {
    extra.window_cost_limit = windowCostLimit
    extra.window_cost_sticky_reserve =
      toPositive(values.windowCostStickyReserve) || DEFAULT_WINDOW_COST_STICKY_RESERVE
  } else {
    delete extra.window_cost_limit
    delete extra.window_cost_sticky_reserve
  }

  const baseRpm = toPositiveInt(values.baseRpm)
  if (baseRpm > 0) {
    const strategy: RpmStrategy = values.rpmStrategy === 'sticky_exempt' ? 'sticky_exempt' : 'tiered'
    extra.base_rpm = baseRpm
    extra.rpm_strategy = strategy
    const stickyBuffer = toPositiveInt(values.rpmStickyBuffer)
    if (strategy === 'tiered' && stickyBuffer > 0) {
      extra.rpm_sticky_buffer = stickyBuffer
    } else {
      delete extra.rpm_sticky_buffer
    }
  } else {
    delete extra.base_rpm
    delete extra.rpm_strategy
    delete extra.rpm_sticky_buffer
  }

  if (values.userMsgQueueMode === 'throttle' || values.userMsgQueueMode === 'serialize') {
    extra.user_msg_queue_mode = values.userMsgQueueMode
  } else {
    delete extra.user_msg_queue_mode
  }
  // 旧版开关，已被 user_msg_queue_mode 取代，保存时顺手清掉
  delete extra.user_msg_queue_enabled

  extra.adaptive_throttle = values.adaptiveThrottle === true
  extra.nurturing_enabled = values.nurturing === true

  return extra
}

// ========== 写入（批量：JSONB 顶层合并，只能增改，关闭一律写显式 0 / false） ==========

/** 套用档位的批量补丁：只写 5 项，0 = 关闭，其余键（策略、缓冲、预留、空闲超时）不动。 */
export function capacityBulkPatch(view: StoredCapacityView): {
  concurrency: number
  extra: Record<string, unknown>
} {
  return {
    concurrency: view.concurrency,
    extra: {
      max_sessions: view.maxSessions,
      base_rpm: view.baseRpm,
      window_cost_limit: view.windowCostLimit,
      adaptive_throttle: view.adaptiveThrottle
    }
  }
}

/** 批量 RPM 补丁：RPM > 0 写入策略与缓冲；0 / 留空 = 关闭，用空值覆盖关联字段（合并语义删不了键）。 */
export function rpmBulkPatch(input: {
  baseRpm: NumberInput
  strategy: RpmStrategy
  stickyBuffer: NumberInput
}): Record<string, unknown> {
  const baseRpm = toPositiveInt(input.baseRpm)
  if (baseRpm <= 0) {
    return { base_rpm: 0, rpm_strategy: '', rpm_sticky_buffer: 0 }
  }
  const patch: Record<string, unknown> = { base_rpm: baseRpm, rpm_strategy: input.strategy }
  const stickyBuffer = toPositiveInt(input.stickyBuffer)
  if (stickyBuffer > 0) {
    patch.rpm_sticky_buffer = stickyBuffer
  }
  return patch
}

/** 批量用户消息限速补丁：null = 本次不改；'' = 清除账号级覆盖。 */
export function umqBulkPatch(mode: string | null): Record<string, unknown> {
  if (mode === null) return {}
  return { user_msg_queue_mode: mode, user_msg_queue_enabled: false }
}
