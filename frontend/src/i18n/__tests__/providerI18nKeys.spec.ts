import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

/**
 * 供号商相关界面引用的 i18n key 必须在 zh 与 en 两侧都存在。
 *
 * 缺失的 key 不会报任何错误，vue-i18n 会把原始 key 直接渲染到界面上
 * （例如设置页的校验提示直接显示 "common.required"）。这类问题在类型检查、
 * 构建、单元测试里全都看不出来，只能靠这样扫源码比对。
 */

const FILES = [
  'src/views/provider/ProviderLogin.vue',
  'src/views/provider/ProviderRegister.vue',
  'src/views/provider/ProviderDashboard.vue',
  'src/views/provider/ProviderOnboard.vue',
  'src/views/provider/ProviderAccounts.vue',
  'src/views/provider/ProviderBilling.vue',
  'src/views/admin/ProvidersView.vue',
  'src/components/admin/provider/ProviderSettingsPanel.vue',
  'src/components/layout/ProviderLayout.vue',
  'src/components/layout/ProviderAuthLayout.vue',
]

/** 匹配 t('a.b.c') / t("a.b.c")，只取静态字面量 key。 */
const T_CALL = /\bt\(\s*(['"])([A-Za-z0-9_.]+)\1/g

function collectKeys(relPath: string): string[] {
  const source = readFileSync(join(process.cwd(), relPath), 'utf8')
  const keys = new Set<string>()
  for (const match of source.matchAll(T_CALL)) {
    keys.add(match[2])
  }
  return [...keys]
}

function resolve(bundle: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>((node, segment) => {
    if (node && typeof node === 'object' && segment in (node as Record<string, unknown>)) {
      return (node as Record<string, unknown>)[segment]
    }
    return undefined
  }, bundle)
}

describe('provider i18n keys', () => {
  for (const file of FILES) {
    it(`${file} 引用的 key 在 zh 与 en 都存在`, () => {
      const keys = collectKeys(file)
      // 没抓到任何 key 说明正则或路径失效了，此时上面的断言会空转通过。
      expect(keys.length, `${file} 没解析出任何 t() 调用，检查正则或文件路径`).toBeGreaterThan(0)

      const missingZh: string[] = []
      const missingEn: string[] = []

      for (const key of keys) {
        if (typeof resolve(zh, key) !== 'string') missingZh.push(key)
        if (typeof resolve(en, key) !== 'string') missingEn.push(key)
      }

      expect(missingZh, `zh 缺少这些 key，界面会直接显示原始 key`).toEqual([])
      expect(missingEn, `en 缺少这些 key，界面会直接显示原始 key`).toEqual([])
    })
  }
})
