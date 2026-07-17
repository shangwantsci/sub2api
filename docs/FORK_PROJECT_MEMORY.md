# Sub2API 二开项目记忆

> 最后更新：2026-07-17  
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
- `main`：保留官方版本线，同时承载 GitHub Actions workflow。
- `upstream`：官方 `Wei-Shaw/sub2api`，禁止 push。

### 2.2 版本和镜像

- 应用语义版本继续读取 `backend/cmd/server/VERSION`，当前为 `0.1.156`。
- 二开源码身份使用 8 位 commit。
- 不为每次二开部署创建 Git tag。
- GitHub Actions `custom_image_only` 同时发布：
  - `ghcr.io/shangwantsci/sub2api:<官方 VERSION>`：生产使用；
  - `ghcr.io/shangwantsci/sub2api:<官方 VERSION>-<commit>`：不可变审计/回滚。

二进制 `--version` 必须同时显示官方 VERSION 与二开 commit。

## 3. 当前生产状态

截至 2026-07-17 首次正式上线：

- 镜像：`ghcr.io/shangwantsci/sub2api:0.1.156`
- 应用 commit：`c8637aab`
- 应用版本：`0.1.156`
- 平台：Linux x86_64 / Docker Compose
- 生产目录：`/opt/sub2api-production`
- Compose：
  - `docker-compose.local.yml`
  - `docker-compose.override.yml`
- 应用本地映射：`127.0.0.1:18080 -> 8080`
- 公网入口：Caddy 反代
- PostgreSQL、Redis、Caddy 与其它项目独立运行；部署只重建 `sub2api`
- 健康状态：healthy
- 设置接口与标定状态接口：HTTP 200
- Persona 全局门控：`false`（尚未灰度启用）
- 生产机不运行 `cc-calibrate` sidecar

部署前旧镜像已保留为本地回滚 tag：

```text
sub2api-rollback:pre-c8637aab
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

当前生产 hotfix 镜像构建：

- run：`29583104613`
- job：`custom-image`
- 结论：success
- 其它 release/tag jobs：skipped

首次 Persona 版本镜像构建 run 为 `29578816168`；随后因 Vue I18n JSON
placeholder 修复重新构建并部署 `c8637aab`。

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

## 11. 后续优化优先级

### P0：上线后观察

- Anthropic 事故恢复后复测 TTFT；
- 对比 proxy 23 与其它美国代理；
- 观察新 profile 下 400/401/429/529、cache read/create、账号寿命；
- 确认 `count_tokens max_tokens` 400 已消失。

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

部署：

```text
.github/workflows/release.yml
docs/FORK_DEPLOY_RUNBOOK_CN.md
docs/FORK_PROJECT_MEMORY.md
```

