# sub2api 二开提交、推送、部署流程

本文档固定二开分支的日常发布流程，避免每次手工部署时遗漏测试、版本号或服务器切换步骤。

## 核心原则

- **镜像 tag 用二开 commit**：例如 `sub2api-local:8885fdf5`，方便回滚和定位代码。
- **应用版本号用官方版本号**：即 `backend/cmd/server/VERSION`，例如 `0.1.139`。不要把 commit hash 当作 `VERSION`，否则后台会把当前版本识别成非官方语义版本，并持续提示有新版本。
- **只切换应用容器镜像**：生产数据目录、Postgres、Redis、Caddy 配置不随应用发布改动。
- **先构建镜像，再切换 `.env`**：构建失败时旧容器继续运行，不进入半部署状态。
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
- Docker 镜像 tag：使用 commit，例如 `sub2api-local:${COMMIT}`。

如果官方发布了新版本，应先合并/同步上游，让 `backend/cmd/server/VERSION` 跟着更新，再重新构建部署。不要为了消除更新提醒手改成一个不存在的版本号。

## 生产部署流程

当前服务器使用 Docker Compose 本地镜像部署：

- 生产目录：`/opt/sub2api-production`
- release 目录：`/opt/sub2api-production/releases/<commit>`
- 应用镜像：`sub2api-local:<commit>`
- 对外入口由 Caddy 转发，sub2api 容器映射到 `127.0.0.1:18080`

本地准备变量：

```bash
BRANCH="$(git branch --show-current)"
COMMIT="$(git rev-parse --short=8 HEAD)"
APP_VERSION="$(tr -d '\r\n' < backend/cmd/server/VERSION)"
SSH_TARGET="root@154.29.158.193"
SSH_PORT="56260"
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
Sub2API 0.1.139 (commit: 8885fdf5, built: ...)
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
