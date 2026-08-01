import { describe, it, expect, vi, afterEach } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

/**
 * 每条文案都必须能被 vue-i18n 真正编译出来。
 *
 * vue-i18n 的消息格式里这些字符有语法含义：
 *   {xxx}  具名插值        @:key  linked message        |  复数分支
 * 文案里出现它们而语法又不完整时，**编译失败不会抛异常**，只会打一条
 * `Message compilation error` 到 console，然后返回一个渲染不出来的结果 ——
 * 结果是整页白屏，而任何 `expect(...).not.toThrow()` 式的断言都发现不了。
 * 所以这里必须去抓 console 输出。
 *
 * 本 fork 已经栽过两次：
 *   4.6  裸 JSON 花括号写进文案 → 系统设置页整页打不开
 *   本次 供号商上号页的 `user:pass@host:port` → /provider/onboard 白屏
 * 正确写法是把字面量包起来，例如 {'@'}，见 admin/resources.ts 的代理格式说明。
 */
function flatten(obj: unknown, prefix = ''): Array<[string, string]> {
  const out: Array<[string, string]> = []
  if (typeof obj !== 'object' || obj === null) return out
  for (const [key, value] of Object.entries(obj as Record<string, unknown>)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (typeof value === 'string') {
      out.push([path, value])
    } else if (typeof value === 'object' && value !== null) {
      out.push(...flatten(value, path))
    }
  }
  return out
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe.each([
  ['zh', zh],
  ['en', en]
])('%s 全部文案可编译', (locale, messages) => {
  it('渲染每条文案时不产生编译错误', () => {
    const entries = flatten(messages)
    expect(entries.length).toBeGreaterThan(100)

    const problems: string[] = []
    const capture = (...args: unknown[]) => {
      const text = args.map((a) => String(a)).join(' ')
      if (text.includes('compilation') || text.includes('Invalid linked')) {
        problems.push(text.split('\n')[0])
      }
    }
    vi.spyOn(console, 'error').mockImplementation(capture)
    vi.spyOn(console, 'warn').mockImplementation(capture)

    const i18n = createI18n({
      legacy: false,
      locale,
      messages: { [locale]: messages as Record<string, unknown> },
      missingWarn: false,
      fallbackWarn: false
    })

    const failed: string[] = []
    for (const [path] of entries) {
      problems.length = 0
      // 带上常见具名参数，避免因缺参数而误判。
      i18n.global.t(path as never, { address: 'x', count: 1, max: 1, id: 1, name: 'x' } as never)
      if (problems.length > 0) {
        failed.push(`${path}: ${problems[0]}`)
      }
    }

    expect(failed, `以下文案无法编译（裸 @ 或花括号需要写成 {'@'} 这类字面量）:\n${failed.join('\n')}`)
      .toEqual([])
  })
})
