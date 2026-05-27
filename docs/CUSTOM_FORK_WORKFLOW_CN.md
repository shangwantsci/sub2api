# Sub2API 二开版本管理与官方同步流程

本文档是晓宇的 Sub2API 二开仓库维护规则。目标是固定一套安全、可重复、可回滚的流程：既能长期保留二开功能，又能持续追官方更新，尤其不能搞乱生产部署分支。

## 核心原则

1. 继续使用 fork 仓库，不新开独立仓库。
2. `origin` 指向晓宇的 fork：`https://github.com/shangwantsci/sub2api.git`。
3. `upstream` 指向官方仓库：`https://github.com/Wei-Shaw/sub2api.git`。
4. `custom/prod` 是唯一生产部署分支。不能在它上面直接试合并、试冲突、试修复。
5. 官方同步必须先进入独立同步分支和独立 worktree，测试通过、晓宇确认后，才合并回 `custom/prod`。
6. `custom/prod` 不做 `rebase`，不做 `force push`，不重写历史。
7. 数据库 migration 只新增，不修改已经上线过的 migration 文件。
8. 任何生产部署前，必须先能回滚：记录当前 commit/tag，备份数据库，确认新版本 migration 风险。

## 术语说明

- fork：从官方仓库复制出来、归晓宇账号管理的仓库。
- origin：晓宇自己的 fork，用来保存二开代码。
- upstream：官方 Sub2API 仓库，只用来拉取官方更新。
- branch：分支，一条独立代码线。
- tag：标签，用来标记一次可部署版本，便于回滚。
- worktree：Git 的独立工作目录。它让同一个仓库同时打开多个分支，避免在生产分支上直接处理冲突。
- migration：数据库结构升级脚本。服务启动时会自动执行尚未执行过的 migration。

## 分支纪律

### `custom/prod`

`custom/prod` 是生产部署分支，只接受已经验证过的合并结果。

禁止在 `custom/prod` 上做这些事：

- 直接 `merge upstream/main` 或 `merge vX.Y.Z`。
- 直接解决冲突。
- `rebase`。
- `push --force`。
- 提交临时调试文件、依赖目录、构建缓存。

### `sync/vX.Y.Z`

官方版本同步分支，例如 `sync/v0.1.131`。

用途：

- 从 `custom/prod` 创建。
- 合并官方 tag。
- 处理冲突。
- 跑测试。
- 写同步记录。

同步完成后，只有在确认通过并得到晓宇同意时，才合并回 `custom/prod`。

### `feature/*`

新增二开功能使用 `feature/*` 分支，例如：

```powershell
git switch custom/prod
git switch -c feature/example-change
```

功能完成并测试通过后，再合并回 `custom/prod`。

### `main`

`main` 不作为二开固定分支，也不作为生产部署分支。它可以保留为接近官方的参考分支，但日常二开和生产部署都围绕 `custom/prod` 进行。

## 当前二开功能边界

官方同步时必须确认这些二开能力仍然存在：

1. Claude 号池动态倍率：目标分组的实际计费倍率随上游号池系数变化。
2. 公开 Claude 号池状态页：`/claude-pool` 不登录可访问，只展示用户可见的空闲率信息。
3. 用户创建 key 或查看分组倍率时，显示用户实际倍率；有专属倍率时显示专属倍率经过动态提升后的实际倍率。
4. 用户侧页面不能暴露内部上游名称、内部来源链接或内部成本细节。

## 官方同步标准流程

下面以官方 tag `v0.1.131` 为例。后续官方版本只替换版本号。

### 1. 确认当前生产分支干净

```powershell
git -C F:\中转站\sub2api status --short --branch
git -C F:\中转站\sub2api log --oneline --decorate -5
```

要求：

- 当前分支是 `custom/prod`。
- 没有未提交改动。
- 当前 commit 是线上可回滚点。

如遇到 Git 提示 `dubious ownership`，只在当前命令上加一次 `-c safe.directory=F:/中转站/sub2api`，不要随手改全局 Git 配置。

### 2. 拉取官方 tag

```powershell
git -c safe.directory=F:/中转站/sub2api -C F:\中转站\sub2api fetch upstream --tags --prune
git -C F:\中转站\sub2api tag --list "v0.1.*"
```

如果要确认某个 tag：

```powershell
git -C F:\中转站\sub2api rev-parse v0.1.131
```

### 3. 创建独立 worktree

```powershell
git -C F:\中转站\sub2api worktree add F:\中转站\sub2api-sync-v0.1.131 -b sync/v0.1.131 custom/prod
```

之后所有同步操作都在 `F:\中转站\sub2api-sync-v0.1.131` 里完成。

### 4. 合并官方 tag，但先不提交

```powershell
git -C F:\中转站\sub2api-sync-v0.1.131 merge --no-commit --no-ff v0.1.131
```

如果没有冲突，也先不要立刻提交，仍然要检查二开功能是否被覆盖。

### 5. 冲突处理原则

优先采用官方新版本的通用逻辑，但必须保留二开功能边界。

重点检查：

- `Dockerfile`：保留可复现 pnpm 构建策略，避免 Zeabur/服务器构建时 pnpm 忽略必要 build scripts。
- `backend/cmd/server/wire_gen.go` 和 `backend/internal/service/wire.go`：官方新增依赖与二开服务依赖都要保留。
- `backend/internal/service/gateway_service.go`：保留 Claude 动态倍率，同时接入官方新增计费/额度逻辑。
- `backend/internal/service/openai_gateway_service.go`：同上。
- `frontend/src/router/index.ts`：保留 `/claude-pool` 公共路由，同时保留官方新增路由。
- `frontend/src/components/common/*`：保留实际倍率展示逻辑。
- `backend/migrations/*`：确认是否有新增 migration；上线前必须备份数据库。

冲突处理后检查：

```powershell
rg -n '^(<{7} |={7}$|>{7} )' F:\中转站\sub2api-sync-v0.1.131
git -C F:\中转站\sub2api-sync-v0.1.131 diff --check
git -C F:\中转站\sub2api-sync-v0.1.131 diff --name-only --diff-filter=U
```

要求：

- 没有冲突标记。
- 没有未解决冲突文件。
- 没有空白错误。

### 6. 后端验证

本地如果 `F:\GO语言\bin\go.exe` 版本低于 `go.mod` 要求，可以使用已经下载的对应 Go toolchain。当前 v0.1.131 要求 Go `1.26.3`。

示例：

```powershell
$env:GOMODCACHE='F:\中转站\.codex-go-v0131\gomod'
$env:GOCACHE='F:\中转站\.codex-go-v0131\gocache'
$env:GOTOOLCHAIN='local'
& 'F:\中转站\.codex-gomod\golang.org\toolchain@v0.0.1-go1.26.3.windows-amd64\bin\go.exe' test ./cmd/server ./internal/service ./internal/handler ./internal/server/...
```

最低要求：

- `cmd/server` 通过。
- `internal/service` 通过。
- `internal/handler` 通过。
- `internal/server/...` 通过。

如果构造函数、Wire 注入、migration、计费逻辑有冲突，通常会在这里暴露。

### 7. 前端验证

```powershell
cd F:\中转站\sub2api-sync-v0.1.131\frontend
pnpm install --frozen-lockfile
pnpm run build
pnpm exec vitest run src/router/__tests__/claude-pool-route.spec.ts src/router/__tests__/guards.spec.ts src/views/public/__tests__/ClaudePoolView.spec.ts src/components/common/__tests__/GroupRateDisplay.spec.ts
```

最低要求：

- 生产构建通过。
- `/claude-pool` 公共路由测试通过。
- 路由守卫测试通过。
- 分组倍率展示测试通过。

### 8. Docker 构建验证

如果本机 Docker daemon 正常运行，执行：

```powershell
docker build -t sub2api:sync-v0.1.131-test F:\中转站\sub2api-sync-v0.1.131
```

如果 Docker daemon 没启动，要在同步记录里明确写“未本地验证 Docker 构建”，不要声称已验证。

### 9. 提交同步分支

所有验证通过后，在 `sync/vX.Y.Z` 分支提交：

```powershell
git -C F:\中转站\sub2api-sync-v0.1.131 status --short --branch
git -C F:\中转站\sub2api-sync-v0.1.131 commit -m "merge upstream v0.1.131 into custom prod"
```

提交前确认没有：

- 密钥、密码、数据库连接串。
- `node_modules`。
- Go cache。
- 临时测试文件。

### 10. 合并回生产分支

只有晓宇确认后才执行：

```powershell
git -C F:\中转站\sub2api switch custom/prod
git -C F:\中转站\sub2api merge --no-ff sync/v0.1.131
```

再次验证 `custom/prod` 状态后再推送：

```powershell
git -C F:\中转站\sub2api status --short --branch
git -C F:\中转站\sub2api push origin custom/prod
```

不要使用 `push --force`。

## 生产部署标准流程

生产服务器当前按自托管 Docker Compose 管理，部署目录为：

```text
/opt/sub2api-production
```

部署前：

1. 确认线上当前 commit/tag。
2. 备份生产数据库。
3. 如果官方同步包含新增 migration，必须确认测试库已跑通。
4. 确认 DNS、Traefik 域名、环境变量、PostgreSQL、Redis 状态正常。

推荐部署步骤：

```bash
cd /opt/sub2api-production
git status --short --branch
git fetch origin
git checkout custom/prod
git pull --ff-only origin custom/prod
docker compose build sub2api
docker compose up -d --no-deps sub2api
docker compose logs --tail=100 sub2api
```

上线后验证：

- 首页能打开。
- 管理员能登录。
- 用户能创建 key。
- API 请求能成功。
- 计费记录正常。
- `/claude-pool` 能公开访问。
- 目标 Claude 分组倍率显示和计费符合预期。

## 回滚规则

部署前给当前生产 commit 打标签：

```powershell
git -C F:\中转站\sub2api tag custom-vYYYY.MM.DD-N
git -C F:\中转站\sub2api push origin custom-vYYYY.MM.DD-N
```

如果上线后出问题：

1. 如果没有数据库 migration，优先回滚应用代码到上一个 tag。
2. 如果有数据库 migration，先判断旧代码是否兼容新数据库结构。
3. 不手动删除 migration 记录，不直接改 `schema_migrations`。
4. 回滚前后都要保留日志和当前 commit，方便复盘。

## 安全规则

1. 不把服务器密码、数据库密码、Redis 密码、JWT Secret、Token 或旧管理密码写入 Git。
2. 不把环境变量截图或复制到公开位置。
3. 服务器 SSH 密码、数据库密码、Redis 密码如曾被多人看到，应轮换。
4. 生产环境必须使用固定的 JWT、TOTP、支付加密密钥，避免重启后登录态或加密数据异常。

## 本次 v0.1.131 同步记录

本次采用的安全流程：

1. `custom/prod` 保持干净，未直接在生产分支处理冲突。
2. 创建独立 worktree：`F:\中转站\sub2api-sync-v0.1.131`。
3. 创建同步分支：`sync/v0.1.131`。
4. 合并官方 tag：`v0.1.131`。
5. 处理冲突时同时保留官方新增用户平台额度逻辑和二开 Claude 动态倍率逻辑。

本次已验证：

- `rg -n '^(<{7} |={7}$|>{7} )'`：无冲突标记。
- `git diff --check`：通过。
- 后端：`go test ./cmd/server ./internal/service ./internal/handler ./internal/server/...` 通过。
- 前端：`pnpm run build` 通过。
- 前端针对性测试 4 个文件、41 个用例通过。

本次未验证：

- Docker 镜像构建未完成，因为本机 Docker daemon 未运行。
