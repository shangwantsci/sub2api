import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))

function readSource(relative: string): string {
  return readFileSync(resolve(here, relative), 'utf-8')
}

/**
 * 供号商上号页必须彻底不含 Chrome OAuth 与人格。
 *
 * 后端已经在 service 层硬拒绝（AssertProviderOAuthClientAllowed / SanitizeProviderExtra），
 * 这里守的是前端：一旦有人为了「顺手支持一下」在上号页加回 chrome 选项或人格字段，
 * 就会暴露平台对账号做了什么处理，这正是整个供号商站点要隐藏的东西。
 *
 * 用源码文本断言而不是挂载组件，是因为要覆盖模板文案、选项值与 API 调用三处，
 * 挂载后只能看到当前渲染分支，藏在 v-if 里的入口反而检查不到。
 */
describe('ProviderOnboard source', () => {
  const source = readSource('../ProviderOnboard.vue')

  it('never mentions Chrome authorization', () => {
    const lowered = stripComments(source).toLowerCase()
    for (const needle of ['chrome', 'claude_chrome', 'chrome-cookie-auth']) {
      expect(lowered).not.toContain(needle)
    }
  })

  it('never exposes persona configuration', () => {
    const lowered = stripComments(source).toLowerCase()
    for (const needle of ['persona', '人格']) {
      expect(lowered).not.toContain(needle)
    }
  })

  it('never exposes the forced mimicry switches', () => {
    const lowered = stripComments(source).toLowerCase()
    for (const needle of [
      'enable_tls_fingerprint',
      'session_id_masking',
      'intercept_warmup_requests',
      'tls',
    ]) {
      expect(lowered).not.toContain(needle)
    }
  })

  it('only offers the two allowed account methods', () => {
    expect(source).toContain('value="oauth"')
    expect(source).toContain('value="setup-token"')
    expect(source).not.toContain('cookie_chrome')
  })
})

/** 去掉注释，只留真正会渲染给用户的字符串。 */
function stripComments(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/^\s*\/\/.*$/gm, '')
}

describe('Provider portal copy', () => {
  it('does not leak internal processing terms to providers', () => {
    for (const locale of ['zh', 'en']) {
      // 只检查文案本身；文件头解释这条规则的注释里必然出现这些词，不算泄露。
      const copy = stripComments(
        readSource(`../../../i18n/locales/${locale}/provider.ts`)
      ).toLowerCase()
      for (const needle of [
        'chrome',
        'persona',
        '人格',
        'tls',
        'content_review',
        'system prompt',
        '系统提示词',
        '内容审查',
      ]) {
        expect(copy, `${locale} provider copy must not mention ${needle}`).not.toContain(needle)
      }
    }
  })
})
