import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe('Claude Code mimicry profile locale copy', () => {
  it('contains the exact Chinese profile copy', () => {
    expect(zh.admin.settings.gatewayForwarding).toMatchObject({
      claudeCodeMimicryProfile: 'Claude Code 伪装 Profile',
      claudeCodeMimicryProfile2206: 'Claude Code 2.1.206 / macOS arm64',
      claudeCodeMimicryProfileHint:
        '控制 OAuth mimic 路径使用的 User-Agent、X-Stainless 头、beta 集合与 billing entrypoint。'
    })
  })

  it('contains the exact English profile copy', () => {
    expect(en.admin.settings.gatewayForwarding).toMatchObject({
      claudeCodeMimicryProfile: 'Claude Code Mimicry Profile',
      claudeCodeMimicryProfile2206: 'Claude Code 2.1.206 / macOS arm64',
      claudeCodeMimicryProfileHint:
        'Controls the User-Agent, X-Stainless headers, beta set, and billing entrypoint used by the OAuth mimic path.'
    })
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
