import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { CAPACITY_EXTRA_KEYS } from '../capacityBuilder'

/**
 * 源码守卫：容量相关 extra 键只能出现在 capacityBuilder.ts。
 *
 * 这个项目已经因为「同一份键列表抄了好几份」吃过亏：新建弹窗里有三条 Anthropic 授权路径，
 * 每条路径各拼一遍 extra，后来改了一条、漏了两条，单测全绿但线上账号没存上容量配置。
 * 现在三个弹窗只能调 capacityBuilder / useAccountCapacity，这里扫源码确认没有人把键写回去；
 * 同时新建弹窗只留一个漏斗函数 buildAnthropicOAuthExtra，三条授权路径各调用一次，
 * 增删任何一条路径都会让调用次数变化而变红。
 */

function readSource(relPath: string): string {
  return readFileSync(join(process.cwd(), 'src', relPath), 'utf8')
}

const MODALS = [
  'components/account/CreateAccountModal.vue',
  'components/account/EditAccountModal.vue',
  'components/account/BulkEditAccountModal.vue'
]

// 与弹窗一起使用 capacityBuilder 的展示组件与 composable，同样不许自己拼键
const BUILDER_CLIENTS = [
  'components/account/AccountCapacityFields.vue',
  'composables/useAccountCapacity.ts',
  'composables/useCapacityPresets.ts'
]

describe('容量 extra 键字面量只出现在 capacityBuilder.ts', () => {
  it('键清单不是空的（防止清单失效后下面的断言空转通过）', () => {
    expect(CAPACITY_EXTRA_KEYS.length).toBeGreaterThan(0)
  })

  // 按标识符边界匹配：全局设置键 account_nurturing_enabled 含有子串 nurturing_enabled，但不是账号 extra 键
  const containsKey = (source: string, key: string) =>
    new RegExp(`(?<![A-Za-z0-9_])${key}(?![A-Za-z0-9_])`).test(source)

  for (const file of [...MODALS, ...BUILDER_CLIENTS]) {
    it(`${file} 里没有任何容量键字面量`, () => {
      const source = readSource(file)
      const leaked = CAPACITY_EXTRA_KEYS.filter((key) => containsKey(source, key))
      expect(
        leaked,
        `${file} 直接写了容量键 ${leaked.join(', ')}；请改用 capacityBuilder / useAccountCapacity`
      ).toEqual([])
    })
  }
})

describe('旧容量实现的残留（人格档位、启用开关、魔法默认 RPM）不能回来', () => {
  const RESIDUE: Array<[string, RegExp]> = [
    ['人格 persona（排除 personal）', /persona(?!l)/i],
    ['窗口费用启用开关 windowCostEnabled', /windowCostEnabled/],
    ['会话限制启用开关 sessionLimitEnabled', /sessionLimitEnabled/],
    ['RPM 启用开关 rpmLimitEnabled', /rpmLimitEnabled/],
    ['魔法默认 RPM DEFAULT_BASE_RPM', /DEFAULT_BASE_RPM/]
  ]

  for (const file of MODALS) {
    it(`${file} 没有旧实现残留`, () => {
      const source = readSource(file)
      const hits = RESIDUE.filter(([, pattern]) => pattern.test(source)).map(([name]) => name)
      expect(hits, `${file} 又出现了：${hits.join('；')}`).toEqual([])
    })
  }
})

describe('新建弹窗只有一个 Anthropic OAuth 漏斗函数', () => {
  it('buildAnthropicOAuthExtra( 恰好出现 3 处（OAuth 兑换、Cookie 授权、SessionKey 批量导入各一次）', () => {
    const source = readSource('components/account/CreateAccountModal.vue')
    const calls = source.match(/buildAnthropicOAuthExtra\(/g) ?? []
    expect(
      calls.length,
      '漏斗函数定义必须写成箭头函数赋值（不含左括号紧跟函数名），调用点必须恰好是三条授权路径'
    ).toBe(3)
  })
})
