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

> 最近验证：2026-08-05 已按本流程部署 `0.1.156-3648784f`（修三个模型 400 +
> 响应侧剥离注入的 thinking block），GitHub Actions run `30978595796`。
> **本轮无数据库迁移**，回滚只需切回镜像。同日同步更新了客户 yihang。
>
> ```text
> immutable image: ghcr.io/shangwantsci/sub2api:0.1.156-3648784f
> digest:          sha256:a42ba9eba434441b6b1285e231563e59c7cd9a454a0d4a9a6932db927ab61e43
> env backup:      backups/.env.20260805-054002.before-3648784f
> rollback tag:    sub2api-rollback:pre-3648784f
>
> 客户 yihang（101.47.39.33）:
> image:           ghcr.io/shangwantsci/sub2api-customer-yihang:0.1.156-3648784f
> digest:          sha256:e0b68d9274ba453f0c3c343f6396e5482f2b0ab17379c9848c585119f3ee0d0a
> env backup:      backups/.env.20260805-134407.before-3648784f
> rollback tag:    sub2api-customer-rollback:pre-3648784f
> ```
>
> 两台均 8 秒转 healthy，panic/fatal 为 0。客户侧 `deployment_license.renewed`
> 正常（instance `inst_j2BIZ4R9YdyFKQHiC1XcJaaa`，租约 +24h），
> `calibration_profile_applied` 仍为 cli_version 2.1.221，授权未受升级影响。
>
> **踩坑：先发的 `50407487` 上线后剥离完全不生效（实测 16/22）。**
> `normalizeClaudeOAuthRequestBody` 有三个调用点，那一版只在
> `gateway_claude_oauth_body.go` 打了注入标记，而原生 `/v1/messages` 实际走
> `gateway_forward.go`，标记从未写入 gin.Context。`3648784f` 抽出
> `markInjectedThinkingIfAdded` 让两条路径共用，并加了一条路径覆盖断言测试
> （新增转发路径漏调用会直接失败）。改动跨路径的请求改写时，务必先确认
> 目标函数的全部调用点，单测过了不等于线上路径覆盖到了。
>
> 验收脚本 `verify_fix.py` 22/22：10 个模型小 max_tokens 全部 200；
> opus-5/sonnet-5 未请求 thinking 时首块为 text（拼接提取从 `"\n3973"` 变
> `"3973"`，即客户测评误判的根因）；显式请求 thinking 时仍原样保留；
> 流式 index 从 0 连续；多轮回传 3/3。
>
> 上一轮为 2026-08-02 的 `0.1.156-8035f77a`（档位标签按实际参数反推、
> 开放自定义档编辑、修 priority 对齐口径、管理端改分组提示），
> GitHub Actions run `30747110860`。**该轮无数据库迁移**，回滚只需切回镜像。
>
> ```text
> immutable image: ghcr.io/shangwantsci/sub2api:0.1.156-8035f77a
> digest:          sha256:828de409e41a3b5394357f48d7759802ece2ebfdcaeca06e31ba6bfac79c66ac
> env backup:      backups/.env.20260802-121100.before-8035f77a
> rollback tag:    sub2api-rollback:pre-8035f77a
> ```
>
> 容器 6 秒转 healthy，panic/fatal 为 0，新路由
> `GET /admin/groups/scheduling-priorities` 无凭证返回 401。
>
> **生产数据验证了这轮的核心修复**：两个被管理员改过参数的账号
> （供号商 45，存的档位分别是 3 和 5，实际参数一个是并发 1000、
> 一个是并发1/会话3/rpm80）标签已从撒谎的「3 档」「5 档」变成「自定义」；
> 参数确实标准的账号仍正确显示原档位名。
>
> 上一轮为同日的 `0.1.156-047f4fb9`（供号商账号管理面板：批量上号、二次编辑、
> 账号邮箱与额度用量），GitHub Actions run `30744920471`：
>
> ```text
> immutable image: ghcr.io/shangwantsci/sub2api:0.1.156-047f4fb9
> digest:          sha256:23502d7bc5abffbe08aeb555a468c7ce5112a10ba41f69929f78325aeeb554ae
> env backup:      backups/.env.20260802-110822.before-047f4fb9
> rollback tag:    sub2api-rollback:pre-047f4fb9
> ```
>
> 那轮三个新路由无凭证均返回 401（**不是 404** —— 这条区分很重要：
> 404 说明路由压根没注册上）：`GET /provider/accounts/:id/usage`、
> `PATCH /provider/accounts/:id`、`POST /provider/accounts/:id/auth-url`。
> 浏览器实拉 `/provider/accounts` 渲染正常、无白屏。
>
> 上一轮为 2026-08-01 的 `0.1.156-a6948086`，GitHub Actions
> run `30707022376`。那轮共三次部署：功能主体 `4d3ff165`（供号商上号新增
> 「由平台提供 IP」、自带代理改为粘贴连接串，并修掉数处会静默算错钱的问题，
> **含迁移 182**）→ `aba0a308`（**迁移 183**，回填历史供号商代理归属）→
> `a6948086`（热修 `/provider/onboard` 白屏）。
>
> **前两个版本的上号页是白的，回滚不要停在它们身上**，要回到本轮之前请用
> `sub2api-rollback:pre-4d3ff165`。白屏原因见 `FORK_PROJECT_MEMORY.md` 4.6。
>
> 上一轮为 2026-07-29 的 `0.1.156-0832ab07`（run `30416049073`）。

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
- **客户授权镜像必须使用独立 GHCR package**：
  `sub2api-customer-<customer>`；不能只用共用 package 的不同 tag 做隔离。
- **客户服务器不 clone 仓库、不本地 build**：只 pull 客户专属镜像，并使用
  `docker-compose.customer.yml` overlay。

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

## 客户自有服务器部署流程（授权镜像，独立于本机生产）

> 本节只用于客户专属镜像。不要给当前 community 生产构建传非 `community`
> `customer_id`，也不要把客户 package 写入 `/opt/sub2api-production/.env`。
>
> 完整小白手册：`docs/CUSTOMER_DEPLOYMENT_LICENSE_CN.md`。

### 授权中心线上实况（2026-08-03 部署完成）

授权中心与生产号池**同机**运行在 `lumos7.cc` 那台（4C/3.8G，Ubuntu 24.04），
互不影响：独立 compose project、独立容器、独立 Postgres。

```text
部署目录    /opt/sub2api-license
容器        sub2api-license-server（127.0.0.1:3900）
            sub2api-license-postgres-1（postgres:16-alpine，独立于生产的 postgres:18）
对外域名    https://license.lumos7.cc      Caddy 站点已配置并签发证书
管理台      https://license.lumos7.cc/console
签名公钥    9l9Pi3CnVBW8IuTS/OqjIOvdkCXMgZmY9XYvnX4qWqI
私钥        /opt/sub2api-license/signing.key（属主 65532:65532，600）
备份        /opt/sub2api-license-backup-<时间戳>/（.env、signing.key、override）
```

`docker-compose.override.yml` 把 license-server 同时接入 `openstaryu-internal`
网络，Caddy 才能按容器名反代；该文件是本机专属、不入库。

**服务器上没有 GitHub 私有仓库凭证**，`git pull` 会失败。更新代码用 git bundle：

```bash
# 本地
cd ../sub2api-license-server && git bundle create /tmp/license.bundle main
scp -P 9646 /tmp/license.bundle root@<服务器>:/tmp/

# 服务器
cd /opt/sub2api-license
cp -p .env signing.key docker-compose.override.yml /opt/sub2api-license-backup-$(date +%Y%m%d-%H%M%S)/
git pull /tmp/license.bundle main
docker compose build && docker compose up -d
rm -f /tmp/license.bundle
```

`signing.key`、`.env`、`docker-compose.override.yml` 都不在 git 里，更新不会覆盖它们；
Postgres 数据在 volume 中，重建 license-server 容器不影响已激活实例。

**升级后必须核对公钥没变**：`docker compose logs license-server | grep public_key`
的值要与 GitHub 变量 `DEPLOYMENT_LICENSE_PUBLIC_KEY` 一致，否则所有客户镜像会拒绝
新签发的 lease。

### 前置检查

| 项 | 状态 |
|---|---|
| 授权中心 HTTPS 正常 | 已完成，`https://license.lumos7.cc/health` 返回 ok |
| 仓库变量 `DEPLOYMENT_LICENSE_PUBLIC_KEY` | 已设置，且与线上私钥匹配 |
| 仓库密钥 `DEPLOYMENT_LICENSE_SERVER_URL` / `_ADMIN_TOKEN` | 已设置（标定 profile 自动下发用） |
| 客户 ID 已确定 | 每次交付前确定，例如 `customer-a` |
| 已为该客户创建一次性 activation code | 在管理台「新建客户并生成激活码」 |
| 已确认 LGPL 商业交付边界 | **未解决，交付前必须确认** |

### 构建客户专属 package

```bash
APP_VERSION="$(tr -d '\r\n' < backend/cmd/server/VERSION)"
CUSTOMER_ID=customer-a

gh workflow run release.yml \
  --repo shangwantsci/sub2api \
  --ref custom/prod \
  -f tag="v${APP_VERSION}" \
  -f custom_image_only=true \
  -f source_ref=custom/prod \
  -f customer_id="${CUSTOMER_ID}" \
  -f simple_release=true

gh run watch --repo shangwantsci/sub2api
```

预期镜像：

```text
ghcr.io/shangwantsci/sub2api-customer-customer-a:<VERSION>
ghcr.io/shangwantsci/sub2api-customer-customer-a:<VERSION>-<COMMIT>
```

构建前 workflow 会检查：

- customer ID 格式；
- 非 community 客户必须存在 `DEPLOYMENT_LICENSE_PUBLIC_KEY`；
- 二进制注入 `ManagedCustomerID` 和 build-time 公钥；
- OCI label 注入 customer/build 水印。

**构建后必须手动把 package 改成 Private——新 package 默认是 public。**
主仓是 public fork，Dockerfile 的 `org.opencontainers.image.source` 指向上游公开仓库，
GHCR 据此继承了公开可见性。GitHub **没有**修改 package 可见性的 REST API，只能在网页操作：

```text
https://github.com/users/shangwantsci/packages/container/sub2api-customer-<客户>/settings
→ Danger Zone → Change visibility → Private
```

改完复验，匿名 pull 应当失败：

```bash
docker logout ghcr.io 2>/dev/null
docker pull ghcr.io/shangwantsci/sub2api-customer-<客户>:<tag>
```

2026-08-04 首次为 yihang 构建时踩了这个坑：镜像可匿名拉取。授权保护本身不受影响
（无有效 lease 跑不起来），但编译产物和客户水印会外泄。生产镜像
`ghcr.io/shangwantsci/sub2api` 同样是 public，改私有会导致服务器 pull 需要认证，
按需决定。

### 准备客户交付目录

客户服务器**不 clone Git 仓库**。只交付：

```text
docker-compose.local.yml
docker-compose.customer.yml
.env
```

`.env` 必须包含：

```text
SUB2API_CUSTOMER_IMAGE=ghcr.io/shangwantsci/sub2api-customer-customer-a:<VERSION>
DEPLOYMENT_LICENSE_SERVER_URL=https://<你的授权域名>
DEPLOYMENT_LICENSE_ACTIVATION_CODE=<一次性激活码>
DEPLOYMENT_LICENSE_IMAGE_DIGEST=sha256:<实际镜像 digest>
```

Compose 检查与启动：

```bash
docker compose \
  -f docker-compose.local.yml \
  -f docker-compose.customer.yml \
  config

docker compose \
  -f docker-compose.local.yml \
  -f docker-compose.customer.yml \
  pull

docker compose \
  -f docker-compose.local.yml \
  -f docker-compose.customer.yml \
  up -d
```

客户 overlay 会只读挂载：

```text
/etc/machine-id -> /host/etc/machine-id
/sys/class/dmi/id -> /host/sys/class/dmi/id
```

非 community release 若读不到宿主机身份信号会拒绝启动；不要用手填假 machine ID 绕过。

### 首次激活检查

```bash
docker compose \
  -f docker-compose.local.yml \
  -f docker-compose.customer.yml \
  logs --since=10m sub2api
```

必须确认：

```text
deployment_license.renewed
status = active
customer_id / managed_customer_id 正确
host_signal_count > 0
expires_at 约为当前时间 + 24h
```

管理员接口：

```text
GET  /api/v1/admin/deployment-license/status
POST /api/v1/admin/deployment-license/refresh
```

首次成功后：

1. 清空 `.env` 的 `DEPLOYMENT_LICENSE_ACTIVATION_CODE`；
2. 只重建 sub2api；
3. 再次确认可凭 `data/license/instance-identity.json` 续租；
4. 备份 `.env` 与授权中心数据库。

### 客户镜像回滚

只能在同一客户 package 内回滚：

```text
ghcr.io/shangwantsci/sub2api-customer-customer-a:<VERSION>-<OLD_COMMIT>
```

客户镜像强制禁用应用内在线更新/二进制回滚；回滚必须由运营方切 Docker tag。

### 授权故障判断

```text
active       正常
grace        lease 已过期但仍在 6h 宽限；网关继续、管理写操作只读
expired      超过 lease + 6h；网关 503、管理面保留
unlicensed   首次激活未完成
revoked      后台明确吊销
limit_exceeded 账号/用户数超出授权上限；先删除超额资源
```

`/health=200` 只说明进程存活，不能替代 deployment license status。

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

其余已知限制见 `FORK_PROJECT_MEMORY.md` 的 4.14.2，其中与运营相关的两条：

- 供号商自己不能改密码，由管理员在「用户管理 → 编辑」代改；
- 备份导出不含 `provider_user_id` / `provider_tier`，恢复后账号归属会丢。

### 供号商代理自动分配发布检查（本轮含迁移 182 / 183）

两个迁移都由应用启动时自动执行，部署顺序不变：

```text
backend/migrations/182_add_proxy_auto_assign.sql          proxies 加 provider_user_id / auto_assignable
backend/migrations/183_backfill_provider_proxy_owner.sql  回填 182 之前建的供号商代理归属
```

182 是 `ADD COLUMN IF NOT EXISTS`，幂等。183 是数据回填，条件里带
`provider_user_id IS NULL`，重跑不会二次改写；它只回填「名字前缀能解析出、且该用户
确实是 `is_provider` 且非管理员」的记录，管理员自己起名叫 `provider-1-xxx` 的代理
不会被误标成私有。

**回滚需要注意**：应用可以直接回滚（旧镜像不认识这两列，读取时忽略）。但 183 回填的
归属不会被撤销 —— 那本来就是修正，留着是对的。

部署后确认：

```sql
-- 两个迁移都已记录
SELECT filename FROM schema_migrations WHERE filename LIKE '18%';
-- 新列与默认值：auto_assignable 必须是 NOT NULL DEFAULT false
SELECT column_name, data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_name='proxies' AND column_name IN ('provider_user_id','auto_assignable');
-- 安全默认：升级后不该有任何代理进入自动分配池
SELECT count(*) FROM proxies WHERE auto_assignable;
-- 回填结果：带 provider- 前缀的代理归属都应非空
SELECT id, name, provider_user_id, auto_assignable
FROM proxies WHERE deleted_at IS NULL AND name ~ '^provider-\d+-' ORDER BY id;
```

还应确认：

- 管理端代理列表出现「自动分配」列，供号商私有代理那一行的开关是**置灰**的；
- 系统设置 → 供货商 tab 出现「供号商可用的 IP 来源」下拉与「单个平台 IP 最多绑定
  账号数」输入（种子值 2）；
- 供号商上号页的代理区块变成来源单选 + 粘贴框；策略为「仅平台 IP」且池子为空时，
  页面只显示「当前无法上号」，**不应同时露出自带代理的输入框**；
- 手工用量清理对覆盖未结算区间的时间范围返回
  `USAGE_CLEANUP_RANGE_COVERS_UNSETTLED`，且删除条件已排除供号商账号的行。

2026-08-01 `a6948086` 热修（`/provider/onboard` 白屏）：

```text
GitHub Actions run: 30707022376 (custom-image success)
容器: 8 秒转 healthy
版本: Sub2API 0.1.156 (commit: a6948086, built: 2026-08-01T15:58:03Z)
本机 /health 与公网 https://lumos7.cc/health: 均 200
无新迁移

事故：4d3ff165 上线后 /provider/onboard 整页白屏，aba0a308 未修复。
后端全程正常 —— chunk 200、options 接口 200、零 ERROR 日志，纯前端渲染崩溃。
根因是 i18n 文案里的 socks5://用户名:密码@1.2.3.4:1080：@ 在 vue-i18n 里是
linked message 语法，编译失败让整页渲染不出来。改成 {'@'} 字面量插值。

产物验证: 生产二进制里已是 {'@'} 转义，裸 @ 写法计数为 0

新增两道防线（见 FORK_PROJECT_MEMORY.md 4.6）：
  src/i18n/__tests__/messageCompilation.spec.ts    遍历 zh/en 全部文案并抓 console 编译错误
  src/views/provider/__tests__/ProviderOnboard.mount.spec.ts   挂载整页，覆盖 7 种策略×库存组合
  vitest.config.ts 补上 __INTLIFY_JIT_COMPILATION__，与 vite.config.ts 对齐
```

2026-07-31 `aba0a308` 生产验证：

```text
GitHub Actions run: 30651632725 (custom-image success)；功能主体 30650654467
image digest: sha256:2eef1b6d78340aeb0231023faf2af0b5df8d1380f587a9cffc6cb2ad28a5eb0f
env backup: backups/.env.20260731-173758.before-aba0a308
            backups/.env.20260731-172623.before-4d3ff165（功能主体那轮）
rollback tag: sub2api-rollback:pre-aba0a308 (= 4d3ff165)
              sub2api-rollback:pre-4d3ff165 (= 0832ab07，回到本轮之前用这个)

容器: 两轮均 8 秒转 healthy
版本: Sub2API 0.1.156 (commit: aba0a308, built: 2026-07-31T17:34:50Z)
镜像 mutable/immutable ID: 一致
迁移: 180/181/182/183 均已记录，无报错

关键验证：
  proxies 新列: auto_assignable boolean NOT NULL DEFAULT false / provider_user_id bigint NULL
  新索引: idx_proxies_provider_user_id、idx_proxies_auto_assign_pool
  安全默认: auto_assignable=true 的代理数 0
  回填结果: 代理 74 -> provider 44、代理 76 -> provider 45
  归属分布: 41 平台自有 + 2 供号商私有，41 条平台代理未被误标
  新的分账号明细 SQL 在真实数据上跑通:
    账号 2871 -> 38 请求 / $29.3089 / 水位 873260（与结算单 #1 完全吻合）
    账号 2873 -> 46 请求 / $23.2679，两者 late_rows 均为 0
  清理守卫: 近 30 天 258003 行可清理 / 84 行因属于供号商账号受保护

号池未受影响：
  账号总数 63，供号商账号 1（账号 2873，归属 45）
  部署后 5 分钟真实流量: 6 条 usage / 4 个账号
  启动窗口 panic / fatal: 0
  本机 /health 与公网 https://lumos7.cc/health: 均 200

注意：本轮发现文档此前记载的「供号商站点未开启」已过期 —— 站点实际已开启，
有 4 个 is_provider 用户、1 个供号商账号在跑，并已产生 1 张结算单（$29.3089）。
迁移 182 当初没做回填正是基于那条过期记载，才需要补 183。
```

2026-07-29 `0832ab07` 生产验证（金额显示自适应位数 + 授权方式改名，纯前端）：

```text
GitHub Actions run: 30416049073 (custom-image success, 4m26s)
env backup: backups/.env.20260729-021451.before-0832ab07
rollback tag: sub2api-rollback:pre-0832ab07

容器: 8 秒转 healthy
版本: Sub2API 0.1.156 (commit: 0832ab07, built: 2026-07-29T02:10:39Z)
镜像 mutable/immutable ID: 一致
无新迁移（本轮只改前端展示与文案）

修复的问题：供号商页面把 total_cost=0.05944 显示成 $0.1（固定 1 位小数），
而管理端用量统计同一笔显示 $0.059，被误判为「给供号商多算钱」。
底层从未算错：数据库该笔仍是 0.0594400000，结算与 CSV 一直用全精度。
改为自适应位数后显示 $0.06。

号池未受影响：
  部署后 10 分钟内被删账号: 0
  近 3 分钟真实流量: 3 请求 / 3 账号
  启动窗口 panic / fatal: 0
  登录冒烟: 错误凭据返回 401
  本机 /health 与公网 https://lumos7.cc/health: 均 200

注：账号总数较 07-27 少 19 个（93 → 74），删除发生在 07-26 14:00 至
07-28 11:00 之间的多个时段，与本次部署无关，属期间的运营操作。
```

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
