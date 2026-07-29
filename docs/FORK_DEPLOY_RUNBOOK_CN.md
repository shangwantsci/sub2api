# sub2api 二开提交、推送、部署流程

> **适用分支：`custom/prod`（二开生产线）。这是日常主力分支。**
>
> 本仓库还有一条 `custom/company` 公司内部部署线，它的流程与本文**完全不同**
> （不推 GHCR、镜像走 artifact + `docker load`、无域名），运维文档是只存在于该
> 分支的 `FORK_COMPANY_DEPLOY_CN.md`。两条线的对照见
> `FORK_PROJECT_MEMORY.md` 第 2.3 节。
>
> 动手前先确认：`git branch --show-current`
>
> **不要用本文的 `custom_image_only` 流程构建 `custom/company`** —— 两个分支的
> `VERSION` 都是 `0.1.156`，那样会把生产正在拉取的 GHCR 可变 tag 覆盖成公司镜像。

本文档固定二开分支的日常发布流程，避免每次手工部署时遗漏测试、版本号或服务器切换步骤。

> 最近验证：2026-07-27 已按本流程部署 `0.1.156-08e222ed`，GitHub Actions
> run `30240480199`；system→messages 迁移不再携带 `[System Instructions]` 标签，
> billing fp 与 metadata session 显式保持真实首轮语义。上一轮为同日供号商站点版本
> `0.1.156-2e4495d9`（run `30230866070`）。

当前生产状态、Persona/自动标定架构、GitHub Actions 运行情况和后续优化路线见：

```text
docs/FORK_PROJECT_MEMORY.md
```

## 核心原则

- **生产镜像由 GitHub Actions 构建**：生产机只 pull，不执行 `docker build`。
- **GHCR 同时保留两种 tag**：官方版本 tag（生产）与 `<官方版本>-<commit>`（不可变审计/回滚）。
- **应用版本号用官方版本号**：即 `backend/cmd/server/VERSION`，例如 `0.1.139`。不要把 commit hash 当作 `VERSION`，否则后台会把当前版本识别成非官方语义版本，并持续提示有新版本。
- **只切换应用容器镜像**：生产数据目录、Postgres、Redis、Caddy 配置不随应用发布改动。
- **先完成 Actions 构建，再在生产 pull/recreate**：构建失败时旧容器继续运行，不进入半部署状态。
- **切换前保留旧 image ID**：必须创建 `sub2api-rollback:pre-<commit>` 本地 tag，并按 commit 命名备份 `.env`。
- **可变与不可变 tag 必须同镜像**：部署前校验 `<VERSION>` 与 `<VERSION>-<commit>` 的 image ID 一致。
- **生产构建使用仓库根 `Dockerfile` 或已对齐的 `deploy/Dockerfile`**：两者都应固定 `pnpm@9` 并支持 `VERSION` 注入。

## 本地提交流程

在仓库根目录执行：

```bash
git status --short
```

确认只有本轮相关改动。然后按改动类型运行测试：

```bash
# 前端组件或管理页改动
cd frontend
npm run test:run -- <相关 spec 文件>
npm run typecheck

# 后端伪装网关或调度改动
cd ../backend
go test -tags unit ./internal/service -run 'Test.*Claude.*Mimic|Test.*OAuth.*Metadata|Test.*CountTokens|Test.*Fingerprint|Test.*Beta'
go test -tags unit ./internal/service -run 'Test(Handle429|HandleUpstreamError_Anthropic|.*Fable.*|.*SessionKey.*|.*ClaudeChrome.*)'
go test -tags unit ./internal/handler -run 'Test.*ClaudeCode|Test.*CountTokens'
```

提交并推送当前分支：

```bash
cd /Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork
git add <files>
git commit -m "<type>: <summary>"
git push origin "$(git branch --show-current)"
```

## 版本号规则

发布前读取官方版本号：

```bash
APP_VERSION="$(tr -d '\r\n' < backend/cmd/server/VERSION)"
COMMIT="$(git rev-parse --short=8 HEAD)"
echo "APP_VERSION=${APP_VERSION}"
echo "COMMIT=${COMMIT}"
```

构建参数必须这样传：

```bash
--build-arg VERSION="${APP_VERSION}" \
--build-arg COMMIT="${COMMIT}"
```

含义：

- `VERSION`：给后台更新检查使用，必须保持官方语义版本。
- `COMMIT`：给排查问题使用，记录当前二开代码提交。
- Docker 镜像 tag：`ghcr.io/shangwantsci/sub2api:${VERSION}` 与
  `ghcr.io/shangwantsci/sub2api:${VERSION}-${COMMIT}`。

如果官方发布了新版本，应先合并/同步上游，让 `backend/cmd/server/VERSION` 跟着更新，再重新构建部署。不要为了消除更新提醒手改成一个不存在的版本号。

## 生产部署流程（推荐：GitHub Actions 构建 GHCR）

生产机内存较小，**禁止在生产机执行 `docker build`**。本仓
`.github/workflows/release.yml` 的 `custom_image_only` 模式会从指定分支构建 GHCR，
不创建、也不要求新的 Git tag：

```bash
APP_VERSION="$(tr -d '\r\n' < backend/cmd/server/VERSION)"

gh workflow run release.yml \
  --repo shangwantsci/sub2api \
  --ref custom/prod \
  -f tag="v${APP_VERSION}" \
  -f custom_image_only=true \
  -f source_ref=custom/prod \
  -f simple_release=true

gh run list --workflow Release --repo shangwantsci/sub2api --limit 3
gh run watch --repo shangwantsci/sub2api
```

其中 `tag` 只是现有 workflow 的兼容必填输入；`custom_image_only=true` 时不会 checkout
该 tag、不会创建 Release，实际构建 `source_ref`。工作流发布两个镜像 tag：

```text
ghcr.io/shangwantsci/sub2api:<官方 VERSION>
ghcr.io/shangwantsci/sub2api:<官方 VERSION>-<8位 commit>
```

前者供生产 `.env` 延续官方版本 tag；后者是不可变回滚/审计 tag。二进制内部：

- `VERSION` = `backend/cmd/server/VERSION`（官方语义版本）；
- `COMMIT` = 二开源码 commit。

GitHub Actions 成功后，在服务器 `/opt/sub2api-production`：

```bash
cd /opt/sub2api-production
APP_VERSION=0.1.156 # 替换为本次 backend/cmd/server/VERSION
COMMIT=8782b30f  # 替换为本次 8 位 commit
MUTABLE="ghcr.io/shangwantsci/sub2api:${APP_VERSION}"
IMMUTABLE="ghcr.io/shangwantsci/sub2api:${APP_VERSION}-${COMMIT}"

OLD_IMAGE_ID="$(docker inspect -f '{{.Image}}' sub2api)"
docker image tag "${OLD_IMAGE_ID}" "sub2api-rollback:pre-${COMMIT}"
cp .env "backups/.env.$(date +%Y%m%d-%H%M%S).before-${COMMIT}"

docker pull "${IMMUTABLE}"
sed -i "s#^SUB2API_IMAGE=.*#SUB2API_IMAGE=${MUTABLE}#" .env
docker compose -f docker-compose.local.yml -f docker-compose.override.yml pull sub2api
test "$(docker image inspect -f '{{.Id}}' "${IMMUTABLE}")" = \
  "$(docker image inspect -f '{{.Id}}' "${MUTABLE}")"

docker compose -f docker-compose.local.yml -f docker-compose.override.yml \
  up -d --no-deps --force-recreate sub2api
curl -fsS http://127.0.0.1:18080/health
docker exec sub2api /app/sub2api --version
```

这样镜像构建完全在 GitHub runner 上完成，不占用生产机 CPU/内存。

2026-07-27 最近一次验证：

```text
immutable image: ghcr.io/shangwantsci/sub2api:0.1.156-08e222ed
digest:          sha256:f7e4e36fc60601151ba60edb2623234e10b729b73844811be106bc686c469c01
Actions run:     30240480199 (custom-image success, 4m55s)
env backup:      backups/.env.20260727-071111.before-08e222ed
rollback tag:    sub2api-rollback:pre-08e222ed
```

部署后容器约 6 秒转 healthy，二进制版本为 `0.1.156 / 08e222ed`，本机与公网
`/health` 均为 200，两个管理接口无凭证均保持 401；启动窗口 panic/fatal 为 0。
首轮 token refresh 为 `total=72, needs_refresh=1, refreshed=0, failed=1`。
Guard 未出现 billing、Agent SDK 身份或 system block 数量 finding；仅出现与 Chrome OAuth
必需 beta 有关的 `unexpected_oauth_beta`，不涉及本次 messages 文本改动。

上一轮 2026-07-25：

```text
immutable image: ghcr.io/shangwantsci/sub2api:0.1.156-8782b30f
digest:          sha256:8ec3a0244e68a12182681abd395765773500f7a18ecbd7f2371eda3e565fdb13
env backup:      backups/.env.20260725-010617.before-8782b30f
rollback tag:    sub2api-rollback:pre-8782b30f
```

更早一轮 2026-07-23：

```text
immutable image: ghcr.io/shangwantsci/sub2api:0.1.156-a16045ee
digest:          sha256:47209037118a083d3bb51a004899768e27d119f1be339a4262c4c3aba849094c
env backup:      backups/.env.20260723-053306.before-a16045ee
rollback tag:    sub2api-rollback:pre-a16045ee
```

## 生产部署流程（仅应急：服务器本地构建）

以下流程仅在 GHCR/GitHub Actions 不可用时应急使用。服务器本地构建会显著争抢
CPU/内存，可能同时造成管理页加载超时和网关 TTFT 升高。

- 生产目录：`/opt/sub2api-production`
- release 目录：`/opt/sub2api-production/releases/<commit>`
- 应用镜像：`sub2api-local:<commit>`
- 对外入口由 Caddy 转发，sub2api 容器映射到 `127.0.0.1:18080`

本地准备变量：

```bash
BRANCH="$(git branch --show-current)"
COMMIT="$(git rev-parse --short=8 HEAD)"
APP_VERSION="$(tr -d '\r\n' < backend/cmd/server/VERSION)"
SSH_TARGET="root@38.244.21.202"
SSH_PORT="9646"
```

在服务器创建/更新 release，并后台构建镜像：

```bash
ssh -p "${SSH_PORT}" "${SSH_TARGET}" "
set -euo pipefail
BRANCH='${BRANCH}'
COMMIT='${COMMIT}'
APP_VERSION='${APP_VERSION}'
PROD=/opt/sub2api-production
RELEASE_DIR=\$PROD/releases/\$COMMIT
IMAGE=sub2api-local:\$COMMIT

if [ ! -d \"\$RELEASE_DIR/.git\" ]; then
  rm -rf \"\$RELEASE_DIR\"
  git clone --depth 1 --branch \"\$BRANCH\" https://github.com/shangwantsci/sub2api.git \"\$RELEASE_DIR\"
fi

cd \"\$RELEASE_DIR\"
git fetch --depth 1 origin \"\$BRANCH\"
git checkout --detach \"\$COMMIT\"
test \"\$(git rev-parse --short=8 HEAD)\" = \"\$COMMIT\"

LOG=\$PROD/build-\$COMMIT.log
rm -f \"\$LOG\"
nohup docker build \
  -f Dockerfile \
  --build-arg VERSION=\"\$APP_VERSION\" \
  --build-arg COMMIT=\"\$COMMIT\" \
  -t \"\$IMAGE\" \
  . > \"\$LOG\" 2>&1 &
echo \$! > \"\$PROD/build-\$COMMIT.pid\"
echo \"build started: pid=\$(cat \"\$PROD/build-\$COMMIT.pid\") log=\$LOG image=\$IMAGE version=\$APP_VERSION\"
"
```

轮询构建结果：

```bash
ssh -p "${SSH_PORT}" "${SSH_TARGET}" "
set -e
COMMIT='${COMMIT}'
PROD=/opt/sub2api-production
PID=\$(cat \"\$PROD/build-\$COMMIT.pid\")
while ps -p \"\$PID\" >/dev/null 2>&1; do
  tail -40 \"\$PROD/build-\$COMMIT.log\"
  sleep 10
done
tail -80 \"\$PROD/build-\$COMMIT.log\"
docker images \"sub2api-local:\$COMMIT\"
"
```

只有镜像存在后，才切换生产：

```bash
ssh -p "${SSH_PORT}" "${SSH_TARGET}" "
set -euo pipefail
COMMIT='${COMMIT}'
PROD=/opt/sub2api-production
IMAGE=sub2api-local:\$COMMIT

cd \"\$PROD\"
docker image inspect \"\$IMAGE\" >/dev/null
cp .env \".env.backup-\$(date +%Y%m%d-%H%M%S)-before-\$COMMIT\"

if grep -q '^SUB2API_IMAGE=' .env; then
  sed -i \"s#^SUB2API_IMAGE=.*#SUB2API_IMAGE=\$IMAGE#\" .env
else
  printf '\\nSUB2API_IMAGE=%s\\n' \"\$IMAGE\" >> .env
fi

docker compose -f docker-compose.local.yml -f docker-compose.override.yml up -d

for i in \$(seq 1 60); do
  status=\$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' sub2api)
  image=\$(docker inspect -f '{{.Config.Image}}' sub2api)
  echo \"health[\$i]=\$status image=\$image\"
  if [ \"\$status\" = healthy ] && [ \"\$image\" = \"\$IMAGE\" ]; then
    break
  fi
  sleep 2
done

test \"\$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' sub2api)\" = healthy
test \"\$(docker inspect -f '{{.Config.Image}}' sub2api)\" = \"\$IMAGE\"
curl -fsS http://127.0.0.1:18080/health
echo
docker ps --filter name=sub2api --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
"
```

## 部署后检查

确认运行镜像：

```bash
ssh -p "${SSH_PORT}" "${SSH_TARGET}" "
cd /opt/sub2api-production
grep '^SUB2API_IMAGE=' .env
docker ps --filter name=sub2api --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
"
```

确认应用版本不是 commit hash：

```bash
curl -fsS http://127.0.0.1:18080/health
# 或在管理后台查看版本信息：current_version 应为 backend/cmd/server/VERSION 中的值。
```

如果需要从服务器内部确认二进制版本：

```bash
ssh -p "${SSH_PORT}" "${SSH_TARGET}" "
docker exec sub2api /app/sub2api --version
"
```

输出应类似：

```text
Sub2API 0.1.156 (commit: 8782b30f, built: ...)
```

### Anthropic 分组 `identity_only` 发布检查

该模式只影响 OAuth/SetupToken + 非真实 Claude Code 客户端，不影响真实 Claude Code
和 Anthropic API-key 透传。部署后应确认：

- 管理后台创建/编辑 Anthropic 分组时，System 注入下拉框包含
  “仅必要身份（<200 tokens）”，内容审查下拉框不包含该选项；
- 保存后 API 返回
  `claude_oauth_system_prompt_policy=identity_only`，重新打开编辑框仍保持该值；
- `/v1/messages` 与 `/v1/messages/count_tokens` 的最终 system 数组都只有两块：
  动态 `x-anthropic-billing-header` 和固定 Agent SDK 身份；
- 最终 body 不包含标准/Fable 长扩展提示词；
- Mimicry Guard 为 `block` 时，两块模式仍能通过 guard；缺 billing 或身份任一块仍应阻断；
- 项目 tokenizer 对增量文本计数 `<200`（两块约 41，连同 system 迁移辅助文本
  当前实测约 53），前端生产 build 和相关后端单元测试均通过。

该模式通过 `179_expand_claude_oauth_system_prompt_policy.sql` 扩展既有 CHECK
constraint，不新增列。数据库约束无需随应用回滚；旧应用遇到该值会归一为
`inherit`，因此应用回滚前应先将使用该模式的分组改回 `inherit` 或 `disabled`，
避免回滚后语义变化。

2026-07-23 `a16045ee` 生产验证：

```text
GitHub Actions run: 29982187637 (custom-image success)
image digest: sha256:47209037118a083d3bb51a004899768e27d119f1be339a4262c4c3aba849094c
env backup: backups/.env.20260723-053306.before-a16045ee
rollback tag: sub2api-rollback:pre-a16045ee

migration 179: schema_migrations 已记录
constraint: inherit / enabled / identity_only / disabled
事务验证: group 14 写入 identity_only 成功，ROLLBACK 后恢复 enabled
部署脚本: 未自动切换客户配置
后续管理操作: group 14 于 13:36:12 启用 identity_only
生产二进制: 已包含 identity_only 与“仅必要身份”标签
真实流量: 13 条成功 usage / 7 个账号 / 覆盖 Fable、Haiku、Opus、Sonnet
缺 billing、身份或块数的 Mimicry Guard findings: 0
本机/公网 health: HTTP 200
首轮 token refresh: total=87, needs_refresh=3, refreshed=3, failed=0
```

### 供号商站点发布检查（本次含两个 DB 迁移）

与最近几次「无 schema 迁移」的部署不同，供号商站点带两个新迁移，**必须按序执行**：

```text
backend/migrations/180_add_provider_portal.sql
backend/migrations/181_provider_settlement_integrity.sql
```

180 给 `users` 加 `is_provider`、给 `accounts` 加 `provider_user_id`（partial index）
与 `provider_tier`、新建 `provider_settlements` 表。

181 依赖 180 建出的表，补财务完整性：`provider_settlements` 加 `last_usage_id`
（封账水位）与 `void_reason`、金额/周期 CHECK 约束、
`(provider_user_id, period_end) WHERE status='settled'` 的 partial unique index，
并新建明细快照表 `provider_settlement_items`。

两者全部是 `ADD COLUMN IF NOT EXISTS` / `CREATE TABLE IF NOT EXISTS` /
`DROP CONSTRAINT IF EXISTS` + `ADD CONSTRAINT`，幂等可重跑，不改动既有数据。
存量账号的 `provider_user_id` 为 NULL 即「管理员自有」。首次发布时
`provider_settlements` 为空表，181 的约束与唯一索引不会与存量数据冲突。

迁移由应用启动时自动执行，因此部署顺序不变；但**回滚需要注意**：旧镜像不认识
这些列，读取时会忽略它们，不会报错，所以应用层可以直接回滚。真正不可逆的是
`provider_settlements` 里已生成的结算单——回滚应用不会删除它们，重新上线后仍然有效。

部署后确认：

```bash
docker compose -f docker-compose.local.yml -f docker-compose.override.yml \
  logs --since=5m sub2api | grep -i migration
```

检查：

- 迁移 180 与 181 都已应用，无报错；
- `/api/v1/auth/me` 返回体多出 `is_provider` 字段（值为 false）；
- 供号商站点默认关闭：`provider_portal_enabled` 未配置时为 false，
  此时 `/api/v1/provider/*` 全部返回 404，注册接口返回 403
  `PROVIDER_PORTAL_DISABLED`；
- 管理端「系统设置 → 供货商」tab 可打开，档位显示 1-5 档种子值；
- 管理端「供号商对账」菜单可打开，含「对账结算」与「供号商管理」两个 tab，列表为空；
- **金额显示与管理端用量统计对得上**：随便挑一笔用量，比较管理端统计里的
  `total_cost` 与供号商页面显示的本期金额。两者应当只差在小数位数上，
  不应出现「$0.059 显示成 $0.1」这类虚高（曾因固定 1 位小数出过，见
  `FORK_PROJECT_MEMORY.md` 的金额精度小节）；
- 既有功能冒烟：能正常新建账号（该路径改成了账号与绑组同事务）、
  能正常登录、用量清理日志无异常。

开启站点前还需要在设置页完成：勾选至少一个 Anthropic 分组作为托管类型并填写对外文案、
设好默认托管类型与默认档位、确认结算时区与结算冷却期，然后才把
`provider_portal_enabled` 打开。调度优先级不需要配，上号时自动对齐所选分组；
设置页会把每个分组实际会用的值列出来，扫一眼确认即可。
邀请码在「兑换码」页选「供号商邀请码」类型生成。

#### 打开 `provider_portal_enabled` 之前需要知道的

结算并发、封账水位、清理保护、邀请码事务、明细快照、优先级对齐、关站阻断登录
这几项**都已解决**。开站前只剩一件事需要人工确认：

- **客户 API Key 分组有没有配渠道自定义定价**。结算按
  `SUM(usage_logs.total_cost)` 计算，而 `total_cost` 的 token 单价可能被渠道定价覆盖。
  注意渠道定价挂在**调用方 API Key 所属分组**上（`resolveChannelPricing` 取的是
  `apiKey.Group.ID`），不是挂在账号或托管分组上——所以决定因素是「谁拿 Key 打进来」，
  不是「号本身怎么配的」。若某个客户 Key 分组配了渠道定价，落到供号商账号上的请求
  就会按渠道价计入应付。没配过渠道定价的话，`total_cost` 就是标准价，无需处理。
  （图片/视频单价只在图片计费路径生效，纯文本请求不会走到。）

其余已知限制见 `FORK_PROJECT_MEMORY.md` 的 4.8.1，其中与运营相关的两条：

- 供号商自己不能改密码，由管理员在「用户管理 → 编辑」代改；
- 备份导出不含 `provider_user_id` / `provider_tier`，恢复后账号归属会丢。

2026-07-27 `2e4495d9` 生产验证：

```text
GitHub Actions run: 30230866070 (custom-image success)
image digest: sha256:7f2572c6e6a6ae60a67801506f8d002775e52264ad5d56d60985db70f50573c4
env backup: backups/.env.20260727-020223.before-2e4495d9
rollback tag: sub2api-rollback:pre-2e4495d9 (sha256:8ec3a024… = 8782b30f)

容器: 8 秒转 healthy
版本: Sub2API 0.1.156 (commit: 2e4495d9, built: 2026-07-27T01:55:37Z)
镜像 mutable/immutable ID: 一致
迁移: 180 与 181 均已记录，无报错
新对象: provider_settlements / provider_settlement_items 建成，
        last_usage_id + void_reason 列、period/amounts/voided CHECK 均就位
users.is_provider、accounts.provider_user_id / provider_tier 就位

号池未受影响（关键验证）：
  账号状态部署前后一致 active/t=76, active/f=2, error/f=15
  93 个账号中 provider_user_id 非空 = 0（全部仍为管理员自有）
  部署后 4 分钟真实流量: 35 请求 / 7 账号 / $6.16（claude-opus-4-8 等）
  启动窗口 panic / fatal: 0
  登录路径冒烟: 错误凭据返回 401 INVALID_CREDENTIALS（非 500）
  首绑 / grantee / PROVIDER_PORTAL 相关错误: 0
  本机 /health 与公网 https://lumos7.cc/health: 均 200

供号商站点默认关闭：
  settings 表无任何 provider_* 键，is_provider 用户数 = 0
  未认证访问 /api/v1/provider/* 返回 401（jwtAuth 在 ProviderOnly 之前，
  比文档预期的 404 更早拦截；已认证的非供号商才会走到 404/403）

首轮 token refresh: total=72, needs_refresh=1, refreshed=0, failed=1
```

该轮唯一 failed 是账号 `2523` 的 SOCKS 代理
`username/password authentication failed`，与 2026-07-25 那轮账号 `2512` 同类，
属存量代理凭证问题，与本次发布无关；该账号仍为 `active/schedulable`。

### Claude Chrome OAuth 401 自动恢复发布检查

这类修复不能只看容器 healthy。还应确认：

- `token_refresh.service_started` 正常出现；
- 下一轮 `token_refresh.cycle_completed` 没有异常失败增长；
- 提前 401 的 `claude_chrome` 账号能进入刷新，不再仅等待 `expires_at`；
- `refresh_token` 为 `invalid_grant` 或缺失时能回退已保存的 `session_key`；
- 成功后数据库、Redis 和进程内临时不可调度状态均被清除；
- SessionKey 回退返回
  `error.details.error_code=account_session_invalid` 时账号必须进入
  `error` 且 `schedulable=false`，不能继续循环临时不可调度；
- Cloudflare `Just a moment...`、代理/网络错误与普通 5xx 仍应保持可重试，
  不能因 403 状态码本身永久禁用账号；
- 普通 `claude_code` OAuth 行为不变。

查看启动和首轮刷新日志：

```bash
docker compose -f docker-compose.local.yml -f docker-compose.override.yml \
  logs --since=10m sub2api
```

2026-07-20 的 `6bf23b45` 部署首轮记录为：

```text
token_refresh.cycle_completed total=158 oauth=158 needs_refresh=3 refreshed=1 skipped=0 failed=2
```

该轮两个 failed 均为明确的 `account_session_invalid`，生产数据库已验证对应账号
`status=error`、`schedulable=false`；这是正确的永久隔离结果，不是刷新服务异常。

### Anthropic 内嵌 429 / Fable 模型级限流发布检查

这类改动不能只检查 HTTP 429 数量。还应确认：

- 外层 `error.message` 中的 JSON 可提取 `resetsAt`、`windows` 与
  `perModelLimit`；
- `perModelLimit=false` 的 5h/7d 超限写账号级 reset；
- `perModelLimit=true` 且代表窗口为
  `seven_day_overage_included`/`7d_oi` 时，只写
  `model_rate_limits["claude-fable-5"]`；
- 同一账号的 Opus/Haiku/Sonnet 不应因 Fable 额度耗尽进入运行时冷却；
- 日志中应出现 `anthropic_fable_window_model_rate_limited`，且对应响应不应出现
  `anthropic_429_runtime_no_reset_time`；
- 剩余 Fable 503 可能是真实 Fable 配额全部耗尽，应按模型拆分 503，不能仅看分组
  会话容量徽标。

2026-07-20 的 `50b68603` 生产验证：

```text
Fable body 被误写为账号运行时阻断：0
no available accounts（观察窗口）：Fable > 0，Opus = 0，Haiku = 0
```

### Fable `credits_required` 模型访问拒绝发布检查

这类 429 没有 reset，不能按普通窗口限流处理。部署后用一个明确没有 Fable
credits 的账号在“账号管理 → 测试连接”中测试 `claude-fable-5`，应确认：

- 返回体的 `error.details.error_code=credits_required` 被优先识别；
- 日志出现 `anthropic_fable_model_access_denied`；
- 账号仍保持 active/schedulable，且账号级 `rate_limit_reset_at` 不应被写入；
- Extra 只新增 Fable 模型拒绝：

```sql
SELECT id, name, status, schedulable, rate_limit_reset_at,
       extra->'model_access_denials' AS model_access_denials
FROM accounts
WHERE id = <ACCOUNT_ID>;
```

- 后续 Fable 请求不再选择该账号，但 Opus/Haiku/Sonnet 仍可选择；
- 当前命中 `credits_required` 的请求应产生账号切换，而不是直接把该 429 返回给用户；
- 为该账号补充 credits 后，再从同一测试入口验证 Fable；测试完整成功后
  `model_access_denials.claude-fable-5` 应被清除。

该功能无数据库迁移。回滚到旧应用时 Extra 字段会被忽略，无需修改数据库。

2026-07-23 `e4fad61d` 生产验证：

```text
GitHub Actions run: 29978772874 (custom-image success)
image digest: sha256:c6467283ce1c770beb28af63622cf19fbc90cc6858bfdec443df1544e169b539
env backup: backups/.env.20260723-041406.before-e4fad61d
rollback tag: sub2api-rollback:pre-e4fad61d

账号 2069 (SetupToken): credits_required / out_of_credits
账号状态: active / schedulable
denial: model_access_denials.claude-fable-5
sticky: 已清除
failover: 2069 -> 2624 (Max)
最终响应: HTTP 200 / 4264ms
denial 后 Fable 再选中 2069: 0
部署后首轮 token refresh: total=87, needs_refresh=1, refreshed=1, failed=0
```

### Anthropic 429 有界 failover 与固定短冷却发布检查

当前生产规则：

- 普通错误仍使用 `max_account_switches=10`；
- 仅 Anthropic 429 可使用 `max_account_switches_anthropic_429=20`；
- 扩大搜索同时受 `anthropic_429_failover_timeout_seconds=20` 约束；
- 无 reset 的 opaque 429 固定冷却 1 分钟，不允许根据重复次数扩大到 10 分钟；
- 读取 `a7ceb185` 已写入的旧 opaque 长冷却时，按
  `triggered_at + 1 分钟` 截断，使热修部署后立即释放账号；
- Fable 候选优先使用 15 分钟内具有
  `passive_usage_7d_oi_utilization < 1` 且 reset 在未来的账号，未知账号继续作为兜底。

部署后应检查以下结构化日志：

```text
gateway.failover_switch_account
gateway.failover_anthropic_429_time_budget_exhausted
anthropic_429_runtime_cooldown_set
```

`anthropic_429_runtime_cooldown_set` 的 `cooldown` 必须为 `1m0s`，且不应再包含
`streak`。观察窗口应同时按最终 HTTP 状态和上游 switch 数统计，不能把一次请求中
的多个上游 429 当成多个用户错误。

`a7ceb185` 初始低流量观察曾为：

```text
Fable HTTP 200: 11
最终 Fable 429 / 503: 0 / 0
Fable usage 成功记录: 5（3 个账号）
Anthropic 429 最大 switch: 13
20 秒预算触发: 1
opaque 429 自适应冷却: 19（其中 streak=2 为 4）
```

但持续流量下，分组 14 出现 13 个真实账号级限流与 9 个 opaque 长冷却重叠，
最近两小时产生 283 个本地 `no available accounts` 503（上游直接 503 为 0）。
因此递增冷却被判定为不安全并由 `0cc87cdd` 回退。

2026-07-22 `0cc87cdd` 热修初始观察：

```text
最终 HTTP 429 / 503: 0 / 0
opaque 429: 8
非 1 分钟 cooldown: 0
streak 字段: 0
```

### 新 Claude 官方模型上线检查

原生 Anthropic 账号（OAuth/SetupToken/API Key）在 `model_mapping` 为空时对未知模型
是透传的，因此"能不能调用"通常不需要发版。真正需要发版的是展示、定价与
Antigravity/Bedrock 这类闭集通道。上线后应确认：

- `/v1/models` 在分组内所有账号都没配 `model_mapping` 时返回新模型；
  一旦任一账号配置了非空 mapping，该账号即退化为白名单，必须显式补条目或通配；
- 新模型的计费不落到错误档位。重点是 `matchByModelFamily`：`claude-opus-5`、
  `claude-opus-4-8` 都不含 `claude-opus-4-<minor>` 模式串，缺少精确定价条目时会掉进
  `opus-4` 兜底，而该分支遍历 map 取首个命中，会在 `$5/$25` 与 `$15/$75` 之间随机跳。
  新模型必须同时补 `resources/model-pricing` 条目和 families 表条目；
- 远程 LiteLLM 定价表尚未收录新模型时，靠 `pricing.fallback_file`
  （默认 `./resources/model-pricing/model_prices_and_context_window.json`）的
  `mergeFallbackPricingData` 补齐。容器内应能查到该 key：

```bash
docker exec sub2api grep -c '"<新模型 ID>"' \
  /app/resources/model-pricing/model_prices_and_context_window.json
```

- mimic profile 归族按 `haiku`/`fable`/`sonnet`/其余归 `opus` 的子串匹配。模型名含这
  四类词时自动继承正确的 beta 头、`max_tokens` 与 thinking 默认值；全新族名会落
  `opus` 档，需要单独评估；
- 1M 上下文类模型不一定需要放行 `context-1m-2025-08-07`。Opus 5 起 1M 是默认行为、
  不需要 beta header，因此现有"只对 `claude-sonnet-5*` 放行、其余过滤"的策略对它
  是安全的，不要为了"看起来一致"而扩大白名单；
- Antigravity/Bedrock 是闭集，不在映射表里的模型会被调度器直接过滤成
  `no available accounts supporting model`。Antigravity 侧是否已上线新模型，必须先用
  管理后台"账号管理 → 同步上游模型"确认，不要凭官方发布公告盲加。

2026-07-25 `8782b30f`（`claude-opus-5`）生产验证：

```text
GitHub Actions run: 30137006332 (custom-image success, 4m54s)
image digest: sha256:8ec3a0244e68a12182681abd395765773500f7a18ecbd7f2371eda3e565fdb13
env backup: backups/.env.20260725-010617.before-8782b30f
rollback tag: sub2api-rollback:pre-8782b30f

容器: 8 秒转 healthy
版本: Sub2API 0.1.156 (commit: 8782b30f, built: 2026-07-25T00:46:41Z)
本机 /health: {"status":"ok"}
公网 https://lumos7.cc/health: HTTP 200
镜像 mutable/immutable ID: 一致
启动窗口 panic / error 级日志: 0
容器内定价兜底表含 claude-opus-5: 1（远程 LiteLLM 表尚未收录，走 merge 补齐）
部署后首轮 token refresh: total=67, needs_refresh=1, refreshed=0, failed=1
```

该轮唯一 failed 是账号 `2512` 的 SOCKS 代理
`username/password authentication failed`，属存量代理凭证问题，与本次发布无关。

未覆盖项：`/v1/models` 需要有效 API Key 才能验证，本轮未做端到端确认；Antigravity
侧 Opus 5 支持情况未探测，因此未加入 `DefaultAntigravityModelMapping`。

## 回滚

查看已有本地镜像：

```bash
ssh -p "${SSH_PORT}" "${SSH_TARGET}" "
docker images sub2api-local --format 'table {{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.CreatedSince}}\t{{.Size}}'
"
```

选择上一个健康 tag，例如 `aae76cfb`，切回：

```bash
ROLLBACK_COMMIT=aae76cfb

ssh -p "${SSH_PORT}" "${SSH_TARGET}" "
set -euo pipefail
PROD=/opt/sub2api-production
IMAGE=sub2api-local:${ROLLBACK_COMMIT}
cd \"\$PROD\"
docker image inspect \"\$IMAGE\" >/dev/null
cp .env \".env.backup-\$(date +%Y%m%d-%H%M%S)-rollback-from-current\"
sed -i \"s#^SUB2API_IMAGE=.*#SUB2API_IMAGE=\$IMAGE#\" .env
docker compose -f docker-compose.local.yml -f docker-compose.override.yml up -d
curl -fsS http://127.0.0.1:18080/health
"
```

## 常见问题

### 后台一直提示有官方新版本

优先检查：

```bash
docker exec sub2api /app/sub2api --version
```

如果版本显示成 `8885fdf5` 这类 commit，说明构建时误传了：

```bash
--build-arg VERSION="${COMMIT}"
```

应重新构建为：

```bash
--build-arg VERSION="$(tr -d '\r\n' < backend/cmd/server/VERSION)"
--build-arg COMMIT="$(git rev-parse --short=8 HEAD)"
```

### `pnpm install --frozen-lockfile` 报 overrides 不匹配

说明构建使用了过新的 pnpm。标准构建必须使用固定 `pnpm@9` 的根 `Dockerfile` 或当前已对齐的 `deploy/Dockerfile`。

### 构建 SSH 断开

使用本文档中的 `nohup docker build ... > build-<commit>.log 2>&1 &`。构建失败不会切换 `.env`，旧容器继续运行。
