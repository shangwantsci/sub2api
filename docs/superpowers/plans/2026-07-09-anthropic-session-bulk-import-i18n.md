# Anthropic Session Bulk Import i18n Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (\`- [ ]\`) syntax for tracking.

**Goal:** Restore the complete Chinese and English copy contract for the existing Anthropic SessionKey bulk-import flow.

**Architecture:** Keep the already-wired Vue flow unchanged and restore the 13 fork-only message keys in the split zh/en account locale modules. Add a direct locale-contract test so future language-pack reorganizations fail on missing or altered copy instead of allowing vue-i18n to return a fallback/raw key.

**Tech Stack:** Vue 3, TypeScript, vue-i18n, Vitest, npm scripts, Vite.

## Global Constraints

- Modify only the two account locale production files and one new locale test file.
- Restore exactly 13 keys: six under admin.accounts and seven under admin.accounts.oauth.
- Use the exact pre-merge Chinese and English values recorded below.
- Do not modify AccountsView.vue, CreateAccountModal.vue, OAuthAuthorizationFlow.vue, router, stores, API code, or i18n initialization.
- Do not include unrelated missing keys such as poolWeight or poolWeightHint.
- Browser verification may use dummy SessionKey-shaped text, but must not submit the import.
- Use the repository's npm scripts in this environment because pnpm and corepack are unavailable; do not install or modify a global package manager.
- Do not deploy or push.

---

## File Structure

- Create frontend/src/i18n/__tests__/anthropicSessionBulkImportLocales.spec.ts: exact zh/en contract for all 13 paths.
- Modify frontend/src/i18n/locales/zh/admin/accounts.ts: Chinese entry, modal, and OAuth copy.
- Modify frontend/src/i18n/locales/en/admin/accounts.ts: English entry, modal, and OAuth copy.

### Task 1: Restore the locale contract with TDD

**Files:**
- Create: frontend/src/i18n/__tests__/anthropicSessionBulkImportLocales.spec.ts
- Modify: frontend/src/i18n/locales/zh/admin/accounts.ts: immediately after createAccount and inside oauth
- Modify: frontend/src/i18n/locales/en/admin/accounts.ts: immediately after createAccount and inside oauth

**Interfaces:**
- Consumes: the existing default exports from frontend/src/i18n/locales/zh/index.ts and frontend/src/i18n/locales/en/index.ts.
- Produces: messages.admin.accounts paths used by AccountsView, CreateAccountModal, and OAuthAuthorizationFlow.

- [ ] **Step 1: Create the exact failing locale test**

Create frontend/src/i18n/__tests__/anthropicSessionBulkImportLocales.spec.ts with:

~~~ts
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
~~~

- [ ] **Step 2: Run the new test and verify RED**

Run:

~~~bash
npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run test:run -- src/i18n/__tests__/anthropicSessionBulkImportLocales.spec.ts
~~~

Expected: non-zero exit. Both locale cases fail because admin.accounts.anthropicSessionBulkImport does not exist. The failure must be a Vitest assertion failure, not a TypeScript collection error.

- [ ] **Step 3: Restore the six Chinese top-level keys**

In frontend/src/i18n/locales/zh/admin/accounts.ts, add immediately after createAccount:

~~~ts
      anthropicSessionBulkImport: '批量导入 Claude SessionKey',
      anthropicSessionBulkImportShort: '批量导入',
      anthropicSessionBulkImportTitle: '批量导入 Claude SessionKey',
      anthropicSessionBulkImportFormHint:
        '下方账号设置会应用到本次导入的 setup-token 账号。代理留空时将自动分配；分组、并发、限额、会话伪装等设置都会随导入保存。',
      anthropicSessionAutoNamePlaceholder: '导入后自动按邮箱 + 订阅类型命名',
      anthropicSessionAutoNameHint: '批量导入会自动命名，单个账号名称无需手填。',
~~~

- [ ] **Step 4: Restore the seven Chinese OAuth keys**

In the oauth object, place these values beside cookieAutoAuth/sessionKey/batchCreateAccounts/startAutoAuth/authorizing so the related fields remain grouped:

~~~ts
        anthropicSessionBulkImport: '批量导入 sessionKey',
        anthropicSessionBulkImportDesc:
          '每行粘贴一个 claude.ai sessionKey。导入时会自动换取 setup-token、自动分配代理，并按邮箱 + 订阅类型命名。',
        sessionKeys: 'sessionKeys',
        batchImportAccounts: '将批量导入 {count} 个账号',
        anthropicSessionBulkImportPlaceholder:
          '每行一个 claude.ai sessionKey，例如：\nsk-ant-sid01-xxxxx...\nsk-ant-sid01-yyyyy...',
        startBatchImport: '开始批量导入',
        importing: '导入中...',
~~~

- [ ] **Step 5: Restore the six English top-level keys**

In frontend/src/i18n/locales/en/admin/accounts.ts, add immediately after createAccount:

~~~ts
      anthropicSessionBulkImport: 'Bulk Import Claude SessionKeys',
      anthropicSessionBulkImportShort: 'Bulk Import',
      anthropicSessionBulkImportTitle: 'Bulk Import Claude SessionKeys',
      anthropicSessionBulkImportFormHint:
        'The account settings below will be applied to imported setup-token accounts. Leave proxy empty for automatic assignment; groups, concurrency, quota controls, session masking, and other settings are saved with the import.',
      anthropicSessionAutoNamePlaceholder: 'Accounts will be named by email + subscription type',
      anthropicSessionAutoNameHint: 'Bulk import auto-names accounts, so no account name is required here.',
~~~

- [ ] **Step 6: Restore the seven English OAuth keys**

In the oauth object, place:

~~~ts
        anthropicSessionBulkImport: 'Bulk import sessionKeys',
        anthropicSessionBulkImportDesc:
          'Paste one claude.ai sessionKey per line. Import exchanges setup tokens, assigns proxies automatically, and names accounts by email + subscription type.',
        sessionKeys: 'sessionKeys',
        batchImportAccounts: 'Will bulk import {count} accounts',
        anthropicSessionBulkImportPlaceholder:
          'One claude.ai sessionKey per line, e.g.:\nsk-ant-sid01-xxxxx...\nsk-ant-sid01-yyyyy...',
        startBatchImport: 'Start Bulk Import',
        importing: 'Importing...',
~~~

- [ ] **Step 7: Run GREEN and i18n regression tests**

Run:

~~~bash
npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run test:run -- src/i18n/__tests__/anthropicSessionBulkImportLocales.spec.ts
npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run test:run -- src/i18n/__tests__
~~~

Expected: the new test reports two passing locale cases; all existing i18n tests pass.

- [ ] **Step 8: Run the complete frontend build**

Run:

~~~bash
npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run build
~~~

Expected: vue-tsc -b and Vite complete successfully.

- [ ] **Step 9: Verify scope and commit**

Run:

~~~bash
git -C '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork' diff --check
git -C '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork' status --short -- frontend/src/i18n/__tests__/anthropicSessionBulkImportLocales.spec.ts frontend/src/i18n/locales/en/admin/accounts.ts frontend/src/i18n/locales/zh/admin/accounts.ts
~~~

Expected source diff:

~~~text
M frontend/src/i18n/locales/en/admin/accounts.ts
M frontend/src/i18n/locales/zh/admin/accounts.ts
?? frontend/src/i18n/__tests__/anthropicSessionBulkImportLocales.spec.ts
~~~

Commit only these files:

~~~bash
git add frontend/src/i18n/__tests__/anthropicSessionBulkImportLocales.spec.ts frontend/src/i18n/locales/en/admin/accounts.ts frontend/src/i18n/locales/zh/admin/accounts.ts
git commit -m 'fix: restore bulk account import translations'
~~~

### Task 2: Verify the real account-management flow

**Files:**
- Verify only; no source changes expected.

**Interfaces:**
- Consumes: the locale contract from Task 1 and the existing account-management route.
- Produces: browser evidence that the desktop entry and modal copy resolve through real vue-i18n.

- [ ] **Step 1: Check whether a local backend is available**

Run:

~~~bash
curl -fsS http://127.0.0.1:8080/health
~~~

Expected: HTTP success from the local backend. If no backend is available, do not start or mutate production-like services; record browser verification as unavailable and rely on the passing locale tests plus build without claiming browser success.

- [ ] **Step 2: Start the frontend dev server**

Run:

~~~bash
VITE_DEV_PROXY_TARGET=http://127.0.0.1:8080 npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run dev -- --host 127.0.0.1 --port 3000
~~~

Expected: Vite reports http://127.0.0.1:3000.

- [ ] **Step 3: Verify Chinese copy without submitting**

Open http://127.0.0.1:3000/admin/accounts with an existing local admin session, set locale to zh, and verify:

1. The desktop key-icon button reads “批量导入” and its title is “批量导入 Claude SessionKey”.
2. The More Actions entry reads “批量导入 Claude SessionKey”.
3. Opening the flow shows the Chinese title, form hint, automatic-name placeholder, and automatic-name hint.
4. Click “下一步” to enter the authorization step, then verify “批量导入 sessionKey”, its description, “sessionKeys”, the eight-row textarea, and “开始批量导入”.
5. Enter exactly two dummy lines, sk-ant-sid01-dummy-one and sk-ant-sid01-dummy-two, and verify “将批量导入 2 个账号”. Do not click the submit button.
6. The browser console has no missing-key warning for any of the 13 admin.accounts paths and the page shows no raw key text.

- [ ] **Step 4: Verify English copy and stop the dev server**

Switch locale to en and verify “Bulk Import”, “Bulk Import Claude SessionKeys”, “Bulk import sessionKeys”, and “Start Bulk Import”. Stop the Vite process after verification. No commit is required because this task must not change files.
