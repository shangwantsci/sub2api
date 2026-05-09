# Sub2API 二开版本管理与部署规则

本文档是晓宇的 Sub2API 二开仓库维护规则。目标是让二开代码可以稳定上线，同时未来仍然能同步官方 Sub2API 的新功能。

## 核心原则

1. 继续使用 fork 仓库，不新开独立仓库。
2. `origin` 指向晓宇的 fork：`https://github.com/shangwantsci/sub2api.git`。
3. `upstream` 指向官方仓库：`https://github.com/Wei-Shaw/sub2api.git`。
4. 生产环境只部署二开生产分支 `custom/prod`，不要直接部署官方 `main`。
5. 数据库数据和代码分开管理：更新代码时保留原生产数据库，除非明确执行数据库迁移或恢复。
6. 已经在生产环境执行过的 migration 文件不能修改、删除、改名；需要改数据库结构时，只新增 migration。

## 术语说明

- fork：从官方仓库复制出来、归晓宇账号管理的仓库。
- upstream：官方 Sub2API 仓库，用来同步官方更新。
- origin：晓宇自己的 fork 仓库，用来保存二开代码。
- branch：分支，一条独立的代码线。
- tag：标签，用来标记一次可部署版本，便于回滚。
- migration：数据库结构升级脚本。项目启动时会自动执行未执行过的 migration。
- staging：测试环境，用生产备份恢复出的测试数据库先试跑新版。

## 分支规则

### `main`

`main` 尽量保持接近官方仓库，只用于同步官方更新。不要直接在 `main` 上写二开功能。

### `custom/prod`

`custom/prod` 是晓宇的二开生产分支。Zeabur 以后应该部署这个分支。

当前二开功能包括：

- Derouter Claude 号池动态倍率。
- 公开 Claude 号池状态页面。
- 用户创建 key 时只显示用户实际倍率。

### `feature/*`

以后新增二开功能时，从 `custom/prod` 创建功能分支，例如：

```powershell
git switch custom/prod
git switch -c feature/payment-tweak
```

功能完成、测试通过后，再合并回 `custom/prod`。

## 提交规则

提交要按功能拆分，避免把多个不相关需求塞进一个 commit。

推荐提交粒度：

- 一个功能一个 commit。
- 一个修复一个 commit。
- 文档规则单独一个 commit。

推荐提交信息：

```text
docs: add custom fork workflow
feat: add derouter claude pool dynamic pricing
fix: show effective group rate in key creation
```

## 官方更新同步流程

未来官方 Sub2API 更新时，按下面流程同步：

```powershell
git fetch upstream
git switch main
git merge --ff-only upstream/main
git push origin main

git switch custom/prod
git merge main
```

如果出现冲突，先解决冲突，再运行测试。测试通过后再打标签并部署。

不推荐新手在生产分支上使用 `rebase`，因为它可能需要强制推送，误操作风险更高。生产二开分支默认使用 `merge`。

## 发布标签规则

每次准备部署前，在 `custom/prod` 上打标签：

```powershell
git switch custom/prod
git tag custom-vYYYY.MM.DD-N
git push origin custom/prod --tags
```

示例：

```powershell
git tag custom-v2026.05.09-1
```

Zeabur 可以部署 `custom/prod`，也可以部署某个稳定标签。正式生产更推荐部署标签，因为标签更容易回滚。

## 部署前检查清单

部署前必须完成：

1. 备份生产数据库。
2. 记录当前生产环境变量，但不要把密钥写进仓库或聊天记录。
3. 记录当前线上版本、commit 或镜像标签。
4. 用生产数据库备份恢复一个测试数据库。
5. 新建 staging 服务，部署 `custom/prod` 或待发布标签。
6. staging 连接测试数据库，不直接连接生产数据库。
7. 测试登录、创建 key、调用 API、计费记录、倍率展示、Claude 号池状态页。
8. 后端和前端验证命令通过。

## 数据库保护规则

更新代码不等于迁移数据。只要 Zeabur 新版本继续连接原来的生产数据库，用户、key、分组、余额、用量等数据就会保留。

生产切换时遵守：

- 不要删除生产数据库。
- 不要创建一个空数据库替代生产数据库。
- 不要同时让旧服务和新服务长期写同一个生产数据库。
- 如果新版本包含 migration，必须先在 staging 的测试数据库跑通。
- migration 执行后如果要回滚代码，要确认数据库结构是否兼容旧代码。

## 当前二开功能的数据库影响

当前两个二开功能不新增数据库表，不修改数据库字段。

也就是说，本次上线主要是应用代码变化，数据保留方式是：Zeabur 新版应用继续使用原来的生产数据库连接配置。

## 推荐上线流程

1. 在本地确认 `custom/prod` 测试通过。
2. 推送 `custom/prod` 到晓宇 fork。
3. 在 Zeabur 新建 staging 服务，选择 `custom/prod`。
4. 使用生产库备份恢复出的测试数据库验证。
5. 打发布标签，例如 `custom-v2026.05.09-1`。
6. 在低峰期将生产服务代码来源切到该标签或 `custom/prod`。
7. 保持原生产数据库连接不变。
8. 验证线上登录、API 调用和公开页面。

## 回滚规则

如果新版本上线后出现问题：

1. 优先回滚到上一个已知可用 tag。
2. 如果本次没有数据库 migration，通常只回滚应用代码即可。
3. 如果本次包含数据库 migration，先评估旧代码是否兼容新数据库结构。
4. 不要直接删除 migration 记录或手动改 `schema_migrations`，除非已经备份并明确知道影响。

## 安全规则

1. 不要把服务器密码、数据库连接串、JWT 密钥、支付密钥写入 Git。
2. 不要把 Zeabur 环境变量截图或复制到公开位置。
3. 如果 SSH 密码或管理员初始密码曾经被多人看到，应尽快轮换。
4. 生产环境建议使用固定的 JWT、TOTP、支付加密密钥，避免重启后登录态或加密数据异常。

## 每次二开任务完成后的标准动作

1. 运行相关后端测试。
2. 运行相关前端测试。
3. 运行前端类型检查。
4. 如果改动影响构建，运行构建。
5. 按功能拆分 commit。
6. 更新本文件中与流程有关的规则。
7. 推送前再次确认没有提交密钥或临时文件。
