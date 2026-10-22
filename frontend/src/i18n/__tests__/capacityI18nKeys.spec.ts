import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

/**
 * 容量模型相关界面引用的 i18n key 必须在 zh 与 en 两侧都存在，且被删掉的旧词条不能回来。
 *
 * 缺失的 key 不会报任何错误，vue-i18n 会把原始 key 直接渲染到界面上
 * （例如设置页的校验提示直接显示 "admin.settings.capacityPresets.errKeyReserved"）。
 * 这类问题在类型检查、构建、单元测试里全都看不出来，只能靠扫源码比对（仿 providerI18nKeys.spec.ts）。
 */

/**
 * 新增 / 重写的组件整份检查；Create / Edit / Bulk / SettingsView 是历史大文件，这里只检查容量模型相关的前缀
 * （这四个文件当年的老缺失词条，例如 admin.accounts.fromModel，已补齐，整份检查见 adminLargeFilesI18nKeys.spec.ts）。
 */
interface FileCheck {
  path: string
  /** 只检查这些前缀下的 key；不填表示整份文件都检查 */
  prefixes?: string[]
}

const CAPACITY_PREFIXES = [
  'admin.accounts.capacityFields.',
  'admin.accounts.capacity.',
  'admin.accounts.quotaControl.',
  'admin.accounts.bulkEdit.',
  'admin.settings.capacityPresets.',
  'admin.settings.gatewayForwarding.accountNurturing'
]

const FILES: FileCheck[] = [
  { path: 'src/components/account/AccountCapacityFields.vue' },
  { path: 'src/components/account/AccountCapacityCell.vue' },
  { path: 'src/components/account/capacityBuilder.ts' },
  { path: 'src/views/admin/settings/CapacityPresetsCard.vue' },
  { path: 'src/components/account/CreateAccountModal.vue', prefixes: CAPACITY_PREFIXES },
  { path: 'src/components/account/EditAccountModal.vue', prefixes: CAPACITY_PREFIXES },
  { path: 'src/components/account/BulkEditAccountModal.vue', prefixes: CAPACITY_PREFIXES },
  { path: 'src/views/admin/SettingsView.vue', prefixes: CAPACITY_PREFIXES }
]

/** 匹配 t('a.b.c') / t("a.b.c")，只取静态字面量 key。 */
const T_CALL = /\bt\(\s*(['"])([A-Za-z0-9_.]+)\1/g

function collectKeys({ path, prefixes }: FileCheck): string[] {
  const source = readFileSync(join(process.cwd(), path), 'utf8')
  const keys = new Set<string>()
  for (const match of source.matchAll(T_CALL)) {
    keys.add(match[2])
  }
  const all = [...keys]
  return prefixes ? all.filter((key) => prefixes.some((prefix) => key.startsWith(prefix))) : all
}

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

describe('容量模型相关 i18n key', () => {
  for (const file of FILES) {
    it(`${file.path} 引用的容量相关 key 在 zh 与 en 都存在`, () => {
      const keys = collectKeys(file)
      // 没抓到任何 key 说明正则或路径失效了，此时上面的断言会空转通过。
      expect(keys.length, `${file.path} 没解析出任何 t() 调用，检查正则或文件路径`).toBeGreaterThan(0)

      const missingZh = keys.filter((key) => typeof resolve(zh, key) !== 'string')
      const missingEn = keys.filter((key) => typeof resolve(en, key) !== 'string')

      expect(missingZh, 'zh 缺少这些 key，界面会直接显示原始 key').toEqual([])
      expect(missingEn, 'en 缺少这些 key，界面会直接显示原始 key').toEqual([])
    })
  }

  // 新增 / 改动的词条子树：zh 与 en 的叶子 key 必须一一对应（两份词条必须同步）
  const SUBTREES = [
    'admin.accounts.capacityFields',
    'admin.accounts.capacity.throttled',
    'admin.accounts.capacity.presetBadgeTitle',
    'admin.accounts.bulkEdit.capacityPreset',
    'admin.accounts.quotaControl.disguiseTitle',
    'admin.accounts.quotaControl.disguiseHint',
    'admin.accounts.quotaControl.tlsFingerprint',
    'admin.settings.capacityPresets',
    'admin.settings.gatewayForwarding.accountNurturing',
    'admin.settings.gatewayForwarding.accountNurturingHint',
    'admin.providerSettings.tiersPresetsUnavailable',
    'admin.providerSettings.colEnabled',
    'admin.providerSettings.colAdaptive',
    'admin.providerSettings.adaptiveOn',
    'admin.providerSettings.adaptiveOff',
    'admin.providerSettings.applyConfirmMessage'
  ]

  for (const path of SUBTREES) {
    it(`${path}：zh 与 en 的词条一一对应`, () => {
      const zhNode = resolve(zh, path)
      const enNode = resolve(en, path)
      expect(zhNode, `zh 缺少 ${path}`).toBeDefined()
      expect(enNode, `en 缺少 ${path}`).toBeDefined()
      expect(leafKeys(enNode, path).sort()).toEqual(leafKeys(zhNode, path).sort())
    })
  }

  // 「应用到存量」（先预览、再确认）：键名以 apply 开头的词条整体检查，以后再加词条不用回来改这份清单。
  // 占位符也要一致：少一个参数界面只会悄悄显示成「将改动  个账号」，没有任何报错。
  const providerSettingsApplyKeys = (bundle: unknown) =>
    Object.keys((resolve(bundle, 'admin.providerSettings') ?? {}) as Record<string, unknown>).filter((key) =>
      key.startsWith('apply')
    )
  const placeholdersOf = (text: unknown) => [...new Set(String(text).match(/\{\w+\}/g) ?? [])].sort()

  it('admin.providerSettings.apply*：zh 与 en 的词条一一对应，占位符一致', () => {
    const keys = [...new Set([...providerSettingsApplyKeys(zh), ...providerSettingsApplyKeys(en)])].sort()
    // 没抓到几条说明前缀或路径失效了，下面的循环会空转通过
    expect(keys.length).toBeGreaterThan(8)

    for (const key of keys) {
      const path = `admin.providerSettings.${key}`
      const zhNode = resolve(zh, path)
      const enNode = resolve(en, path)
      expect(zhNode, `zh 缺少 ${path}`).toBeDefined()
      expect(enNode, `en 缺少 ${path}`).toBeDefined()

      const zhLeaves = leafKeys(zhNode, path).sort()
      expect(leafKeys(enNode, path).sort(), `${path} 的叶子 key 在 zh 与 en 不一致`).toEqual(zhLeaves)
      for (const leaf of zhLeaves) {
        expect(placeholdersOf(resolve(en, leaf)), `${leaf} 的占位符在 zh 与 en 不一致`).toEqual(
          placeholdersOf(resolve(zh, leaf))
        )
      }
    }
  })

  it('applyField 的键名恰好是预览接口 changes[].field 的全部取值，zh 与 en 都有', () => {
    for (const bundle of [zh, en]) {
      const labels = resolve(bundle, 'admin.providerSettings.applyField') as Record<string, unknown>
      expect(Object.keys(labels).sort()).toEqual([
        'adaptive_throttle',
        'base_rpm',
        'concurrency',
        'load_factor',
        'max_sessions',
        'window_cost_limit'
      ])
      for (const label of Object.values(labels)) expect(typeof label).toBe('string')
    }
  })

  // 人格 / 防封档位 / 旧版人格门控已整体移除，词条也要一起清掉
  const REMOVED = [
    'admin.accounts.persona',
    'admin.settings.gatewayForwarding.personaGating',
    'admin.settings.gatewayForwarding.personaGatingHint',
    'admin.providerSettings.colPersonaPreset',
    'admin.providerSettings.personaPresetNone',
    'admin.providerSettings.personaPresetOff',
    'admin.providerSettings.personaPresetAggressive',
    'admin.providerSettings.personaPresetBalanced',
    'admin.providerSettings.personaPresetConservative',
    // 魔法默认 RPM 15 已删除，占位提示也不能再提它
    'admin.accounts.quotaControl.rpmLimit.baseRpmPlaceholder',
    // 窗口费用 / 会话 / RPM 三个「启用」开关已取消，随开关一起存在的标题与说明没有地方显示了
    'admin.accounts.quotaControl.windowCost.hint',
    'admin.accounts.quotaControl.windowCost.limit',
    'admin.accounts.quotaControl.windowCost.limitPlaceholder',
    'admin.accounts.quotaControl.sessionLimit.label',
    'admin.accounts.quotaControl.sessionLimit.hint',
    'admin.accounts.quotaControl.sessionLimit.maxSessionsPlaceholder',
    'admin.accounts.quotaControl.rpmLimit.hint'
  ]

  for (const path of REMOVED) {
    it(`已删除的词条不再存在：${path}`, () => {
      expect(resolve(zh, path), `zh 里还留着 ${path}`).toBeUndefined()
      expect(resolve(en, path), `en 里还留着 ${path}`).toBeUndefined()
    })
  }

  it('档位下拉文案的占位符与 presetOptionLabel 传入的参数一致', () => {
    for (const bundle of [zh, en]) {
      const text = resolve(bundle, 'admin.accounts.capacityFields.presetOption') as string
      for (const token of ['{label}', '{concurrency}', '{sessions}', '{rpm}', '{cost}']) {
        expect(text, `presetOption 缺少占位符 ${token}`).toContain(token)
      }
    }
  })
})
