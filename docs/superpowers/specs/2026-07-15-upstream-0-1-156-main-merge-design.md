# 合并官方 upstream/main（0.1.156）设计

日期：2026-07-15
状态：设计确认，进入实施

## 1. 背景与证据基线

当前二开基线：

- 分支：`custom/prod`
- 提交：`e95abe6cb0f781045084065512786033169d6674`（上一轮 0.1.155 合并产物，已部署生产）
- 应用 VERSION：`0.1.155`

本轮冻结的官方目标：

- `upstream/main`：`be6cd1250c10ba2812305dcf071da133e4d738f6`
- 官方 VERSION：`0.1.156`；tag `v0.1.156`（`9cc1b469a`）是 tip 祖先，tip 另含 14 个 tag 后提交（Grok 自定义上游地址/请求头、OpenAI Agent Identity 导入、管理员充值返利、套餐币种）

三方 merge-base 为 `7c717365ef728e53cdcf6d639a4dd68226db03b2`（上一轮合并进来的 0.1.155 上游提交）。本轮范围 145 个提交、301 个文件（57 新增），`git merge-tree` 只读预演报告 **9 个内容冲突**。

## 2. 目标与非目标

### 目标

1. 将冻结的 `upstream/main@be6cd125` 合并进二开分支。
2. 保留全部 fork 定制（不变量见 §5）。
3. 吸收官方 0.1.156 及 tip 改动：Grok 生态（OAuth 刷新/池健康路由、免费额度函数缓存、视觉 image_url 桥接、图片模型 Responses 拦截、账号级自定义 base_url/请求头）、OpenAI Agent Identity 导入、账号一键复制（Duplicate + 幂等恢复）、client-gone failover 静默终止（499）、credential failure 客户端响应映射、Responses Lite 工具规格化、Read 工具流终结/参数流清洗、outbox degraded rebuild latch、xai unsafe base URL 拒绝、订阅套餐币种、管理员充值返利开关、Keys 列表 ID 列、前端 datatable row cache、web 指纹资源缓存等。
4. 代码、Ent 生成物、SQL migration、前后端类型、Docker 构建定义保持一致。
5. 通过后端、前端、生成器和构建门禁，并由独立审查代理审查合并结果。

### 非目标

1. 本轮实施阶段不推送 `origin`、不部署、不执行生产 migration（合并完成后按用户指示走部署流程）。
2. 不启用批量生图/队列/Vertex；默认开关保持关闭。
3. 不更新 Claude HTTP/TLS 抓包资料；profile 保持 2.1.206。
4. 不用 `--ours`/`--theirs`/整文件覆盖解决冲突。
5. 不改写历史 migration。

## 3. 合并策略

### 3.1 隔离与提交拓扑

- 工作树：`.worktrees/upstream-0.1.156-main-merge`
- 分支：`merge/upstream-0.1.156-main`（自 `custom/prod`）
- `git merge --no-ff --no-commit be6cd1250c10ba2812305dcf071da133e4d738f6`；merge commit 第一父为基线（含本设计与计划文档提交），第二父为 `be6cd125`。

### 3.2 九个文本冲突

后端 6 个：

1. `backend/internal/handler/admin/account_handler.go`：取并集。fork 侧：`PoolWeight` 字段（Create/Update 请求 + service 透传）、`anthropicSessionImportMu/Jobs/Active` 状态与构造器初始化；上游侧：`grokOAuthService service.GrokOAuthTokenService` 结构体字段 + 构造器参数、`Duplicate` handler（幂等 + 恢复）、`refreshSingleAccount` 的 Grok 刷新分支。构造器签名变化需与 `wire_gen.go`（自动合并）对齐验证。
2. `backend/internal/handler/failover_loop.go`：取并集。fork 的 `shouldClearStickySessionAfterFailover` 与上游的 client-gone 检查（`HandleFailoverError`/`HandleSelectionExhausted` 顶部 ctx 检查、`failoverClientGone`、gin import、499 标记）互不重叠，全部保留。注意上游在 `HandleFailoverError` 还新增了 `failoverErr == nil || !ShouldRetryNextAccount()` 的提前 FailoverExhausted 返回，一并吸收。
3. `backend/internal/handler/failover_loop_test.go`：双方新增测试取并集。
4. `backend/internal/handler/gateway_handler_chat_completions.go`：取并集。保留 fork 的 content-safety 前置检查与 streaming-aware 错误改造；吸收上游 loop 顶部 `ctx.Err()` 短路、`FailoverCanceled` 分支的 `failoverClientGone(c)`。`handleCCFailoverExhausted` 合成顺序：silent refusal（含 fork streaming 分支）→ fork streaming-aware exhausted → 上游 `copyFailoverRetryAfter` + credential-failure 映射（仅非流式路径，头部仍可写）→ 默认错误。
5. `backend/internal/handler/gateway_handler_responses.go`：与上条同构。
6. `backend/internal/service/domain_constants.go`：fork 侧是 gofmt 重排 + content-safety/mimicry 键；上游新增 `AdminRechargeRebateEnabledDefault` 常量与 `SettingKeyAffiliateAdminRechargeEnabled`。把上游两处新增并入 fork 版式（保持对齐）。

前端 3 个：

7. `frontend/src/components/account/CreateAccountModal.vue`：取并集。fork 侧：Anthropic session 批量导入 UI/逻辑；上游侧：`isHeaderOverridePlatform` → `isHeaderOverrideCapable(platform,'apikey')`、`HeaderOverrideJsonTools` 组件、`show-agent-identity-option`、`isAgentIdentityImportContent` 校验、antigravity `buildCredentials(tokenInfo, refreshTokens[i])` 第二参。
8. `frontend/src/components/account/OAuthAuthorizationFlow.vue`：取并集。fork 侧：`anthropicSessionBulkImport` 流程；上游侧：`showAgentIdentityOption` prop、agent_identity 单选与 codex_session 输入面板复用、`isAgentIdentityInput` 派生文案、`methodOptionCount` 计入新选项。
9. `frontend/src/views/admin/__tests__/SettingsView.spec.ts`：双方新增测试取并集（fork fast-policy payload 用例 + 上游 admin recharge rebate 用例）。

### 3.3 Ent 与生成物

上游 schema 仅改 `subscription_plan.go`（currency）。`ent/mutation.go`、`migrate/schema.go`、`runtime/runtime.go`、`wire_gen.go` 均自动合并成功，但必须以生成器复核：合并后从 `backend` 运行 `go generate ./ent` 与（如可用）wire 校验，要求无非预期 diff。fork 的 Group/Account/UsageLog 字段（video pricing、mixed-type weight、pool_weight、long_context_billing_applied）不受本轮影响。

### 3.4 快照与迁移

- `apiKeyAuthSnapshotVersion`：上游仍为 15，fork 为 16 且上游未触碰该文件——无撞车，无需动作，验证 v16 仍在即可。
- 官方新增 migration 仅 `177_add_subscription_plan_currency.sql`（新增列，非破坏）。fork 独有 `154_anthropic_mixed_type_weight_scheduling.sql` 保留；无 renumber。

## 4. 自动合并文件的语义审查重点

51 个双方共同修改的自动合并文件，P0 复核：

- `gateway_handler.go`（fork sticky/CountTokens/user_id 日志 vs 上游 Messages stream error surfacing）
- `wire_gen.go`（NewAccountHandler 新参数注入完整）
- `ent/*` 生成物（生成器幂等）
- `scheduler_cache.go`、`gateway_service.go`（fork mixed-pool 权重 vs 上游 outbox rebuild latch）
- settings 全链路 6 文件（fork fast-policy 原子写 + mimicry/content-safety vs 上游 admin recharge rebate）
- `AccountsView.vue`、`credentialsBuilder.ts`、`EditAccountModal.vue`（fork sessionKey 导入/池权重 vs 上游 header override capable/Grok base_url/一键复制入口）
- `openai_gateway_cc_pipeline.go`、`openai_chat_completions.go`（fork Fable prompt/messages 定制邻接区域）

上游零 diff 的 fork 核心文件（`gateway_forward.go`、`gateway_count_tokens.go`、`gateway_upstream_request.go`、`claude_code_profile.go`、`tlsmimic/`、`api_key_auth_cache*.go`）合并后必须与 `custom/prod` 完全一致。

## 5. Fork 不变量（与 0.1.155 轮相同）

1. `CLICurrentVersion=2.1.206`，profile `cc-2.1.206-sdk-cli-macos-arm64`。
2. Fable 5 专属 system expansion；operator override 优先；非 Fable 回退通用 prompt。
3. `/messages` 与 `/messages/count_tokens` 独立 beta、max_tokens、body defaults。
4. billing block `cc_version=2.1.206.{fp}; cc_entrypoint=sdk-cli;`。
5. TLS 常量与 `ResolveTLSProfileForClaudeMimic` 路径不变。
6. mixed-pool scheduler、runtime block、sticky 清理、Bedrock guard 有效。
7. Claude 5 whitelist/pricing、Anthropic session 批量导入、内容安全、公共 pool-health 有效。
8. fast policy 后端原子写、apicompat namespace tool_choice fallback、tool_search 撞名拒绝、快照 v16 有效。

## 6. 验证设计

- 静态：`git diff --check`；VERSION 精确为 `0.1.156`；`go generate ./ent` 幂等；不变量锚点检索全绿。
- 后端：Claude profile/TLS/count_tokens/scheduler/failover/Grok/agent-identity/apicompat focused tests → `go test -tags=unit ./... -count=1` 全量。Go 缓存 `/private/tmp/sub2api-go-build-cache`；httptest 回环端口受沙箱阻断时申请沙箱外重跑。
- 前端：受影响 spec 聚焦跑 → `pnpm test:run` 全量 → `pnpm build`。
- 审查：实现完成后固定 diff 包交独立审查代理；Critical/Important 必须修复并复审。

## 7. 失败处理与完成标准

- 全部在隔离工作树；禁 `git reset --hard`、`--ours`、`--theirs`。
- 完成标准：merge commit 第二父 = `be6cd125`；VERSION=0.1.156；9 冲突逐一按本设计解决；生成器幂等；不变量证据齐全；后端/前端/构建门禁通过；独立审查无 Critical/Important；主工作区干净。
