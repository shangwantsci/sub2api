import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

/**
 * 账号创建 / 编辑 / 批量编辑弹窗与系统设置页引用的 i18n key，整份文件都必须在 zh 与 en 里存在。
 *
 * 缺失的 key 不会报任何错误，vue-i18n 会把原始 key 直接渲染到界面上。这四个历史大文件里曾经有 10 个
 * 这样的缺失词条（模型映射两侧的占位符、混合池权重、伪装守卫模式的下拉与说明、Codex PAT 导入成功提示），
 * 类型检查、构建、单元测试都发现不了。capacityI18nKeys.spec.ts 只检查容量相关前缀；这些词条补齐之后，
 * 这里改成整份检查：以后谁在这四个文件里新写了 t('...') 却没加词条，用例会直接指出是哪个 key。
 */

const LARGE_FILES = [
  'src/components/account/CreateAccountModal.vue',
  'src/components/account/EditAccountModal.vue',
  'src/components/account/BulkEditAccountModal.vue',
  'src/views/admin/SettingsView.vue'
]

/** 匹配 t('a.b.c') / t("a.b.c")（含换行后的写法），只取静态字面量 key。 */
const T_CALL = /\bt\(\s*(['"])([A-Za-z0-9_.]+)\1/g

function collectKeys(relPath: string): string[] {
  const source = readFileSync(join(process.cwd(), relPath), 'utf8')
  return [...new Set([...source.matchAll(T_CALL)].map((match) => match[2]))]
}

function resolve(bundle: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>((node, segment) => {
    if (node && typeof node === 'object' && segment in (node as Record<string, unknown>)) {
      return (node as Record<string, unknown>)[segment]
    }
    return undefined
  }, bundle)
}

function text(bundle: unknown, key: string): string {
  const value = resolve(bundle, key)
  return typeof value === 'string' ? value : ''
}

/** 把一棵词条子树拍平成叶子 key 列表。 */
function leafKeys(node: unknown, prefix = ''): string[] {
  if (typeof node === 'string') return [prefix]
  if (!node || typeof node !== 'object') return []
  return Object.entries(node as Record<string, unknown>).flatMap(([key, value]) =>
    leafKeys(value, prefix ? `${prefix}.${key}` : key)
  )
}

describe('历史大文件引用的 i18n key 整份存在', () => {
  for (const path of LARGE_FILES) {
    it(`${path}：所有静态 key 在 zh 与 en 都有非空文案`, () => {
      const keys = collectKeys(path)
      // 没抓到足够多的 key 说明正则或路径失效了，此时下面的断言会空转通过。
      expect(keys.length, `${path} 只解析出 ${keys.length} 个 t() key，检查正则或文件路径`).toBeGreaterThan(50)

      const missingZh = keys.filter((key) => text(zh, key).trim() === '')
      const missingEn = keys.filter((key) => text(en, key).trim() === '')

      expect(missingZh, 'zh 缺少这些 key，界面会直接显示原始 key').toEqual([])
      expect(missingEn, 'en 缺少这些 key，界面会直接显示原始 key').toEqual([])
    })
  }
})

/** 本次补齐 / 新增的词条：zh 与 en 都要有非空文案，并且仍有界面在引用（不留孤儿词条）。 */
const ADDED_KEYS = [
  // 模型映射输入框两侧的占位符（创建 / 编辑 / 批量编辑）
  'admin.accounts.fromModel',
  'admin.accounts.toModel',
  // API Key 账号的混合池权重
  'admin.accounts.poolWeight',
  'admin.accounts.poolWeightHint',
  // Codex PAT 导入成功提示
  'admin.accounts.messages.accountCreated',
  // 伪装守卫模式
  'admin.settings.gatewayForwarding.claudeMimicryGuardMode',
  'admin.settings.gatewayForwarding.claudeMimicryGuardModeHint',
  'admin.settings.gatewayForwarding.guardModeWarn',
  'admin.settings.gatewayForwarding.guardModeBlock',
  'admin.settings.gatewayForwarding.guardModeOff',
  // 功能开关页签里的「公开号池监控」卡片
  'admin.settings.features.poolHealth.title',
  'admin.settings.features.poolHealth.description',
  'admin.settings.features.poolHealth.enabled',
  'admin.settings.features.poolHealth.enabledHint',
  'admin.settings.features.poolHealth.groupName',
  'admin.settings.features.poolHealth.groupNameHint',
  // 429 默认回避秒数的说明（末尾补了 Anthropic 固定 60 秒）
  'admin.settings.rateLimit429Cooldown.cooldownSecondsHint'
]

describe('本次新增 / 补齐的词条', () => {
  it.each(ADDED_KEYS)('%s 在 zh 与 en 都有非空文案', (key) => {
    expect(text(zh, key).trim(), `zh 缺少 ${key}`).not.toBe('')
    expect(text(en, key).trim(), `en 缺少 ${key}`).not.toBe('')
  })

  it('这些词条都仍被源码引用（词条与界面一一对应，不留孤儿）', () => {
    const referenced = new Set(LARGE_FILES.flatMap(collectKeys))
    expect(ADDED_KEYS.filter((key) => !referenced.has(key))).toEqual([])
  })

  it('公开号池监控卡片：zh 与 en 的词条一一对应', () => {
    const path = 'admin.settings.features.poolHealth'
    expect(resolve(zh, path), `zh 缺少 ${path}`).toBeDefined()
    expect(resolve(en, path), `en 缺少 ${path}`).toBeDefined()
    expect(leafKeys(resolve(en, path), path).sort()).toEqual(leafKeys(resolve(zh, path), path).sort())
  })

  it('admin.accounts.messages：zh 与 en 的词条一一对应', () => {
    const path = 'admin.accounts.messages'
    expect(leafKeys(resolve(en, path), path).sort()).toEqual(leafKeys(resolve(zh, path), path).sort())
    expect(leafKeys(resolve(zh, path), path)).toContain('admin.accounts.messages.accountCreated')
  })
})

describe('429 默认回避秒数的说明', () => {
  const KEY = 'admin.settings.rateLimit429Cooldown.cooldownSecondsHint'

  it('末尾写明 Anthropic 账号固定 60 秒、不受此值影响（zh 与 en 都在）', () => {
    expect(text(zh, KEY)).toContain('Anthropic 账号固定 60 秒，不受此值影响')
    expect(text(en, KEY)).toContain('Anthropic accounts always use a fixed 60 seconds regardless of this value')
  })

  it('这句话是补在原说明之后，原有含义（取值范围、上游明确 reset 时优先）没有被改掉', () => {
    for (const hint of [text(zh, KEY), text(en, KEY)]) {
      expect(hint).toContain('1-7200')
      expect(hint).toContain('reset')
    }
    expect(text(zh, KEY).indexOf('1-7200')).toBeLessThan(text(zh, KEY).indexOf('Anthropic'))
    expect(text(en, KEY).indexOf('1-7200')).toBeLessThan(text(en, KEY).indexOf('Anthropic'))
  })
})

describe('伪装守卫模式的文案', () => {
  const BASE = 'admin.settings.gatewayForwarding'

  it('缺省值 warn 的选项带「默认」标记，拦截与关闭不带', () => {
    expect(text(zh, `${BASE}.guardModeWarn`)).toContain('默认')
    expect(text(en, `${BASE}.guardModeWarn`).toLowerCase()).toContain('default')
    for (const key of ['guardModeBlock', 'guardModeOff']) {
      expect(text(zh, `${BASE}.${key}`)).not.toContain('默认')
      expect(text(en, `${BASE}.${key}`).toLowerCase()).not.toContain('default')
    }
  })

  it('说明里三个取值的含义都讲到了', () => {
    const zhHint = text(zh, `${BASE}.claudeMimicryGuardModeHint`)
    for (const word of ['仅告警', '拦截', '关闭']) {
      expect(zhHint).toContain(word)
    }
    const enHint = text(en, `${BASE}.claudeMimicryGuardModeHint`)
    for (const word of ['Warn only', 'Block', 'Off']) {
      expect(enHint).toContain(word)
    }
  })
})
