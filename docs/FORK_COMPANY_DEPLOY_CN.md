# 公司部署 runbook（custom/company）

> 最后更新：2026-07-25
> 适用分支：`custom/company`（见 `FORK_PROJECT_MEMORY.md` 第 2.3 节）
> 与 `FORK_DEPLOY_RUNBOOK_CN.md`（现有生产）**流程不同**，不要混用。

## 0. 与现有生产的关键差异

| 项 | 现有生产 `custom/prod` | 公司部署 `custom/company` |
|---|---|---|
| 构建 | `release.yml` 的 `custom_image_only` | `company-image.yml` |
| 产物 | 推送到 GHCR | `docker save` 成 workflow artifact |
| 分发 | 服务器 `docker compose pull` | 下载 tar → scp → `docker load` |
| 镜像 tag | `ghcr.io/shangwantsci/sub2api:<VER>` | `sub2api-company:<VER>-<commit>` |
| registry 凭据 | 服务器需要 | **服务器完全不需要** |
| 标定 profile | GitHub 定时自动发布 | 手工同步 |

**为什么不复用现有 workflow**：`custom_image_only` 的可变 tag 只由
`backend/cmd/server/VERSION` 决定，两个分支都是 `0.1.156`。用它构建公司分支会把
GHCR 上的 `sub2api:0.1.156` 覆盖成公司镜像，而现有生产的 `.env` 正好钉在这个可变
tag 上 —— 下次生产 `pull` 就会拉到公司版。`company-image.yml` 全程 `push: false`，
从根上避免这个问题。

## 1. 首次 bootstrap（只做一次）

`company-image.yml` 刻意不放在默认分支 `main`，以免和 `release.yml` 的定时标定
产生任何牵连。代价是 GitHub 不会自动注册它 —— `workflow_dispatch` 要求工作流要么
在默认分支上，要么曾经成功运行过。

因此文件里带了一个**临时** `push` 触发器。第一次推分支时它会自动跑，既完成注册，
也顺带产出第一个镜像：

```bash
cd /Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork
git switch custom/company
git push -u origin custom/company
gh run watch --repo shangwantsci/sub2api
```

确认成功后，删掉 `push` 触发器，避免以后每次推分支都触发一次完整构建：

```bash
# 编辑 .github/workflows/company-image.yml，删除 on: 下的 push: 整段
git add .github/workflows/company-image.yml
git commit -m "chore: drop company-image bootstrap push trigger"
git push origin custom/company
```

之后改用手工触发：

```bash
gh workflow run company-image.yml --ref custom/company
```

注意：注册状态依赖运行历史。如果哪天把该工作流的运行记录全部删除，它会重新变成
未注册状态，需要再 bootstrap 一次。

## 2. 常规构建

```bash
gh workflow run company-image.yml --ref custom/company
gh run list --workflow "Company Image" --repo shangwantsci/sub2api --limit 3
gh run watch --repo shangwantsci/sub2api
```

工作流会：checkout 指定 ref → 校验 VERSION 格式 → buildx 构建 linux/amd64（不推送）
→ 跑一次 `--version` 自检 → `docker save | gzip` → 连同 sha256 上传为 artifact。
run summary 里会直接给出带真实 run id 的下载命令。

## 3. 取回并传输

```bash
RUN_ID=<上一步的 run id>
ART=sub2api-company-0.1.156-<commit>

gh run download "$RUN_ID" --repo shangwantsci/sub2api --name "$ART" --dir ./transfer
cd transfer
shasum -a 256 -c "$ART.tar.gz.sha256"     # 必须 OK，否则重下

scp -P <PORT> "$ART.tar.gz" <USER>@<公司主机>:/tmp/
```

本机不需要 Docker，`gh` 就够了。

## 4. 全新机器首次部署

服务器上准备目录（示例用 `/opt/sub2api-company`）：

```bash
mkdir -p /opt/sub2api-company && cd /opt/sub2api-company
mkdir -p data postgres_data redis_data backups
```

从仓库取三个文件放进去：

```text
deploy/docker-compose.local.yml          -> docker-compose.local.yml
deploy/company/docker-compose.override.yml -> docker-compose.override.yml
deploy/company/.env.example              -> .env（然后按注释填写）
```

`.env` 至少要填 `POSTGRES_PASSWORD`（缺失时 compose 直接拒绝启动）、
`ADMIN_PASSWORD`、`JWT_SECRET`、`TOTP_ENCRYPTION_KEY`，并确认 `SUB2API_IMAGE`
是本次要加载的 tag。密钥用 `openssl rand -hex 32` 生成。

```bash
chmod 600 .env
chown root:root .env
```

导入镜像并启动：

```bash
docker load -i /tmp/sub2api-company-0.1.156-<commit>.tar.gz
docker images sub2api-company

docker compose -f docker-compose.local.yml -f docker-compose.override.yml up -d
```

首次启动会跑数据库迁移，比平时慢。验证：

```bash
docker compose -f docker-compose.local.yml -f docker-compose.override.yml ps
curl -fsS http://127.0.0.1:18080/health
docker exec sub2api /app/sub2api --version
```

`--version` 应输出 `Sub2API 0.1.156 (commit: <commit>, ...)`，commit 要和镜像 tag
里的一致。确认后删掉临时文件：

```bash
rm -f /tmp/sub2api-company-*.tar.gz
```

最后在前面挂一个反向代理终止 TLS，指向 `127.0.0.1:18080`。仓库
`deploy/Caddyfile` 可以作为参考。**不要**把 `BIND_HOST` 改成 `0.0.0.0`，
否则管理后台会以明文 HTTP 暴露在网络上。

## 5. 后续升级

```bash
# 服务器：先留回滚点
cd /opt/sub2api-company
cp .env "backups/.env.$(date +%Y%m%d-%H%M%S).before-<新commit>"
docker inspect -f '{{.Config.Image}}' sub2api    # 记下当前 tag 即回滚目标

# 导入新镜像并切换（注意：不要 pull）
docker load -i /tmp/sub2api-company-0.1.156-<新commit>.tar.gz
sed -i 's#^SUB2API_IMAGE=.*#SUB2API_IMAGE=sub2api-company:0.1.156-<新commit>#' .env
docker compose -f docker-compose.local.yml -f docker-compose.override.yml \
  up -d --no-deps --force-recreate sub2api

curl -fsS http://127.0.0.1:18080/health
docker exec sub2api /app/sub2api --version
```

旧镜像 tag 不要急着 `docker rmi`，它就是回滚点。回滚只需把 `.env` 的
`SUB2API_IMAGE` 改回旧 tag 再 `up -d --force-recreate sub2api`，不动数据库和 Redis。

## 6. 标定 profile 手工同步

公司机器不在 GitHub 定时标定的 publish 目标里，默认走编译内置常量（安全，但伪装
特征会随官方 CLI 升级变旧）。同步步骤见 `FORK_PROJECT_MEMORY.md` 第 2.3 节，要点：

```bash
gh run download <定时标定的 RUN_ID> --repo shangwantsci/sub2api \
  --name "claude-calibration-<RUN_ID>" --dir /tmp/cal
node tools/cc-calibrate/publish.js /tmp/cal/profile-*.json \
  https://<公司网关域名> "<公司 ADMIN_API_KEY>"
```

标定 artifact 只保留 14 天。节奏按官方 CLI 版本变化走，不必每 6 小时同步。

## 7. 访问控制（决定源码是否会外流）

镜像里是静态编译、前端已 embed 的单个二进制，任何拿到 root 或 docker 权限的人都能
`docker save` 带走并在别处运行。构建已用 `-s -w -trimpath`，拿不到逐行源码，但
`redress` 一类工具仍能还原包名函数名。**能挡住人的是权限，不是编译选项：**

- 不给其他人 root；不把任何人加进 `docker` 组（等价于 root）；
- `.env` 保持 600 且属主 root；
- 本流程已确保服务器上没有任何 GHCR 凭据，别再手工 `docker login`；
- 传输用的 tar 包用完立刻删除，不要留在 `/tmp` 或家目录；
- 管理员账号只有你一个人持有 —— 批量建号、数据导入这些能力全靠它把守
  （详见 `FORK_PROJECT_MEMORY.md` 2.3 节「已知且刻意接受的残留通路」）。

如果要求是"公司任何人都不能拿到这个项目"，唯一可靠的做法是让它跑在只有你能碰的
机器上，只向公司提供 API 地址和 key。
