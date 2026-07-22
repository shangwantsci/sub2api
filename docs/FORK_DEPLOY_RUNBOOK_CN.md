# sub2api 二开提交、推送、部署流程

本文档固定二开分支的日常发布流程，避免每次手工部署时遗漏测试、版本号或服务器切换步骤。

> 最近验证：2026-07-22 已按本流程部署 `0.1.156-0cc87cdd`，GitHub Actions
> run `29912599037`，本机与公网健康检查均通过。

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
COMMIT=0cc87cdd  # 替换为本次 8 位 commit
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

2026-07-22 最近一次验证：

```text
immutable image: ghcr.io/shangwantsci/sub2api:0.1.156-0cc87cdd
digest:          sha256:475a3f31f4eb75c5f24bd9135d7486c62e950add756f648101aed63d5cf86381
env backup:      backups/.env.20260722-104114.before-0cc87cdd
rollback tag:    sub2api-rollback:pre-0cc87cdd
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
Sub2API 0.1.156 (commit: 0cc87cdd, built: ...)
```

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
