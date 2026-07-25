# 公司部署 runbook（custom/company）

> 最后更新：2026-07-25
> **本文件只存在于 `custom/company` 分支。** 如果你在 `custom/prod` 上看到它，
> 说明发生了错误的合并，应当撤销。
>
> 动手前先确认分支：
>
> ```bash
> git branch --show-current    # 必须是 custom/company
> ```
>
> 生产线（`custom/prod`）的运维流程见 `FORK_DEPLOY_RUNBOOK_CN.md`，
> 两者流程不同，不要混用。分支矩阵见 `FORK_PROJECT_MEMORY.md` 第 2.3 节。

## 1. 与生产线的隔离

这是本分支最需要小心的部分。已经落地的隔离措施：

| 隔离点 | 措施 |
|---|---|
| 镜像 tag | 公司镜像叫 `sub2api-company:<VER>-<commit>`，不进 GHCR 命名空间 |
| registry | `company-image.yml` 全程 `push: false`，不推送任何 registry |
| 构建守卫 | `company-image.yml` 拒绝构建 `main` / `custom/prod` |
| workflow 位置 | `company-image.yml` 不放默认分支，与 `release.yml` 无牵连 |
| 定时标定 | schedule 硬编码 `custom/prod`，且只从 `main` 调度 |
| 服务器凭据 | 公司机器没有 GHCR 凭据，无法拉取生产镜像 |
| 文档 | 记忆文档与生产 runbook 两分支逐字一致，公司细节只在本文件 |

**仍然要靠人守住的两条：**

1. **不要用 `release.yml` 构建 `custom/company`。** `custom_image_only` 的可变
   tag 只由 `backend/cmd/server/VERSION` 决定，两分支都是 `0.1.156`。这样做会把
   生产正在拉取的 `ghcr.io/shangwantsci/sub2api:0.1.156` 覆盖成公司镜像，生产
   下次 `docker compose pull` 就会静默换代码。`company-image.yml` 里的守卫只能
   拦住反方向，拦不住这个方向 —— 因为守卫无法写进 `release.yml`（那个文件在
   `main` 上，动它就会威胁定时标定）。
2. **永远不要把 `custom/company` 合回 `custom/prod`。** 同步是单向的：
   `custom/prod` → `custom/company`。

### 从生产线同步更新

```bash
git switch custom/company
git merge custom/prod
```

合并后必检：

```bash
cd frontend
npm run test:run -- \
  src/components/account/__tests__/OAuthAuthorizationFlow.spec.ts \
  src/components/admin/account/__tests__/AccountActionMenu.spark_shadow.spec.ts
```

这两个 spec 是**反向断言**（断言入口不存在）。如果它们失败，说明合并把删掉的
入口带回来了，必须重新删除后再构建。这是本分支最重要的回归护栏。

`backend/cmd/server/VERSION` 以 `custom/prod` 为准，冲突时一律取生产侧。

## 2. 本分支删除了什么

### 批量导入账号（Anthropic SessionKey 批量导入）

整条链路移除：

```text
backend/internal/handler/admin/account_anthropic_session_import.go   已删除
POST   /api/v1/admin/accounts/import/anthropic-session               已删除
GET    /api/v1/admin/accounts/import/anthropic-session/:id           已删除
POST   /api/v1/admin/accounts/import/anthropic-session/:id/cancel    已删除
```

前端同时移除账号页工具菜单项、工具栏「批量导入」按钮、`CreateAccountModal` 的
`initialMode='anthropic-session-import'` 模式与任务进度面板、`accounts.ts` 的三个
API、`types/index.ts` 的四个接口，以及 zh/en 的 13 个 i18n key。

### 通过 Chrome OAuth 授权添加账号

只删入口，不动底层：

```text
POST /api/v1/admin/accounts/chrome-cookie-auth        已删除
POST /api/v1/admin/accounts/:id/refresh-cookie-auth   已删除
```

前端移除创建/重授权弹窗的「Chrome Cookie 授权」单选、`OAuthAuthorizationFlow` 的
`cookie_chrome` 输入方式与 `chrome-cookie-auth` 事件、账号操作菜单的
「使用 SessionKey 重新授权」。

### 刻意保留的后端逻辑（不要顺手清理）

记忆文档 4.7 与 4.10 描述的后端 `claude_chrome` 运行时逻辑**全部保留**：
`token_refresher.go` 的 SessionKey 回退、`oauth_refresh_api.go` 的 owned lock、
`account_repo.go` 的 CAS、`gateway_upstream_request.go` 的
`ensureClaudeChromeOAuthBeta`、`claude_oauth_error.go` 的错误分层。

原因是这些代码与 `claude_code` 主链路深度交织，剥离会威胁主链路。保留后存量
`claude_chrome` 账号仍能正常刷新和调度，只是不能再新建或手工重授权。

同样保留：单个 cookie 授权（`/cookie-auth`、`/setup-token-cookie-auth`）、JSON
数据导入、Codex session 导入、Grok SSO 导入、`POST /accounts/batch` 批量创建。

## 3. 已知且刻意接受的残留通路

删除的是「专用入口」，不是「批量建号能力」。以下三条 2026-07-25 评审后决定保留，
**不是遗漏，不要当 bug 修**：

1. **普通 Cookie 自动授权仍支持多行。** `CreateAccountModal` 的 `allow-multiple`
   对 anthropic 仍为 true，`handleCookieAuth` 按换行 split 后逐个调 `/cookie-auth`
   建号，UI 提示「将批量创建 N 个账号」。等价于批量导入的简化版（无异步任务、
   进度、查重、自动命名、自动分代理）。保留原因：维护者自己要用。
2. **JSON 数据导入**（`POST /accounts/data`）可一次导入多个账号，`credentials`
   是自由字段。
3. **Chrome 账号仍可手工构造。** `POST /accounts`、`POST /accounts/batch`、数据
   导入的 `credentials` 都不校验 `oauth_client`，写入
   `{oauth_client: "claude_chrome", session_key: "..."}` 后，保留下来的后台刷新器
   会自动完成 Chrome SessionKey 授权。点界面做不到，curl 可以。

三条都要求**管理员**权限（`/admin` 路由组挂 `AdminAuthMiddleware`），普通用户
无法触达。**安全边界靠的是管理员账号只有你一个人持有，不是入口删除。**

已确认安全：`POST /accounts/batch-update-credentials` 的 `field` 被
`binding:"oneof=account_uuid org_uuid intercept_warmup_requests"` 限死，无法用它
改 `oauth_client` 或 `session_key`。

## 4. 首次 bootstrap（只做一次）

`company-image.yml` 刻意不放在默认分支 `main`，以免和 `release.yml` 的定时标定
产生任何牵连。代价是 GitHub 不会自动注册它 —— `workflow_dispatch` 要求工作流要么
在默认分支上，要么曾经成功运行过。

因此文件里带了一个**临时** `push` 触发器。第一次推分支时它会自动跑，既完成注册，
也顺带产出第一个镜像：

```bash
git switch custom/company
git push -u origin custom/company
gh run watch --repo shangwantsci/sub2api
```

确认成功后，删掉 `push` 触发器，避免以后每次推分支都触发完整构建：

```bash
# 编辑 .github/workflows/company-image.yml，删除 on: 下的 push: 整段
git commit -am "chore: drop company-image bootstrap push trigger"
git push origin custom/company
```

之后改用手工触发。注意：注册状态依赖运行历史，若该工作流的运行记录被全部删除，
需要再 bootstrap 一次。

## 5. 常规构建

```bash
gh workflow run company-image.yml --ref custom/company
gh run list --workflow "Company Image" --repo shangwantsci/sub2api --limit 3
gh run watch --repo shangwantsci/sub2api
```

工作流会：守卫检查 ref → checkout → 校验 VERSION 格式 → buildx 构建 linux/amd64
（不推送）→ 跑一次 `--version` 自检 → `docker save | gzip` → 连同 sha256 上传为
artifact。run summary 里会给出带真实 run id 的下载命令。

## 6. 取回并传输

```bash
RUN_ID=<上一步的 run id>
ART=sub2api-company-0.1.156-<commit>

gh run download "$RUN_ID" --repo shangwantsci/sub2api --name "$ART" --dir ./transfer
cd transfer
shasum -a 256 -c "$ART.tar.gz.sha256"     # 必须 OK，否则重下

scp -P <PORT> "$ART.tar.gz" <USER>@<公司主机>:/tmp/
```

本机不需要 Docker，`gh` 就够了。

## 7. 全新机器首次部署

### 7.1 网络拓扑

```text
┌─────────────────────── 公司服务器 ───────────────────────┐
│                                                          │
│  ┌──────────┐   http://sub2api:8080   ┌───────────────┐  │
│  │  NewAPI  │ ──────────────────────► │    sub2api    │  │
│  └──────────┘   （共享 docker 网络）   └───────┬───────┘  │
│                                                │          │
│                                    sub2api-network        │
│                                        ┌───────┴───────┐  │
│                                        │ postgres redis│  │
│                                        └───────────────┘  │
│                                                          │
│  127.0.0.1:18080 ← 仅回环，供 SSH 隧道访问管理后台        │
└──────────────────────────────────────────────────────────┘
```

没有域名，没有反向代理，不对办公网暴露任何端口。sub2api 以 `external` 方式加入
NewAPI 已有的 docker 网络，因此**不需要改动 NewAPI 自己的 compose**。

### 7.2 准备目录与配置

```bash
mkdir -p /opt/sub2api-company && cd /opt/sub2api-company
mkdir -p data postgres_data redis_data backups
```

从仓库取三个文件放进去：

```text
deploy/docker-compose.local.yml            -> docker-compose.local.yml
deploy/company/docker-compose.override.yml -> docker-compose.override.yml
deploy/company/.env.example                -> .env（按注释填写）
```

查出 NewAPI 的网络名并填进 `.env` 的 `NEWAPI_NETWORK`：

```bash
docker inspect <newapi容器名> \
  -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{"\n"}}{{end}}'
```

`.env` 至少要填 `NEWAPI_NETWORK`、`POSTGRES_PASSWORD`（缺失时 compose 直接拒绝
启动）、`ADMIN_PASSWORD`、`JWT_SECRET`、`TOTP_ENCRYPTION_KEY`，并确认
`SUB2API_IMAGE` 是本次要加载的 tag。密钥用 `openssl rand -hex 32` 生成。

```bash
chmod 600 .env && chown root:root .env
```

### 7.3 导入镜像并启动

```bash
docker load -i /tmp/sub2api-company-0.1.156-<commit>.tar.gz
docker images sub2api-company

docker compose -f docker-compose.local.yml -f docker-compose.override.yml up -d
```

首次启动会跑数据库迁移，比平时慢。验证：

```bash
docker compose -f docker-compose.local.yml -f docker-compose.override.yml ps
docker inspect -f '{{.State.Health.Status}}' sub2api      # 应为 healthy
docker exec sub2api /app/sub2api --version                # commit 要和 tag 一致
docker exec sub2api wget -qO- http://localhost:8080/health
```

确认后删掉临时文件：`rm -f /tmp/sub2api-company-*.tar.gz`

### 7.4 验证 NewAPI 能解析到

从 NewAPI 容器内部打一次（把 `<newapi容器名>` 换成实际名字）：

```bash
docker exec <newapi容器名> sh -c 'wget -qO- http://sub2api:8080/health'
```

期望输出 `{"status":"ok"}`。如果解析不到，先确认两个容器确实在同一网络：

```bash
docker network inspect "$(grep '^NEWAPI_NETWORK=' /opt/sub2api-company/.env | cut -d= -f2)" \
  -f '{{range .Containers}}{{.Name}}{{"\n"}}{{end}}'
```

### 7.5 接入 NewAPI

先在 sub2api 管理后台建一个 API Key（走 SSH 隧道访问）：

```bash
# 在你自己的笔记本上
ssh -N -L 18080:127.0.0.1:18080 <USER>@<公司主机>
# 然后浏览器打开 http://localhost:18080
```

用 `.env` 里的 `ADMIN_EMAIL` / `ADMIN_PASSWORD` 登录，**立即改密码**，然后创建
分组、添加账号、生成 API Key。

在 NewAPI 侧新建渠道：

```text
Base URL:  http://sub2api:8080
密钥:      sub2api 里生成的 API Key（不是管理员密码）
```

sub2api 对外暴露的网关路由（都在 API Key 鉴权之后）：

```text
POST /v1/messages                 Anthropic 原生
POST /v1/messages/count_tokens
POST /v1/chat/completions         OpenAI 兼容
POST /chat/completions            同上，无 v1 前缀别名
POST /v1/responses
GET  /v1/models
```

渠道类型按 NewAPI 侧的习惯选 Anthropic 或 OpenAI 兼容，二者路由都在。

## 8. 后续升级与回滚

```bash
cd /opt/sub2api-company
cp .env "backups/.env.$(date +%Y%m%d-%H%M%S).before-<新commit>"
docker inspect -f '{{.Config.Image}}' sub2api    # 记下当前 tag，即回滚目标

docker load -i /tmp/sub2api-company-0.1.156-<新commit>.tar.gz
sed -i 's#^SUB2API_IMAGE=.*#SUB2API_IMAGE=sub2api-company:0.1.156-<新commit>#' .env
docker compose -f docker-compose.local.yml -f docker-compose.override.yml \
  up -d --no-deps --force-recreate sub2api

docker inspect -f '{{.State.Health.Status}}' sub2api
docker exec sub2api /app/sub2api --version
```

**注意：这台机器上永远不要执行 `docker compose pull`。** 镜像不在任何 registry
里，pull 必然失败；override 里的 `pull_policy: never` 就是为此设置的。

旧镜像 tag 不要急着 `docker rmi`，它就是回滚点。回滚只需把 `.env` 的
`SUB2API_IMAGE` 改回旧 tag 再 `up -d --force-recreate sub2api`，不动数据库和 Redis。

## 9. 标定 profile 手工同步

公司机器不在 GitHub 定时标定的 publish 目标里，默认走编译内置常量（安全，但伪装
特征会随官方 CLI 升级变旧）。

```bash
# 1. 取最近一次定时标定的 profile（artifact 保留 14 天）
gh run list --workflow Release --repo shangwantsci/sub2api --limit 10
gh run download <RUN_ID> --repo shangwantsci/sub2api \
  --name "claude-calibration-<RUN_ID>" --dir /tmp/cal

# 2. 通过 SSH 隧道发布到公司网关
ssh -N -L 18080:127.0.0.1:18080 <USER>@<公司主机> &
node tools/cc-calibrate/publish.js /tmp/cal/profile-*.json \
  http://localhost:18080 "<公司 ADMIN_API_KEY>"
```

`publish.js` POST 到 `/api/v1/admin/settings/claude-calibrated-profile`，用
`x-api-key` 头；HTTP 200 即成功，网关约 60 秒内热加载。发布后在设置页确认
published + valid、CLI 版本与 guard 结果。

节奏按官方 CLI 版本变化走，不必每 6 小时同步。artifact 只保留 14 天，超期需要
手工触发一次 `calibrate_only=true` 重新产出。

**不要**把公司网关地址写进 `CC_CALIBRATE_GATEWAY_URL` secret —— 那个 secret 是
生产线在用的，改它会让定时标定发到错误的目标。

## 10. 访问控制（决定源码是否会外流）

镜像里是静态编译、前端已 embed 的单个二进制，任何拿到 root 或 docker 权限的人都能
`docker save` 带走并在别处运行。构建已用 `-s -w -trimpath`，拿不到逐行源码，但
`redress` 一类工具仍能还原包名函数名。**能挡住人的是权限，不是编译选项：**

- 不给其他人 root；不把任何人加进 `docker` 组（等价于 root）；
- `.env` 保持 600 且属主 root；
- 本流程已确保服务器上没有任何 GHCR 凭据，别再手工 `docker login`；
- 传输用的 tar 包用完立刻删除，不要留在 `/tmp` 或家目录；
- 管理员账号只有你一个人持有 —— 第 3 节那三条残留通路全靠它把守；
- 没有对外端口意味着攻击面只剩 docker 内网和 SSH，不要为了方便临时把
  `BIND_HOST` 改成 `0.0.0.0`。

如果要求是「公司任何人都不能拿到这个项目」，唯一可靠的做法是让它跑在只有你能碰的
机器上，只向公司提供 API 地址和 key。
