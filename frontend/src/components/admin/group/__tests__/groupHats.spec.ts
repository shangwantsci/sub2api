import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import type { ClaudeOAuthSystemPromptPolicy } from '@/types'
import {
  HAT_IDS,
  HAT_META,
  applyHat,
  deriveHat,
  describeHat,
  hasFallback,
  normalizePolicy,
  type HatFields,
  type HatId,
  type HatInput
} from '../groupHats'

/**
 * 帽子是 claude_code_only / fallback_group_id / claude_oauth_system_prompt_policy 三字段的完整划分。
 * 这里的期望值全部写字面量，不用被测函数反算；6 顶帽子的清单也在本文件里再写一遍，
 * 免得被测模块的 HAT_IDS 缩水时测试跟着缩水。
 */

const ALL_HATS: HatId[] = ['cc_line', 'cc_fallback', 'open_follow', 'open_full', 'open_light', 'open_keep_prompt']

// 开放类四顶与注入策略的对应关系
const OPEN_HATS: Array<[HatId, ClaudeOAuthSystemPromptPolicy]> = [
  ['open_follow', 'inherit'],
  ['open_full', 'enabled'],
  ['open_light', 'identity_only'],
  ['open_keep_prompt', 'disabled']
]

// claude_code_only × 降级分组 × 注入策略 共 16 种组合，每一行期望的帽子都是字面量
const TRUTH_TABLE: Array<[boolean, number | null, ClaudeOAuthSystemPromptPolicy, HatId]> = [
  [true, null, 'inherit', 'cc_line'],
  [true, null, 'enabled', 'cc_line'],
  [true, null, 'identity_only', 'cc_line'],
  [true, null, 'disabled', 'cc_line'],
  [true, 7, 'inherit', 'cc_fallback'],
  [true, 7, 'enabled', 'cc_fallback'],
  [true, 7, 'identity_only', 'cc_fallback'],
  [true, 7, 'disabled', 'cc_fallback'],
  [false, null, 'inherit', 'open_follow'],
  [false, 7, 'inherit', 'open_follow'],
  [false, null, 'enabled', 'open_full'],
  [false, 7, 'enabled', 'open_full'],
  [false, null, 'identity_only', 'open_light'],
  [false, 7, 'identity_only', 'open_light'],
  [false, null, 'disabled', 'open_keep_prompt'],
  [false, 7, 'disabled', 'open_keep_prompt']
]

const DEFAULT_FIELDS: HatFields = {
  claude_code_only: false,
  fallback_group_id: null,
  claude_oauth_system_prompt_policy: 'inherit'
}

interface Start {
  name: string
  input: HatInput | null | undefined
  /** 这个起点归一后的三字段（字面量） */
  normalized: HatFields
}

// applyHat / 往返测试共用的起点：全缺省、各种合法组合、残留组合、脏数据
const STARTS: Start[] = [
  { name: 'undefined', input: undefined, normalized: DEFAULT_FIELDS },
  { name: 'null', input: null, normalized: DEFAULT_FIELDS },
  { name: '空对象', input: {}, normalized: DEFAULT_FIELDS },
  {
    name: '开放·完整 + 残留降级分组 9',
    input: { claude_code_only: false, fallback_group_id: 9, claude_oauth_system_prompt_policy: 'enabled' },
    normalized: { claude_code_only: false, fallback_group_id: 9, claude_oauth_system_prompt_policy: 'enabled' }
  },
  {
    name: 'CC 专线 + 隐藏策略 disabled',
    input: { claude_code_only: true, fallback_group_id: null, claude_oauth_system_prompt_policy: 'disabled' },
    normalized: { claude_code_only: true, fallback_group_id: null, claude_oauth_system_prompt_policy: 'disabled' }
  },
  {
    name: 'CC 降级池(9) + 策略 identity_only',
    input: { claude_code_only: true, fallback_group_id: 9, claude_oauth_system_prompt_policy: 'identity_only' },
    normalized: { claude_code_only: true, fallback_group_id: 9, claude_oauth_system_prompt_policy: 'identity_only' }
  },
  {
    name: '脏数据：CC 是字符串 true、降级分组 0、未知策略',
    input: { claude_code_only: 'true' as never, fallback_group_id: 0, claude_oauth_system_prompt_policy: 'bogus' },
    normalized: { claude_code_only: false, fallback_group_id: null, claude_oauth_system_prompt_policy: 'inherit' }
  },
  {
    name: '脏数据：CC 真、降级分组 -2、策略 null',
    input: { claude_code_only: true, fallback_group_id: -2, claude_oauth_system_prompt_policy: null },
    normalized: { claude_code_only: true, fallback_group_id: null, claude_oauth_system_prompt_policy: 'inherit' }
  }
]

/** 任何写入都会抛错的只读代理：不依赖严格模式，也能抓到对入参的修改。 */
function readOnly<T extends object>(value: T): T {
  return new Proxy(value, {
    set() {
      throw new Error('入参被写入')
    },
    deleteProperty() {
      throw new Error('入参被删除属性')
    },
    defineProperty() {
      throw new Error('入参被重新定义属性')
    }
  })
}

describe('deriveHat：三字段 16 种组合各落恰好一顶', () => {
  it.each(TRUTH_TABLE)('claude_code_only=%s 降级分组=%s 策略=%s -> %s', (cc, fb, policy, expected) => {
    const fields = { claude_code_only: cc, fallback_group_id: fb, claude_oauth_system_prompt_policy: policy }
    expect(deriveHat(fields)).toBe(expected)
    expect(describeHat(fields).hat).toBe(expected)
  })

  it('16 个输入互不相同，且恰好覆盖 6 顶（每顶至少出现一次）', () => {
    expect(TRUTH_TABLE).toHaveLength(16)
    expect(new Set(TRUTH_TABLE.map(([cc, fb, policy]) => `${cc}/${fb}/${policy}`)).size).toBe(16)
    // 表里写的期望本身也要覆盖 6 顶，否则下面的断言空转
    expect(new Set(TRUTH_TABLE.map((row) => row[3])).size).toBe(6)

    const derived = TRUTH_TABLE.map(([cc, fb, policy]) =>
      deriveHat({ claude_code_only: cc, fallback_group_id: fb, claude_oauth_system_prompt_policy: policy })
    )
    expect([...new Set(derived)].sort()).toEqual([
      'cc_fallback',
      'cc_line',
      'open_follow',
      'open_full',
      'open_keep_prompt',
      'open_light'
    ])
  })
})

describe('容错输入', () => {
  it.each([
    ['undefined', undefined],
    ['null', null],
    ['空对象', {}]
  ])('deriveHat 入参为 %s -> 按全缺省处理：开放·跟随', (_name, input) => {
    expect(deriveHat(input)).toBe('open_follow')
  })

  describe('注入策略', () => {
    it.each([
      ['undefined', undefined],
      ['null', null],
      ['空串', ''],
      ['未知字符串', 'bogus'],
      ['大写的合法值（接口 oneof 只认小写）', 'ENABLED'],
      ['数字 123', 123],
      ['布尔 true', true],
      ['数组 [enabled]', ['enabled']]
    ])('策略为 %s -> 按 inherit：normalizePolicy 回落，开放·跟随', (_name, raw) => {
      expect(normalizePolicy(raw)).toBe('inherit')
      expect(deriveHat({ claude_code_only: false, claude_oauth_system_prompt_policy: raw as never })).toBe('open_follow')
    })

    it.each(['inherit', 'enabled', 'identity_only', 'disabled'])('normalizePolicy(%s) 是合法值，原样返回', (policy) => {
      expect(normalizePolicy(policy)).toBe(policy)
    })
  })

  describe('降级分组', () => {
    it.each([
      ['0', 0],
      ['-1', -1],
      ['NaN', Number.NaN],
      ['Infinity', Number.POSITIVE_INFINITY],
      ["字符串 '7'", '7'],
      ['null', null],
      ['undefined', undefined]
    ])('CC 为真且降级分组为 %s -> 视为没有降级分组：CC 专线', (_name, fb) => {
      expect(hasFallback(fb)).toBe(false)
      expect(deriveHat({ claude_code_only: true, fallback_group_id: fb as never })).toBe('cc_line')
    })

    it.each([
      ['1', 1],
      ['7', 7],
      ['很大的正整数', 2147483647]
    ])('CC 为真且降级分组为 %s -> CC 降级池', (_name, fb) => {
      expect(hasFallback(fb)).toBe(true)
      expect(deriveHat({ claude_code_only: true, fallback_group_id: fb })).toBe('cc_fallback')
    })

    it.each([
      [1, true],
      [7, true],
      [0, false],
      [-1, false],
      [Number.NaN, false],
      [Number.POSITIVE_INFINITY, false],
      [Number.NEGATIVE_INFINITY, false],
      ['7', false],
      [true, false],
      [null, false],
      [undefined, false],
      [{}, false]
    ])('hasFallback(%o) = %s', (id, expected) => {
      expect(hasFallback(id)).toBe(expected)
    })
  })

  describe('claude_code_only 只有严格 true 才算真', () => {
    it.each([
      ["字符串 'true'", 'true'],
      ['数字 1', 1],
      ["字符串 'false'", 'false'],
      ['数字 0', 0],
      ['null', null],
      ['undefined', undefined],
      ['空对象', {}]
    ])('claude_code_only 为 %s -> 不算真：带着降级分组也是开放·跟随', (_name, cc) => {
      expect(deriveHat({ claude_code_only: cc as never, fallback_group_id: 7 })).toBe('open_follow')
    })
  })
})

describe('applyHat 字段写入', () => {
  describe.each(OPEN_HATS)('开放类 %s（写入策略 %s）', (hat, policy) => {
    it.each(STARTS)('从「$name」出发 -> 关 CC、降级分组严格为 null、策略写成该帽子的值', (start) => {
      const result = applyHat(hat, start.input)
      expect(result).toEqual({ claude_code_only: false, fallback_group_id: null, claude_oauth_system_prompt_policy: policy })
      expect(result.claude_code_only).toBe(false)
      // 严格 null：不是 undefined，更不是 0（创建接口对 0 会校验失败）
      expect(result.fallback_group_id).toBe(null)
    })

    it('第三个参数（降级分组 id）对开放类帽子无效', () => {
      const result = applyHat(
        hat,
        { claude_code_only: true, fallback_group_id: 9, claude_oauth_system_prompt_policy: 'disabled' },
        5
      )
      expect(result).toEqual({ claude_code_only: false, fallback_group_id: null, claude_oauth_system_prompt_policy: policy })
      expect(result.fallback_group_id).toBe(null)
    })
  })

  describe('cc_line', () => {
    it.each(STARTS)('从「$name」出发 -> 开 CC、降级分组 null、策略保持归一后的当前值', (start) => {
      const result = applyHat('cc_line', start.input)
      expect(result).toEqual({
        claude_code_only: true,
        fallback_group_id: null,
        claude_oauth_system_prompt_policy: start.normalized.claude_oauth_system_prompt_policy
      })
      expect(result.fallback_group_id).toBe(null)
    })

    it('从 CC 降级池切到 CC 专线（不降级）：清掉降级分组，策略不动', () => {
      expect(
        applyHat('cc_line', { claude_code_only: true, fallback_group_id: 9, claude_oauth_system_prompt_policy: 'enabled' })
      ).toEqual({ claude_code_only: true, fallback_group_id: null, claude_oauth_system_prompt_policy: 'enabled' })
    })
  })

  describe('cc_fallback', () => {
    it.each(STARTS)('带正整数 id=5：从「$name」出发 -> 开 CC、降级分组=5、策略保持', (start) => {
      expect(applyHat('cc_fallback', start.input, 5)).toEqual({
        claude_code_only: true,
        fallback_group_id: 5,
        claude_oauth_system_prompt_policy: start.normalized.claude_oauth_system_prompt_policy
      })
    })

    const NO_VALID_ID: Array<[string, number | null | undefined]> = [
      ['不带 id', undefined],
      ['id 为 0', 0],
      ['id 为负数', -1],
      ['id 为 null', null],
      ['id 为 NaN', Number.NaN]
    ]
    describe.each(NO_VALID_ID)('%s', (_name, id) => {
      it.each(STARTS)('从「$name」出发 -> 不切换：原样返回当前三字段的归一值', (start) => {
        expect(applyHat('cc_fallback', start.input, id)).toEqual(start.normalized)
      })
    })

    it('从开放帽（带残留降级分组 9）切过来且不给 id：不写任何字段，残留的 9 不被当作选择', () => {
      const residual: HatInput = {
        claude_code_only: false,
        fallback_group_id: 9,
        claude_oauth_system_prompt_policy: 'enabled'
      }
      const result = applyHat('cc_fallback', residual)
      expect(result).toEqual({ claude_code_only: false, fallback_group_id: 9, claude_oauth_system_prompt_policy: 'enabled' })
      expect(result.claude_code_only).toBe(false)
      // 没被残留的 9 带进 CC 降级池：反推出来仍是原来的开放帽
      expect(deriveHat(result)).toBe('open_full')
    })

    it('同样的起点给 id=5：写入 5，而不是残留的 9', () => {
      const result = applyHat(
        'cc_fallback',
        { claude_code_only: false, fallback_group_id: 9, claude_oauth_system_prompt_policy: 'enabled' },
        5
      )
      expect(result).toEqual({ claude_code_only: true, fallback_group_id: 5, claude_oauth_system_prompt_policy: 'enabled' })
      expect(result.fallback_group_id).toBe(5)
    })
  })

  describe('CC 类帽子不改动已存的注入策略（界面隐藏但保留）', () => {
    it.each(['enabled', 'disabled', 'identity_only'])('起点策略 %s：切到 cc_line / cc_fallback 之后策略原样保留', (policy) => {
      const start: HatInput = { claude_code_only: false, fallback_group_id: null, claude_oauth_system_prompt_policy: policy }
      expect(applyHat('cc_line', start).claude_oauth_system_prompt_policy).toBe(policy)
      expect(applyHat('cc_fallback', start, 5).claude_oauth_system_prompt_policy).toBe(policy)
    })

    it('起点策略是未知值：按归一后的 inherit 带回，不把脏值写回去', () => {
      const start: HatInput = { claude_code_only: false, claude_oauth_system_prompt_policy: 'bogus' }
      expect(applyHat('cc_line', start).claude_oauth_system_prompt_policy).toBe('inherit')
      expect(applyHat('cc_fallback', start, 5).claude_oauth_system_prompt_policy).toBe('inherit')
    })
  })

  describe('纯函数：不修改入参，返回新对象', () => {
    const makeInput = (): HatInput => ({
      claude_code_only: false,
      fallback_group_id: 9,
      claude_oauth_system_prompt_policy: 'enabled'
    })

    it.each(ALL_HATS)('%s：入参只读代理不报错，内容不变，返回值不是入参本身', (hat) => {
      const input = makeInput()
      const guarded = readOnly(input)

      const withId = applyHat(hat, guarded, 5)
      const withoutId = applyHat(hat, guarded)
      deriveHat(guarded)
      const described = describeHat(guarded)

      expect(input).toEqual(makeInput())
      expect(withId).not.toBe(input)
      expect(withoutId).not.toBe(input)
      expect(described.fields).not.toBe(input)
    })

    it('连续两次调用互不影响：返回值被调用方改写，不会污染下一次结果', () => {
      const first = applyHat('open_full', undefined)
      first.claude_oauth_system_prompt_policy = 'disabled'
      expect(applyHat('open_full', undefined).claude_oauth_system_prompt_policy).toBe('enabled')
    })
  })
})

describe('往返：applyHat 之后 deriveHat 回到该帽子', () => {
  describe.each(ALL_HATS)('目标帽子 %s', (hat) => {
    it.each(STARTS)('从「$name」出发（cc_fallback 带 id=5）', (start) => {
      const result = applyHat(hat, start.input, 5)
      expect(deriveHat(result)).toBe(hat)
      // 套用之后不会留下残留的降级分组
      expect(describeHat(result).staleFallback).toBe(false)
    })
  })

  describe.each(ALL_HATS)('从 %s 出发', (from) => {
    it.each(ALL_HATS)('切到 %s 再切回', (to) => {
      const base: HatInput = {
        claude_code_only: false,
        fallback_group_id: null,
        claude_oauth_system_prompt_policy: 'identity_only'
      }
      const atFrom = applyHat(from, base, 5)
      expect(deriveHat(atFrom)).toBe(from)

      const atTo = applyHat(to, atFrom, 6)
      expect(deriveHat(atTo)).toBe(to)

      const back = applyHat(from, atTo, 5)
      expect(deriveHat(back)).toBe(from)
    })
  })

  it('连续切换逐步断言三字段：CC 帽保留策略，开放帽显式覆盖策略并清掉降级分组', () => {
    let state: HatFields = {
      claude_code_only: false,
      fallback_group_id: null,
      claude_oauth_system_prompt_policy: 'enabled'
    }
    const steps: Array<[HatId, number | undefined, HatFields]> = [
      ['cc_line', undefined, { claude_code_only: true, fallback_group_id: null, claude_oauth_system_prompt_policy: 'enabled' }],
      ['cc_fallback', 5, { claude_code_only: true, fallback_group_id: 5, claude_oauth_system_prompt_policy: 'enabled' }],
      ['cc_line', undefined, { claude_code_only: true, fallback_group_id: null, claude_oauth_system_prompt_policy: 'enabled' }],
      ['open_keep_prompt', undefined, { claude_code_only: false, fallback_group_id: null, claude_oauth_system_prompt_policy: 'disabled' }],
      ['cc_fallback', 8, { claude_code_only: true, fallback_group_id: 8, claude_oauth_system_prompt_policy: 'disabled' }],
      ['open_light', undefined, { claude_code_only: false, fallback_group_id: null, claude_oauth_system_prompt_policy: 'identity_only' }],
      ['cc_line', undefined, { claude_code_only: true, fallback_group_id: null, claude_oauth_system_prompt_policy: 'identity_only' }],
      ['open_follow', undefined, { claude_code_only: false, fallback_group_id: null, claude_oauth_system_prompt_policy: 'inherit' }]
    ]
    for (const [hat, id, expected] of steps) {
      state = applyHat(hat, state, id)
      expect(state, `切到 ${hat} 之后`).toEqual(expected)
    }
  })
})

describe('describeHat', () => {
  it.each([undefined, null, {}])('入参为 %j：开放·跟随，三字段为缺省归一值，没有残留', (input) => {
    expect(describeHat(input)).toEqual({ hat: 'open_follow', fields: DEFAULT_FIELDS, staleFallback: false })
  })

  it('fields 是归一值：不是严格 true 的 CC、0 或负数的降级分组、未知策略都被规整', () => {
    expect(
      describeHat({ claude_code_only: 'true' as never, fallback_group_id: 0, claude_oauth_system_prompt_policy: 'bogus' })
    ).toEqual({
      hat: 'open_follow',
      fields: { claude_code_only: false, fallback_group_id: null, claude_oauth_system_prompt_policy: 'inherit' },
      staleFallback: false
    })
    expect(
      describeHat({ claude_code_only: true, fallback_group_id: -3, claude_oauth_system_prompt_policy: 'identity_only' })
    ).toEqual({
      hat: 'cc_line',
      fields: { claude_code_only: true, fallback_group_id: null, claude_oauth_system_prompt_policy: 'identity_only' },
      staleFallback: false
    })
    expect(
      describeHat({ claude_code_only: true, fallback_group_id: 7, claude_oauth_system_prompt_policy: 'disabled' })
    ).toEqual({
      hat: 'cc_fallback',
      fields: { claude_code_only: true, fallback_group_id: 7, claude_oauth_system_prompt_policy: 'disabled' },
      staleFallback: false
    })
  })

  it.each([
    [false, 3, true],
    [true, 3, false],
    [false, null, false],
    [false, 0, false],
    [true, null, false],
    // 缺字段的 mock：没有 claude_code_only 就不算 CC
    [undefined, 3, true]
  ])('claude_code_only=%s 降级分组=%s -> staleFallback=%s', (cc, fb, expected) => {
    expect(describeHat({ claude_code_only: cc, fallback_group_id: fb }).staleFallback).toBe(expected)
  })

  it('残留的降级分组仍然出现在 fields 里（界面要能说出「残留的是 #3」），帽子按策略反推', () => {
    expect(
      describeHat({ claude_code_only: false, fallback_group_id: 3, claude_oauth_system_prompt_policy: 'enabled' })
    ).toEqual({
      hat: 'open_full',
      fields: { claude_code_only: false, fallback_group_id: 3, claude_oauth_system_prompt_policy: 'enabled' },
      staleFallback: true
    })
  })
})

describe('HAT_IDS / HAT_META', () => {
  it('HAT_IDS 的顺序与长度：cc_line, cc_fallback, open_follow, open_full, open_light, open_keep_prompt', () => {
    expect(HAT_IDS).toHaveLength(6)
    expect([...HAT_IDS]).toEqual(['cc_line', 'cc_fallback', 'open_follow', 'open_full', 'open_light', 'open_keep_prompt'])
    expect(new Set(HAT_IDS).size).toBe(6)
  })

  it('HAT_META 恰好覆盖 6 顶，条目的 id 与键一致', () => {
    expect(Object.keys(HAT_META).sort()).toEqual([...ALL_HATS].sort())
    for (const id of ALL_HATS) {
      expect(HAT_META[id].id).toBe(id)
    }
  })

  describe('词条 key', () => {
    it.each(ALL_HATS)('%s：name / short / purpose / detail 恰为 admin.groups.hats.<id>.*', (id) => {
      expect(HAT_META[id].nameKey).toBe(`admin.groups.hats.${id}.name`)
      expect(HAT_META[id].shortKey).toBe(`admin.groups.hats.${id}.short`)
      expect(HAT_META[id].purposeKey).toBe(`admin.groups.hats.${id}.purpose`)
      expect(HAT_META[id].detailKey).toBe(`admin.groups.hats.${id}.detail`)
    })

    it('抽两条写成纯字面量对照，防止上面的拼接规则本身写错', () => {
      expect(HAT_META.cc_line.nameKey).toBe('admin.groups.hats.cc_line.name')
      expect(HAT_META.open_keep_prompt.detailKey).toBe('admin.groups.hats.open_keep_prompt.detail')
    })

    it('24 个 key 互不重复', () => {
      const keys = ALL_HATS.flatMap((id) => [
        HAT_META[id].nameKey,
        HAT_META[id].shortKey,
        HAT_META[id].purposeKey,
        HAT_META[id].detailKey
      ])
      expect(keys).toHaveLength(24)
      expect(new Set(keys).size).toBe(24)
    })
  })

  describe('徽章 class', () => {
    const BADGES: Array<[HatId, string]> = [
      ['cc_line', 'badge badge-success'],
      ['cc_fallback', 'badge badge-primary'],
      ['open_follow', 'badge badge-gray'],
      ['open_full', 'badge badge-purple'],
      ['open_light', 'badge badge-purple'],
      ['open_keep_prompt', 'badge badge-warning']
    ]

    it.each(BADGES)('%s 的 badgeClass 是 %s，且匹配 badge badge-* 全串', (id, expected) => {
      expect(HAT_META[id].badgeClass).toBe(expected)
      expect(HAT_META[id].badgeClass).toMatch(/^badge badge-(primary|success|warning|danger|gray|purple)$/)
    })
  })

  describe('family / requiresFallback / showsPolicySelect / fixedPolicy', () => {
    const BEHAVIOR: Array<[HatId, 'cc' | 'open', boolean, boolean, ClaudeOAuthSystemPromptPolicy | null]> = [
      ['cc_line', 'cc', false, false, null],
      ['cc_fallback', 'cc', true, true, null],
      ['open_follow', 'open', false, false, 'inherit'],
      ['open_full', 'open', false, false, 'enabled'],
      ['open_light', 'open', false, false, 'identity_only'],
      ['open_keep_prompt', 'open', false, false, 'disabled']
    ]

    it.each(BEHAVIOR)('%s：家族 %s，requiresFallback=%s，showsPolicySelect=%s，fixedPolicy=%s', (id, family, requires, shows, fixed) => {
      expect(HAT_META[id].family).toBe(family)
      expect(HAT_META[id].requiresFallback).toBe(requires)
      expect(HAT_META[id].showsPolicySelect).toBe(shows)
      expect(HAT_META[id].fixedPolicy).toBe(fixed)
    })

    it.each(OPEN_HATS)('%s：fixedPolicy、applyHat 写入的策略、deriveHat 的反推三者一致（%s）', (id, policy) => {
      expect(HAT_META[id].fixedPolicy).toBe(policy)
      expect(applyHat(id, undefined).claude_oauth_system_prompt_policy).toBe(policy)
      expect(deriveHat({ claude_code_only: false, claude_oauth_system_prompt_policy: policy })).toBe(id)
    })

    it.each(['inherit', 'enabled', 'identity_only', 'disabled'])('CC 类帽子 fixedPolicy 为 null：applyHat 保留当前策略 %s', (policy) => {
      const current: HatInput = { claude_code_only: false, claude_oauth_system_prompt_policy: policy }
      expect(HAT_META.cc_line.fixedPolicy).toBe(null)
      expect(HAT_META.cc_fallback.fixedPolicy).toBe(null)
      expect(applyHat('cc_line', current).claude_oauth_system_prompt_policy).toBe(policy)
      expect(applyHat('cc_fallback', current, 5).claude_oauth_system_prompt_policy).toBe(policy)
    })
  })

  describe('源码守卫：徽章 class 与词条 key 必须是完整字面量（Tailwind 与 key 扫描只认源码里的完整字符串）', () => {
    // 运行时的值相同并不代表源码里没有拼接，所以直接读源码
    const source = readFileSync(join(process.cwd(), 'src/components/admin/group/groupHats.ts'), 'utf8')

    it('6 个 badgeClass 逐条都是完整的 badge badge-* 字面量，顺序与 HAT_IDS 一致', () => {
      const literals = [...source.matchAll(/badgeClass: (['"])(badge badge-[a-z]+)\1/g)].map((match) => match[2])
      expect(literals).toEqual([
        'badge badge-success',
        'badge badge-primary',
        'badge badge-gray',
        'badge badge-purple',
        'badge badge-purple',
        'badge badge-warning'
      ])
    })

    it.each(ALL_HATS)('%s 的 4 个词条 key 在源码里都是完整字面量', (id) => {
      for (const part of ['name', 'short', 'purpose', 'detail']) {
        const key = `admin.groups.hats.${id}.${part}`
        // 单双引号都认：引号风格以后可能被格式化工具改掉，守卫只关心「是不是完整字面量」
        expect(source.includes(`'${key}'`) || source.includes(`"${key}"`), `${key} 没有以完整字面量出现在源码里`).toBe(true)
      }
    })
  })
})
