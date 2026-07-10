# Claude Code 2.1.206 伪装 Profile 与批量导入文案修复设计

状态：晓宇已于 2026-07-09 确认设计方向。

## 1. 背景

本轮同时解决两个独立问题：

1. 本机 Claude Code 已更新到 2.1.206，但仓库默认伪装 profile 仍固定在 2.1.197。必须以本地受控抓包为依据更新请求形态，不能只改版本字符串。
2. 账号管理页保留了 Anthropic SessionKey 批量导入功能，但合并 3c733adc 时没有把 fork 专属翻译迁移到拆分后的中英文语言模块，导致入口及后续流程显示英文回退或原始翻译 key。

当前工作树在调查前后均保持干净，分支为 custom/prod。本设计不包含部署或推送。

## 2. 目标

- 让 synthetic Claude Code mimic 的 messages 请求与本机 Claude Code 2.1.206 抓包证据一致。
- 按模型族分别维护 Opus、Sonnet、Haiku、Fable 的 beta 集合、输出上限及 thinking/output_config 默认值。
- 为 Fable 5 使用其真实的模型专属静态系统提示词，而不是继续复用 Opus/Sonnet 模板。
- 保持管理员自定义 Claude OAuth system prompt/system blocks 的优先级和行为不变。
- 将默认 profile ID、后台默认值、管理页选项及旧值加载行为统一到 2.1.206。
- 恢复批量导入完整路径使用的 13 个中英文翻译 key。
- 通过先失败测试、后最小实现的 TDD 流程完成。

## 3. 非目标

- 不修改 TLS ClientHello、JA3/JA4、ALPN、HTTP/2 settings 或内置 TLS profile。普通 HTTP dummy listener 不足以证明这些字段。
- 不推断或刷新 count_tokens 专属 beta/body 形态。本轮抓包没有触发该端点。
- 不修改 Anthropic API-key 账号的 beta 注入策略。
- 不重构账号调度、认证、计费或代理逻辑。
- 不保存真实 token，不修改系统代理，不安装证书，不访问真实 Anthropic。
- 不部署、不推送。

## 4. 已验证证据

### 4.1 共享字段

| 字段 | 仓库当前值 | Claude Code 2.1.206 抓包 |
| --- | --- | --- |
| Profile ID | cc-2.1.197-sdk-cli-macos-arm64 | 应更新为 cc-2.1.206-sdk-cli-macos-arm64 |
| User-Agent | claude-cli/2.1.197 (external, sdk-cli) | claude-cli/2.1.206 (external, sdk-cli) |
| Billing | cc_version=2.1.197.{fp}; cc_entrypoint=sdk-cli; | cc_version=2.1.206.129; cc_entrypoint=sdk-cli; |
| X-Stainless package | 0.94.0 | 0.94.0 |
| X-Stainless runtime | node / v26.3.0 | node / v26.3.0 |
| OS / arch | MacOS / arm64 | MacOS / arm64 |
| metadata.user_id | JSON：device_id/account_uuid/session_id | 同结构；device_id 为 64 位 hex，session_id 为 UUID |
| Session header | 从 metadata.user_id 派生 | X-Claude-Code-Session-Id 存在且为 UUID |
| Identity system block | Claude Agent SDK 固定身份句 | 完全一致 |

Billing 尾部 fingerprint 值依赖请求体，本设计只固定版本前缀和 entrypoint，不固定示例中的 129。

### 4.2 模型族差异

| 模型族 | messages beta（按 wire 顺序） | max_tokens | thinking | output_config |
| --- | --- | --- | --- | --- |
| Sonnet | claude-code, interleaved-thinking, tool-search-tool, effort | 32000 | adaptive | effort=high |
| Opus | claude-code, interleaved-thinking, tool-search-tool, effort | 64000 | adaptive | effort=high |
| Haiku | claude-code, tool-search-tool | 32000 | enabled，budget_tokens=31999 | 无 |
| Fable | claude-code, interleaved-thinking, tool-search-tool, effort, fallback-credit | 64000 | adaptive | effort=high |

完整 token 值使用抓包中的字面量：

- claude-code-20250219
- interleaved-thinking-2025-05-14
- tool-search-tool-2025-10-19
- effort-2025-11-24
- fallback-credit-2026-06-01

### 4.3 系统提示词差异

- Opus 抓包第三个 system block 长度为 27758，Sonnet 为 27765，均使用以 “# System / # Doing tasks / # Using your tools” 为主体的完整 harness。
- 当前仓库通用静态 expansion 去除首尾空白后为 12805 字符；其 53 个非空行全部存在于 2.1.206 Opus 抓包中，完整文件也是抓包正文的连续子串。因此通用静态文件无需改写。
- Fable 抓包第三个 system block 长度为 10650，章节以 “# Harness / # Communicating with the user” 开头。当前通用静态 expansion 的 53 个非空行中有 50 行不在 Fable 抓包里，不能复用。

产品层面为何选择不同 harness 无法从抓包证明；本设计只依据 wire 行为做模型感知选择。

## 5. 方案比较与决策

### 方案 A：模型感知的精确静态模板（采用）

- 保留已被 2.1.206 再验证的通用 expansion。
- 对 Fable 进行两次完整本地抓包，提取两次都一致的模型静态内容，形成独立内嵌文件。
- 根据最终 body.model 选择默认 expansion。
- 管理员显式配置的 expansion/system blocks 始终优先。

优点：每个模型族与真实 wire 一致；不会把本机动态信息硬编码进网关。缺点：新增一个小型模型选择分支和一个资源文件。

### 方案 B：用 Fable 短模板替换全局模板（不采用）

会让 Opus、Sonnet、Haiku 偏离已验证抓包。

### 方案 C：保留当前全局模板（不采用）

改动最少，但 Fable 已被抓包证明不匹配。

## 6. 详细设计

### 6.1 Fable 完整抓包与静态提取

抓包继续使用只监听 127.0.0.1 的本地 raw HTTP listener：

- Claude Code 使用 2.1.206。
- 禁用 user/local settings source，显式设置 CLAUDE_CODE_USE_GATEWAY=1。
- Base URL 仅指向本地 listener，认证值使用 dummy token。
- 请求模型固定为 claude-fable-5。
- listener 对认证头脱敏，只处理首个完整请求。

进行两次抓包，使用不同 session ID 和不同受控工作目录。对 system 第三个 text block 做逐行比较：

- 只保留两次完全一致、且不含用户路径、会话 ID、日期、memory 内容或环境值的静态部分。
- Memory、Environment、Context management 等动态章节不进入仓库。
- 如果两次抓包无法得到稳定边界，停止实现并报告“证据不足”，不得主观拼接。
- 原始抓包仅作为临时本地工件；提取完成后清理，不提交。

提取结果保存为：

- backend/internal/service/prompts/claude_code_fable_system_prompt_expansion.txt

### 6.2 Profile 常量与模型解析

backend/internal/pkg/claude/constants.go 将：

- CLICurrentVersion 更新为 2.1.206。
- DefaultClaudeCodeMimicryProfileID 更新为 cc-2.1.206-sdk-cli-macos-arm64。
- 新增 tool-search-tool-2025-10-19 常量。
- 用第 4.2 节的字面量列表更新 messages profile。
- 将当前合并的 opus-sonnet 分支拆成 sonnet 与 opus/default，保留不同 max_tokens。
- 保留 Haiku 和 Fable 的现有 thinking/output_config 形态，只更新已抓到的 beta。

count_tokens 边界必须显式保持：

- CountTokensBetas 不再由更新后的 messages beta 自动推导。
- 继续保留本轮开始前的 count_tokens 专属列表和 token-counting 行为。
- 模型 profile 增加独立的 count_tokens 默认 max_tokens；Sonnet 在 messages 更新为 32000 时，count_tokens 继续保持本轮前的 64000。
- count_tokens 规范化调用必须显式标识端点类型，不能复用更新后的 messages max_tokens。
- 新测试必须证明本轮没有改变 count_tokens 专属 beta 列表。
- 共享的 CLI 版本身份会自然更新到 2.1.206，但不修改 count_tokens 专属算法、body 默认值、默认 expansion 或 beta 组合。

### 6.3 模型感知的默认 expansion

新增一个小型纯函数，根据规范化后的 model ID 返回默认静态 expansion：

- 包含 fable 时返回 Fable 文件。
- 其他模型返回现有通用文件。
- messages synthetic mimic 使用上述模型感知选择。
- count_tokens 在管理员 legacy prompt 为空时继续显式使用本轮前的通用 expansion，避免未抓到该端点却间接切换到 Fable 模板。

选择发生在构造第三个 system block 之前。优先级保持为：

1. 管理员配置的 system blocks
2. 管理员配置的 legacy expansion prompt
3. 模型感知的内置默认 expansion

不改变 billing block、身份 block、原始 system 迁移到 messages 的逻辑。

### 6.4 管理页 profile 一致性

frontend/src/views/admin/SettingsView.vue 将：

- 默认值、选项值和提交 fallback 更新到 2.1.206。
- 使用单一前端常量，避免三处字面量再次漂移。
- 加载设置时，如果后端返回旧 profile ID 或未知 ID，将表单归一化为当前唯一受支持的 profile。

中英文 settings 语言模块增加当前 profile 的标题、2.1.206 选项文案及说明。后台 runtime 已通过 ResolveClaudeCodeMimicryProfile 将旧值归一化到当前默认，继续保留该兼容行为。

### 6.5 批量导入国际化修复

仅修改以下两个文件：

- frontend/src/i18n/locales/zh/admin/accounts.ts
- frontend/src/i18n/locales/en/admin/accounts.ts

从合并前已验证版本恢复 13 个 fork 专属 key。

账号顶层 6 个：

- anthropicSessionBulkImport
- anthropicSessionBulkImportShort
- anthropicSessionBulkImportTitle
- anthropicSessionBulkImportFormHint
- anthropicSessionAutoNamePlaceholder
- anthropicSessionAutoNameHint

oauth 子对象 7 个：

- anthropicSessionBulkImport
- anthropicSessionBulkImportDesc
- sessionKeys
- batchImportAccounts
- anthropicSessionBulkImportPlaceholder
- startBatchImport
- importing

不修改 AccountsView、CreateAccountModal 或 OAuthAuthorizationFlow 的渲染逻辑。

## 7. 数据流

### 7.1 Synthetic mimic

客户端请求 → 判断 OAuth synthetic mimic → 规范化模型 ID → 选择模型 profile → 应用 body 默认值 → 选择管理员或模型默认 expansion → 构造三段 system blocks → 重写 metadata → 计算 messages beta → 强制 headers → 同步 session header → guard 校验 → 上游请求。

### 7.2 批量导入文案

浏览器 locale → 动态加载 zh/en → t(翻译 key) → 新语言模块命中对应文案 → 账号入口、创建弹窗和 OAuth 批量流程使用同一套完整翻译。

## 8. 测试设计

### 8.1 RED：后端

先修改或新增字面量测试，使当前 2.1.197 实现按预期失败：

- 默认 profile ID、CLI 版本和 User-Agent 必须为 2.1.206。
- 四个模型族的 messages beta、max_tokens、thinking/output_config 使用抓包字面量断言，不能从生产常量反推期望。
- Sonnet 与 Opus 的 max_tokens 必须分别为 32000 和 64000。
- Fable 默认 expansion 必须选择新文件；其他模型必须继续选择现有文件。
- messages wire 测试必须验证字面量 beta 顺序、billing 版本前缀和模型默认值。
- count_tokens 回归测试记录本轮前的专属 beta 列表、Sonnet 64000 默认值及通用 expansion 哈希，并断言更新后完全不变。

### 8.2 RED：前端

- 新增语言包完整性测试，直接导入 zh/en 聚合消息并断言 13 个路径均存在且非空。
- 明确断言中文短按钮为“批量导入”，英文为“Bulk Import”。
- SettingsView 测试断言 2.1.206 profile，并覆盖加载旧 2.1.197 后归一化为新值。

### 8.3 GREEN 与回归验证

后端至少运行：

- GOCACHE=/private/tmp/sub2api-go-build-cache go test -tags=unit ./internal/pkg/claude ./internal/service

前端至少运行：

- npm run test:run -- 相关 i18n 与 SettingsView 测试
- npm run build

最后启动本地前端，用浏览器在中文 locale 下实际检查：

- 账号管理入口显示“批量导入”。
- 展开批量导入弹窗后，标题、说明、输入提示、按钮和 loading 文案均为中文。
- Claude Code profile 选项显示 2.1.206，旧值不会造成空选项。

## 9. 安全、失败处理与兼容性

- listener 仅绑定 loopback，认证头必须脱敏，禁止真实 Anthropic 请求。
- 抓包正文中的本机路径、memory、环境变量、日期与 session 数据不得提交。
- Fable 静态提取不稳定时停止，不以猜测补齐。
- 旧 profile ID 在 runtime 和前端加载时都归一化到当前默认；不需要数据库迁移。
- 管理员自定义 prompt/system blocks 不被覆盖。
- 不改变真实 Claude Code passthrough 分支。
- 不改变 TLS 与 count_tokens 专属行为。

## 10. 预计修改范围

后端：

- backend/internal/pkg/claude/constants.go
- backend/internal/pkg/claude/constants_test.go
- backend/internal/service/gateway_service.go
- backend/internal/service/gateway_claude_oauth_body.go
- backend/internal/service/gateway_count_tokens.go
- backend/internal/service/gateway_beta_test.go
- backend/internal/service/gateway_body_order_test.go
- backend/internal/service/gateway_context_management_test.go
- backend/internal/service/gateway_oauth_metadata_test.go
- backend/internal/service/gateway_claude_wire_test.go
- backend/internal/service/prompts/claude_code_fable_system_prompt_expansion.txt
- 模型 prompt 选择与 count_tokens 保持测试

前端：

- frontend/src/views/admin/SettingsView.vue
- frontend/src/views/admin/__tests__/SettingsView.spec.ts
- frontend/src/i18n/locales/zh/admin/accounts.ts
- frontend/src/i18n/locales/en/admin/accounts.ts
- frontend/src/i18n/locales/zh/admin/settings.ts
- frontend/src/i18n/locales/en/admin/settings.ts
- frontend/src/i18n/__tests__/anthropicSessionBulkImportLocales.spec.ts
- frontend/src/i18n/__tests__/claudeCodeMimicryProfileLocales.spec.ts

不应修改模型定价、模型白名单、账号调度、TLS profile 或部署文件。

## 11. 验收标准

- 本地 Claude Code 2.1.206 的四个模型族 messages 抓包字段与 synthetic mimic 的测试期望一致。
- Fable 使用独立、经双抓包确认的静态 expansion；其他模型继续使用已验证通用 expansion。
- count_tokens 专属 beta、Sonnet max_tokens 和默认 expansion 与本轮前一致。
- 管理页 profile 完整迁移到 2.1.206，旧值加载正常。
- 批量导入完整流程的 13 个 key 在中英文语言包中存在。
- 后端测试、前端测试和前端 build 全部通过。
- 中文浏览器实测通过。
- 工作区不含原始抓包、真实凭据或其他临时文件。
- 未部署、未推送。
