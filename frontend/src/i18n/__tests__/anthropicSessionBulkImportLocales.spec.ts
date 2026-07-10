import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

const zhExpected = [
  ['anthropicSessionBulkImport', '批量导入 Claude SessionKey'],
  ['anthropicSessionBulkImportShort', '批量导入'],
  ['anthropicSessionBulkImportTitle', '批量导入 Claude SessionKey'],
  [
    'anthropicSessionBulkImportFormHint',
    '下方账号设置会应用到本次导入的 setup-token 账号。代理留空时将自动分配；分组、并发、限额、会话伪装等设置都会随导入保存。'
  ],
  ['anthropicSessionAutoNamePlaceholder', '导入后自动按邮箱 + 订阅类型命名'],
  ['anthropicSessionAutoNameHint', '批量导入会自动命名，单个账号名称无需手填。'],
  ['oauth.anthropicSessionBulkImport', '批量导入 sessionKey'],
  [
    'oauth.anthropicSessionBulkImportDesc',
    '每行粘贴一个 claude.ai sessionKey。导入时会自动换取 setup-token、自动分配代理，并按邮箱 + 订阅类型命名。'
  ],
  ['oauth.sessionKeys', 'sessionKeys'],
  ['oauth.batchImportAccounts', '将批量导入 {count} 个账号'],
  [
    'oauth.anthropicSessionBulkImportPlaceholder',
    '每行一个 claude.ai sessionKey，例如：\nsk-ant-sid01-xxxxx...\nsk-ant-sid01-yyyyy...'
  ],
  ['oauth.startBatchImport', '开始批量导入'],
  ['oauth.importing', '导入中...']
] as const

const enExpected = [
  ['anthropicSessionBulkImport', 'Bulk Import Claude SessionKeys'],
  ['anthropicSessionBulkImportShort', 'Bulk Import'],
  ['anthropicSessionBulkImportTitle', 'Bulk Import Claude SessionKeys'],
  [
    'anthropicSessionBulkImportFormHint',
    'The account settings below will be applied to imported setup-token accounts. Leave proxy empty for automatic assignment; groups, concurrency, quota controls, session masking, and other settings are saved with the import.'
  ],
  ['anthropicSessionAutoNamePlaceholder', 'Accounts will be named by email + subscription type'],
  ['anthropicSessionAutoNameHint', 'Bulk import auto-names accounts, so no account name is required here.'],
  ['oauth.anthropicSessionBulkImport', 'Bulk import sessionKeys'],
  [
    'oauth.anthropicSessionBulkImportDesc',
    'Paste one claude.ai sessionKey per line. Import exchanges setup tokens, assigns proxies automatically, and names accounts by email + subscription type.'
  ],
  ['oauth.sessionKeys', 'sessionKeys'],
  ['oauth.batchImportAccounts', 'Will bulk import {count} accounts'],
  [
    'oauth.anthropicSessionBulkImportPlaceholder',
    'One claude.ai sessionKey per line, e.g.:\nsk-ant-sid01-xxxxx...\nsk-ant-sid01-yyyyy...'
  ],
  ['oauth.startBatchImport', 'Start Bulk Import'],
  ['oauth.importing', 'Importing...']
] as const

const localeCases = [
  ['zh', zh, zhExpected],
  ['en', en, enExpected]
] as const

describe('Anthropic session bulk import locale copy', () => {
  it.each(localeCases)('contains the exact %s copy', (_locale, messages, expected) => {
    for (const [path, value] of expected) {
      expect(messages.admin.accounts).toHaveProperty(path, value)
    }
  })
})
