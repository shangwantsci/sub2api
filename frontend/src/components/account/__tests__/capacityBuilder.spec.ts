import { describe, expect, it } from 'vitest'

import type { CapacityPreset } from '@/api/admin/settings'
import {
  CAPACITY_EXTRA_KEYS,
  DEFAULT_SESSION_IDLE_TIMEOUT_MINUTES,
  DEFAULT_WINDOW_COST_STICKY_RESERVE,
  capacityBulkPatch,
  isCapacityManagedAccount,
  isCapacityManagedFilter,
  isCapacityManagedSelection,
  matchPreset,
  normalizePreset,
  presetOptionLabel,
  presetToValues,
  readCapacityFromAccount,
  rpmBulkPatch,
  storedViewFromAccount,
  storedViewFromValues,
  toPositive,
  umqBulkPatch,
  writeCapacityToExtra,
  type CapacityFormValues
} from '../capacityBuilder'

// 5 个种子模板（与后端 DefaultCapacityPresets 一致），期望值全部写字面量。
const SEED: CapacityPreset[] = [
  { key: '1', label: '入门', concurrency: 1, max_sessions: 1, base_rpm: 10, window_cost_limit: 20, adaptive_throttle: true },
  { key: '2', label: '保守', concurrency: 2, max_sessions: 2, base_rpm: 20, window_cost_limit: 40, adaptive_throttle: true },
  { key: '3', label: '均衡', concurrency: 3, max_sessions: 3, base_rpm: 30, window_cost_limit: 60, adaptive_throttle: true },
  { key: '4', label: '激进', concurrency: 5, max_sessions: 5, base_rpm: 50, window_cost_limit: 100, adaptive_throttle: true },
  { key: '5', label: '旗舰', concurrency: 8, max_sessions: 8, base_rpm: 80, window_cost_limit: 0, adaptive_throttle: true }
]

function formValues(overrides: Partial<CapacityFormValues> = {}): CapacityFormValues {
  return {
    concurrency: 3,
    maxSessions: 0,
    sessionIdleTimeout: null,
    windowCostLimit: 0,
    windowCostStickyReserve: null,
    baseRpm: 0,
    rpmStrategy: 'tiered',
    rpmStickyBuffer: null,
    userMsgQueueMode: '',
    adaptiveThrottle: false,
    nurturing: false,
    ...overrides
  }
}

const ALL_ON = formValues({
  concurrency: 5,
  maxSessions: 4,
  sessionIdleTimeout: 7,
  windowCostLimit: 80,
  windowCostStickyReserve: 12,
  baseRpm: 40,
  rpmStrategy: 'tiered',
  rpmStickyBuffer: 6,
  userMsgQueueMode: 'throttle',
  adaptiveThrottle: true,
  nurturing: true
})

describe('toPositive', () => {
  it.each([
    [5, 5],
    [2.5, 2.5],
    ['7', 7],
    [' 7 ', 7],
    ['', 0],
    ['   ', 0],
    ['abc', 0],
    [0, 0],
    [-3, 0],
    [Number.NaN, 0],
    [Number.POSITIVE_INFINITY, 0],
    [null, 0],
    [undefined, 0],
    [true, 0]
  ])('toPositive(%j) = %j（v-model.number 清空会得到空串，必须按 0 处理）', (input, expected) => {
    expect(toPositive(input)).toBe(expected)
  })
})

describe('资格谓词真值表', () => {
  it.each([
    ['anthropic', 'oauth', true],
    ['anthropic', 'setup-token', true],
    ['anthropic', 'apikey', false],
    ['anthropic', 'bedrock', false],
    ['anthropic', 'service_account', false],
    ['openai', 'oauth', false],
    ['gemini', 'oauth', false],
    ['antigravity', 'oauth', false]
  ])('isCapacityManagedAccount(%s/%s) = %s', (platform, type, expected) => {
    expect(isCapacityManagedAccount({ platform, type })).toBe(expected)
  })

  it('空账号 / 缺字段一律不受管理', () => {
    expect(isCapacityManagedAccount(null)).toBe(false)
    expect(isCapacityManagedAccount(undefined)).toBe(false)
    expect(isCapacityManagedAccount({})).toBe(false)
    expect(isCapacityManagedAccount({ platform: 'anthropic' })).toBe(false)
  })

  it.each([
    [['anthropic'], ['oauth'], true],
    [['anthropic'], ['setup-token'], true],
    [['anthropic'], ['oauth', 'setup-token'], true],
    // types 为空：every() 对空数组恒为 true，必须被显式挡掉
    [['anthropic'], [], false],
    [['anthropic'], ['oauth', 'apikey'], false],
    [['anthropic'], ['apikey'], false],
    [['anthropic'], ['bedrock'], false],
    [['anthropic', 'openai'], ['oauth'], false],
    [['openai'], ['oauth'], false],
    [[], ['oauth'], false],
    [[], [], false]
  ])('isCapacityManagedSelection(%j, %j) = %s', (platforms, types, expected) => {
    expect(isCapacityManagedSelection(platforms, types)).toBe(expected)
  })

  it.each([
    [{ platform: 'anthropic', type: 'oauth' }, true],
    [{ platform: 'anthropic', type: 'setup-token' }, true],
    // 筛选条件没限定类型：结果集里可能混着 apikey，预览前 100 条看起来全是 oauth 也不能放行
    [{ platform: 'anthropic', type: '' }, false],
    [{ platform: 'anthropic' }, false],
    [{ platform: 'anthropic', type: 'apikey' }, false],
    [{ platform: '', type: 'oauth' }, false],
    [{ platform: 'openai', type: 'oauth' }, false],
    [{}, false],
    [undefined, false],
    [null, false]
  ])('isCapacityManagedFilter(%j) = %s', (filters, expected) => {
    expect(isCapacityManagedFilter(filters)).toBe(expected)
  })
})

describe('normalizePreset / presetToValues', () => {
  it('规整脏数据：数字字段缺失或非法按 0，label 缺省用 key，adaptive 只认严格 true', () => {
    expect(
      normalizePreset({
        key: 7 as never,
        label: '',
        concurrency: '4' as never,
        max_sessions: undefined as never,
        base_rpm: 'x' as never,
        window_cost_limit: null as never,
        adaptive_throttle: 'true' as never
      })
    ).toEqual({
      key: '7',
      label: '7',
      concurrency: 4,
      max_sessions: 0,
      base_rpm: 0,
      window_cost_limit: 0,
      adaptive_throttle: false
    })
  })

  it('合法数据原样保留', () => {
    expect(normalizePreset(SEED[3])).toEqual(SEED[3])
  })

  it('presetToValues 把模板行的 5 项复制成账号侧的 5 项', () => {
    expect(presetToValues(SEED[4])).toEqual({
      concurrency: 8,
      maxSessions: 8,
      baseRpm: 80,
      windowCostLimit: 0,
      adaptiveThrottle: true
    })
  })
})

describe('presetOptionLabel', () => {
  // 翻译函数把 key 与参数原样拼出来，便于断言传了什么
  const t = (key: string, params?: Record<string, unknown>) =>
    params ? `${key}::${JSON.stringify(params)}` : key

  it('带上名称与 4 个数值，费用加美元符号', () => {
    expect(presetOptionLabel(SEED[2], t)).toBe(
      'admin.accounts.capacityFields.presetOption::' +
        JSON.stringify({ label: '均衡', concurrency: 3, sessions: '3', rpm: '30', cost: '$60' })
    )
  })

  it('0 显示为「不限」（会话数、RPM、5h 额度）', () => {
    expect(presetOptionLabel({ ...SEED[4], max_sessions: 0, base_rpm: 0 }, t)).toBe(
      'admin.accounts.capacityFields.presetOption::' +
        JSON.stringify({
          label: '旗舰',
          concurrency: 8,
          sessions: 'admin.accounts.capacityFields.unlimited',
          rpm: 'admin.accounts.capacityFields.unlimited',
          cost: 'admin.accounts.capacityFields.unlimited'
        })
    )
  })
})

describe('storedViewFromValues', () => {
  it('缺省 / null / 空串 / 非法值归一成 0 与 false', () => {
    expect(storedViewFromValues({})).toEqual({
      concurrency: 0,
      maxSessions: 0,
      baseRpm: 0,
      windowCostLimit: 0,
      adaptiveThrottle: false
    })
    expect(
      storedViewFromValues({
        concurrency: null,
        maxSessions: '',
        baseRpm: 'x',
        windowCostLimit: undefined,
        adaptiveThrottle: 'true'
      })
    ).toEqual({ concurrency: 0, maxSessions: 0, baseRpm: 0, windowCostLimit: 0, adaptiveThrottle: false })
  })

  it('字符串数字按数字处理；整数项向下取整，费用保留小数；adaptive 只认严格 true', () => {
    expect(
      storedViewFromValues({
        concurrency: '3',
        maxSessions: 2.9,
        baseRpm: '30',
        windowCostLimit: '60.5',
        adaptiveThrottle: true
      })
    ).toEqual({ concurrency: 3, maxSessions: 2, baseRpm: 30, windowCostLimit: 60.5, adaptiveThrottle: true })
  })
})

describe('readCapacityFromAccount / storedViewFromAccount', () => {
  it('并发读账号列，其余读 extra 里的显式值', () => {
    const account = {
      platform: 'anthropic',
      type: 'oauth',
      concurrency: 5,
      extra: {
        max_sessions: 5,
        session_idle_timeout_minutes: 9,
        window_cost_limit: 100,
        window_cost_sticky_reserve: 15,
        base_rpm: 50,
        rpm_strategy: 'sticky_exempt',
        rpm_sticky_buffer: 8,
        user_msg_queue_mode: 'serialize',
        adaptive_throttle: true,
        nurturing_enabled: true,
        org_uuid: 'org-1'
      }
    }
    expect(readCapacityFromAccount(account)).toEqual({
      concurrency: 5,
      maxSessions: 5,
      sessionIdleTimeout: 9,
      windowCostLimit: 100,
      windowCostStickyReserve: 15,
      baseRpm: 50,
      rpmStrategy: 'sticky_exempt',
      rpmStickyBuffer: 8,
      userMsgQueueMode: 'serialize',
      adaptiveThrottle: true,
      nurturing: true
    })
    expect(storedViewFromAccount(account)).toEqual({
      concurrency: 5,
      maxSessions: 5,
      baseRpm: 50,
      windowCostLimit: 100,
      adaptiveThrottle: true
    })
  })

  it('extra 里没有的键：限制类归 0，可选类归 null，开关类归 false', () => {
    expect(readCapacityFromAccount({ concurrency: 2, extra: {} })).toEqual({
      concurrency: 2,
      maxSessions: 0,
      sessionIdleTimeout: null,
      windowCostLimit: 0,
      windowCostStickyReserve: null,
      baseRpm: 0,
      rpmStrategy: 'tiered',
      rpmStickyBuffer: null,
      userMsgQueueMode: '',
      adaptiveThrottle: false,
      nurturing: false
    })
    expect(readCapacityFromAccount(null).concurrency).toBe(0)
  })

  it('adaptive_throttle / nurturing_enabled 只认严格 true，字符串 "true" 不算', () => {
    const view = readCapacityFromAccount({
      concurrency: 1,
      extra: { adaptive_throttle: 'true', nurturing_enabled: 1 }
    })
    expect(view.adaptiveThrottle).toBe(false)
    expect(view.nurturing).toBe(false)
  })

  it('不读 DTO 提升出来的有效值字段，避免把默认值固化进账号', () => {
    const view = readCapacityFromAccount({
      concurrency: 1,
      extra: {},
      // @ts-expect-error 刻意塞一个 DTO 提升字段，确认它被忽略
      base_rpm: 99
    })
    expect(view.baseRpm).toBe(0)
  })

  it('兼容旧版 user_msg_queue_enabled=true 读成串行队列，非法模式视为未设置', () => {
    expect(
      readCapacityFromAccount({ concurrency: 1, extra: { user_msg_queue_enabled: true } }).userMsgQueueMode
    ).toBe('serialize')
    expect(
      readCapacityFromAccount({ concurrency: 1, extra: { user_msg_queue_mode: 'bogus' } }).userMsgQueueMode
    ).toBe('')
  })
})

describe('matchPreset：只比 4 个数值，重复元组取第一个', () => {
  it('每个种子档位都能被自己的数值反推出来', () => {
    for (const preset of SEED) {
      expect(
        matchPreset(
          {
            concurrency: preset.concurrency,
            maxSessions: preset.max_sessions,
            baseRpm: preset.base_rpm,
            windowCostLimit: preset.window_cost_limit
          },
          SEED
        )
      ).toBe(preset.key)
    }
  })

  it('任意一个数值对不上就是 null（自定义）', () => {
    const base = { concurrency: 3, maxSessions: 3, baseRpm: 30, windowCostLimit: 60 }
    expect(matchPreset(base, SEED)).toBe('3')
    expect(matchPreset({ ...base, concurrency: 4 }, SEED)).toBeNull()
    expect(matchPreset({ ...base, maxSessions: 4 }, SEED)).toBeNull()
    expect(matchPreset({ ...base, baseRpm: 31 }, SEED)).toBeNull()
    expect(matchPreset({ ...base, windowCostLimit: 61 }, SEED)).toBeNull()
  })

  it('不含 adaptive_throttle：单独切动态降速开关不会让标签变成自定义', () => {
    const base = { concurrency: 3, maxSessions: 3, baseRpm: 30, windowCostLimit: 60 }
    expect(matchPreset({ ...base, adaptiveThrottle: false } as never, SEED)).toBe('3')
    const adaptiveOffSeed = SEED.map((p) => ({ ...p, adaptive_throttle: false }))
    expect(matchPreset(base, adaptiveOffSeed)).toBe('3')
  })

  it('重复元组取第一个', () => {
    const dup: CapacityPreset[] = [
      { ...SEED[2], key: 'b', label: '后出现' },
      { ...SEED[2], key: 'a', label: '先出现' }
    ]
    // 数组顺序决定先后，而不是 key 的字典序
    expect(matchPreset({ concurrency: 3, maxSessions: 3, baseRpm: 30, windowCostLimit: 60 }, dup)).toBe('b')
  })

  it('旗舰档 5h 额度 0 = 不限，空串 / null 也按 0 参与比较', () => {
    expect(matchPreset({ concurrency: 8, maxSessions: 8, baseRpm: 80, windowCostLimit: 0 }, SEED)).toBe('5')
    expect(matchPreset({ concurrency: 8, maxSessions: 8, baseRpm: 80, windowCostLimit: '' }, SEED)).toBe('5')
    expect(matchPreset({ concurrency: 8, maxSessions: 8, baseRpm: 80, windowCostLimit: null }, SEED)).toBe('5')
  })

  it('没有模板时返回 null（调用方必须区分「没加载」与「自定义」，这里只给出 null）', () => {
    expect(matchPreset({ concurrency: 3, maxSessions: 3, baseRpm: 30, windowCostLimit: 60 }, [])).toBeNull()
  })
})

describe('5 个种子模板往返还原', () => {
  it.each(SEED)('模板 $key（$label）：presetToValues → write → 读回 → matchPreset 还原自身', (preset) => {
    const view = presetToValues(preset)
    // 复制写入：并发在账号列，其余 4 项经 writeCapacityToExtra 进 extra
    const extra = writeCapacityToExtra(
      {},
      formValues({
        concurrency: view.concurrency,
        maxSessions: view.maxSessions,
        baseRpm: view.baseRpm,
        windowCostLimit: view.windowCostLimit,
        adaptiveThrottle: view.adaptiveThrottle
      })
    )
    const account = { platform: 'anthropic', type: 'oauth', concurrency: view.concurrency, extra }

    expect(storedViewFromAccount(account)).toEqual(view)
    expect(matchPreset(storedViewFromAccount(account), SEED)).toBe(preset.key)
    expect(extra.adaptive_throttle).toBe(true)
  })
})

describe('writeCapacityToExtra', () => {
  it('全部开启：写入所有容量键，值与输入一致，并写显式 bool', () => {
    expect(writeCapacityToExtra({}, ALL_ON)).toEqual({
      max_sessions: 4,
      session_idle_timeout_minutes: 7,
      window_cost_limit: 80,
      window_cost_sticky_reserve: 12,
      base_rpm: 40,
      rpm_strategy: 'tiered',
      rpm_sticky_buffer: 6,
      user_msg_queue_mode: 'throttle',
      adaptive_throttle: true,
      nurturing_enabled: true
    })
  })

  it('写入 / 删除对称：全部开启后再全部关闭，数值键被删光，只剩两个显式 false', () => {
    const extra = writeCapacityToExtra({}, ALL_ON)
    writeCapacityToExtra(extra, formValues({ concurrency: 3 }))
    expect(extra).toEqual({ adaptive_throttle: false, nurturing_enabled: false })
  })

  it('不属于容量的键原样保留（包括旧版遗留键）', () => {
    const extra = writeCapacityToExtra(
      { org_uuid: 'org-1', enable_tls_fingerprint: true, mixed_scheduling: false, custom_flag: 7 },
      ALL_ON
    )
    expect(extra.org_uuid).toBe('org-1')
    expect(extra.enable_tls_fingerprint).toBe(true)
    expect(extra.mixed_scheduling).toBe(false)
    expect(extra.custom_flag).toBe(7)
  })

  it('adaptive_throttle 与 nurturing_enabled 始终是显式 bool，关闭也写 false', () => {
    const off = writeCapacityToExtra({}, formValues())
    expect(off.adaptive_throttle).toBe(false)
    expect(off.nurturing_enabled).toBe(false)
    expect(typeof off.adaptive_throttle).toBe('boolean')

    const on = writeCapacityToExtra({}, formValues({ adaptiveThrottle: true, nurturing: true }))
    expect(on.adaptive_throttle).toBe(true)
    expect(on.nurturing_enabled).toBe(true)
  })

  it('空串 / 非法输入（v-model.number 清空）等同于 0 = 关闭，不会写出 ""', () => {
    const extra = writeCapacityToExtra(
      {},
      formValues({
        maxSessions: '',
        windowCostLimit: 'abc',
        baseRpm: null,
        sessionIdleTimeout: '',
        windowCostStickyReserve: '',
        rpmStickyBuffer: ''
      })
    )
    expect(extra).toEqual({ adaptive_throttle: false, nurturing_enabled: false })
  })

  it('RPM 留空不再静默写 15：没有任何 base_rpm 就没有 RPM 键', () => {
    const extra = writeCapacityToExtra({}, formValues({ baseRpm: '' }))
    expect('base_rpm' in extra).toBe(false)
    expect('rpm_strategy' in extra).toBe(false)
    expect(Object.values(extra)).not.toContain(15)
  })

  it('会话数 > 0 但没填空闲超时 / 窗口费用 > 0 但没填预留：写后端同款默认值 5 与 10', () => {
    const extra = writeCapacityToExtra({}, formValues({ maxSessions: 3, windowCostLimit: 60 }))
    expect(extra.session_idle_timeout_minutes).toBe(DEFAULT_SESSION_IDLE_TIMEOUT_MINUTES)
    expect(extra.window_cost_sticky_reserve).toBe(DEFAULT_WINDOW_COST_STICKY_RESERVE)
    expect(DEFAULT_SESSION_IDLE_TIMEOUT_MINUTES).toBe(5)
    expect(DEFAULT_WINDOW_COST_STICKY_RESERVE).toBe(10)
  })

  it('会话数 / 窗口费用关闭时，从属的空闲超时与预留一起删除', () => {
    const extra = writeCapacityToExtra(
      { session_idle_timeout_minutes: 9, window_cost_sticky_reserve: 15, max_sessions: 3, window_cost_limit: 60 },
      formValues()
    )
    expect('session_idle_timeout_minutes' in extra).toBe(false)
    expect('window_cost_sticky_reserve' in extra).toBe(false)
    expect('max_sessions' in extra).toBe(false)
    expect('window_cost_limit' in extra).toBe(false)
  })

  it('RPM 策略：粘性豁免不带缓冲；三区才写缓冲；RPM 关闭时策略与缓冲一起删', () => {
    const exempt = writeCapacityToExtra(
      { rpm_sticky_buffer: 9 },
      formValues({ baseRpm: 30, rpmStrategy: 'sticky_exempt', rpmStickyBuffer: 6 })
    )
    expect(exempt.rpm_strategy).toBe('sticky_exempt')
    expect('rpm_sticky_buffer' in exempt).toBe(false)

    const tiered = writeCapacityToExtra({}, formValues({ baseRpm: 30, rpmStrategy: 'tiered', rpmStickyBuffer: 6 }))
    expect(tiered.rpm_sticky_buffer).toBe(6)

    const tieredNoBuffer = writeCapacityToExtra(
      { rpm_sticky_buffer: 9 },
      formValues({ baseRpm: 30, rpmStrategy: 'tiered', rpmStickyBuffer: null })
    )
    expect('rpm_sticky_buffer' in tieredNoBuffer).toBe(false)

    const off = writeCapacityToExtra(
      { base_rpm: 30, rpm_strategy: 'tiered', rpm_sticky_buffer: 6 },
      formValues({ baseRpm: 0 })
    )
    expect('base_rpm' in off).toBe(false)
    expect('rpm_strategy' in off).toBe(false)
    expect('rpm_sticky_buffer' in off).toBe(false)
  })

  it('整数项取整，非法 RPM 策略回落为三区', () => {
    const extra = writeCapacityToExtra(
      {},
      formValues({ maxSessions: 3.9, baseRpm: 30.7, rpmStrategy: 'weird' as never })
    )
    expect(extra.max_sessions).toBe(3)
    expect(extra.base_rpm).toBe(30)
    expect(extra.rpm_strategy).toBe('tiered')
  })

  it('用户消息限速：只认 throttle / serialize，其余删键，并清掉旧版 user_msg_queue_enabled', () => {
    const serialize = writeCapacityToExtra({ user_msg_queue_enabled: true }, formValues({ userMsgQueueMode: 'serialize' }))
    expect(serialize.user_msg_queue_mode).toBe('serialize')
    expect('user_msg_queue_enabled' in serialize).toBe(false)

    const off = writeCapacityToExtra({ user_msg_queue_mode: 'throttle' }, formValues({ userMsgQueueMode: '' }))
    expect('user_msg_queue_mode' in off).toBe(false)

    const bogus = writeCapacityToExtra({}, formValues({ userMsgQueueMode: 'bogus' }))
    expect('user_msg_queue_mode' in bogus).toBe(false)
  })

  it('原地修改并返回同一个对象', () => {
    const extra: Record<string, unknown> = {}
    expect(writeCapacityToExtra(extra, ALL_ON)).toBe(extra)
  })

  it('永远不写任何 persona 开头的键', () => {
    const extra = writeCapacityToExtra({}, ALL_ON)
    expect(Object.keys(extra).filter((key) => key.startsWith('persona'))).toEqual([])
  })
})

describe('批量补丁', () => {
  it('capacityBulkPatch：套用档位只写 5 项，0 = 关闭显式写 0', () => {
    expect(capacityBulkPatch(presetToValues(SEED[3]))).toEqual({
      concurrency: 5,
      extra: { max_sessions: 5, base_rpm: 50, window_cost_limit: 100, adaptive_throttle: true }
    })
    expect(capacityBulkPatch(presetToValues(SEED[4]))).toEqual({
      concurrency: 8,
      extra: { max_sessions: 8, base_rpm: 80, window_cost_limit: 0, adaptive_throttle: true }
    })
  })

  it('rpmBulkPatch：开启时写 RPM 与策略，缓冲留空就不写；关闭 / 留空 / 0 写显式空值', () => {
    expect(rpmBulkPatch({ baseRpm: 40, strategy: 'tiered', stickyBuffer: null })).toEqual({
      base_rpm: 40,
      rpm_strategy: 'tiered'
    })
    expect(rpmBulkPatch({ baseRpm: 40, strategy: 'sticky_exempt', stickyBuffer: 6 })).toEqual({
      base_rpm: 40,
      rpm_strategy: 'sticky_exempt',
      rpm_sticky_buffer: 6
    })
    const off = { base_rpm: 0, rpm_strategy: '', rpm_sticky_buffer: 0 }
    expect(rpmBulkPatch({ baseRpm: 0, strategy: 'tiered', stickyBuffer: 6 })).toEqual(off)
    expect(rpmBulkPatch({ baseRpm: '', strategy: 'tiered', stickyBuffer: null })).toEqual(off)
    expect(rpmBulkPatch({ baseRpm: null, strategy: 'tiered', stickyBuffer: null })).toEqual(off)
  })

  it('umqBulkPatch：null 本次不改，空串清除账号级覆盖，并清理旧开关', () => {
    expect(umqBulkPatch(null)).toEqual({})
    expect(umqBulkPatch('')).toEqual({ user_msg_queue_mode: '', user_msg_queue_enabled: false })
    expect(umqBulkPatch('serialize')).toEqual({ user_msg_queue_mode: 'serialize', user_msg_queue_enabled: false })
  })
})

describe('CAPACITY_EXTRA_KEYS 与实际读写涉及的键一致', () => {
  /** 记录一个 extra 对象上被 set / delete 过的所有键。 */
  function spyExtra(): { extra: Record<string, unknown>; touched: Set<string> } {
    const touched = new Set<string>()
    const extra = new Proxy({} as Record<string, unknown>, {
      set(target, prop, value) {
        touched.add(String(prop))
        return Reflect.set(target, prop, value)
      },
      deleteProperty(target, prop) {
        touched.add(String(prop))
        return Reflect.deleteProperty(target, prop)
      }
    })
    return { extra, touched }
  }

  it('等于 write 函数（开 / 关 / 粘性豁免）与三个 bulk 函数涉及键的并集', () => {
    const touched = new Set<string>()
    const scenarios = [
      ALL_ON,
      formValues(),
      formValues({ baseRpm: 30, rpmStrategy: 'sticky_exempt' }),
      formValues({ userMsgQueueMode: 'serialize' })
    ]
    for (const scenario of scenarios) {
      const spy = spyExtra()
      writeCapacityToExtra(spy.extra, scenario)
      spy.touched.forEach((key) => touched.add(key))
    }

    const bulkPatches = [
      capacityBulkPatch(presetToValues(SEED[2])).extra,
      rpmBulkPatch({ baseRpm: 30, strategy: 'tiered', stickyBuffer: 6 }),
      rpmBulkPatch({ baseRpm: 0, strategy: 'tiered', stickyBuffer: null }),
      umqBulkPatch('serialize'),
      umqBulkPatch('')
    ]
    for (const patch of bulkPatches) {
      Object.keys(patch).forEach((key) => touched.add(key))
    }

    expect([...touched].sort()).toEqual([...CAPACITY_EXTRA_KEYS].sort())
  })

  it('清单里没有重复键，也没有 persona 键', () => {
    expect(new Set(CAPACITY_EXTRA_KEYS).size).toBe(CAPACITY_EXTRA_KEYS.length)
    expect(CAPACITY_EXTRA_KEYS.filter((key) => key.startsWith('persona'))).toEqual([])
  })
})
