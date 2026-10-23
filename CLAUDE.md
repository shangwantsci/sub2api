# sub2api fork：给 AI 协作者的项目规则

以代码为准。这里只放「改代码之前必须知道的事」；功能清单见 `docs/FEATURES.md`，当前行为见 `docs/CURRENT_BEHAVIOR.md`。文档与代码不一致时，改文档，不要信文档。

## 这是什么

官方开源项目 sub2api（Wei-Shaw/sub2api）的私有 fork：把 Claude / OpenAI / Gemini 等订阅账号聚合成 API 中转站。后端 Go（`backend/`，Gin + Ent + Wire + Redis + PostgreSQL），前端 Vue3（`frontend/`）。fork 自有的东西集中在三块：Claude 防封（容量与节流、请求伪装、养号）、供号商上号站点（Provider Portal）、一批网关修复。官方没有的功能，改之前先查 `docs/FEATURES.md`。

当前没有线上部署，这是一个训练项目。本地标签 `archive/pre-cleanup-20261022` 是「项目恢复有序」整理之前的完整状态，被移除的功能都能从那里找回。

## 本机命令与环境限制

- 后端：`cd backend && go build ./... && go vet -tags unit ./... && go test -tags unit ./... -count=1`。**service 包测试必须带 `-tags unit`**，否则报 `mockClaudeOAuthClient undefined`（上游既有问题）。
- 前端：本机没有 pnpm，直接用 `frontend/node_modules/.bin/`：`vue-tsc --noEmit`、`eslint . --ext .vue,.js,.ts`、`vitest run`。根目录 `Makefile` 的 `FRONTEND_CRITICAL_VITEST` 是关键 spec 清单（改了账号弹窗、设置页、供货商面板就确认相关 spec 都在里面）。
- 本机没有 docker / golangci-lint / redis-server：testcontainers 集成测试跑不了。但能起一套**真实的栈**：临时 PostgreSQL + 用 Go 库 miniredis 起的 Redis 兼容服务 + 真实后端进程（再加 Vite 开发服务器与无头 Chrome 就能点界面），配方见 `docs/CURRENT_BEHAVIOR.md` 第 6 节。改了接口形状、设置、账号字段、迁移、供货商流程之后建议走一遍；走不了就在汇报里明说「没有在真实库上验证」「界面未实际点过」。
- **已知既有失败**（不是你引入的，别当成回归）：后端 `internal/handler` 的 `TestGatewayHandlerMessages_InterceptWarmup_AntigravityAccount_MixedSchedulingV1` 与 `..._ForcePlatform`；前端 `src/features/channel-monitor-v2/__tests__/designSystem.structure.spec.ts` 的 2 个用例。除此之外全绿，新增失败就是你引入的。
- 本机 `grep` 遵守 `.gitignore`：`docs/*` 被忽略规则覆盖，用 `grep -r` 会漏掉。残留检查用 `git grep`（只查已跟踪文件；刚建、未跟踪的新文件用 `grep -n 文件`）。
- **代码生成**：改 Ent schema 用 `cd backend && go generate ./ent`。**不要跑 `make -C backend generate`，也不要直接用 wire 重新生成**：`backend/cmd/server/wire_gen.go` 里有手工维护的行（`BindCompositeRouteRepository` / `BindAdminCompositeRoutes`），重新生成会静默删掉它们，编译和单测都不报错，运行时复合分组路由绑定却失效。改了构造函数签名就手改 `wire_gen.go`，可以先重新生成到别处做交叉 diff；`cmd/server/wire_gen_test.go` 里有按位置传参的 `nil`，要同步。
- **迁移**（`backend/migrations/`，下一个编号 228）：runner 以文件名为主键并校验 SHA256，**已应用的迁移文件不能再改**；必须幂等；不要在同一文件里写 Down。绕过应用层的 `UPDATE accounts` 要同时写 `scheduler_outbox`（`account_changed`），否则调度快照不会刷新。写新迁移先在临时 PostgreSQL 上彩排，方法见 `docs/CURRENT_BEHAVIOR.md`。

## 铁律（每一条都是踩过的坑）

1. **一个概念一个入口。** 新增设置、开关、字段之前先查 `docs/FEATURES.md`：已有同类就扩展它，别另起一套。同一个数值只能在一处配置（容量数值只在账号字段里，档位只是复制写入的模板）。
2. **选号阶段读的 extra 键必须登记。** 调度快照里的账号是精简副本，白名单就是 `service.SchedulerProjectedExtraKeys()`（`backend/internal/service/scheduler_projection_keys.go`）。选号、候选过滤、拿槽位用到的任何新 extra 键，必须登记，并在 `backend/internal/repository/scheduler_projection_contract_unit_test.go` 加用例。否则缓存命中时读到空值，限流静默失效，而单测直接构造完整账号，全绿也抓不到。**只选部分列的专用查询同理**（例如公开号池健康页用的 `ListPoolHealthAccounts`，曾因复用运维面板的窄查询缺列，真实库里账号数恒为 0）：计算器新读 `Account` 的字段，要同步加进 SELECT 的列，并用真实 repository + SQLite 内存库的测试守住（`backend/internal/repository/ops_pool_health_repo_unit_test.go` 是范例）；用 stub 直接返回完整账号的测试管不到这件事。
3. **容量键只在一个地方读写。** 前端容量相关 extra 键的字面量只允许出现在 `frontend/src/components/account/capacityBuilder.ts`；新建 / 编辑 / 批量 / 供货商建号都走它，不要复制键列表。新建弹窗只有一个漏斗函数 `buildAnthropicOAuthExtra`，三条授权路径各调一次，源码守卫 spec（`accountCapacity.guard.spec.ts`）会盯着。
4. **删功能 = UI + API + 配置 + 测试 + 文档一起删**，并更新 `docs/FEATURES.md` 的「已移除」。留下读不到的开关、没人调用的函数就是死代码。残留用 `git grep` 查；不可达函数用 `cd backend && go run golang.org/x/tools/cmd/deadcode ./cmd/server` 扫（输出里约 300 条是官方代码的遗留，只看自己改动相关的；先判断是不是只被测试用、是不是走接口调用，别直接删）。
5. **设置项必须有读取点。** 没有读取点的设置不要加；加了设置，要么界面里有入口，要么在登记表里写明为什么没有。
6. **网关 / 调度改动先写会失败的测试，再修**（先红后绿），并做一次变异检验：把修复临时去掉，测试必须变红，然后恢复。
7. **同一逻辑在多个调用点各写一份是最大的坑。**（请求体 normalize 有三个调用点，账号创建有三条授权路径，都踩过。）改之前先找全调用点，优先收成一个函数 + 一个守卫测试。
8. **文档以代码为准，不写流水账。** 不要新增「部署实况」「合并记录」「设计稿」类长文档；行为说明写进 `docs/CURRENT_BEHAVIOR.md`，功能写进登记表。
9. **小步提交，一次一个主题**，提交信息写清楚为什么。不要 push，除非被明确要求。

## 一次改动做完前的检查清单

- 跑上面的后端 / 前端命令，对照「已知既有失败」。
- 改了 API 形状：前后端字段名逐字对照（两边 `git grep`），写契约测试。
- 改了设置或功能：更新 `docs/FEATURES.md`。
- 改了选号阶段读取的字段：确认登记表与契约测试。
- 改了迁移：临时 PostgreSQL 上彩排（含重复执行与空库跑全链）。
- 改了管理端接口、设置、账号字段或供货商流程：跑一遍本地全栈冒烟（`docs/CURRENT_BEHAVIOR.md` 第 6 节），至少调到你改的那些接口；单测全绿不等于真实库里能用。
- 临时文件放 scratchpad，结束清理；不留 `zz_*` 测试文件、备份文件。

## 目录速览

- `backend/internal/handler/`：HTTP 处理器（`admin/` 管理端，`provider/` 供货商前台）。
- `backend/internal/service/`：业务逻辑。选号 `gateway_scheduling.go`，降速 `account_throttle.go`，容量档位模板 `capacity_presets.go`，选号快照登记表 `scheduler_projection_keys.go`，供货商 `provider_*.go`，养号 `account_nurturing_service.go`。
- `backend/internal/repository/`：数据访问与 Redis 缓存（调度快照在 `scheduler_cache.go`）。
- `backend/internal/server/`：路由与中间件。`backend/migrations/`：SQL 迁移。`backend/ent/`：Ent schema 与生成代码。
- `frontend/src/components/account/`：账号弹窗与容量卡（`capacityBuilder.ts`、`AccountCapacityFields.vue`）；`frontend/src/composables/`：`useAccountCapacity.ts`、`useCapacityPresets.ts`；`frontend/src/views/admin/settings/`：设置页的独立卡片；`frontend/src/i18n/locales/{zh,en}/`：词条（zh 与 en 必须同步，文案里不要出现裸的 `{`、`@`、`|`）。
- `tools/cc-calibrate/`：对真实 Claude Code 客户端抓包标定请求形态；`tools/account-metrics/`：账号寿命与并发基线查询。
