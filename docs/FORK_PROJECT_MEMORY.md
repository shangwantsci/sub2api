# Sub2API 二开项目记忆

> 最后更新：2026-07-31
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

截至 2026-08-02 供号商账号管理面板上线（同日两轮部署）：

- 镜像：`ghcr.io/shangwantsci/sub2api:0.1.156`
- 不可变镜像：`ghcr.io/shangwantsci/sub2api:0.1.156-8035f77a`
- 应用 commit：`8035f77a`（档位标签按实际参数反推、开放自定义档编辑、
  修 `MinPriorityByGroup` 口径、管理端改分组提示优先级门槛，见 4.14.3）；
  前一轮 `047f4fb9`（批量上号、二次编辑、账号邮箱与额度用量，见 4.14.2）
- 应用版本：`0.1.156`
- GitHub Actions run：`30747110860`（`custom-image` success）；前一轮 `30744920471`
- **两轮均无数据库迁移**，最高迁移仍是 `183`，回滚只需切回镜像
- 上一轮为 2026-08-01 的 `a6948086`（run `30707022376`，共三次部署：
  功能主体 `4d3ff165` → 归属回填迁移 `aba0a308` → 上号页白屏热修 `a6948086`）
- 平台：Linux x86_64 / Docker Compose
- 生产目录：`/opt/sub2api-production`
- Compose：
  - `docker-compose.local.yml`
  - `docker-compose.override.yml`
- 应用本地映射：`127.0.0.1:18080 -> 8080`
- 公网入口：Caddy 反代
- PostgreSQL、Redis、Caddy 与其它项目独立运行；部署只重建 `sub2api`
- 健康状态：healthy（容器 8 秒转 healthy）
- 本机与公网 `/health`：HTTP 200
- 设置接口与标定状态接口保持鉴权保护；无凭证请求为 HTTP 401
- Persona 全局门控：`false`（尚未灰度启用）
- **供号商站点 `provider_portal_enabled`：已开启**。4 个 `is_provider` 用户
  （id 44/45/46/47），其中两家已实际上号：**供号商 44 共 16 个账号（1 个已下线）、
  供号商 45 共 11 个账号**，各自都用了 3 种不同档位（2026-08-02 查）。
  已产生 1 张结算单（供号商 44，$29.3089，38 请求，水位 873260）。
  上号量从「1 个账号」涨到二十多个正是这轮做批量上号与账号管理面板的起因
- 生产机不运行 `cc-calibrate` sidecar
- 标定 profile：published + valid，CLI `2.1.218`
- 已应用的最高数据库迁移：`183_backfill_provider_proxy_owner.sql`
- 代理归属分布：41 条平台自有 + 2 条供号商私有（74→44、76→45）；
  `auto_assignable = true` 的为 0，即当前没有任何代理进入自动分配池
- 使用 `identity_only` 的 Anthropic 分组数：1（分组 14）
- 账号总数：63（未软删）
- 部署时 `.env` 备份：`backups/.env.20260731-173758.before-aba0a308`
  （功能主体那轮为 `backups/.env.20260731-172623.before-4d3ff165`）
- 部署前运行镜像 commit：`0832ab07`

部署前旧镜像已保留为本地回滚 tag：

```text
sub2api-rollback:pre-a6948086   # = aba0a308 的镜像（该版本上号页白屏，别回滚到它）
sub2api-rollback:pre-aba0a308   # = 4d3ff165 的镜像（同样白屏）
sub2api-rollback:pre-4d3ff165   # = 0832ab07 的镜像，要回到本轮之前用这个
```

**本轮出过一次生产事故**：`4d3ff165` 上线后 `/provider/onboard` 整页白屏，
`aba0a308` 未修复，`a6948086` 才修好。原因是 i18n 文案里的裸 `@` 被 vue-i18n 当成
linked message 语法，编译失败让整页渲染不出来 —— 详见 4.6，那一节记录了
「为什么两轮测试都没抓到」，是本次最值得记住的部分。

本轮验证：

- 迁移 182（proxies 加 `provider_user_id` / `auto_assignable`）与 183（回填历史
  供号商代理归属）均已记录；41 条平台代理未被误标
- 回填前代理 74/76 的归属为空。它们不会被自动分配（`auto_assignable` 默认 FALSE），
  但管理端开关按归属是否为空置灰，归属为空就能被勾上 —— 一勾就把这两家供号商
  自费的出口分给别人。183 用事务在生产上先验证再提交
- 新的分账号明细 SQL（含 `MAX(id)` 与迟到行计数）在真实数据上跑通：
  账号 2871 → 38 请求 / $29.3089 / 水位 873260（与结算单 #1 完全吻合），
  账号 2873 → 46 请求 / $23.2679，两者 `late_rows` 均为 0
- 手工清理守卫生效：近 30 天 258003 行可清理、84 行因属于供号商账号受保护
- 启动窗口 panic / fatal 为 0；两轮均 8 秒转 healthy
- 本机 `/health` 与公网 `https://lumos7.cc/health` 均 200；部署后 5 分钟内
  真实流量 6 条 usage / 4 个账号

### 3.1 上一轮：2026-07-29 `0832ab07`

供号商金额改自适应小数位（修 `total_cost=0.05944` 被显示成 `$0.1`）、授权方式改回
OAuth / Setup Token 通用叫法，纯前端无迁移。run `30416049073`，`.env` 备份
`backups/.env.20260729-021451.before-0832ab07`。

再往前一轮是 2026-07-27 的 `08e222ed`（system→messages 迁移标签移除，见 4.13，
run `30240480199`，digest `sha256:f7e4e36f…`）。逐轮部署记录见
`FORK_DEPLOY_RUNBOOK_CN.md`。

更早轮次的生产验证结论按功能记录在各自小节：`identity_only` 与迁移 179 见 4.8，
Fable `credits_required` 见 4.11，`claude-opus-5` 定价与模型清单见 4.12，
供号商站点首发（迁移 180/181）见 4.14。逐轮部署记录见
`FORK_DEPLOY_RUNBOOK_CN.md`。

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
- 网关必须跳过迁移 system prompt 产生的 synthetic user，见 4.13。

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

2026-07-31 又栽了一次，这次是 `@`：供号商上号页的
`socks5://用户名:密码@1.2.3.4:1080` 让 `/provider/onboard` 整页白屏。
`@` 在 vue-i18n 里是 linked message 语法（`@:key`），后面不跟合法 key 就编译失败。

**为什么两轮测试都没抓到**（这才是真正要记住的）：

1. **编译失败不抛异常**，只往 console 打一条 `Message compilation error`，然后返回
   一个渲染不出来的结果。任何 `expect(...).not.toThrow()` 式的断言都测不出来 ——
   必须去抓 console 输出。
2. **测试环境与生产的 i18n 编译模式不一致**：`vite.config.ts` 有
   `__INTLIFY_JIT_COMPILATION__: true`（配 runtime-only 版本，避开 CSP unsafe-eval），
   而 `vitest.config.ts` 当时没有这个 define，简单字符串被原样返回，语法有问题的
   文案在测试里根本不会报错。已补齐，两边现在一致。
3. `providerI18nKeys.spec.ts` 只检查 key 在不在，源码文本断言只看有没有敏感词，
   都覆盖不到渲染期。

处理规则：

- i18n 文案里**不要出现裸 `{`、`}`、`@`、`|`**。要展示它们就写成字面量插值：
  `{'@'}`、`{'{'}`、`{'}'}`。`admin/resources.ts` 的代理格式说明与
  `admin/settings.ts` 的邮箱后缀说明是既有的正确写法；
- `src/i18n/__tests__/messageCompilation.spec.ts` 会遍历 zh/en 全部文案、抓 console
  编译错误，**新增文案必须让它保持绿色**；
- 页面级改动要有挂载测试（`ProviderOnboard.mount.spec.ts` 是模板），源码文本断言
  抓不到渲染期异常；
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

### 4.13 system→messages 迁移不再带 `[System Instructions]` 标签

OAuth/SetupToken + 非真实 Claude Code 客户端时，客户端 `system` 必须让位给 Claude Code
的 system blocks，因此被迁移成 messages 开头的一对合成消息。上游 sub2api 会给这条
user 消息加 `[System Instructions]\n` 前缀，本 fork 已去掉：

- 该字符串是上游自带的可读性标签，**不是伪装要素**——真实 Claude Code CLI 从不发送
  它，留着反而是一个稳定的第三方特征；客户侧也能感知到自己的提示词被改写；
- 语义不丢：紧随其后的 assistant 应答 `Understood. I will follow these instructions.`
  才是让模型把前一条当指令的支点，**保留不动**；
- assistant ack 未一并删除是有依据的：删掉会让 body 出现两条连续 user 消息，而真实
  CLI 在 normalize 阶段就把同角色相邻消息合并掉了，wire 上不会出现该形态。用一个
  已知特征换一个结构特征不是净收益。若要进一步贴近真身，正确方向是把客户 system
  作为**首条真实 user 消息里的附加 text block**（CLI 处理 CLAUDE.md 的方式），
  那会同时改变 fp 输入与 cache 断点，属于独立项目。

**关键约束**：去掉 wire marker 后，绝不能靠固定 ack 或消息结构猜哪一条是 synthetic。
合法真实对话完全可能恰好出现相同的 user/assistant 形态，误判会让
`cc_version=X.Y.Z.{fp}` 与 `metadata.session_id` 一起静默改用第二轮文本。

当前实现是在改写前读取真实首轮，并通过请求本地上下文显式贯穿：

- system blocks 初建直接使用改写前文本计算 billing fp；
- calibrated profile 改变 CLI 版本时，`syncBillingHeaderVersionWithFirstUserText` 用同一
  文本重算 fp；
- metadata session seed 使用同一文本；普通 retry、signature retry 与 failover 保留该值；
- `extractFirstUserText` 不按 ack 猜测，只取当前 body 第一条 user；旧标签仍作为历史格式
  的显式兼容信号；
- `count_tokens` 从 full 转成 `identity_only` 时只重建 system blocks，不重复迁移 messages；
  该兼容路径缺少进程内上下文时才用 body 内已有的 12-bit fp 反证候选文本，正常生产
  原始请求不依赖推断。

Mimicry Guard、system blocks、metadata 格式、headers、betas 都未改变。回归测试锁定：
迁移前后 fp 相同、真实对话逐字命中 ack 时不误跳过、calibrated UA 重算仍取真实首轮、
full→identity-only 不产生嵌套消息对，且 `c=nil` 不改错 fp。

部署时会有一次性影响：所有走 OAuth 伪装的客户第一条 user 消息内容变化，prompt cache
前缀失效，之后恢复正常。

### 4.14 供号商站点（Provider Portal）

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
- 代理由供号商自带且**必填**（`binding:"required"` + `ValidateProviderProxy` 逐项校验
  协议/host/端口），没有「平台随机分配」这条路。授权阶段走平台默认出口，
  代理在提交那一步才创建，记录名带 `provider-<id>-` 归属前缀；
- 界面上账号类型用 **OAuth / Setup Token** 这两个行业通用叫法，各配一句说明。
  曾经写成「完整授权 / 仅推理授权」这类自造词，供号商反而看不出该选哪个——
  凭据类型是供号商自己手里的东西，不属于需要隐藏的内部细节，别再改回去；
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

`priority` 在调度里是**硬门槛而不是权重**：`filterByMinPriority`
（`gateway_scheduling.go`）只保留候选中数值最小的那批账号，其余**完全不参与**后续选择。
不存在「优先级低就少分一点流量」这回事，只有「拿全部」或「一个都拿不到」。
取值不一致的后果是单向且静默的：数值大的一边永远拿不到一个请求，
账号状态显示正常、用量恒为 0、结算金额恒为 0，没有任何报错。

因此**不让人手工配置**：上号时由 `ResolveProviderAccountPriority` 自动取目标托管分组内
现有账号的最小 priority（`MinPriorityByGroup`，含供号商账号——要对齐的是分组里实际生效的
调度门槛，不区分归属），与该分组里正在跑的账号平起平坐。分组还是空的时才回落设置
`provider_account_priority`（种子值 1，与 `CreateAccountModal.vue` 的默认值一致）。

分组最小值本身非法（0 或越界）时同样不采信——0 是最高优先级，会让供号商账号独占整个分组。

设置页不再提供输入框，改为只读展示每个已开放托管分组上号时实际会用的值。

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
  只在展示时收敛位数；
- CSV 导出输出完整精度原值，不做展示层收敛。

展示位数是**自适应**的，不能改回固定 1 位：

- 默认 2 位（货币惯例）：`$29.31`、`$0.06`；
- 非零但会被收敛成 `0.00` 时放宽到 6 位并去掉末尾零：`$0.0001`，
  避免把有用量显示成零。

曾经用过固定 1 位小数，线上真实用量 `total_cost=0.05944` 被显示成 `$0.1`，
比实际虚高近 70%，而管理端用量统计同一笔显示 `$0.059`——两边对不上，
在对账页面上直接被当成「给供号商多算钱」。底层从未算错（存储、快照、CSV
一直是全精度），但对账界面上的数字虚高是不可接受的。
`providerMoney.spec.ts` 用这个真实值钉住了回归。

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

站点开关是双向的：关站后不仅 `/api/v1/provider/*` 全部 404
（`ProviderOnly`），供号商**也无法登录**——`Login` 里的 `assertProviderPortalOpenFor`
对 `IsProviderUser()` 返回 `PROVIDER_PORTAL_DISABLED`。
不拦的话供号商仍能拿到 JWT 进入面板、每个接口都 404，对外表现成「系统坏了」。
管理员即便带 `is_provider` 也不受影响（`IsProviderUser` 已排除管理员），
否则关站后没人能进去把站点重新打开。

邀请码：校验 → 建用户 → CAS 消费在同一事务内完成。与普通注册的「标记失败只记日志」
刻意不同——普通邀请码只影响赠送，供号商邀请码是准入凭证，
绝不能出现「码没消费掉但人已经进来了」。并发抢同一枚码时输的一方整体回滚。

### 4.14.1 代理来源：平台分配与自带

上号时供号商二选一，实际开放哪些由设置 `provider_proxy_mode_policy` 决定
（`both` / `auto_only` / `manual_only`）。策略是 service 层硬约束
（`AssertProviderProxyModeAllowed`），不是 UI 隐藏 —— 供号商能直接构造请求。

**归属必须是列，不能是命名约定。** `proxies` 新增 `provider_user_id`
（NULL = 平台自有）与 `auto_assignable`（管理员显式开放），迁移 182。在此之前归属
只靠记录名的 `provider-<id>-` 前缀，全仓库没有一行代码依赖它；自动分配一旦按
「绑定账号最少」选号，就会把一家供号商自费的出口分给另一家，两家账号还共用同一个
出口 IP。迁移 183 回填了 182 之前建的那批（生产上是代理 74→44、76→45）。

选号规则（`SelectAutoAssignProxy`）：平台自有 + 管理员已开放 + active 且未过期 +
当前绑定数 `< provider_auto_proxy_max_accounts`（种子 2），取绑定最少的，并列按 ID
升序。**不做随机**：最少优先本身就会轮转。

几条不能改回去的约束：

- `auto_assignable` 默认 FALSE。升级后存量代理一条都不会被分配出去，必须管理员在
  代理管理页逐条勾选。管理端开关对「归属非空」的代理置灰，service 层同样硬拒
  （`ErrProxyProviderOwnedNotAssignable`）。
- **失败回收只回收本次新建的私有代理。** `cleanupOrphanProxy` 原来无条件删，
  而 auto 模式拿到的是共享的平台代理 —— `DeleteProxy` 仅在还有账号引用时才拒绝，
  恰好选中一条当前零绑定的平台代理就会被真的删掉。
- manual 模式按 `(provider_user_id, protocol, host, port, username, password)` 复用
  已有代理，否则同一供号商反复用同一条代理会把代理池撑爆，绑定数统计也跟着失真。

已知且被接受的竞态：两个供号商同时提交可能选中同一条代理，最终绑定数超出上限 1 个。
选号必须发生在换票之前（换票就要走这个出口），而锁没法横跨 OAuth 换票这段外部慢 IO。
后果只是某个出口多挂一个账号，下次分配会自动跳过它。

自带代理接受一整行连接串，三种写法：`scheme://[user:pass@]host:port`、
`host:port[:user:pass]`、`[user:pass@]host:port`。后两种补默认协议 `socks5h`
（DNS 由代理端解析，不把目标域名从本机出口漏出去）。解析以后端
`ParseProviderProxyURL` 为准，前端 `utils/proxyUrl.ts` 只做即时预览；IPv6 必须带方括号。

### 4.14.2 账号管理面板：批量上号、二次编辑、5 小时额度

「我的账号」从一张只读列表变成供号商侧的账号管理面板。四项改动互相咬合，
起因都是同一件事：供号商手上有一批号，但页面上既看不出哪些已经上了、也改不动。

**账号邮箱下发。** `AccountView.Email` 取自 `extra.email_address`
（上号时 `MirrorProviderIdentityToExtra` 从 credentials 复刻过去的那份），
读不到时回落 `credentials` —— 该机制上线之前建的存量账号 extra 里没有这个键。
名称由供号商自填、可以重复也可以乱填，只有邮箱能让他把列表和手上的号单对上。

**批量上号：前端按行拆分、逐条串行调用现有的单账号接口。**

刻意不复用管理端那套 `account_anthropic_session_import.go`：它的逻辑全在
`handler/admin` 包内（provider 包 import 不到）、有全进程单任务锁（多个供号商并发
上号互相 409）、job 查询没有归属校验（凭 job id 能读到别人的邮箱与代理名），
而且整条路径绕开了 `AssertProviderProxyModeAllowed`、`BuildProviderAccountInput`
的档位与 priority 对齐、`ApplyProviderForcedCredentials`、extra 白名单。
复用等于把这些一起搬过来。走单账号接口则所有护栏原封不动。

**串行不是图省事**：auto 代理模式下 `SelectAutoAssignProxy` 按「当前绑定最少」选
出口，只有等上一条落库、绑定数 +1，下一条才会挑到别的 IP；并发发出去整批号会全
挤在同一个出口上。换票打的又是 claude.ai，同 IP 高频本身也容易被风控。

**去重是必需项而不是锦上添花。** `CookieAuth` 内部串行打三次 claude.ai、最坏 180 秒，
而前端 axios 默认超时 30 秒 —— 「前端已报错、后端仍把号建成了」是常态，供号商看到
失败必然重试。所以：`/provider/onboard/submit` 拿到 tokenInfo 后按
**`account_uuid`** 在该供号商名下查一遍，命中就回既有账号并置
`OnboardResultView.Duplicate=true`，不重复建；同时 `api/provider/index.ts` 把这个
请求的 timeout 单独放宽到 200 秒。只认 `account_uuid`：名称可重复，邮箱在部分换票
结果里为空，都不足以判定「是同一个 Anthropic 账号」。已下线的账号被软删除拦截器
滤掉，所以下线后重新上同一个号仍走正常创建。

不去重的后果是同一个 Anthropic 账号变成两条账号记录各自参与调度，共用一份上游配额
互相打架，结算时同一份用量算成两个号。

`OnboardRequest.Name` 因此去掉了 `binding:"required"`：留空时按换票拿到的邮箱命名，
邮箱也没有才退到 `Anthropic <uuid 前 8 位>`，仍然为空就交给
`BuildProviderAccountInput` 报 `NAME_REQUIRED` —— 不编造一个无从对照的占位名。

**二次编辑：`PATCH /provider/accounts/:id`，只开放名称、备注、速率档位。**

- 名称与备注走 `UpdateAccountInput{Name, Notes}`，**其余字段必须全部留零值/nil**。
  这是唯一安全的调用形态，`buildProfileUpdateInput` 拆成纯函数就是为了让
  `account_update_test.go` 能逐个字段钉住它：`Extra` 非 nil 即整体覆盖，会把
  `enable_tls_fingerprint`、`session_id_masking_enabled`、`account_uuid` 一起清掉，
  而伪装失效完全静默；`Priority` 传 0 是最高优先级，该账号会独占整个分组；
  `GroupIDs` 非 nil 是全量替换，空切片直接解绑所有分组。
- `Name` 传空串在 `UpdateAccountInput` 里表示「不改」，所以显式传空名字必须报错，
  否则是一次没有任何提示的静默失败。`Notes` 传空串则确实是「清空备注」。
- 改档位**用不了 `UpdateAccount`**：`provider_tier` 列只在 `Create` 的 builder 里。
  新增了 `UpdateProviderAccountTier`（定向更新，同时写 tier / concurrency /
  load_factor / extra），与管理端「应用到存量」的 `UpdateProviderTierParams` 是两条：
  后者账号仍留在原档位、只刷新参数，前者是换档、档位标识本身要变。
  extra 一律走 `ApplyProviderTierToExtra(account.Extra, tier)` 增量合并 —— 落库是
  整列覆盖，构造一个只含档位键的 map 会清掉 `persona_*`、`window_cost_sticky_reserve`
  与身份字段。新增接口方法照例要在 5 个 `provider_repo_stubs*_test.go` 里补空实现。
- **不开放托管类型与出口代理**：改分组必须同步重算 priority（硬门槛，取值不对会让
  账号被静默饿死或反过来独占分组），换代理要处理旧私有代理的回收与同参数去重。
  两件事各自需要单独设计。
- **编辑里不给自定义档**：自定义档的并发/会话数/RPM 不下发到供号商侧，弹窗只能把
  输入框预填成默认值，供号商本来只想改个名字、一保存就把自己原先填的参数静默重置了。
  自定义档的号可以切到任一固定档，改自定义参数本身仍需管理员。
- 重新授权链接单独走 `POST /provider/accounts/:id/auth-url` 而不复用
  `/onboard/auth-url`：链接类型必须与账号原有类型一致（Setup Token 的号拿 OAuth
  链接换回来的凭据 scope 对不上），而账号是 oauth 还是 setup-token 属于内部处理方式、
  不下发给供号商，前端没有这个信息也不该有，只能由后端按账号自己定。
  后端 `Reauth` 换票时用的一直是 `account.Type`。

**账号额度用量：`GET /provider/accounts/:id/usage`。**

供号商要看的是**这个号在 Anthropic 那边用掉了多少额度**，与管理端账号管理里那一栏
是同一套数据，前端也直接复用了 `components/account/UsageProgressBar.vue`：
标签徽章 + 进度条 + 百分比 + 重置倒计时，`5h` / `7d` / `7d S` / `7d F` 四个窗口，
配色与管理端一致 —— 同一个号在两边不该看出不同结论。

**别和平台自设的 `window_cost_limit` 混为一谈**，那是两套完全独立的「5 小时」：

| | 账号额度（这里下发的） | 平台费用窗口（不下发） |
|---|---|---|
| 数据 | Anthropic 响应头 `anthropic-ratelimit-unified-5h-*` | 本地 `usage_logs` 金额聚合 |
| 单位 | utilization 百分比 | 美元 |
| 用途 | 号本身还剩多少余量 | 平台档位限流的调度准入 |

曾经错做成后者：在账号列表里显示 `$12.34 / $60`（已用金额 / 档位上限）。
那是平台对账号做的限流处理，既不是供号商想知道的，也把档位参数暴露了出去。
`AccountView` 的 `window_cost_*` 字段、`ProviderUsageReader` 上的批量聚合方法、
`view_test.go` 白名单里对 `window_cost_limit` 的放开**全部已回退**，
该禁止子串仍在名单里。

实现约束：

- 响应走 `AccountUsageView` 白名单收敛，**绝不透传 `service.UsageInfo`**：后者挂着
  Grok / Gemini / Antigravity 各平台的一大堆字段，以及三种口径的金额 ——
  `WindowStats.Cost` 含账号倍率、`UserCost` 是向客户收的价，漏任何一个都等于把平台
  定价告诉供号商。窗口视图只搬 `utilization` / `resets_at` / `remaining_seconds` /
  `requests` / `tokens`，**一个金额都不带**。
  `StandardCost` 虽然与结算同口径也不带：列表里已经有「本期金额」，
  再放一个区间不同的金额只会让人以为对账对不上。
- 默认 `source=passive`，走 `AccountUsageService.GetPassiveUsage` —— 它只读账号 extra
  里的被动采样值（网关每次请求顺带采回来的响应头），**零外部调用**，列表页逐行拉也
  不会打爆什么。`source=active` 才真的问一次 Anthropic，只在供号商手动点刷新时用。
- 上游没给某个窗口时该窗口保持 nil，前端整条不渲染。补一个 0% 会被读成
  「这个号完全没用过」。
- 前端逐账号拉、限并发 4，且**不阻塞表格渲染**（`void loadUsage(...)`）；
  单个账号取用量失败只让那一格留白，不把整张表打成错误状态 —— 账号本身的信息还是好的。
- `provider.Handler` 因此新注入了 `*service.AccountUsageService`
  （`NewHandler` 参数与 `cmd/server/wire_gen.go` 的构造调用同步改了）。

### 4.14.3 档位标签反推、参数下发，与不开放供号商换分组的理由

**档位标签改为按账号实际参数反推**（`ResolveProviderAccountTier`）。

原先直接读 `provider_tier` 列。问题是那一列只有两条写入路径（建号、供号商自己换档），
**管理端既没有改它的 API 也没有 UI**；而管理员在管理端编辑账号时改的并发 /
会话数 / RPM / 5 小时上限，恰好就是档位映射的同一组参数
（`EditAccountModal.vue` 写 `newExtra.window_cost_limit` / `max_sessions` /
`base_rpm` + `concurrency` 列，与 `providerTierExtraKeys` 完全重合）。

生产上这个不一致已经真实发生：同一个「3 档」下既有标准值（并发 3 / RPM 30 /
会话 3 / 上限 60）的账号，也有并发被改成 **1000**、extra 档位键全被清空的账号，
而供号商页面上两者都写着「3 档」。

改成反推之后：四项参数精确匹配某个固定档就显示那个档的 label，匹配不上一律
显示「自定义」。标签在任何情况下都不会撒谎，也不关心参数是谁改的。几个约束：

- 比对**全量** `CapacityTiers` 而不是 `EnabledTiers()`：档位被停用后，已经在用它的
  账号参数并没有变，显示原档位名仍然准确。停用只该影响「能不能换到该档」。
- 金额用容差比（`1e-9`）而不是 `==`：`window_cost_limit` 是 float64，
  JSON 里 60 与 60.0 都可能出现。
- 匹配不上时后端回**空 label + `tier='custom'`**，「自定义」三个字由前端 i18n 出。
  原先是后端硬编码中文，英文界面的供号商也会看到中文。
- `provider_tier` 列仍然保留：管理端「应用到存量」按档位批量回填还要靠它
  （`ListByProviderTier`）。**注意副作用**：被管理员手工调过参数的账号，
  `provider_tier` 还是旧值，下次「应用到存量」会把手工调整静默刷回标准值。

**四个档位参数下发给供号商**（`concurrency` / `max_sessions` / `base_rpm` /
`window_cost_limit`）。这不是新泄露：上号页 `OnboardOptionsView.Tiers` 里每个档位的
这四个值一直是明着展示的，供号商就是照着它选档的。不下发的后果很实际——
**线上 26 个在跑的供号商账号里 21 个是自定义档**，编辑时既看不到当前参数也改不了，
预填默认值则会让「只想改个名字」的操作把自己原先设的参数覆盖成 1/1/10/20。
`view_test.go` 的禁止子串名单相应去掉了这四项，其余调度参数仍然不下发。

编辑弹窗用账号真实取值预填，并保存一份**预填快照**来判断供号商有没有真的动过
输入框 —— 预填时把 0 规整成了合法下限（护栏要求四项都 `> 0`，0 在运行时表示
「不启用该限制」），拿账号原值比会把「没动过」误判成「改过」而白跑一次换档。

**`MinPriorityByGroup` 的统计口径修正（P0，与本轮功能无关的现存漏洞）。**

调度里 `filterByMinPriority` 是从**已经过状态过滤的可调度候选**里取最小 priority，
而这个 SQL 原先只过滤 `deleted_at`。两者口径不一致时：

> 分组里有个 `priority=1` 的账号被暂停了（供号商自己点的 pause，或 status=error），
> 在跑的账号全是 `priority=5`。新号上号时对齐到 1 —— 于是它成为分组内唯一的
> `priority=1` 可调度账号，`filterByMinPriority` 只保留它，**独吞该分组全部流量，
> 其余账号静默饿死**，账号状态显示正常、用量恒为 0。

而且可以被主动构造：先 pause 一个号，再上新号。SQL 已加
`status='active' AND schedulable=true`。生产数据验证：新旧口径在全部 6 个分组上
结果一致（都是 1），本次修复对线上行为零影响，纯粹是堵住未来的洞。

**供号商侧不开放改托管类型（分组），管理员代操作。**

分组倍率确实与供号商结算无关（`total_cost` 恒为 1 倍率），但换分组的风险不在倍率：

1. **越权，且与 priority 无关。** `validateGroupIDsExist` 只检查分组存在，
   不检查它在不在托管类型白名单里。上号路径有 `FindHostingType` 这道闸，
   直接复用 `UpdateAccountInput.GroupIDs` 就没有 —— 供号商能把号塞进内部专用分组、
   独占分组、非 Anthropic 分组、`content_review_policy=disabled` 的分组。
2. **粘性会话被静默打断。** sticky key 是 `sticky_session:{groupID}:{sessionHash}`，
   换组后旧绑定活到 TTL 到期，期间该会话回落到「换一个账号」——
   对 OAuth 号意味着换身份、换 session_id 伪装上下文。而 key 按 sessionHash 索引，
   **反查不到某个账号的绑定**，做不到主动清理。
3. **激励问题。** 不同托管分组的客户请求量差别很大而供号商按用量拿钱，
   开放自助换组的理性行为就是所有人往最忙的分组挤。这需要产品规则
   （冷却期、目标分组名额上限），不是写代码能解决的。

供号商侧的托管类型**本来就是实时同步的**（`GroupIDs` 每次查询实时 JOIN、
provider settings 每次现读 DB，全链路无缓存），管理员改完分组供号商刷新就能看到。

**管理端改分组时提示优先级门槛**（`GET /admin/groups/scheduling-priorities`）。
`UpdateAccount` 改分组时不会重算 priority（只在管理员显式传 `Priority` 时才写），
等于绕开了供号商侧刻意不开放换组所要防的那件事。管理端账号列表的 priority 列
**默认是隐藏的**，所以光写一句「请确认优先级」没用——管理员连账号自己的值都看不到。
现在编辑弹窗在分组被改动、且目标分组门槛与本账号 priority 不一致时直接把两个数字
摆出来。这是提示不是阻断：仍然由管理员决定怎么填。

### 4.14.4 已知未决问题

以下问题在代码审查中被识别，**上线前必须评估**：

- ~~备份导出结构不含 `provider_user_id` / `provider_tier`~~ 已于 `4d3ff165` 修复：
  账号备份补齐了归属、档位、`status`、`schedulable` 与 `group_ids`，导入时校验归属
  指向的确实是本实例的供号商（不是就丢弃归属并报出来），分组按 id 匹配、对不上的
  过滤掉并列在结果里。代理备份补齐了归属与 `auto_assignable`，`proxy_key` 叠加归属
  后缀以免两家供号商的同参数代理被合并成一条。
  **仍然只对同实例恢复有效**：`users` 表没有任何导出功能，换实例恢复时归属指向的
  用户根本不存在。跨实例迁移只能用整库备份。
- 供号商自己没有改密入口（`ProviderDenyConsumerRoutes` 挡掉了 `/user/password`），
  由管理员代改：用户管理 → 目标行「编辑」→ 填密码。`PUT /admin/users/:id` 对
  `is_provider` 无任何限制，留空则不改密。
- 若给托管分组配了**渠道自定义单价或分组图片单价**，这些加价会进 `total_cost`，
  等于按加价后的金额付给供号商。上线前确认托管分组是否配了自定义定价。
- 删除供号商用户时不检查其名下账号与待结算金额。
- 对账 SQL 的三条口径约束（1 倍率、不过滤软删账号、强制结算时区）目前靠代码注释与
  单元测试约束，**仍无 DB 集成测试覆盖**。
- `usage_logs` 是否已在生产库转成分区表未确认；若已分区，保留清理走的是
  DROP 月分区路径，冷却期与未结算保护同样适用，但分区边界是日历月，
  粒度比行删除粗，需要确认最早未结算周期不会落在待 DROP 的分区内。
- **迟到行水位是 `MAX(id)`，本身有结构性洞**：一行 id 低于本期最大 id、却在封账后
  才提交，下期的补计条件 `id > 水位` 判不出来，这笔钱既不在已封金额里也进不了下一期。
  冷却期只能压低概率，不构成证明。真正兜底需要一个由数据库赋值的提交时刻列
  （或改成按 id 区间记账），属独立改造。手工清理已经不会碰供号商的行（见下条），
  所以当前的风险面只剩这一个。
- **手工用量清理不再删除供号商账号的任何 usage_logs**（`4d3ff165` 起，删除条件里
  硬排除）。代价是这些行只能由定时保留策略清理，长期会持续增长。若要一个「清理已封账
  的供号商历史数据」的入口，应当按结算单水位单独设计，而不是放宽这道守卫。
- **结算的总额与明细已改为同源**（`4d3ff165`）。此前是两次独立查询，中间提交的行
  让导出凭证高于结算单、且会在下期被重复计入。`GetProviderPeriodTotals` 仍保留给
  批量结算的空判断与列表页用，但**不可再与 `GetProviderAccountBreakdown` 配对**。
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
provider_auto_proxy_max_accounts       # 种子 2，区间 1..50，单个平台 IP 的账号上限
provider_proxy_mode_policy             # 种子 both；both | auto_only | manual_only
```

数据库迁移：

```text
backend/migrations/180_add_provider_portal.sql
backend/migrations/181_provider_settlement_integrity.sql
backend/migrations/182_add_proxy_auto_assign.sql
backend/migrations/183_backfill_provider_proxy_owner.sql
```

180 新增 `users.is_provider`、`accounts.provider_user_id`（partial index）、
`accounts.provider_tier` 与 `provider_settlements` 表。

181 补财务完整性：`provider_settlements.last_usage_id`（封账水位）与 `void_reason`、
金额/周期 CHECK 约束、`(provider_user_id, period_end) WHERE status='settled'` 的
partial unique index，以及明细快照表 `provider_settlement_items`。

### 4.15 客户服务器部署授权（未部署，代码验收中）

2026-08-02 开始实现客户自有 root 服务器的交付保护。目标不是阻止客户复制 Docker
镜像（root 无法技术上绝对阻止），而是：

1. 客户服务器只有编译镜像，没有 Git 仓库和源码；
2. 客户专属镜像复制到另一台机器后不能获得有效 lease；
3. 授权中心由我们控制，可查看、限制和吊销实例；
4. 授权续租不进入 AI 请求热路径，不触碰 Claude 伪装字节。

#### 架构与仓库边界

- 主仓只放授权**客户端**和客户部署模板；
- 独立授权中心放在同级私有项目 `../sub2api-license-server/`；
- Sub2API 是 LGPL-3.0-or-later，商业交付前必须确认对应源码/重新链接义务；
- 授权中心私钥永不进入本仓、GitHub Actions、客户 `.env` 或客户镜像。

授权中心：

```text
客户首次启动 -> 一次性 activation code -> 绑定 machine hash + instance Ed25519 公钥
每小时续租   -> instance 私钥签名 renewal -> 授权中心签 24h lease
```

客户客户端：

- `backend/internal/deploymentlicense/`：
  - Ed25519 三段 lease 验签；
  - 实例 keypair/install ID 持久化；
  - machine-id + DMI + install identity 指纹；
  - activation/renew HTTP client；
  - lease 原子落盘。
- `DeploymentLicenseService`：
  - 后台每小时续租；
  - `atomic.Pointer` 保存运行时状态；
  - 有效期内 `active`；
  - lease 过期后再进入最多 6 小时 `grace`（刚续租后断网的总离线时间可接近 30h）；
  - `expired/unlicensed/revoked` 时停止 gateway，但保留管理和授权恢复入口。

#### 客户镜像不可由环境变量直接关闭

客户镜像由 build-time ldflags 固定：

```text
main.ManagedCustomerID=<customer>
main.LicensePublicKey=<Ed25519 public key>
```

`ManagedCustomerID != community` 时：

- `DEPLOYMENT_LICENSE_ENABLED=false` 被忽略；
- 配置公钥不能覆盖 build-time 公钥；
- grace 不能超过 6 小时；
- 禁止 `DEPLOYMENT_LICENSE_MACHINE_ID` 手工覆盖；
- release 构建必须至少读到一个宿主机身份信号，否则启动直接失败；
- `update.disabled` 被强制为 true，不能在线替换成无授权的上游二进制。

**实测结论（2026-08-02）**：entrypoint 用 `su-exec` 降权到 uid 1000，而
`/sys/class/dmi/id/product_uuid` 与 `board_serial` 在多数发行版上是 `0400 root:root`，
所以容器内**通常只能读到 `/etc/machine-id`**，`host_signal_count` 实际是 1 而不是 3。
不要在文档或对外说明里宣称三信号指纹；部署后用
`GET /api/v1/admin/deployment-license/status` 的 `host_signal_count` 核对实际值。

这只能防普通复制，不能宣称对 root 下专业 patch/内存 dump 绝对安全；高价值客户第二阶段
需要云实例签名 identity 或 TPM 2.0。

#### 按客户裁剪功能：用 lease features，不开分支

客户版屏蔽功能**不通过新分支实现**。`custom/company` 那套（删代码 + 反向断言测试 +
独立 workflow）已经是第二条线，再加客户线就是第三份要手工同步的裁剪。

改用运行时能力白名单，复用已有的 lease `features` 字段：

```text
service.managedCapabilities   受管能力清单（当前只有 chrome_cookie_auth）
service.HasCapability(name)   community/未启用 → 恒 true；客户镜像 → 看 lease.features
```

- 不在 `managedCapabilities` 里的名字**永远开放**，不用穷举全部功能面；
- 客户镜像下，受管能力必须在 lease.features 里显式出现才启用；
- 无 lease / 已吊销 → 受管能力一律关闭；
- 后端拦截在 `DeploymentLicenseEnforcement` middleware，前端读
  `Snapshot().Capabilities`（后端算好，前端不重复推导规则）。

Chrome Cookie 屏蔽的端点**只有**这两个，不要扩大：

```text
POST /api/v1/admin/accounts/chrome-cookie-auth
POST /api/v1/admin/accounts/{id}/refresh-cookie-auth   （Chrome 账号专用的轮换刷新）
```

`/cookie-auth`（普通 Cookie 自动授权）和 `/setup-token-cookie-auth`（setup token）
是**不同功能**，误拦会砸掉客户的正常上号，有回归测试守着。

前端默认"不隐藏"：能力未知（未加载/请求失败）时照常显示，安全由后端保证。否则接口
一抖动，自己的生产后台就少功能。

#### 标定 profile 经授权中心下发

客户服务器不跑 cc-calibrate（需要 node + CLI + 源码目录），CI 的 `publish.js` 又只推
单个 `CC_CALIBRATE_GATEWAY_URL`。客户实例因此收不到 profile 更新，伪装会停在构建时的
内置常量上逐渐落后——这是 2026-08-02 排查出的运营缺口。

解决方式是复用已有的续租通道，而不是让客户暴露 Admin API：

```text
CI 标定完成 → POST 授权中心 /admin/calibration-profile
            → 客户端每小时续租，响应里带独立签名的 profile envelope
            → 客户端验签 → SettingService.PublishClaudeCalibratedProfile → 热加载
```

关键约束：

- profile envelope 用同一把私钥**独立签名**，`type` 为 `sub2api-calibration-profile`，
  与 lease 的 type 不同，两者不可互相冒用（有测试）；
- 存储和传输都是发布时的**原始字节**，不重新序列化；
- profile 读取/签名失败**不影响续租**——绝不能因为伪装更新丢掉客户授权；
- 客户端按 `cli_version` 去重，版本没变不重复写库；
- 发布失败的 profile 下次续租会重试（不会错误标记为已应用）。

需要的仓库 secret：`DEPLOYMENT_LICENSE_SERVER_URL`、`DEPLOYMENT_LICENSE_ADMIN_TOKEN`。

#### 请求路径与性能约束

全局 license middleware 只读取进程内 snapshot：

```text
用户请求 -> O(1) 内存状态 -> 原有 API key / 调度 / mimicry / TLS
```

禁止在以下路径发 license HTTP/DB 请求：

```text
GatewayHandler.Messages
SelectAccountWithLoadAwareness
GetAccessToken
buildUpstreamRequest
Claude mimicry guard
TLS ClientHello
```

因此 license 不改变 headers、body、betas、billing fingerprint、metadata 或 TLS 指纹。

#### 状态与分级行为

```text
disabled     community 普通部署，保持历史行为
active       正常
grace        已有 gateway 流量继续；管理写操作只读
expired      gateway 503；管理/授权恢复保留
unlicensed   首次激活未成功；gateway 503
revoked      授权中心明确拒绝；gateway 立即关闭
limit_exceeded 账号/用户数超过 lease 上限；gateway 关闭，管理删除能力保留
```

管理接口：

```text
GET  /api/v1/admin/deployment-license/status
POST /api/v1/admin/deployment-license/refresh
```

`/health` 仍只做 O(1) liveness，不把授权中心网络依赖塞进 Docker healthcheck。

#### 构建与交付

`release.yml custom_image_only` 新增 `customer_id`：

- `community` 保持原 `ghcr.io/<owner>/sub2api` package/tag；
- 客户构建使用独立 package
  `ghcr.io/<owner>/sub2api-customer-<customer>:<VERSION>[-<COMMIT>]`；
- Repository Variable `DEPLOYMENT_LICENSE_PUBLIC_KEY` 在构建时注入；
- 客户构建缺公钥时 workflow 直接失败；
- OCI label 记录 customer/build 水印。

禁止只用同一个 package 的不同 tag 隔离客户：GHCR pull 权限通常按 package 授予，客户若能
看到 community tag 就能绕过授权。

客户部署文件：

```text
deploy/docker-compose.customer.yml
deploy/.env.customer.example
docs/CUSTOMER_DEPLOYMENT_LICENSE_CN.md
```

首次激活成功后必须清空 `.env` 的一次性 activation code；实例私钥和 lease 位于
`data/license/`，权限 0600。换服务器时先吊销旧 instance、生成新激活码，并清空新机的
`data/license/`。

#### 当前发布状态

截至本节写入时：

- 客户授权功能默认关闭，现有生产/community 镜像行为不变；
- 无数据库 schema 迁移；
- 客户授权镜像尚未构建/部署；
- 独立授权中心与客户端仍需完成端到端联调后才能用于收费客户；
- 不得把“单元测试通过”写成“生产已验证”。

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
- 客户部署授权默认关闭；community/现有生产不因该功能改变行为。
- 客户专属 release 镜像必须内置非 community customer ID 与授权公钥；不能只靠可编辑 env 开关。
- 授权签名私钥、ADMIN_TOKEN 和真实激活码不得进入主仓、Actions 日志或客户通用模板。
- `/health` 不依赖授权中心；license readiness 走独立管理员接口。

## 11. 后续优化优先级

### P0：上线后观察

- 客户授权功能在首个收费客户前完成 license-server ↔ 客户镜像端到端联调：
  首次激活、小时续租、复制到第二机拒绝、6h grace、吊销与恢复；
- 对比授权 middleware 开/关的本地基准，确认只有 O(1) 内存读取、TTFT 无可测回归；
- 完成 LGPL 商业交付法律复核；当前实现不能替代许可证合规意见；
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

客户部署授权：

```text
../sub2api-license-server/
backend/internal/deploymentlicense/
backend/internal/service/deployment_license_service.go
backend/internal/server/middleware/deployment_license.go
backend/internal/server/routes/admin.go
deploy/docker-compose.customer.yml
deploy/.env.customer.example
docs/CUSTOMER_DEPLOYMENT_LICENSE_CN.md
.github/workflows/release.yml
Dockerfile
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

