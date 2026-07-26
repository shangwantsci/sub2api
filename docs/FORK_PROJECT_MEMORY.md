# Sub2API 二开项目记忆

> 最后更新：2026-07-25
> 目的：记录本 fork 的设计目标、生产状态、GitHub 自动化、上线/回滚流程、已验证结论和后续优化方向。后续 Agent 或维护者应先读本文，再修改 Claude 伪装、账号调度或部署流程。

## 1. 唯一核心目标

让池中每个 Anthropic 账号在服务器上的使用行为和出站字节，看起来像一个真人在一台稳定的本机工作站中使用官方 Claude Code CLI，而不是被大量员工流量复用的 API 中转账号。

这个目标拆成三层：

1. **身份层**：账号绑定稳定代理出口、时区、locale、设备/TLS 指纹。
2. **行为层**：作息窗口、低并发、日请求上限、会话粘性，把流量整形成单人节奏。
3. **字节层**：真实 CLI 自动标定 headers、betas、billing fingerprint，网关热加载版本化 profile。

可选的“每账号一个真实 CLI 沙箱”仍属于后续基础设施项目，不是当前生产请求主链路。

## 2. 分支与版本策略

### 2.1 分支职责

- `custom/prod`：二开应用代码的生产来源。
- `custom/company`：公司内部部署分支，从 `custom/prod` 派生，只做减法，见 2.3。
- `main`：保留官方版本线，同时承载 GitHub Actions workflow。
- `upstream`：官方 `Wei-Shaw/sub2api`，禁止 push。

本文档与 `FORK_DEPLOY_RUNBOOK_CN.md` 在 `custom/prod` 与 `custom/company` 上保持
**逐字一致**，避免每次同步都在文档上产生冲突。公司线的全部细节只写在
`FORK_COMPANY_DEPLOY_CN.md`，该文件只存在于 `custom/company`。

### 2.2 版本和镜像

- 应用语义版本继续读取 `backend/cmd/server/VERSION`，当前为 `0.1.156`。
- 二开源码身份使用 8 位 commit。
- 不为每次二开部署创建 Git tag。
- GitHub Actions `custom_image_only` 同时发布：
  - `ghcr.io/shangwantsci/sub2api:<官方 VERSION>`：生产使用；
  - `ghcr.io/shangwantsci/sub2api:<官方 VERSION>-<commit>`：不可变审计/回滚。

二进制 `--version` 必须同时显示官方 VERSION 与二开 commit。

### 2.3 部署线矩阵

本 fork 有两条互不相同的部署线。动手前先确认自己在哪条线上：

```bash
git branch --show-current
```

| | 生产线 | 公司线 |
|---|---|---|
| 分支 | `custom/prod` | `custom/company` |
| 应用代码 | 完整 | 删除了两个账号录入入口 |
| 构建 workflow | `release.yml`（`custom_image_only`） | `company-image.yml` |
| 产物 | 推送 GHCR | `docker save` 成 artifact，不推任何 registry |
| 镜像 tag | `ghcr.io/shangwantsci/sub2api:<VER>[-<commit>]` | `sub2api-company:<VER>-<commit>` |
| 分发 | 服务器 `docker compose pull` | `gh run download` → `scp` → `docker load` |
| 对外入口 | Caddy 反代 + 域名 | 无域名、无反代，NewAPI 走本机回环 |
| 标定 profile | GitHub 定时自动发布 | 手工同步 |
| 运维文档 | `FORK_DEPLOY_RUNBOOK_CN.md` | `FORK_COMPANY_DEPLOY_CN.md` |

**危险操作：不要用 `release.yml` 构建 `custom/company`。** `custom_image_only`
的可变 tag 只由 `backend/cmd/server/VERSION` 决定，两个分支都是 `0.1.156`；这样做
会把生产正在拉取的 `ghcr.io/shangwantsci/sub2api:0.1.156` 覆盖成公司镜像，生产
下一次 `docker compose pull` 就会静默换成公司版代码。公司线必须走
`company-image.yml`，它已内置守卫，拒绝构建 `main` 与 `custom/prod`。

**同步方向单向**：`custom/prod` → `custom/company`。公司分支只做减法，不在其上
开发新功能，也永远不合回 `custom/prod`。`backend/cmd/server/VERSION` 以
`custom/prod` 为准，公司分支不单独改。

日常在 `custom/prod` 上开发，改动需要带到公司线时：

```bash
git switch custom/company
git merge custom/prod
# 必检：这两个 spec 是反向断言（断言被删入口不存在），失败即说明合并把入口带回来了
cd frontend && npm run test:run -- \
  src/components/account/__tests__/OAuthAuthorizationFlow.spec.ts \
  src/components/admin/account/__tests__/AccountActionMenu.spark_shadow.spec.ts
git push origin custom/company
gh workflow run company-image.yml --ref custom/company
git switch custom/prod          # 记得切回来
```

不同步也不会出问题——公司机器跑的是已 `docker load` 的固定 tag，不会自动更新。

三份文档的分工（本文与 `FORK_DEPLOY_RUNBOOK_CN.md` 在两分支上逐字一致，
`FORK_COMPANY_DEPLOY_CN.md` 只存在于 `custom/company`）：

```text
FORK_PROJECT_MEMORY.md      共享知识库：设计目标、已实现功能、生产状态、分支矩阵
FORK_DEPLOY_RUNBOOK_CN.md   生产线运维（custom/prod → GHCR → lumos7.cc）
FORK_COMPANY_DEPLOY_CN.md   公司线运维（custom/company → artifact → 154.29.158.57）
```

定时标定不受影响：`release.yml` 的 `claude-calibration` job 在 schedule 触发时
**硬编码** `ref: custom/prod`，且 GitHub 定时 workflow 只从默认分支 `main` 的
workflow 文件调度。公司分支的存在与部署对它零影响，反之亦然。

## 3. 当前生产状态

截至 2026-07-25 `claude-opus-5` 上线：

- 镜像：`ghcr.io/shangwantsci/sub2api:0.1.156`
- 不可变镜像：`ghcr.io/shangwantsci/sub2api:0.1.156-8782b30f`
- 镜像 digest：`sha256:8ec3a0244e68a12182681abd395765773500f7a18ecbd7f2371eda3e565fdb13`
- 应用 commit：`8782b30f`
- 应用版本：`0.1.156`
- GitHub Actions run：`30137006332`（`custom-image` success，4m54s）
- 平台：Linux x86_64 / Docker Compose
- 生产目录：`/opt/sub2api-production`
- Compose：
  - `docker-compose.local.yml`
  - `docker-compose.override.yml`
- 应用本地映射：`127.0.0.1:18080 -> 8080`
- 公网入口：Caddy 反代
- PostgreSQL、Redis、Caddy 与其它项目独立运行；部署只重建 `sub2api`
- 健康状态：healthy
- 本机与公网 `/health`：HTTP 200
- 设置接口与标定状态接口保持鉴权保护；无凭证请求为 HTTP 401
- Persona 全局门控：`false`（尚未灰度启用）
- 生产机不运行 `cc-calibrate` sidecar
- 标定 profile：published + valid，CLI `2.1.218`
- 数据库迁移 `179_expand_claude_oauth_system_prompt_policy.sql` 已应用，CHECK
  constraint 已包含 `identity_only`；PostgreSQL、Redis、Caddy 均未重建
- 事务验证中分组 14 可写入 `identity_only` 并成功回滚到原值 `enabled`；部署脚本
  没有自动切换客户配置。随后该分组于 13:36:12 经管理操作启用
  `identity_only`，当前使用该模式的分组数为 1
- 生产二进制已确认包含 `identity_only` 与“仅必要身份”前端标签
- 两块提示词及 system 迁移辅助文本由测试实测增量约 53 tokens；主 messages、
  count_tokens 与 Mimicry Guard block 模式均通过端到端 wire 测试
- 分组 14 启用后的首个观察窗口有 13 条成功 usage、7 个账号、覆盖
  Fable/Haiku/Opus/Sonnet；Mimicry Guard 的 `missing_billing_block`、
  `missing_agent_sdk_identity`、`system_block_count` 合计为 0
- 首个生产 `credits_required`：SetupToken 账号 `2069` 写入
  `model_access_denials["claude-fable-5"]` 后保持 active/schedulable；同一请求清理
  粘性并切换到 Max 账号 `2624`，最终 HTTP 200（4264ms）
- denial 生效后账号 `2069` 的 Fable 再次选中数为 0；Opus/Haiku/Sonnet 不受该
  模型拒绝影响
- `claude-opus-5` 已进入 `claude.DefaultModels`、Bedrock 默认映射与前端模型列表；
  容器内定价兜底表含该 key（远程 LiteLLM 表尚未收录，由 `mergeFallbackPricingData`
  补齐）；Antigravity 侧未加入，待 sync-upstream 探测后再定
- 本轮启动窗口 panic / error 级日志为 0；容器 8 秒转 healthy
- 部署后首轮 token refresh：`total=67, needs_refresh=1, refreshed=0, failed=1`；
  唯一 failed 为账号 `2512` 的 SOCKS 代理
  `username/password authentication failed`，属存量代理凭证问题
- 部署时 `.env` 备份：`backups/.env.20260725-010617.before-8782b30f`
- 上一轮 `a16045ee`（2026-07-23）：digest
  `sha256:47209037118a083d3bb51a004899768e27d119f1be339a4262c4c3aba849094c`，
  run `29982187637`，`.env` 备份 `backups/.env.20260723-053306.before-a16045ee`，
  首轮 token refresh `total=87, needs_refresh=3, refreshed=3, failed=0`

部署前旧镜像已保留为本地回滚 tag：

```text
sub2api-rollback:pre-8782b30f
```

## 4. 已实现功能

### 4.1 Persona 信封

配置存放在 `account.Extra`（JSONB），不需要数据库 schema 迁移：

- `persona_enabled`
- `persona_timezone`
- `persona_locale`
- `persona_active_start_hour`
- `persona_active_end_hour`
- `persona_max_concurrency`
- `persona_daily_request_cap`

运行时采用双开关：

```text
enable_persona_gating == true
AND
account.extra.persona_enabled == true
```

任一条件不成立，账号行为与改造前一致。

已经接入：

- 作息窗口候选过滤；
- 人格并发上限；
- Redis 日请求计数；
- 按人格时区的本地午夜跨日重置；
- Persona 门控覆盖普通调度、粘性账号、回退候选；
- Redis/设置读取异常时 fail-open。

### 4.2 Persona 地理默认

创建/编辑账号时：

- 时区选“自动”时，根据代理出口国家/地区补全 IANA timezone；
- locale 选“自动”时，根据代理国家补全 BCP-47 locale；
- 美国代理按 Region 优先映射东部/中部/山地/西部时区；
- 显式时区和代理国家不一致时只告警，不阻断保存。

前端不再要求手填枚举字段：

- 时区：自动、东八区、新加坡、日本、美国东/中/山地/西部；
- Locale：自动、`zh-CN`、`en-US`、`en-SG`、`ja-JP`；
- 选择时区会联动推荐 locale；
- 历史自定义值继续作为兼容选项显示；
- 作息小时、并发、日上限仍使用数字输入。

`persona_locale` 仅是组织元数据，不注入 `Accept-Language`。真实 CLI 不发送该头，擅自注入会产生额外第三方特征。

### 4.3 Claude Code profile 热加载

`tools/cc-calibrate` 运行真实 Claude Code CLI，将 API base URL 指向本地 capture shim，使用 dummy token，不访问真实 Anthropic 账号。

产物包括：

- CLI 版本；
- headers 模板；
- 真身明确不发送的 headers；
- `endpoint × model family × request feature` beta 规则；
- 指纹守卫结果；
- 标定时间和来源。

网关：

- 从 DB setting `claude_code_calibrated_profile` 加载；
- 进程内缓存，发布实例立即生效，其它实例最多约 60 秒；
- headers、betas、UA、billing version、mimicry guard 使用同一 profile；
- 未发布、schema 不兼容、UA/version 不一致或 guard 未通过时回退内置常量；
- 发布/加载都进行校验，错误 profile 不进入出站热路径。

Admin API：

```text
GET    /api/v1/admin/settings/claude-calibrated-profile
POST   /api/v1/admin/settings/claude-calibrated-profile
DELETE /api/v1/admin/settings/claude-calibrated-profile
```

设置页显示：

- 是否已发布/有效；
- CLI 版本、UA、OS/Arch；
- guard 结果；
- 标定时间和来源；
- beta rule keys；
- 手工发布/清除兜底入口。

### 4.4 Billing fingerprint

官方 Claude Code 2.1.211 native binary 已验证算法：

```javascript
chars = [4, 7, 20].map(i => text[i] || "0").join("")
fp = sha256("59cf53e54c78" + chars + cliVersion).slice(0, 3)
```

关键语义：

- JavaScript 字符串索引是 UTF-16 code unit，不是 UTF-8 bytes；
- 主请求输入是 normalize 前第一条非 meta 用户消息；
- side query 输入是 side-query body 第一条 user text；
- 网关必须跳过迁移 system prompt 产生的 `[System Instructions]` synthetic user。

标定 harness 会在每次 CLI invocation 前写 canary marker，同时尝试 marker 与 wire user 候选。任何请求匹配失败都会阻止 profile 发布。

### 4.5 已修复的重要回归

- `count_tokens` 不再注入/透传 generation-only `max_tokens`；
- 修复过生产上游 HTTP 400：

```text
max_tokens: Extra inputs are not permitted
```

- billing version 变化时会用相同版本重新计算 fp；
- 不再注入真身 CLI 不发送的 `x-client-request-id`；
- JSON schema 请求按条件追加 `structured-outputs` beta。
- Anthropic 账号级 429 可从 `error.message` 内嵌 JSON 的 `resetsAt/windows`
  恢复真实重置时间，不再只做短冷却；
- `perModelLimit=true + seven_day_overage_included/7d_oi` 只写 Fable 家族级
  model rate limit，不再把整个账号临时移出 Opus/Haiku 调度池。

### 4.6 设置页 Vue I18n 花括号事故

2026-07-17 首次部署后，系统设置页因以下翻译文案无法渲染：

```text
{ "schema_version": 1, "cli_version": "2.1.211", ... }
```

Vue I18n 会把裸 `{ ... }` 当作 placeholder 表达式，并抛出：

```text
SyntaxError: Invalid token in placeholder: '"schema_version":'
```

处理规则：

- i18n 文案中不要直接放原始 JSON 花括号；
- 示例改为不带花括号的普通文本，例如
  `profile JSON: schema_version=1, cli_version=2.1.212, …`；
- `claudeCodeMimicryProfileLocales.spec.ts` 必须断言该 placeholder 不含 `{}`；
- 设置页相关改动除组件测试外，发布前必须跑生产 frontend build。

### 4.7 Claude for Chrome Cookie OAuth

新增独立于 Claude Code Cookie OAuth 的 `claude_chrome` profile：

- 使用 Claude for Chrome Extension 的 OAuth client、redirect URI 与 scopes；
- token exchange、refresh 和网关请求带 `anthropic-beta: oauth-2025-04-20`；
- 授权、换 token、refresh token、SessionKey 回退全部使用账号配置代理；
- Chrome 链路代理解析失败时 fail-closed，不允许静默直连；
- `credentials.oauth_client=claude_chrome` 用于隔离两种 OAuth profile；
- `session_key` 按当前产品决定明文保存在 credentials JSONB，不做应用层加密；
- access token 按上游 `expires_in/expires_at` 管理，当前通常约 8 小时；
- Anthropic 可能在 `expires_at` 前吊销 access token；Chrome 账号收到 OAuth 401
  临时不可调度信号后会忽略远期过期时间，进入后台自动刷新；
- 优先使用轮换后的 `refresh_token`；永久 `invalid_grant` 时，在同一刷新锁内用
  已保存的 `session_key` 重新授权；
- 即使 `refresh_token` 已缺失，只要 Chrome 账号仍保存有效 `session_key`，候选查询
  仍会拾取该账号并直接进入 SessionKey 回退，不会提前永久禁用；
- 自动或手工恢复成功后同时清除数据库、Redis 与进程内的临时调度阻断；专用清理
  不会误删模型级 rate limit；
- SessionKey 明确失效（包括
  `error.details.error_code=account_session_invalid`）时账号进入 error、停止调度；
  Cloudflare challenge、临时上游/网络错误保持可重试；
- 管理后台支持创建/重授权时选择 Chrome Cookie Authorization，并提供
  “使用 SessionKey 重新授权”的手工入口。

Admin API：

```text
POST /api/v1/admin/accounts/chrome-cookie-auth
POST /api/v1/admin/accounts/:id/refresh-cookie-auth
```

并发与状态安全：

- Chrome 自动刷新使用 Redis owner-aware lock；
- lock 支持租约续期，旧 owner 不能释放新 owner 的锁；
- Chrome 链路在 Redis/owned-lock 不可用时 fail-closed；
- token 成功写入、永久错误、临时不可调度、手工重授权均使用 credentials CAS；
- 并发手工重授权或 Code/Chrome profile 切换获胜后，迟到的刷新结果和错误不会覆盖；
- OAuth 身份字段与 `extra` 中的 `org_uuid/account_uuid/email_address` 原子更新，
  其它账号配置继续保留。

旧 `claude_code` 链路兼容性：

- 空或未知 `oauth_client` 继续回落 `claude_code`；
- 原 Client ID、redirect URI、scope、Referer 和错误语义不变；
- 旧链路不添加 Chrome beta、不保存 SessionKey、不启用 SessionKey 回退；
- 旧代理解析及普通 Redis refresh lock 继续沿用 fail-open 行为；
- 共享行为的实际变化只有 credentials/error CAS 和 profile 切换防陈旧写入。

### 4.8 Anthropic 分组级客户策略

Anthropic 分组客户策略：

```text
content_review_policy:
  inherit | enabled | disabled

claude_oauth_system_prompt_policy:
  inherit | enabled | identity_only | disabled
```

- `content_review_policy`：同时控制本地 `ContentSafetyGuard` 与风控中心
  `ContentModeration` 的分组参与状态；
- `claude_oauth_system_prompt_policy`：控制第三方客户端经 Anthropic
  OAuth/SetupToken mimic 路径时是否注入 Claude Code system blocks；
- `identity_only` 强制只注入动态 billing attribution 与固定 Claude Code Agent SDK
  身份两块，不注入标准/Fable 长扩展提示词；项目 `cl100k` tokenizer 实测两块文本
  合计约 41 tokens，连同 system 迁移辅助文本的增量约 53 tokens，并由单元测试
  约束为 `<200`；
- 存量分组默认 `inherit`，继续沿用全局设置，升级不改变既有行为；
- 非 Anthropic 分组强制归一为 `inherit`；
- `enabled` 可覆盖全局 enable 开关或风控分组范围，但审查模式、关键词、阈值和
  外部审计配置仍由全局风控配置提供；
- `disabled` 会跳过两层内容审查；Anthropic 上游自身的安全策略不受影响；
- 关闭 system 注入不会影响真实 Claude Code 客户端或 Anthropic API-key 透传，
  只影响 OAuth/SetupToken + 非真实 Claude Code 客户端；
- `identity_only` 的 `/v1/messages` 与 `/messages/count_tokens` 使用相同两块形态；
  Mimicry Guard 会继续严格检查 billing、身份、metadata、headers 与 betas，但该模式
  的合法 system block 数必须恰好为 2，`count_tokens` 遇到已有第三块时也会重建为
  两块；
- Mimicry Guard 为 `warn` 时，关闭注入只记录伪装 findings，不阻断请求；
- API Key auth cache schema 已升级到 v17，分组修改后会主动失效对应认证缓存。
- API Key 认证使用 Ent 精简 SELECT；新增分组运行时字段时必须同步加入
  `GetByKeyForAuth` 的 `WithGroup(...Select(...))` 列表。首次部署遗漏这两列会让
  运行时空值回退到 `inherit`，已由 `c32d41b7` 修复并增加 SQLite 回归测试。

生产实测（分组 `content_review_policy=disabled`、
`claude_oauth_system_prompt_policy=disabled`）：

```text
客户期望 input_tokens: 5127
网关实际 input_tokens: 5127
差值: 0
HTTP: 200
```

数据库迁移：

```text
backend/migrations/178_add_anthropic_group_policies.sql
```

`identity_only` 复用既有 `VARCHAR(16)` 策略列，但需要扩展数据库 CHECK constraint：

```text
backend/migrations/179_expand_claude_oauth_system_prompt_policy.sql
```

`a16045ee` 已在生产应用迁移 179；事务验证 identity-only 可写入并回滚，前端标签已
嵌入生产二进制。部署脚本未自动切换客户配置；随后分组 14 经管理操作启用该模式，
首个观察窗口 13 条成功 usage，且缺 billing/身份/块数的 Guard findings 为 0。

### 4.9 Anthropic 429 响应体分层限流

部分 Anthropic OAuth 入口不返回完整 unified rate-limit headers，而把真正的窗口
快照作为 JSON 字符串放入外层 `error.message`。当前处理规则：

- `perModelLimit=false`，且 5h/7d 窗口超限：写账号级
  `rate_limit_reset_at`，到真实 reset 前停止整个账号调度；
- `perModelLimit=true` 且代表窗口为
  `seven_day_overage_included`/`7d_oi`：只写
  `model_rate_limits["claude-fable-5"]`，Opus、Haiku、Sonnet 继续可用；
- Anthropic 429 可在普通 10 次切换上限之外最多切换 20 次，但受 20 秒总预算约束；
- 明确模型级响应不得落入 `anthropic_429_runtime_no_reset_time` 的账号级一分钟冷却；
- 无法证明窗口和 reset 的普通 429 固定使用账号级 1 分钟运行时冷却；
- `a7ceb185` 曾尝试 1/2/5/10 分钟递增冷却，但持续流量下多个账号同时进入
  10 分钟阻断，分组 14 在两小时内产生 283 个本地 503，已由 `0cc87cdd`
  回退；旧长冷却在读取时按 `triggered_at + 1 分钟` 截断，无需等待 Redis TTL；
- Fable 调度优先选择 15 分钟内具有 `7d_oi utilization < 1`、未来 reset
  的账号；无证据账号仍保留为兜底，不影响其它模型。

生产验证：

- body-only Fable 429 已产生 `anthropic_fable_window_model_rate_limited`；
- Redis 中 Fable body 被误写为账号运行时阻断的数量为 0；
- `a7ceb185` 初始低流量观察窗口内 Fable HTTP 200 为 11、usage 成功记录 5 条
  （3 个账号），但后续持续流量暴露递增冷却池耗尽问题；
- failover 已观察到 switch 11/12/13，证明不会再固定停在原 `max_switches=10`；
- `0cc87cdd` 热修后观察窗口内最终 429/503 均为 0，8 次 opaque 429
  冷却均严格为 1 分钟。

会话数量限制仍是账号级：Redis key 为
`session_limit:account:<accountID>`，某账号满额后调度器继续尝试同组其它账号。
管理后台分组容量的会话分母只汇总配置了正数 `max_sessions` 的当前 DB 可调度账号，
不代表分组总上限，也不包含 Redis 运行时阻断，混合“有限+不限”账号时可能误导。

### 4.10 Claude Chrome SessionKey 永久失效判定

Chrome refresh token 为 `invalid_grant`/缺失时会在同一 owned lock 内回退已保存的
SessionKey。回退结果按证据分层：

- JSON `error.details.error_code=account_session_invalid` 是账号会话明确失效，
  转换为 `CLAUDE_SESSION_KEY_INVALID`，通过 credentials/proxy CAS 将账号置为
  `error`、`schedulable=false`；
- Cloudflare `Just a moment...`、代理/网络错误、5xx 与无明确 session 证据的普通
  403 仍可重试，重试耗尽后临时不可调度 10 分钟；
- 二次 code exchange 的 `invalid_grant` 不能单独证明 SessionKey 死亡，继续保留
  可重试语义；
- 并发重授权或 profile 切换先获胜时，失败 CAS 不覆盖新凭证。

生产首轮已验证两个 `account_session_invalid` 账号进入 `error`，没有继续进入
10 分钟临时不可调度循环。

### 4.11 Fable `credits_required` 模型访问拒绝

Anthropic 可能在 `/v1/models` 中列出 Fable 5，但账号没有可扣减的 usage credits
时，请求返回 HTTP 429：

```text
error.details.error_code=credits_required
error.details.model=claude-fable-5
error.details.disabled_reason=out_of_credits
```

该信号不是带 reset 的 5h/7d/`7d_oi` 窗口限流，处理规则为：

- 优先读取上述结构化字段，文本
  `Usage credits are required for this model` 仅作为旧响应兼容兜底；
- 在 `account.extra.model_access_denials["claude-fable-5"]` 持久化无 reset 的
  模型访问拒绝，不写账号级 `rate_limit_reset_at`，也不写时间型
  `model_rate_limits`；
- 当前请求立即 failover；普通候选、粘性候选和回退候选后续均跳过该账号的
  所有 Fable 变体，Opus、Haiku、Sonnet 不受影响；
- 管理后台“账号管理 → 测试连接”仍可绕过调度直接测试指定账号；Fable 测试完整
  成功后只清除该账号的 Fable access denial；
- denial 写入/清除使用原子嵌套 JSONB 更新并同步调度快照，不新增数据库列或迁移；
- 旧版本应用会忽略该 Extra 字段，应用回滚不需要数据库回滚。

2026-07-23 `e4fad61d` 生产首个真实样本：

```text
账号 2069 (SetupToken) -> credits_required / out_of_credits
model_access_denials.claude-fable-5 -> 写入成功
sticky -> 清除
switch -> 账号 2624 (Max)
最终 HTTP -> 200，4264ms
denial 后账号 2069 的 Fable 再次选中数 -> 0
```

### 4.12 Claude 模型清单来源与新模型接入

本 fork 不在热路径上动态拉取 Anthropic 模型目录。"有哪些 Claude 模型"由三层决定：

1. **硬编码 canonical 清单** `claude.DefaultModels`
   （`backend/internal/pkg/claude/constants.go`），用于 `/v1/models` 兜底与管理端
   "账号可用模型"兜底；
2. **每账号 `credentials.model_mapping`**：空 = 放行一切；非空 = 白名单语义
   （支持 `*` 通配，并可重写为上游真实模型名）；
3. **`/v1/models` 聚合**：`GetAvailableModels` 取分组内所有可调度账号 mapping 的
   key 并集；全空时回退第 1 层。分组 `models_list_config` 只做展示层过滤。

因此**转发路径上没有全局模型白名单**：原生 Anthropic 账号在 mapping 为空时，未知
模型只经过 `NormalizeModelID`（仅 3 条短名→带日期名）就原样透传给上游。会拒绝未知
模型的只有：账号非空 mapping、Antigravity/Bedrock 闭集映射、渠道 `restrict_models`。

管理端 `POST /api/v1/admin/accounts/:id/models/sync-upstream` 会用账号凭证真的去
`GET https://api.anthropic.com/v1/models`，是确认官方模型 ID 最快的手段，但它**只
回显、不写入** `model_mapping`。

接入一个新官方模型的最小清单（模板 commit：`514ac5c6a` 加 opus-4-8、`8782b30f`
加 opus-5）：

- `backend/internal/pkg/claude/constants.go` 的 `DefaultModels`；有短名/带日期两种
  写法时才需要 `ModelIDOverrides` / `ModelIDReverseOverrides`；
- `frontend/src/composables/useModelWhitelist.ts` 的 `claudeModels` 与
  `anthropicPresetMappings`；
- 定价：`backend/resources/model-pricing/model_prices_and_context_window.json`
  加条目，并在 `pricing_service.go` 的 `matchByModelFamily` families 表加同代家族档；
- 用 Bedrock 时补 `domain.DefaultBedrockModelMapping` 与前端 `bedrockPresetMappings`；
- 用 Antigravity 时补 `DefaultAntigravityModelMapping`、
  `pkg/antigravity/claude_types.go`、`request_transformer.go` 的 `modelInfoMap`，
  以及一条照抄 `144_add_opus48_to_model_mapping.sql` 的迁移回填已持久化 mapping。

#### Claude Opus 5（2026-07-24 官方发布）

- API ID `claude-opus-5`，**无日期后缀**，API ID 与 alias 相同（4.6 代起官方改用
  无日期的 pinned snapshot 命名），因此不需要短名/长名互转；
- Bedrock `anthropic.claude-opus-5`、Vertex `claude-opus-5`。Bedrock 形态无区域前缀、
  无 `-v1` 版本段，与 `claude-fable-5` 一致；`AdjustBedrockModelRegionPrefix` 对不含
  已知区域前缀的 ID 原样返回，因此无论账号 region 都会发出官方 ID；
- 定价 `$5 / $25` 每百万 token，与 Opus 4.8 相同、为 Fable 5 的一半；缓存写入
  5m `$6.25`、1h `$10`，缓存命中 `$0.50`，Batch `$2.50 / $12.50`；
- 1M 上下文，**1M 既是默认也是最大值**，不需要 beta header、无长上下文溢价；
  最大输出 128k。因此 `context-1m-2025-08-07` 的白名单保持"只放行
  `claude-sonnet-5*`"即可，过滤该头不影响 Opus 5 拿到 1M；
- 归族落 `opus` 档，自动继承 `adaptive` thinking + `effort=high`，与官方默认语义
  一致，mimic profile 无需改动；不含 `fable` 子串，不进 Fable 限流桶；
- 行为变化（当前代码不受影响，但排查时需知道）：thinking 默认开启；
  `thinking:{"type":"disabled"}` 配 `effort=xhigh/max` 返回 400；effort 梯度扩展到
  `max`；不支持 Priority Tier 与 web fetch 工具；prompt cache 最小长度降到 512 tokens。

#### 已修复：Opus 档定价随机误匹配

`claude-opus-5` 与 `claude-opus-4-8` 都不包含 `claude-opus-4-<minor>` 模式串。定价表
缺少精确条目时，`matchByModelFamily` 的 Phase 1 匹配不上，Phase 2 关键字兜底落进
`opus-4` 家族，Phase 3 再遍历 map 找首个含 `claude-opus-4` 的 key —— **Go map 迭代
顺序随机**，表中 11 个命中 key 里有 3 个是 Opus 4 / 4.1 的 `$15/$75`，其余是
`$5/$25`，导致同一模型每次查价在 1 倍与 3 倍之间跳。`BillingService.getFallbackPricing`
有同样问题，opus 分支对非 4.5/4.6/4.7 一律回落 `claude-3-opus` 的 `$15/$75`。

`8782b30f` 补齐了 `opus-5`、`opus-4.8` 两档家族条目与硬编码兜底价，并加了 50 次
循环的确定性回归测试，同时覆盖 `anthropic.claude-opus-5` 这类 Bedrock 形态 ID。
旧代 Opus 3 / 4 / 4.1 仍保留 `$15/$75`。

### 4.8 供号商站点（Provider Portal）

面向外部供号商的独立入口，与主站共用应用和数据库，但路由前缀、登录注册页与布局
完全分开（`/provider/*`）。核心目标是让供号商能自助上号并核对结算金额，同时
**完全看不到平台对账号做了什么处理**。

准入与身份：

- `users.is_provider` 能力位，`role` 仍为 `user`；`User.IsProviderUser()` 排除管理员；
- 注册强制 `provider_invite` 类型邀请码（新 redeem 类型，`redeem_codes.type` 无枚举约束，
  无需改表），建号时 `balance=0`，不走赠送与订阅分配；
- 双向隔离：`ProviderOnly` 守 `/provider/*`，`ProviderDenyConsumerRoutes` 让供号商
  无法访问 `/user`、`/keys`、`/usage` 等消费侧接口；
- 站点总开关 `provider_portal_enabled` 读取失败时 fail-closed。

上号（彻底排除 Chrome OAuth）：

- 四种方式：手动 OAuth、手动 Setup Token、Cookie 授权(oauth)、Cookie 授权(setup-token)；
- 排除 chrome 是 service 层硬约束而非 UI 隐藏：provider 链路构造 `CookieAuthInput`
  时永不设置 `OAuthClient`（留空即 `claude_code`），且 `AssertProviderOAuthClientAllowed`
  拒绝任何显式传入的其它 profile；
- 代理由供号商自带且必填，创建的代理记录名带 `provider-<id>-` 归属前缀；
- 三项伪装强制开启且不可见。**注意写入路径不同**：
  `credentials.intercept_warmup_requests`，而 `extra.enable_tls_fingerprint`
  与 `extra.session_id_masking_enabled`；
- `extra` 走白名单过滤，任何 `persona_` 前缀键一律丢弃并记日志。人格由管理员事后单配。

托管类型与速率档位（全部在管理端设置页可视化配置，代码只提供种子值）：

- 托管类型来自现有 Anthropic 分组，靠 `content_review_policy` 与
  `claude_oauth_system_prompt_policy` 组合区分；对外只呈现管理员填写的中性名与效果描述，
  真实策略与倍率**绝不下发**；
- 1-5 固定档 + 自定义档。种子值：并发 1/2/3/5/8，会话同数，RPM 10/20/30/50/80，
  5 小时窗口上限 20/40/60/100/不限，默认 3 档；
- 自定义档受 `provider_custom_tier_caps` 护栏约束；
- 改档位默认只影响新上号账号，另有「应用到存量」按钮。回填**只增量合并档位相关的
  extra 键**，绝不整体覆盖，否则会清掉 `persona_*`、`window_cost_sticky_reserve` 等持久设置。

调度优先级（**踩坑重灾区**）：

供号商账号的 `priority` 取自设置 `provider_account_priority`，种子值 **1**。

关键在于 `priority` 在调度里是**硬门槛而不是权重**：`filterByMinPriority`
（`gateway_scheduling.go`）只保留候选中数值最小的那批账号，其余**完全不参与**后续选择。
不存在「优先级低就少分一点流量」这回事，只有「拿全部」或「一个都拿不到」。

因此种子值必须与管理端新建账号表单的默认值对齐（`CreateAccountModal.vue` 是 `priority: 1`，
Anthropic session 批量导入不传时也是 1）。取值不一致的后果是单向且静默的：
若供号商账号是 50 而同分组自有账号是 1，供号商的号永远拿不到一个请求，
账号状态显示正常、用量恒为 0、结算金额恒为 0，没有任何报错。

设置页会扫描已开放的托管分组，发现组内自有账号 priority 与配置值不同时显式告警
（后端 `DistinctNonProviderPrioritiesByGroup` 提供数据）。

同一 priority 内的选择顺序是：最低负载率 → 最久未用（LRU）→ 完全并列时随机
（`selectByLRU` 的 `mathrand.Intn`）。所以所有供号商的账号是公平轮转的，谁也不优先。
想让某些供号商多分流量的话 `priority` 做不到，需要另设机制。

结算（封账，不物理清零）：

- 新表 `provider_settlements`，每次结算插入一条不可变记录，`usage_logs` 一行不动；
- 当前周期起点 = 最近一条 `status='settled'` 的 `period_end`，无记录则取供号商注册时间；
  半开区间 `[period_start, period_end)`；
- 作废只允许最近一期（周期是链式的，作废中间期会让后续起点错位）；作废后金额自动回到待结算；
  作废必须填原因，写入独立的 `void_reason` 列而不覆盖结算时的 `notes`；
- 批量结算共用同一个 `period_end`，无用量的供号商直接跳过（不再「先插入再作废」，
  那会在账本里留下一串无意义的作废单并干扰「只能作废最近一期」的判定）。

并发与封账水位（财务正确性的三道防线）：

1. **串行化**：`Settle` 与 `Void` 全程持有 `pg_advisory_xact_lock("provider_settlement:user:{id}")`
   并在同一事务内完成「取起点 → 聚合 → 插入」。起点必须在锁内重新读取，
   锁外读到的可能已被并发结算推进。复用既有的 `lockRepositoryScopedKeys`。
   DB 侧另有 partial unique index `(provider_user_id, period_end) WHERE status='settled'` 兜底。
2. **冷却期**：`usage_logs` 由异步 worker 写入（worker 任务超时 5s、批处理窗口 20ms、
   调用方 detached 超时 15s），`created_at` 是 worker 赋值而非 COMMIT 时刻，
   存在「created_at 早、提交晚」的记录。封账终点取 `now - provider_settlement_cooldown_seconds`
   （种子 600 秒），让这段窗口内的写入先落定。注意这只把漏网概率压到极低，**不是证明**。
   待结算视图的终点仍取当前时刻，供号商能立刻看到刚产生的用量。
3. **id 水位**：每张结算单记录本期计入的最大 `usage_logs.id`（`last_usage_id`）。
   下一期的聚合条件是
   `(created_at 落在本期区间) OR (created_at 早于本期起点 AND id > 上期水位)`，
   于是冷却期没兜住的迟到行会在下一期被补计，而 `id > 水位` 保证不会重复计入。
   捕获到迟到行时打 WARN 日志（说明冷却期可能设短了）。
   空周期必须沿用上期水位而不能退回 0，否则已计入的旧行会被重复捕获。

明细快照与清理保护：

- 新表 `provider_settlement_items` 在封账时快照分账号明细，与结算单同事务写入；
  导出与历史重算都读快照，**不再回查 `usage_logs`**。
  原因：`usage_logs` 默认 90 天后被 dashboard 保留策略硬删
  （`maybeCleanupRetention`，`usage_logs_days`），管理员也可手工发起清理任务。
  靠活表重算会让历史凭证残缺，而结算单上的总额还在，两者对不上无法向供号商解释。
- `CleanupUsageLogs` 的截止点会被夹到「最早未结算周期起点」之前
  （`SetProviderSettlementGuard` + `EarliestUnsettledStart`），
  避免未结算区间的行被删掉导致应付金额静默缩水。取不到该下界时**跳过本轮清理**
  而不是按原截止点删——宁可多留一轮数据。

对账口径（三条不可违反的约束，已写成代码注释；**尚无 DB 集成测试覆盖**，见下方待办）：

1. 金额只用 `SUM(usage_logs.total_cost)`，即恒定 1 倍率标准价。禁止改用 `actual_cost`
   或 `account_stats_cost`——那两者含分组/账号倍率，会把平台自设的分组倍率泄露给供号商。
2. `JOIN accounts` 禁止追加 `AND a.deleted_at IS NULL`。供号商下线（软删除）账号后，
   本期已产生的金额必须继续计入，加了过滤会凭空少付钱且无任何报错。
   这些是 raw SQL，不走 Ent 软删除 interceptor，天然能查到已删账号，是有意为之。
3. 按日聚合时区取自 `provider_settlement_timezone` 设置（种子值 `Asia/Shanghai`），
   禁止沿用 `apiClient` 给所有 GET 自动注入的浏览器时区，否则管理员与供号商看到的
   每日明细按各自时区切分，总额一致但逐日对不上。

`total_cost` 的确切语义（核对过写入链路，别只看名字猜）：

`CostBreakdown.TotalCost = 各分项之和`，`ActualCost = TotalCost × rateMultiplier`
（`billing_service.go`）。所以 `total_cost` **不含**分组倍率、用户专属倍率、高峰倍率、
账号倍率——这正是它能当「1 倍率标准价」的原因，有测试锁定
（`billing_service_test.go`：倍率只改 ActualCost）。

但它**不等于 Anthropic 官方目录价**，下列因素会进 `total_cost`：
渠道自定义单价、分组图片/视频单价、service tier（priority ×2 / flex ×0.5）、
OpenAI 长上下文加价。其中 **service tier 与长上下文加价的赋值点全部在 `openai_*`
文件里，Anthropic 路径只读不写**，而供号商账号强制 `PlatformAnthropic`，
所以这两项对供号商结算不生效。渠道单价与图片单价理论上仍可能影响，
取决于托管分组是否配了自定义定价——配了就等于按加价后的钱付给供号商。

哪些请求不会计费给供号商（已逐一核对）：后台测试账号、定时探测、count_tokens、
被拦截的预热请求、Forward 失败/限流/重试中失败的那几次，都不写 usage_logs。
会计费的：流式中途客户端断开（drain 完 usage 后正常计费，与向客户收费一致）、
未被拦截的预热请求——后者不用担心，供号商账号强制开启了预热拦截。

不会重复计费：一次请求只写一条 usage_log，影子账号（`parent_account_id` /
`quota_dimension='spark'`）不会母子各写一条，且 `CreateShadow` 与 `DuplicateAccount`
都不继承 `provider_user_id`。

`provider_user_id` 不会被常规操作清掉：`UpdateAccount`、`BulkUpdate`、CRS 同步的
builder 里都没有这一列，只有创建时写入。

金额精度：**全程十进制，不经过二进制浮点**。

- 数据库列 `decimal(20,10)`；
- Go 侧一律 `shopspring/decimal`，从 SQL 扫描、聚合、快照到序列化都不转 float64；
- JSON **以十进制字符串传输**（`decimal.Decimal` 的 `MarshalJSON` 默认带引号）。
  这一点是刻意的：JSON 数字在 JS 侧被解析成 float64，`decimal(20,10)` 的精度当场丢失；
- 前端 `frontend/src/utils/providerMoney.ts` 用 **BigInt 按 10^10 缩放**做精确解析与求和，
  只在展示时收敛到 1 位小数。大于 0 小于 0.05 显示 `<$0.1` 避免被误读为无用量；
- CSV 导出输出完整精度原值，不做展示层收敛。

CSV 公式注入：账号名由供号商自填，可能以 `=` `+` `-` `@` 开头。
后端 `handler/provider.CSVCell` 与前端 `csvCell` 在这些首字符前补单引号，
两边必须保持一致——只防一边等于没防。

分页：结算历史与账号列表都走仓库标准分页（`response.ParsePagination` +
`response.Paginated`，响应 `items/total/page/page_size/pages`，前端用
`components/common/Pagination.vue` + `getPersistedPageSize`）。

结算历史**必须返回 total**：它是财务凭证，供号商要能确认看到的就是全部，
而不是被静默截断到前 N 条——这正是分页前的行为。

账号列表页需要的「本期起始时间」不塞进分页体，改由轻量端点
`GET /provider/billing/period` 提供（只读最近一条结算单，不跑任何用量聚合）。
不要为拿这一个时间戳去调 `GET /billing/current`，那会连带算出按日明细。

管理端对账面板不允许一键直接结算：先看分账号明细（可导出核对），明细逐行合计与
后端总额**精确不一致**时显示警告并**禁用结算按钮**，再二次确认。
一致性判定用 BigInt 精确比较而非容差：两边同源，任何差异都是真实的取数口径问题。

身份字段必须同时写进 extra（**踩坑重灾区之二**）：

换票拿到的 `account_uuid` / `org_uuid` / `email_address` 除了写 credentials，
还必须复刻进 `extra`（`MirrorProviderIdentityToExtra`）。**网关只认 extra 里的那份**：

- `gateway_upstream_request.go` 用 `account.GetExtraString("account_uuid")`，
  并以 `accountUUID != ""` 为硬前提；为空时 `RewriteUserIDWithMasking` **整段跳过**，
  也就是说 `session_id_masking_enabled=true` 写了也白写；
- `gateway_claude_oauth_body.go` 的 `FormatMetadataUserID` 同样从 extra 取，
  缺失时拼出来的 `metadata.user_id` 少一段账号 UUID，本身就是破绽。

管理端走 `buildExtraInfo`（`useAccountOAuth.ts`）写 extra，CRS 同步也专门把这两个键
从 credentials 复制进 extra。供号商上号必须对齐，否则伪装静默失效且无任何报错。

注意调用顺序：必须在 `SanitizeProviderExtra` **之后**复刻。白名单只放行档位键，
先写会被丢掉。重新授权（Reauth）时还要先 `RefreshProviderIdentityExtra` 清掉旧值
——供号商换个 Anthropic 账号重新授权时，用错身份比没有身份更糟。

档位并发下限是 1，不是 0：`ConcurrencyService.AcquireAccountSlot` 对
`maxConcurrency <= 0` 直接返回「无限制」，填 0 会让本该最小的档位变成不限并发。
`max_sessions` / `base_rpm` / `window_cost_limit` 的 0 表示「不启用该限制」，
是既有约定，与并发不同，不要一起改。

上号原子性：

- 账号与分组绑定走 `CreateWithAccountGroups`（同事务）。原先是 `Create` + `BindGroups`
  两段写，绑组失败会留下一个没有任何分组的账号，而调用方只收到错误、以为什么都没发生，
  于是去删自己刚建的代理，却因为账号还引用着而删不掉，最终留下孤儿。
- 上号失败回收代理时用 `context.WithoutCancel` + 独立超时：上号失败常常正是因为
  请求超时或客户端断开，此时请求 ctx 已取消，拿它做清理必然也失败。

邀请码：校验 → 建用户 → CAS 消费在同一事务内完成。与普通注册的「标记失败只记日志」
刻意不同——普通邀请码只影响赠送，供号商邀请码是准入凭证，
绝不能出现「码没消费掉但人已经进来了」。并发抢同一枚码时输的一方整体回滚。

### 4.8.1 已知未决问题

以下问题在代码审查中被识别，**上线前必须评估**：

- 备份导出结构不含 `provider_user_id` / `provider_tier`，恢复后账号会退回「管理员自有」。
- **关站不阻断登录**：`/api/v1/auth/login` 不检查 `provider_portal_enabled`，
  已存在的供号商在关站后仍能登录并进入前端面板（业务接口全 404）。
  门户从未开启过时不存在供号商账号，该缺口不可达；开站之后若要再关站需先补校验。
- 供号商没有改密入口（`ProviderDenyConsumerRoutes` 挡掉了 `/user/password`）。
- 若给托管分组配了**渠道自定义单价或分组图片单价**，这些加价会进 `total_cost`，
  等于按加价后的金额付给供号商。上线前确认托管分组是否配了自定义定价。
- 删除供号商用户时不检查其名下账号与待结算金额。
- 对账 SQL 的三条口径约束（1 倍率、不过滤软删账号、强制结算时区）目前靠代码注释与
  单元测试约束，**仍无 DB 集成测试覆盖**。
- `usage_logs` 是否已在生产库转成分区表未确认；若已分区，保留清理走的是
  DROP 月分区路径，冷却期与未结算保护同样适用，但分区边界是日历月，
  粒度比行删除粗，需要确认最早未结算周期不会落在待 DROP 的分区内。
- **档位里的 RPM 目前不参与选号门控**（平台既有问题，非供号商特有）。
  生产走 Redis 调度快照，而 `filterSchedulerExtra`（`scheduler_cache.go`）的白名单
  收了 `window_cost_limit` / `max_sessions` 却**没收 `base_rpm`**，
  于是选号时 `GetBaseRPM()` 恒为 0，`isAccountSchedulableForRPM` 恒放行。
  影响所有配了 `base_rpm` 的账号，管理端自建号同样如此。
  后果：1-5 档之间的 RPM 差异（10/20/30/50/80）实际不生效，只有并发、会话数、
  窗口成本三项在起作用。
  **修复需谨慎**：把 `base_rpm` 加进白名单会让线上所有已配置 RPM 的存量账号
  突然开始受限，等于一次性收紧可用容量，属于行为变更而非纯 bug 修复，
  应当先评估存量配置再决定。

设置项：

```text
provider_portal_enabled
provider_selectable_groups
provider_default_group_id
provider_capacity_tiers
provider_default_tier
provider_custom_tier_enabled
provider_custom_tier_caps
provider_settlement_timezone
provider_settlement_cooldown_seconds   # 种子 600，区间 60..86400
provider_account_priority              # 种子 1，区间 0..100，硬门槛语义见上文
```

数据库迁移：

```text
backend/migrations/180_add_provider_portal.sql
backend/migrations/181_provider_settlement_integrity.sql
```

180 新增 `users.is_provider`、`accounts.provider_user_id`（partial index）、
`accounts.provider_tier` 与 `provider_settlements` 表。

181 补财务完整性：`provider_settlements.last_usage_id`（封账水位）与 `void_reason`、
金额/周期 CHECK 约束、`(provider_user_id, period_end) WHERE status='settled'` 的
partial unique index，以及明细快照表 `provider_settlement_items`。

## 5. 当前自动标定状态

首次真实标定：

- GitHub Actions run：`29582278101`
- CLI：`2.1.212`
- 平台：`Linux/x64`
- User-Agent：`claude-cli/2.1.212 (external, sdk-cli)`
- 来源：`cc-calibrate`
- guard：`16/16`
- `salt_verified=true`
- 匹配输入：`canary_prompt`
- profile：已发布、有效、已被生产网关热加载

当前抓到的 beta rule keys：

```text
messages|fable|tools
messages|haiku|tools
messages|opus|tools
messages|sonnet|tools
```

### 已知标定覆盖缺口

真实 CLI 在当前 canary 中始终带工具，因此尚未抓到：

- `messages|<family>|` 无 tools 档；
- `count_tokens|<family>|...` 档；
- 更细的 JSON schema / 1h cache 等组合。

加载器会对缺失组合回退编译内置常量，因而安全但尚未做到所有组合完全动态化。扩展 canary 矩阵是后续优先事项。

## 6. GitHub Actions 运行方式

Workflow：`.github/workflows/release.yml`

默认分支 `main` 中的 workflow commit：

```text
e9c5166c4
```

### 6.1 无 Git tag 构建 GHCR

```bash
APP_VERSION="$(tr -d '\r\n' < backend/cmd/server/VERSION)"

gh workflow run release.yml \
  --repo shangwantsci/sub2api \
  --ref custom/prod \
  -f tag="v${APP_VERSION}" \
  -f custom_image_only=true \
  -f source_ref=custom/prod \
  -f simple_release=true
```

`tag` 是旧 workflow contract 的兼容必填值；`custom_image_only=true` 时不会 checkout 或创建该 Git tag。

当前生产 `claude-opus-5` 上线镜像构建：

- run：`30137006332`
- source commit：`8782b30f`
- job：`custom-image`
- 结论：success（4m54s）
- 其它 release/tag jobs：skipped

上一生产 `identity_only` 两块提示词模式镜像构建：

- run：`29982187637`
- source commit：`a16045ee`
- job：`custom-image`
- 结论：success
- 其它 release/tag jobs：skipped

更早的 Fable `credits_required` 模型访问拒绝镜像构建：

- run：`29978772874`
- source commit：`e4fad61d`
- job：`custom-image`
- 结论：success
- 其它 release/tag jobs：skipped

更早的 opaque 429 固定短冷却热修镜像构建：

- run：`29912599037`
- source commit：`0cc87cdd`
- job：`custom-image`
- 结论：success
- 其它 release/tag jobs：skipped

更早的 Anthropic 429 调度可靠性修复镜像构建：

- run：`29896614913`
- source commit：`a7ceb185`
- job：`custom-image`
- 结论：success
- 其它 release/tag jobs：skipped

上一生产 Claude Chrome SessionKey 永久失效修复镜像构建：

- run：`29717634840`
- source commit：`6bf23b45`
- job：`custom-image`
- 结论：success
- 其它 release/tag jobs：skipped

body-only Fable 模型级 429 修复构建 run 为 `29713133369`
（source `50b68603`）。Anthropic 内嵌 429 reset 解析构建 run 为 `29694578585`
（source `ab82a31e`）。Anthropic 分组级客户策略 auth hotfix 构建 run 为 `29687558261`
（source `c32d41b7`）。首次 Persona 版本镜像构建 run 为 `29578816168`；
随后因 Vue I18n JSON placeholder 修复重新构建并部署 `c8637aab`。

### 6.2 自动真身标定

定时计划：

```text
17 */6 * * *
```

每 6 小时在 GitHub-hosted Linux x64 runner：

1. checkout `custom/prod`；
2. 安装 latest 真实 Claude Code；
3. 禁用 telemetry/nonessential traffic；
4. 对本地 shim 运行 canary；
5. 校验 fingerprint guard；
6. 上传 raw capture + profile artifact（保留 14 天）；
7. guard 通过才 POST 到生产 Admin API。

Repository Secrets（只记录名称，禁止在文档写值）：

```text
CC_CALIBRATE_GATEWAY_URL
CC_CALIBRATE_ADMIN_API_KEY
```

手工触发：

```bash
gh workflow run release.yml \
  --repo shangwantsci/sub2api \
  --ref main \
  -f tag=v0.1.156 \
  -f calibrate_only=true \
  -f source_ref=custom/prod \
  -f simple_release=true
```

生产机不运行 sidecar，原因是生产机内存约 3.8GB，还承载数据库、Redis、Caddy 和其它项目。npm install 与多个真实 CLI 进程不应和网关争抢资源。

## 7. 标准部署流程

### 7.1 发布代码

```bash
git switch custom/prod
git status --short
# 验证后
git push origin custom/prod
```

### 7.2 GitHub 构建

运行第 6.1 节 `custom_image_only` workflow，并等待成功。

### 7.3 生产只拉镜像

```bash
cd /opt/sub2api-production
cp .env "backups/.env.$(date +%Y%m%d-%H%M%S)"
docker compose -f docker-compose.local.yml -f docker-compose.override.yml pull sub2api
docker compose -f docker-compose.local.yml -f docker-compose.override.yml \
  up -d --no-deps --force-recreate sub2api
curl -fsS http://127.0.0.1:18080/health
docker exec sub2api /app/sub2api --version
```

禁止在小内存生产机执行 `docker build`。

### 7.4 部署后

```bash
docker compose -f docker-compose.local.yml -f docker-compose.override.yml ps sub2api
docker compose -f docker-compose.local.yml -f docker-compose.override.yml logs --tail=120 sub2api
curl -fsS https://lumos7.cc/health
```

检查：

- 容器 healthy；
- `--version` 的 VERSION/commit 正确；
- `/api/v1/admin/settings` 返回 200；
- `/api/v1/admin/settings/claude-calibrated-profile` 返回 200；
- profile 为 published + valid；
- Persona 全局开关保持预期状态。

## 8. 回滚

部署前必须给正在运行的 image ID 增加本地回滚 tag，例如：

```text
sub2api-rollback:pre-<new-commit>
```

回滚只重建应用：

```bash
cd /opt/sub2api-production
sed -i 's#^SUB2API_IMAGE=.*#SUB2API_IMAGE=sub2api-rollback:pre-<commit>#' .env
docker compose -f docker-compose.local.yml -f docker-compose.override.yml \
  up -d --no-deps --force-recreate sub2api
curl -fsS http://127.0.0.1:18080/health
```

不回滚数据库、Redis、Caddy。

标定 profile 可在设置页清除；清除后网关立即回退编译内置常量。

## 9. TTFT 调查结论

2026-07-17 曾观察到 TTFT 急剧上升。证据：

- Anthropic 官方在 06:47 UTC 宣布 API degraded performance；
- 生产数据在北京时间 15:00 同步突变；
- 服务器 CPU 96–99% idle、可用内存约 2.6GB；
- sidecar 已不存在；
- 大量慢请求带 90k–300k tokens 上下文；
- `>=100k` context 的 p90 约 82 秒；
- 代理 23 到 Anthropic 响应头约 2.4 秒，其它代理约 0.4 秒。

结论：

1. 主因是 Anthropic 上游事故叠加超长上下文；
2. 代理 23 额外贡献约 2–3 秒，但不能解释 30–80 秒；
3. 本地生产构建曾造成管理设置接口 14 秒延迟，因此已永久改为 GHCR 离机构建；
4. 不应为事故期间的短期速度收益大规模改变账号固定代理/IP。

当前决定：

- 不临时降级模型；
- 不在事故期间调整缓存计费策略；
- 不迁移既有账号代理；
- 等官方恢复后重新建立 TTFT baseline。

## 10. 当前安全默认

- Persona gating 默认关闭；
- 未配置 Persona 的存量账号不受影响；
- Persona 读取/计数失败 fail-open；
- 标定失败不发布；
- profile 无效回退内置常量；
- locale 不进入 wire；
- 自动标定使用 dummy token，不消耗账号；
- GitHub Secret 和生产密码不得写入仓库、日志或本文。
- Chrome `session_key` 当前按产品决定明文存储，数据库备份与管理接口必须按敏感凭证保护。
- Chrome OAuth 分布式刷新锁不可用时 fail-closed，避免并发消费旋转 token。

## 11. 后续优化优先级

### P0：上线后观察

- Anthropic 事故恢复后复测 TTFT；
- 对比 proxy 23 与其它美国代理；
- 观察新 profile 下 400/401/429/529、cache read/create、账号寿命；
- `count_tokens max_tokens` 400 已修复；继续调查生产出现过的
  `metadata: Extra inputs are not permitted`；
- 观察账号级 5h/7d 与 Fable `7d_oi` 分层命中、真实 reset 和跨模型可用性；
- 观察 Anthropic 429 switch 深度、20 秒预算触发率、固定 1 分钟 opaque
  冷却与最终 429/503；按流量评估 20 次上限和 15 分钟 Fable 证据窗口；
- 观察 Chrome OAuth 约 8 小时轮换、提前 401 自动恢复、`invalid_grant`/缺失
  `refresh_token` 回退成功率、`account_session_invalid` 永久隔离以及 Cloudflare
  challenge 临时重试；
- 观察 `oauth_refresh` 的 lock lease lost、CAS skipped 与临时不可调度日志；
- `claude-opus-5` 上线后确认 `/v1/models` 实际返回该模型（需有效 API Key，
  `8782b30f` 部署时未做端到端验证）、usage 记录的单价为 `$5/$25` 而非
  `$15/$75`，并用 sync-upstream 探测 Antigravity 是否已支持后再决定是否补白名单。

### P1：Persona 小批灰度

1. 为少量新账号选择“按代理自动识别”；
2. 设置合理作息/并发/日上限；
3. 开启全局 `enable_persona_gating`；
4. 对比寿命、24h 活跃曲线、并发、复用度；
5. 再扩大范围。

复测 SQL：

```text
tools/account-metrics/persona-baseline.sql
```

### P1：扩展标定矩阵

- 真身无 tools 请求；
- `/messages/count_tokens`；
- `json_schema`；
- 1h cache；
- thinking on/off；
- stream/non-stream；
- 每个模型族至少一个稳定样本。

### P2：profile 可观测性

- 设置页显示最近 Actions run URL/结论；
- 显示 profile 覆盖矩阵和缺失 fallback 组合；
- 标定失败告警；
- profile 版本落后 npm latest 告警。

### P3：可选 per-account 真 CLI 沙箱

只作为：

- 账号预热；
- golden wire 校准；
- 特殊高价值流量；
- 行为真实性实验。

不要在没有容量评估和编排设计前让所有主流量穿过真实 CLI 容器。

## 12. 关键文件索引

Persona：

```text
backend/internal/service/account_persona.go
backend/internal/service/account_persona_geo.go
backend/internal/service/gateway_scheduling.go
frontend/src/constants/persona.ts
frontend/src/components/account/CreateAccountModal.vue
frontend/src/components/account/EditAccountModal.vue
```

标定与 loader：

```text
tools/cc-calibrate/
backend/internal/pkg/claude/calibrated_profile.go
backend/internal/service/setting_claude_profile.go
backend/internal/service/gateway_upstream_request.go
backend/internal/service/gateway_billing_block.go
backend/internal/service/claude_mimicry_guard.go
```

供号商站点：

```text
backend/migrations/180_add_provider_portal.sql
backend/ent/schema/provider_settlement.go
backend/internal/service/provider_settings.go
backend/internal/service/provider_tier.go
backend/internal/service/provider_onboard.go
backend/internal/service/provider_settlement.go
backend/internal/service/auth_provider_register.go
backend/internal/repository/provider_settlement_repo.go
backend/internal/repository/usage_log_repo_provider.go
backend/internal/server/middleware/provider_guard.go
backend/internal/server/routes/provider.go
backend/internal/handler/provider/
backend/internal/handler/admin/provider_handler.go
frontend/src/views/provider/
frontend/src/views/admin/ProvidersView.vue
frontend/src/components/admin/provider/ProviderSettingsPanel.vue
frontend/src/components/layout/ProviderLayout.vue
frontend/src/utils/providerMoney.ts
frontend/src/api/provider/
frontend/src/api/admin/providers.ts
```

Claude Chrome Cookie OAuth：

```text
backend/internal/pkg/oauth/oauth.go
backend/internal/repository/claude_oauth_service.go
backend/internal/service/oauth_service.go
backend/internal/service/claude_oauth_error.go
backend/internal/service/oauth_refresh_api.go
backend/internal/service/token_refresher.go
backend/internal/service/token_refresh_service.go
backend/internal/repository/account_repo.go
backend/internal/handler/admin/account_handler.go
frontend/src/components/account/OAuthAuthorizationFlow.vue
frontend/src/components/admin/account/AccountActionMenu.vue
```

Anthropic 429 / 模型级限流：

```text
backend/internal/service/ratelimit_service.go
backend/internal/service/model_rate_limit.go
backend/internal/service/gateway_scheduling.go
backend/internal/service/account_fable_availability.go
backend/internal/repository/temp_unsched_cache.go
backend/internal/repository/rpm_cache.go
```

模型清单与新模型接入：

```text
backend/internal/pkg/claude/constants.go
backend/internal/domain/constants.go
backend/internal/service/account.go
backend/internal/service/gateway_service.go
backend/internal/service/upstream_models.go
backend/internal/service/pricing_service.go
backend/internal/service/billing_service.go
backend/resources/model-pricing/model_prices_and_context_window.json
frontend/src/composables/useModelWhitelist.ts
```

Anthropic 分组级客户策略：

```text
backend/migrations/178_add_anthropic_group_policies.sql
backend/internal/service/group_anthropic_policy.go
backend/internal/service/content_safety_guard.go
backend/internal/service/content_moderation.go
backend/internal/service/gateway_claude_oauth_body.go
backend/internal/handler/content_safety_helper.go
backend/internal/handler/content_moderation_helper.go
frontend/src/views/admin/GroupsView.vue
```

部署（生产线 `custom/prod`）：

```text
.github/workflows/release.yml
docs/FORK_DEPLOY_RUNBOOK_CN.md
docs/FORK_PROJECT_MEMORY.md
```

部署（公司线，以下文件**只存在于 `custom/company`**）：

```text
.github/workflows/company-image.yml          构建 + docker save 成 artifact，不推 registry
deploy/company/docker-compose.override.yml   image 指向 ${SUB2API_IMAGE} + pull_policy: never
deploy/company/.env.example                  精简 env 模板
deploy/company/nginx-sub2api-admin.conf      管理后台 HTTPS 反代，网关路由挡在公网外
docs/FORK_COMPANY_DEPLOY_CN.md               公司线运维 runbook
```

