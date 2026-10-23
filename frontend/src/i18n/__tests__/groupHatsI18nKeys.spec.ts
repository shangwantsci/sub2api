import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { HAT_META } from '@/components/admin/group/groupHats'
import en from '../locales/en'
import zh from '../locales/zh'

/**
 * 帽子相关界面引用的 i18n key 必须在 zh 与 en 两侧都存在，hats 子树与 HAT_META 引用的 key 一一对应。
 *
 * 缺失的 key 不会报任何错误，vue-i18n 会把原始 key 直接渲染到界面上（列表徽章会显示 admin.groups.hats.xxx.short）；
 * 孤儿词条（HAT_META 已不再引用）会让翻译越积越多、没人维护。这类问题在类型检查、构建、其它单元测试里都看不出来，
 * 只能靠比对（仿 capacityI18nKeys.spec.ts）。
 */

const HATS_SUBTREE = 'admin.groups.hats'
const CARD_SUBTREE = 'admin.groups.hatCard'

/** 匹配 t('a.b.c') / t("a.b.c")，只取静态字面量 key。 */
const T_CALL = /\bt\(\s*(['"])([A-Za-z0-9_.]+)\1/g

function resolve(bundle: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>((node, segment) => {
    if (node && typeof node === 'object' && segment in (node as Record<string, unknown>)) {
      return (node as Record<string, unknown>)[segment]
    }
    return undefined
  }, bundle)
}

/** 把一棵词条子树拍平成叶子 key 列表。 */
function leafKeys(node: unknown, prefix = ''): string[] {
  if (typeof node === 'string') return [prefix]
  if (!node || typeof node !== 'object') return []
  return Object.entries(node as Record<string, unknown>).flatMap(([key, value]) =>
    leafKeys(value, prefix ? `${prefix}.${key}` : key)
  )
}

/** 文案里的 {xxx} 占位符集合：少一个参数界面只会悄悄显示成空，没有任何报错。 */
const placeholdersOf = (text: unknown) => [...new Set(String(text).match(/\{\w+\}/g) ?? [])].sort()

const BUNDLES: Array<[string, unknown]> = [
  ['zh', zh],
  ['en', en]
]

const metas = Object.values(HAT_META)
const referencedKeys = metas.flatMap((meta) => [meta.nameKey, meta.shortKey, meta.purposeKey, meta.detailKey])

describe('HAT_META 引用的词条', () => {
  it('6 顶帽子共引用 24 个互不相同的 key（防止下面的遍历空转通过）', () => {
    expect(metas).toHaveLength(6)
    expect(referencedKeys).toHaveLength(24)
    expect(new Set(referencedKeys).size).toBe(24)
  })

  describe.each(BUNDLES)('%s', (_locale, bundle) => {
    it.each(referencedKeys)('%s 解析成非空字符串', (key) => {
      const value = resolve(bundle, key)
      expect(typeof value, `${key} 缺失，界面会直接显示原始 key`).toBe('string')
      expect((value as string).length, `${key} 是空串`).toBeGreaterThan(0)
    })
  })
})

describe('hats 子树与 HAT_META 一一对应', () => {
  const expected = [...referencedKeys].sort()

  it.each(BUNDLES)('%s：叶子 key 集合恰好等于 HAT_META 引用的 key 集合（不缺失、无孤儿）', (_locale, bundle) => {
    const node = resolve(bundle, HATS_SUBTREE)
    expect(node, `缺少 ${HATS_SUBTREE}`).toBeDefined()
    expect(leafKeys(node, HATS_SUBTREE).sort()).toEqual(expected)
  })

  it('zh 与 en 的叶子 key 集合相等', () => {
    const zhLeaves = leafKeys(resolve(zh, HATS_SUBTREE), HATS_SUBTREE).sort()
    expect(zhLeaves.length).toBeGreaterThan(0)
    expect(leafKeys(resolve(en, HATS_SUBTREE), HATS_SUBTREE).sort()).toEqual(zhLeaves)
  })
})

describe('列表列标题', () => {
  it.each(BUNDLES)('%s：admin.groups.columns.hat 是非空字符串', (_locale, bundle) => {
    const value = resolve(bundle, 'admin.groups.columns.hat')
    expect(typeof value, 'admin.groups.columns.hat 缺失，列设置里会直接显示原始 key').toBe('string')
    expect((value as string).length).toBeGreaterThan(0)
  })
})

describe('hatCard 子树（列表徽章的 title 与编辑卡片共用）', () => {
  it('zh 与 en 的叶子 key 一一对应', () => {
    const zhNode = resolve(zh, CARD_SUBTREE)
    const enNode = resolve(en, CARD_SUBTREE)
    expect(zhNode, `zh 缺少 ${CARD_SUBTREE}`).toBeDefined()
    expect(enNode, `en 缺少 ${CARD_SUBTREE}`).toBeDefined()
    const zhLeaves = leafKeys(zhNode, CARD_SUBTREE).sort()
    expect(zhLeaves.length).toBeGreaterThan(0)
    expect(leafKeys(enNode, CARD_SUBTREE).sort()).toEqual(zhLeaves)
  })

  it('每条词条的 {xxx} 占位符集合在 zh 与 en 一致', () => {
    const leaves = leafKeys(resolve(zh, CARD_SUBTREE), CARD_SUBTREE)
    expect(leaves.length).toBeGreaterThan(0)
    for (const leaf of leaves) {
      expect(placeholdersOf(resolve(en, leaf)), `${leaf} 的占位符在 zh 与 en 不一致`).toEqual(
        placeholdersOf(resolve(zh, leaf))
      )
    }
  })

  // GroupHatBadge 的 title 就是用这些参数名调用 t() 的：词条里的占位符名对不上，界面只会悄悄显示成空
  it.each([
    ['tipFallbackTo', ['{name}']],
    ['tipFallbackPolicy', ['{policy}']],
    ['tipStaleFallback', ['{name}']]
  ])('%s 的占位符是 %j，与 GroupHatBadge 传入的参数一致', (key, placeholders) => {
    for (const [locale, bundle] of BUNDLES) {
      expect(placeholdersOf(resolve(bundle, `${CARD_SUBTREE}.${key}`)), `${locale} 的 ${key}`).toEqual(placeholders)
    }
  })
})

describe('GroupHatBadge.vue 里的静态 t() key', () => {
  const source = readFileSync(join(process.cwd(), 'src/components/admin/group/GroupHatBadge.vue'), 'utf8')
  const keys = [...new Set([...source.matchAll(T_CALL)].map((match) => match[2]))]

  it('抓到了 t() 调用（防止正则或路径失效后下面的断言空转通过）', () => {
    expect(keys.length).toBeGreaterThan(0)
    expect(keys).toEqual(
      expect.arrayContaining([
        'admin.groups.hatCard.tipFallbackTo',
        'admin.groups.hatCard.tipFallbackPolicy',
        'admin.groups.hatCard.tipStaleFallback',
        'admin.groups.anthropicPolicies.inherit',
        'admin.groups.anthropicPolicies.enabled',
        'admin.groups.anthropicPolicies.identityOnly',
        'admin.groups.anthropicPolicies.disabled'
      ])
    )
  })

  it.each(BUNDLES)('%s：每个静态 key 都存在且是字符串', (_locale, bundle) => {
    const missing = keys.filter((key) => typeof resolve(bundle, key) !== 'string')
    expect(missing, '缺少这些 key，界面会直接显示原始 key').toEqual([])
  })
})

describe('GroupsView.vue 里的帽子相关静态 t() key', () => {
  // GroupsView 是历史大文件，这里只检查帽子相关的前缀；列标题的 key 只在它里面被引用，写错了表头会直接显示原始 key
  const PREFIXES = ['admin.groups.columns.hat', 'admin.groups.hats.', 'admin.groups.hatCard.']
  const source = readFileSync(join(process.cwd(), 'src/views/admin/GroupsView.vue'), 'utf8')
  const keys = [...new Set([...source.matchAll(T_CALL)].map((match) => match[2]))].filter((key) =>
    PREFIXES.some((prefix) => key.startsWith(prefix))
  )

  it('抓到了列标题的 t() 调用（防止正则或路径失效后下面的断言空转通过）', () => {
    expect(keys).toContain('admin.groups.columns.hat')
  })

  it.each(BUNDLES)('%s：每个静态 key 都存在且是字符串', (_locale, bundle) => {
    const missing = keys.filter((key) => typeof resolve(bundle, key) !== 'string')
    expect(missing, '缺少这些 key，界面会直接显示原始 key').toEqual([])
  })
})

describe('文案字符检查（vue-i18n 把 { } @ | 当语法解析）', () => {
  const textsOf = (bundle: unknown): Array<[string, string]> =>
    [HATS_SUBTREE, CARD_SUBTREE].flatMap((path) =>
      leafKeys(resolve(bundle, path), path).map((key): [string, string] => [key, String(resolve(bundle, key))])
    )
  const withoutPlaceholders = (text: string) => text.replace(/\{\w+\}/g, '')

  it.each(BUNDLES)('%s：去掉 {name} 这类合法占位符后，不得再含 { } @ |', (_locale, bundle) => {
    const entries = textsOf(bundle)
    // hats 24 条 + hatCard 至少 3 条；条数不够说明子树路径失效了，下面的断言会空转
    expect(entries.length).toBeGreaterThanOrEqual(27)
    const bad = entries.filter(([, text]) => /[{}@|]/.test(withoutPlaceholders(text))).map(([key]) => key)
    expect(bad, '这些词条含裸 { } @ |，vue-i18n 会当语法解析，可能渲染错位甚至整页白屏').toEqual([])
  })

  it.each(BUNDLES)('%s：hats 词条不含任何占位符（调用处不传参数，带占位符会静默渲染成空）', (_locale, bundle) => {
    const leaves = leafKeys(resolve(bundle, HATS_SUBTREE), HATS_SUBTREE)
    expect(leaves).toHaveLength(24)
    const bad = leaves.filter((key) => placeholdersOf(resolve(bundle, key)).length > 0)
    expect(bad).toEqual([])
  })

  it('en 词条不含撇号（含 ’）：统一写 do not 与 of 结构', () => {
    const bad = textsOf(en)
      .filter(([, text]) => /['’]/.test(text))
      .map(([key]) => key)
    expect(bad, 'en 词条里有撇号').toEqual([])
  })
})
