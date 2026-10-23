import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe('Claude Code mimicry profile locale copy', () => {
  it('contains the exact Chinese profile copy', () => {
    expect(zh.admin.settings.gatewayForwarding).toMatchObject({
      claudeCodeMimicryProfile: 'Claude Code 伪装 Profile',
      claudeCodeMimicryProfileHint:
        '控制 OAuth mimic 路径使用的 User-Agent、X-Stainless 头、beta 集合与 billing entrypoint。'
    })
  })

  it('contains the exact English profile copy', () => {
    expect(en.admin.settings.gatewayForwarding).toMatchObject({
      claudeCodeMimicryProfile: 'Claude Code Mimicry Profile',
      claudeCodeMimicryProfileHint:
        'Controls the User-Agent, X-Stainless headers, beta set, and billing entrypoint used by the OAuth mimic path.'
    })
  })

  // 设置页直接展示服务端返回的 profile ID，不再为某个具体版本写死词条。
  // 写死版本的词条会在后端升级后悄悄过期（曾经就是界面写 2.1.206、网关实际用 2.1.211）。
  it.each([
    ['zh', zh.admin.settings.gatewayForwarding],
    ['en', en.admin.settings.gatewayForwarding],
  ])('has no version-pinned mimicry profile label in %s', (_locale, copy) => {
    expect(copy).not.toHaveProperty('claudeCodeMimicryProfile2206')
  })

  it.each([
    ['zh', zh.admin.settings.calibratedProfile.rawPlaceholder],
    ['en', en.admin.settings.calibratedProfile.rawPlaceholder],
  ])('keeps the %s calibrated-profile placeholder free of i18n token braces', (_locale, message) => {
    // Vue I18n treats a raw JSON object (`{ "schema_version": ... }`) as a
    // placeholder expression and throws during render, blanking the settings page.
    expect(message).toContain('schema_version=1')
    expect(message).not.toMatch(/[{}]/)
  })
})
