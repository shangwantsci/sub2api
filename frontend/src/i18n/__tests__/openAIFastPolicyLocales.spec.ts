import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

const localeCases = [
  [
    'en',
    en,
    {
      userIds: 'Specific user IDs',
      userIdsHint:
        'Leave empty to apply to all Sub2API users. Specified users match requests from their API keys and take precedence over global rules.',
      userIdPlaceholder: 'e.g., 1001',
      addUserId: 'Add user ID',
      removeUserId: 'Remove user ID'
    }
  ],
  [
    'zh',
    zh,
    {
      userIds: '指定用户 ID',
      userIdsHint: '留空表示对全部 Sub2API 用户生效。指定后仅匹配这些用户的 API Key 请求，且优先于全局规则。',
      userIdPlaceholder: '例如: 1001',
      addUserId: '添加用户 ID',
      removeUserId: '移除用户 ID'
    }
  ]
] as const

describe.each(localeCases)('%s OpenAI Fast/Flex policy locale', (_locale, messages, expected) => {
  it('owns the user-ID copy instead of the Anthropic beta policy', () => {
    expect(messages.admin.settings.openaiFastPolicy).toMatchObject(expected)

    for (const key of Object.keys(expected)) {
      expect(Object.prototype.hasOwnProperty.call(messages.admin.settings.betaPolicy, key)).toBe(false)
    }
  })
})
