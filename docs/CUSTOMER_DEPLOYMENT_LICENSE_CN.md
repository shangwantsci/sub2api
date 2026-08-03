# 客户服务器授权部署手册

> 适用场景：把本 fork 的客户专属镜像部署到客户拥有 root 权限的服务器，同时做到：
>
> - 客户服务器不保存 Git 仓库和源码；
> - Docker 镜像复制到另一台机器后不能继续续租；
> - 你可以在自己的授权中心查看、续期或吊销实例；
> - 授权续租不进入 AI 请求链路，不改变 Claude 伪装和 TTFT。

## 0. 执行顺序（先看这个）

**授权中心必须先上线，客户实例才能激活。** 下面 A 段全部不依赖客户服务器，务必在拿到
客户机器之前做完；拿到客户服务器后只剩 B 段。

### A. 客户服务器到手之前（在你自己的机器/服务器上做）

| 步 | 做什么 | 在哪做 | 章节 |
|---|---|---|---|
| A1 | 生成 Ed25519 密钥对，私钥只留在授权服务器 | 你的服务器 | §3 |
| A2 | 部署授权中心 + Postgres，前面挂 HTTPS 反代 | 你的服务器 | §3 |
| A3 | 确认 `https://<域名>/health` 返回 `{"status":"ok"}` | 任意 | §3 |
| A4 | 设置 GitHub 仓库变量 `DEPLOYMENT_LICENSE_PUBLIC_KEY` | GitHub | §4 |
| A5 | 用 `customer_id=<客户>` 触发构建，产出私有客户镜像 | GitHub | §4 |
| A6 | 确认新 GHCR package 是 Private | GitHub | §4 |
| A7 | 建一个只读 PAT，只授权这一个 package，交给客户 | GitHub | §4 |
| A8 | 调 `/admin/activations` 生成一次性激活码（只显示一次） | 你的服务器 | §3 |

### B. 拿到客户服务器之后

| 步 | 做什么 | 章节 |
|---|---|---|
| B1 | 装 Docker，创建部署目录 | §5 |
| B2 | 只上传三个文件：`docker-compose.local.yml`、`docker-compose.customer.yml`、`.env` | §5 |
| B3 | 用只读 PAT `docker login ghcr.io` | §6 |
| B4 | `docker compose ... config` 检查，再 `pull`、`up -d` | §6 |
| B5 | 查 `deployment-license/status`，确认 `status=active` 且 `host_signal_count>=1` | §7 |
| B6 | 回你的授权中心 `/admin/instances`，确认这台实例已登记 | §7 |

**不要**在客户服务器上 clone 仓库、跑 `git`、跑 `go build`、放 `deploy/` 整个目录。

### 按客户开关功能（不需要单独分支或单独构建）

客户能用哪些受管功能，由 lease 里的 `features` 决定，在**创建激活码时**指定：

```json
{"customer_id": "customer-a", "features": ["gateway"]}
```

目前受管的能力只有一个：

| 能力名 | 不给会怎样 |
|---|---|
| `chrome_cookie_auth` | 后台隐藏「Chrome Cookie 授权」选项，后端拒绝该端点（403） |

**不写进 `features` 就是关闭**。要给某个客户开，把 `chrome_cookie_auth` 加进去即可：

```json
{"customer_id": "customer-a", "features": ["gateway", "chrome_cookie_auth"]}
```

改动在授权中心侧生效，**不需要重新构建镜像、不需要客户重新部署**，客户下次续租
（最多 1 小时）后刷新后台即可。

注意这只影响受管能力：普通「Cookie 自动授权」、setup token 授权等都不受影响，客户
照常使用。community 构建（你自己的生产）不受任何限制。

### 标定 profile 会自动下发

客户服务器**不运行**标定器（那需要 node + Claude Code CLI + 源码目录）。它通过每小时
那次续租顺带取回你在授权中心发布的最新 profile，验签后写入自己的 setting 并热加载。

- 你只需要在授权中心发布一次，所有客户自动跟进；
- 客户不需要暴露任何入站 Admin API；
- 授权中心没发布过 profile 时，客户回退到镜像内置常量，功能正常，只是不跟随新版 CLI。

部署后确认客户实例已收到：

```bash
curl -sS http://127.0.0.1:8080/api/v1/admin/settings/claude-calibrated-profile \
  -H "Authorization: Bearer <管理员 token>"
```

### 关键前置条件

- 授权中心地址**必须是 HTTPS**。客户端会拒绝非 localhost 的 http 地址，直接启动失败。
- 授权中心自己不能长期挂。客户实例最长扛 24h lease + 6h 宽限 ≈ 30 小时，超过就停网关。
- 激活码只显示一次，数据库只存 SHA-256。丢了就重新生成一个。
- A5 之前必须先做 A4，否则非 community 构建会直接失败退出（workflow 里有硬检查）。

## 1. 先理解三个组件

```text
你的 GitHub Actions
  └─ 构建客户专属镜像（内置客户 ID 与授权验签公钥）
         ↓
你的私有 GHCR
         ↓ 客户只能 pull
客户服务器上的 Sub2API
         ↕ 每小时一次 HTTPS（不经过用户请求）
你控制的 sub2api-license-server
```

代码位置：

- 主项目：`sub2api-fork/`
- 独立授权中心：与主项目同级的 `sub2api-license-server/`
- 客户 Compose overlay：`deploy/docker-compose.customer.yml`
- 客户环境变量模板：`deploy/.env.customer.example`

## 2. 为什么客户不能只关掉授权

客户专属镜像构建时会把以下值写进二进制：

- `ManagedCustomerID=<客户 ID>`
- 授权中心 Ed25519 验签公钥
- 当前 commit/build 标识

当 `ManagedCustomerID != community` 时：

- 即使客户设置 `DEPLOYMENT_LICENSE_ENABLED=false`，程序仍强制启用授权；
- 客户配置的另一把公钥不会覆盖镜像内置公钥；
- 宽限期不能通过环境变量调到超过 6 小时；
- 不能通过 `DEPLOYMENT_LICENSE_MACHINE_ID` 手工伪造机器 ID；
- 内置在线更新/回滚被禁用，不能从官方 release 替换成无授权二进制。

这仍然不是“绝对不可破解”：客户拥有 root 时可以反编译或 patch 二进制，但不能靠改几个
环境变量直接绕过。

## 2.1 你自己的生产环境不受影响

**现有的自用生产不需要授权，也不会被这套机制拦住。** 判定只看构建时写进二进制的
`ManagedCustomerID`：

| 构建方式 | `ManagedCustomerID` | 授权是否启用 | 自更新 |
|---|---|---|---|
| 现有生产（不传 `customer_id`，或传 `community`） | `community` | **否** | 保留 |
| 源码本地构建（不走 CI） | `community`（默认值） | 否 | 保留 |
| 客户镜像（`customer_id=customer-a`） | `customer-a` | **是，强制** | 禁用 |

community 构建下：`DeploymentLicenseService` 直接返回未启用，中间件对所有请求
`c.Next()` 放行，后台不起续租循环，也不会向授权中心发任何请求——即使 CI 把公钥编进了
二进制也一样。

这一点有测试锁定，不是约定：

```bash
cd backend && go test -tags unit ./internal/service/ -run TestCommunityBuild
cd backend && go test ./internal/server/middleware/ -run Community
```

覆盖内容：community 构建不启用授权、网关和 admin 路径均放行、`RefreshNow` 不联网、
保留自更新、以及"CI 编入公钥也不会意外开启"。

唯一需要注意的是 `.env`：**不要**在自用生产的 `.env` 里写 `DEPLOYMENT_LICENSE_ENABLED=true`
或 `UPDATE_DISABLED=true`。这两个开关对 community 构建是生效的（设计如此，便于本地联调
授权），误设会把自己的生产也拖进授权流程。现有生产 `.env` 里没有这两项，保持原样即可。

## 3. 第一次准备授权中心

授权中心必须部署在**你控制的公网服务器**，不要部署在客户服务器。

最低资源：

```text
1 vCPU / 1 GB RAM / 10 GB 磁盘
PostgreSQL
一个 HTTPS 域名，例如 license.example.com
```

授权中心的签名私钥只保存在你控制的服务器；客户镜像只包含公钥。

具体安装、生成 Ed25519 密钥、创建激活码、吊销和备份命令见：

```text
../sub2api-license-server/README.md
```

首次部署完成后应得到：

```text
授权中心 URL      https://license.example.com
Ed25519 公钥      一行 base64
ADMIN_TOKEN       只保存在你自己的密码管理器/授权服务器
客户激活码         一次性，只给对应客户
```

## 4. 配置 GitHub 私有构建

在 GitHub 仓库设置 Repository Variable：

```text
DEPLOYMENT_LICENSE_PUBLIC_KEY=<授权中心输出的 base64 Ed25519 公钥>
```

该值是公钥，不是签名私钥。私钥严禁进入 GitHub 仓库、Actions 日志、客户 `.env` 或镜像。

构建某个客户的专属镜像：

```bash
APP_VERSION="$(tr -d '\r\n' < backend/cmd/server/VERSION)"

gh workflow run release.yml \
  --repo shangwantsci/sub2api \
  --ref custom/prod \
  -f tag="v${APP_VERSION}" \
  -f custom_image_only=true \
  -f source_ref=custom/prod \
  -f customer_id=customer-a \
  -f simple_release=true
```

`customer_id` 只能使用小写字母、数字、点、下划线和连字符。

客户镜像使用独立 GHCR package：

```text
ghcr.io/shangwantsci/sub2api-customer-customer-a:<VERSION>
ghcr.io/shangwantsci/sub2api-customer-customer-a:<VERSION>-<COMMIT>
```

不要把客户镜像放回公共/生产共用的 `sub2api` package，否则客户的 pull 凭证可能看到不受
授权约束的 community tag。

构建完成后在 GitHub Packages 中确认新 package 为 Private，并只给该客户的只读凭证
授予这个 package；不要给仓库写权限或 Actions 权限。

## 5. 客户服务器准备

客户服务器只需要以下文件，不需要仓库：

```text
docker-compose.local.yml
docker-compose.customer.yml
.env
data/
postgres_data/
redis_data/
```

从本仓模板复制：

```bash
cp deploy/docker-compose.local.yml <交付目录>/docker-compose.local.yml
cp deploy/docker-compose.customer.yml <交付目录>/docker-compose.customer.yml
cp deploy/.env.example <交付目录>/.env
```

把 `deploy/.env.customer.example` 中的变量合并到客户 `.env`，至少填写：

```text
SUB2API_CUSTOMER_IMAGE
DEPLOYMENT_LICENSE_SERVER_URL
DEPLOYMENT_LICENSE_ACTIVATION_CODE
```

授权机器身份依赖以下只读挂载，`docker-compose.customer.yml` 已配置：

```text
/etc/machine-id
/sys/class/dmi/id
```

如果客户系统没有这些路径，不要随便填写一个假的 ID，应先为该云厂商增加实例身份/TPM
适配后再交付。

### 关于 DMI 信号实际能不能读到

容器 entrypoint 会用 `su-exec` 把进程降权到 uid 1000 运行，而多数 Linux 发行版上
`/sys/class/dmi/id/product_uuid` 和 `board_serial` 的权限是 `0400 root:root`。
**这意味着降权后的进程通常只能读到 `/etc/machine-id`（0444），DMI 两项读不到。**

这不会导致启动失败（只要至少有一个信号即可），但机器指纹的强度会低于三信号。部署后
必须实测确认，不要假设：

```bash
curl -sS http://127.0.0.1:8080/api/v1/admin/deployment-license/status \
  -H "Authorization: Bearer <管理员 token>" | grep host_signal_count
```

- `host_signal_count >= 1`：绑定生效，机器指纹至少包含 machine-id；
- `host_signal_count == 0`：**绑定形同虚设**，必须排查挂载后再交付。

在宿主机上确认这些文件对非 root 是否可读：

```bash
ls -l /etc/machine-id /sys/class/dmi/id/product_uuid /sys/class/dmi/id/board_serial
```

想让 DMI 也参与指纹，只能在宿主机放宽这两个文件的权限——但那会把主板序列号暴露给
机器上所有用户，通常不划算。machine-id 在跨机复制时同样会变化，单信号已经能挡住
"直接把镜像和数据卷搬到第二台机器"这一类复制。

## 6. 第一次激活与启动

客户先登录只允许读取其专属 package 的 GHCR 账号：

```bash
docker login ghcr.io
```

检查最终 Compose：

```bash
docker compose \
  -f docker-compose.local.yml \
  -f docker-compose.customer.yml \
  config
```

拉取并启动：

```bash
docker compose \
  -f docker-compose.local.yml \
  -f docker-compose.customer.yml \
  pull

docker compose \
  -f docker-compose.local.yml \
  -f docker-compose.customer.yml \
  up -d
```

首次启动会：

1. 在 `data/license/instance-identity.json` 生成实例 Ed25519 私钥（权限 0600）；
2. 读取宿主机 machine-id/DMI 信号；
3. 使用一次性激活码向你的授权中心注册；
4. 把签名 lease 保存为 `data/license/lease.jwt`；
5. 后台每小时自动续租。

检查日志：

```bash
docker compose \
  -f docker-compose.local.yml \
  -f docker-compose.customer.yml \
  logs --since=10m sub2api
```

成功日志应包含：

```text
deployment_license.renewed
```

首次激活成功后：

1. 把 `.env` 中 `DEPLOYMENT_LICENSE_ACTIVATION_CODE` 清空；
2. 重建一次 `sub2api` 容器确认仅凭实例私钥也能续租；
3. 备份 `.env`、`data/license/instance-identity.json` 和数据库。

激活码是一次性的，不能用于第二台机器。

## 7. 授权状态

管理员接口：

```text
GET  /api/v1/admin/deployment-license/status
POST /api/v1/admin/deployment-license/refresh
```

状态语义：

```text
disabled     community/普通部署，授权功能未启用
active       lease 有效，全部功能正常
grace        授权中心暂时不可达；已有网关流量继续，管理写操作只读
expired      lease + 6h 宽限均已结束；网关业务返回 503
unlicensed   从未成功激活；管理面保留，网关业务关闭
revoked      授权中心明确拒绝/吊销；立即关闭网关业务
limit_exceeded 账号/用户数已超过 lease 上限；停止网关，保留管理删除能力
```

`/health` 仍只表示进程存活，不代表授权有效。Agent 部署后必须同时检查：

```text
/health
/api/v1/admin/deployment-license/status
```

## 8. 断网与授权中心故障

正常参数：

```text
lease 有效期    24 小时（授权中心签发）
自动续租        每小时
请求超时        10 秒
故障宽限        lease 过期后再宽限 6 小时
```

因此最坏情况下（刚续租成功就断网），实例可离线运行接近 30 小时；不是断网满 6 小时就
立即停止。

授权中心短暂不可用时：

- 不会让每个用户请求等待授权服务器；
- 已有 AI 网关流量继续；
- 进入 `grace` 后禁止新增账号、用户和配置修改；
- 超过宽限才停止网关业务；
- 授权恢复后后台自动续租，不必重启。

## 9. 更换客户服务器

不要直接复制 `data/license/` 后开机。

标准流程：

1. 在授权中心吊销旧 instance；
2. 为同一客户生成新的激活码；
3. 新服务器使用空的 `data/license/`；
4. 用新激活码启动并检查状态；
5. 确认新机正常后再下线旧机。

数据库和普通 `/app/data` 迁移可以照常做，但必须删除旧的：

```text
data/license/instance-identity.json
data/license/lease.jwt
```

否则新机器会拿旧实例私钥续租，并因 machine hash 不一致被拒绝。

## 10. 回滚

客户部署只能回滚到同一客户 package 中、同样内置授权公钥和 customer ID 的旧镜像：

```text
ghcr.io/shangwantsci/sub2api-customer-<customer>:<VERSION>-<OLD_COMMIT>
```

禁止：

- 回滚到 community/官方 `weishaw/sub2api` 镜像；
- 使用管理后台在线更新；
- 把其它客户的镜像 tag 用在本客户；
- 将激活码或实例私钥复制到其它机器。

## 11. 安全边界

- Private GHCR 只能限制“谁能下载”，不能阻止 root 用户执行 `docker save`。
- 机器绑定能防普通复制，但 root 可以伪造 machine-id/DMI；高价值客户后续应增加云实例签名
  identity 或 TPM 2.0。
- 前端 JavaScript、SQL schema 和 Go binary 都可能被逆向；技术措施只能提高成本。
- 合同中仍需明确禁止逆向、再分发和跨机器运行，并使用客户专属镜像水印追责。
- Sub2API 主体是 LGPL-3.0-or-later；商业交付前必须确认对应源码/重新链接义务。授权中心
  位于独立私有项目，不应直接并入公开主仓。

## 12. Agent 每次部署前后的固定检查

部署前：

```text
[ ] customer_id 正确，不是 community
[ ] DEPLOYMENT_LICENSE_PUBLIC_KEY 已配置
[ ] 镜像 package 是 sub2api-customer-<customer>
[ ] 激活码属于该客户且尚未使用
[ ] 客户服务器能访问授权中心 HTTPS
[ ] machine-id 与 DMI 只读挂载存在
[ ] 已保留旧客户镜像不可变 tag
```

部署后：

```text
[ ] 容器 healthy
[ ] 二进制 version/commit 正确
[ ] deployment-license/status = active
[ ] instance_id/customer_id 正确
[ ] host_signal_count > 0
[ ] 机器/用户上限正确
[ ] 日志出现 deployment_license.renewed
[ ] 清空一次性激活码后重建仍能续租
[ ] Claude mimicry/calibration 状态未变化
```
