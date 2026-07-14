# Upstream 0.1.155 Main Merge Implementation Plan

**目标：** 将冻结的官方 `upstream/main@7c717365`（VERSION 0.1.155）合并进 `custom/prod`，逐一解决 11 个文本冲突，保留全部 fork 不变量，经分门禁验证与独立审查后交付。

**架构：** 在隔离 worktree 内做一次原子三方 merge。生成物（Ent、go.sum）用工具链重生成，逐块解决 11 个文本冲突，再分运行时、产品契约、迁移、全量回归四段门禁，每段独立审查。

**技术栈：** Git merge/worktrees、Go 1.26.5、Ent ORM、PostgreSQL SQL migrations、Vue 3 + TypeScript、Vitest、Vite、pnpm、Docker 多阶段构建。

## 全局约束

- 记录基线 `BASE_SHA` 为含本计划与设计文档提交的 `custom/prod` tip。
- 上游冻结于 `7c717365ef728e53cdcf6d639a4dd68226db03b2`，不静默前移。
- merge-base 为 `e316ebf52838a89d57fc790981cce7520f819ac8`。
- 最终 `backend/cmd/server/VERSION` 精确为 `0.1.155`。
- 保留：Claude Code 2.1.206、Fable 5/Sonnet 5、messages/count_tokens 分离、TLS mimic、mixed-pool 调度、Bedrock guard、Anthropic session 批量导入、内容安全、公共 pool-health、fast policy 后端原子写、apicompat namespace/tool_search 定制。
- 不用 `--ours`/`--theirs`/整文件覆盖/rebase/squash/`reset --hard`。
- 不推送、不部署、不跑生产迁移。
- `batch_image.enabled|queue_enabled|vertex_enabled` 维持代码默认 `false`。
- Go 命令统一 `GOTOOLCHAIN=go1.26.5`、`GOCACHE=/private/tmp/sub2api-go-build-cache`、`GOPATH=/private/tmp/sub2api-go-path`。
- 验证-only 任务不产生空提交，报告写入被忽略的 `.superpowers/sdd/`。

---

## Task 0：创建隔离工作树并记录基线

- 校验 `custom/prod` 干净，记录 `BASE_SHA`；确认 `.worktrees` 被 gitignore；`git rev-parse 7c717365`。
- `git worktree add .worktrees/upstream-0.1.155-main-merge -b merge/upstream-0.1.155-main "$BASE_SHA"`。
- 在 worktree 内 `backend` 跑 `go mod download`；前端 `pnpm install --frozen-lockfile`。
- 跑 fork 基线聚焦测试（claude profile、mimic、count_tokens、mixed-pool、Bedrock、fast policy locale、SettingsView、useModelWhitelist）。
- 写 `.superpowers/sdd/upstream-155-task-0-baseline.md`（无提交）。

## Task 1：原子合并并解决 11 个冲突

- `git merge --no-ff --no-commit 7c717365`，确认冲突清单正好为 11 个预期文件。
- 合并 Ent schema 源（`group.go`/`account.go`/`usage_log.go`）取并集，`go generate ./ent` 重生成，替换 `ent/mutation.go` 等生成物冲突。
- auth 快照升 v16：`api_key_auth_cache.go` 字段并集、`api_key_auth_cache_impl.go` 版本号与 from/to 双向赋值；补旧快照拒绝回归测试。
- 后端 handler 冲突取并集（account/gateway/apicompat bridge），保留 fork 行为 + 吸收上游行为。
- 前端 5 个冲突：SettingsView 换上游 fast policy 组件并保留 fork 定制；CreateAccountModal/OAuthAuthorizationFlow 取并集并统一 `initialInputMethod`；en/zh settings locale 取上游 fast policy 键 + 保留 fork 键。
- 引入上游新文件（`responses_namespace.go`、`grok_import_probe.go`、`OpenAIFastPolicyUserSelector.vue` 等），删除 fork `openAIFastPolicyLocales.spec.ts`（大写）。
- `go.mod` 合并后 `go mod tidy`；清除所有冲突标记；`gofmt`。
- 跑聚焦冲突契约测试（snapshot v16、429、mimic、count_tokens、mixed-pool、namespace bridge、fast policy locale/组件、useModelWhitelist）。
- 暂存全部结果，`git commit -m "chore: merge upstream 0.1.155"`，校验第一父=BASE_SHA、第二父=7c717365、VERSION=0.1.155。
- 写 `.superpowers/sdd/upstream-155-task-1-report.md`，生成审查包做两段审查。

## Task 2：网关运行时顺序与 fork 不变量审查

- 对比自动合并的 gateway/handler/scheduler/rate-limit/apicompat/billing 文件与两父，复核调用顺序（严格客户端校验、lenient JSON、content safety、slot 获取、body rewrite、stream commit、in-band error、failover、pool retry count、cooldown）。
- 锚点检索确认 2.1.206、Fable、count_tokens 分离、TLS mimic、mixed-pool、Bedrock 均有生产码与测试。
- 跑聚焦运行时套件（claude、tlsfingerprint、gateway routes messages/count_tokens、repository dispatch/scheduling、service Claude/Mimic/CountTokens/ContentSafety/Runtime429/MixedTypeWeight/Bedrock/Failover/namespace/long-context）。
- 写 `.superpowers/sdd/upstream-155-task-2-runtime-audit.md`；语义违规则停并开一个有失败测试的修复任务。

## Task 3：Group/缓存/模型定价/账号/路由/Docker 产品契约与 fast policy 审查

- `go generate ./ent` 幂等校验；`git diff --exit-code`。
- 校验 snapshot v16 三组字段在类型、写入、恢复、round-trip 测试均出现。
- pricing JSON 可解析；Claude/Fable/GPT-5.6/Grok 模型并集覆盖。
- 校验 sessionKey 批量导入 + 上游 Grok 导入/probe/long-context 校验共存；`/pool-health` 保留；AccountsView 逐块复核。
- 验证 fast policy：上游 UserSelector 组件 + fork 后端原子写 + locale 结构。
- 跑产品契约后端测试 + 前端相关测试 + `pnpm build`；`Dockerfile`/`deploy/Dockerfile` 锚点校验。
- 写 `.superpowers/sdd/upstream-155-task-3-product-contract.md`。

## Task 4：官方迁移与默认关配置验证

- 证明旧 migration 未被改写；新增仅 174/174/174/175/175/175a/176 + 3 测试；fork 154 保留。
- 迁移测试 PASS（long-context billing、api_key IP index、channel monitor grok、request type、config batch-image 默认 false）。
- 写 `.superpowers/sdd/upstream-155-task-4-migrations.md`（无提交）。

## Task 5：全量回归、构建与最终审查

- `go test -tags=unit ./... -count=1`；`pnpm test:run`；`pnpm build`。
- `-tags=embed` 构建二进制并 `--version` 校验为 `Sub2API 0.1.155`。
- 校验 git 干净、父提交来源、VERSION。
- 生成固定 diff 包做独立整分支审查；Critical/Important 修复复审。
- 写 `.superpowers/sdd/upstream-155-task-5-final-verification.md`。
- 交付选项：本地合并回 `custom/prod` / 推送开 PR / 保留分支 / 丢弃。未经授权不推送、不部署。
