# 合并官方 upstream/main（0.1.155）设计

日期：2026-07-14
状态：设计已确认，进入实施计划

## 1. 背景与证据基线

当前二开基线：

- 分支：`custom/prod`
- 提交：`b974a5fd595c787c4687316316eef5ab183cddbf`
- 应用 VERSION：`0.1.151`（`backend/cmd/server/VERSION`）
- 远端：`origin/custom/prod` 与本地 HEAD 一致

本轮冻结的官方目标：

- `upstream/main`：`7c717365ef728e53cdcf6d639a4dd68226db03b2`
- 官方 VERSION：`0.1.155`（提交信息 `chore: sync VERSION to 0.1.155 [skip ci]`）
- 覆盖官方 0.1.152、0.1.153、0.1.155 三个版本

三方合并基点（merge-base）为 `e316ebf52838a89d57fc790981cce7520f819ac8`，即上一轮合并进来的 0.1.151 上游提交。相对该基点，本轮范围约 152 个提交、500 个文件。`git merge-tree --write-tree` 只读预演报告 **11 个内容冲突**。

## 2. 目标与非目标

### 目标

1. 将冻结的 `upstream/main@7c717365` 合并进二开分支。
2. 保留全部 fork 定制：Claude Code 2.1.206 profile、Fable 5/Sonnet 5、messages/count_tokens 分离、TLS mimic、mixed-pool 调度、Bedrock guard、Anthropic session 批量导入、公共 pool-health、内容安全、fast policy 后端原子写、apicompat namespace/tool_search 定制。
3. 吸收官方 0.1.152–0.1.155 的改动：Grok 生态（API Key 账号、OAuth 路由、免费额度探测、Web SSO 导入、视频编辑/扩展、channel monitor grok provider、监控 UI）、OpenAI 长上下文计费开关与网页搜索按次计费、Responses/namespace tools 兼容、Codex 连接与调度稳定性、调度器 rebuild 合并与 outbox lag 修复、ops 系统日志 host 过滤、admin server timing metrics、Apple Container 部署。
4. 让代码、Ent schema、生成文件、SQL migration、前后端类型和 Docker 构建定义保持一致。
5. 通过后端、前端、生成器、JSON 和构建门禁，并由独立审查代理审查合并结果。

### 非目标

1. 本轮不推送 `origin`，不部署生产，不执行生产数据库 migration。
2. 不启用批量生图、队列或 Vertex；相关默认开关继续保持关闭。
3. 不顺带更新 Claude HTTP/TLS 抓包资料；HTTP profile 保持 2.1.206。
4. 不用 `--ours`、`--theirs` 或整文件覆盖解决冲突。
5. 不改写历史 migration，不修改生产 `.env`、数据库、卷或服务。

## 3. 合并策略

### 3.1 隔离与提交拓扑

从记录基线 `custom/prod` 创建：

- 工作树：`.worktrees/upstream-0.1.155-main-merge`
- 分支：`merge/upstream-0.1.155-main`

使用普通 merge commit 合并冻结 ref：

```bash
git merge --no-ff --no-commit 7c717365ef728e53cdcf6d639a4dd68226db03b2
```

不 rebase、不 squash 官方历史，便于以后计算新 merge-base、审计官方来源和增量同步。最终 merge commit 第一父为记录的 `BASE_SHA`（含本设计与计划两份文档提交），第二父为 `7c717365`。

### 3.2 十一个文本冲突

后端 6 个：

- `backend/ent/mutation.go`：生成物，不手改；先合并 schema 源再 `go generate ./ent`。合并后的 `GroupMutation` 必须同时包含 fork 的三个 mixed-type weight 字段、既有 video pricing 字段与上游 `web_search_price_per_call`；`AccountMutation` 含 `pool_weight`（fork）；`UsageLogMutation` 含 `long_context_billing_applied`（上游）。
- `backend/go.sum`：合并 `go.mod`（保留 fork direct deps + 吸收上游新 require），删冲突 `go.sum` 后 `go mod tidy`，`go build ./...` 校验。
- `backend/internal/handler/admin/account_handler.go`：取并集。保留 fork 的 Anthropic session 批量导入 state（`anthropicSessionImportMu/Jobs/Active`）与 `PoolWeight` 全链路；吸收上游 `grokImportProber`（新文件 `grok_import_probe.go`）、`ValidateOpenAILongContextBillingExtra`、`scheduleGrokImportProbe`；核对 wire/DI 注入。
- `backend/internal/handler/gateway_handler.go`：取并集。保留 fork content-safety guard、`shouldPrewriteSSEWhileWaitingForSlot`、`clearStickySessionAfterFailover`、CountTokens 分离与 user_id 日志；`HandleFailoverError` 补上游 `account.GetPoolModeRetryCount()` 参数；吸收订阅响应 `weekly_window_start`。
- `backend/internal/pkg/apicompat/chatcompletions_responses_bridge.go`：取并集。以上游 `EffectiveResponsesTools(req)`（含 input 内 `additional_tools`）为工具来源，保留 fork 的 `responsesNamespaceToolChoice` 过滤与 `!hasNamespaceChoice` guard，让 namespace 逻辑覆盖 `effectiveTools`；保留 tool_search 撞名显式拒绝；上游新增 `responses_namespace.go` 等文件随合并引入。
- `backend/internal/service/api_key_auth_cache_impl.go`（+ `api_key_auth_cache.go`）：版本撞车（双方均 15，字段集不同）。升为 **16**，快照结构体与 `snapshotFromAPIKey`/`snapshotToAPIKey` 同时包含 fork 三个 mixed-type weight 字段、既有 video pricing 与上游 `WebSearchPricePerCall`；补 v15/v16 拒绝旧快照回归测试。

前端 5 个：

- `frontend/src/views/admin/SettingsView.vue`：fast policy 模板改用上游组件 `OpenAIFastPolicyUserSelector`；删除 fork 手工 ID 模板与 `add/remove/normalizeOpenAIFastPolicyUserID*`、`OpenAIFastPolicyRuleForm` 草稿类型；保留 `openaiFastPolicyLoaded` 守卫与 fork 其它全部定制（mimicry profile/guard mode、system prompt 模板变量化、content safety 接线等）。
- `frontend/src/components/account/CreateAccountModal.vue`：取并集。账号名必填合并为 `!isAnthropicSessionImportMode && !isGrokSSOInputMethod`；保留 fork 批量导入 UI/进度与上游 Grok API Key/SSO、long-context billing 开关；对 `OAuthAuthorizationFlow` 传参改用上游命名。
- `frontend/src/components/account/OAuthAuthorizationFlow.vue`：取并集。统一 prop 为上游 `initialInputMethod`（弃 `defaultInputMethod`），保留 fork `anthropicSessionBulkImport` 逻辑 + 上游 Grok SSO UI（`showSsoOption`/`methodOptionCount` 等），同步更新其 spec。
- `frontend/src/i18n/locales/en/admin/settings.ts` 与 `zh/admin/settings.ts`：`openaiFastPolicy` 段采用上游搜索型键（`userSearchPlaceholder`/`userSearchEmpty`/`userDeleted`/`userIdFallback`/`removeUser`），删除 fork 手工键；保留 fork `claudeCodeMimicryProfile*` 等键与「userIds 只在 openaiFastPolicy、不在 betaPolicy」的结构修复。

### 3.3 Ent 生成规则

先合并生成源取并集：

- `backend/ent/schema/group.go`：video pricing + mixed-type weight ×3（fork）+ `web_search_price_per_call`（上游）
- `backend/ent/schema/account.go`：`pool_weight`（fork）
- `backend/ent/schema/usage_log.go`：`long_context_billing_applied`（上游）

再从 `backend` 运行 `go generate ./ent`，检查全部伴生生成文件。SQL migration 不由 Ent 生成器替代，也不得因生成结果而删除。

### 3.4 Fast policy 调和

- 保留 fork 后端原子写：`cd468204` 控制面接线 + `2c3efba7` 的 `UpdateSettingsWithAuthSourceDefaults(..., fastPolicy)` 三参 `SetMultiple`；handler 不再单独调 `SetOpenAIFastPolicySettings`。
- 引入上游 `frontend/src/views/admin/settings/OpenAIFastPolicyUserSelector.vue` 及其 spec、`openaiFastPolicyLocales.spec.ts`（小写）。
- 删除 fork `openAIFastPolicyLocales.spec.ts`（大写），其 betaPolicy 隔离断言并入上游 locale 测试；改造 `SettingsView.spec.ts` 手工 ID 用例。

## 4. 自动合并文件的语义审查

Git 未报冲突不代表语义安全。重点复核：

### P0 网关运行时顺序

`gateway_handler*.go`、`gateway_forward.go`、`gateway_count_tokens.go`、`gateway_upstream_request.go`、`gateway_scheduling.go`、`ratelimit_service.go`、runtime settings。必须同时成立：upstream lenient JSON、SSE heartbeat、in-band error、failover、pool retry count；fork content safety、pre-slot 不提前 flush、sticky failover 清理、Claude mimic body/system/metadata/TLS 顺序与 count_tokens 分离。`gateway_forward.go`/`gateway_count_tokens.go`/`gateway_upstream_request.go` 上游零 diff，保留 fork。

### P0 Group、缓存与调度契约

schema、Ent 生成物、repository、DTO、admin service、cache snapshot、前端 Group 类型和表单必须同时携带 mixed-pool、video pricing 与 web_search_price_per_call 字段。调度器保留 setup-token/API-key 分池权重、runtime block、sticky 清理与 Bedrock guard，并吸收 upstream scheduler rebuild coalesce 与 outbox lag 修复。`scheduler_snapshot_service.go` 上游有实质改动需复核。

### P0 模型、定价与构建

Claude Sonnet/Fable 5 whitelist 与定价、GPT-5.6 与 Grok 4.5/视频 aliases/pricing/usage 取并集；`Dockerfile`/`deploy/Dockerfile` 保持 Go 1.26.5、`resolve-version.sh`、VERSION/COMMIT ldflags 与 `-trimpath`（双方一致，无冲突）。

### P1 账号管理与路由

保留 sessionKey 批量导入 API/类型/UI/中英文 i18n，同时吸收上游 Grok API Key/OAuth/SSO 导入、OpenAI long-context billing 校验、Grok import probe；`AccountsView.vue`（sessionKey 导入 UI vs 上游虚拟化/plan type 展示）为高冲突自动合并文件，须逐块复核；fork `/pool-health` 路由不得丢失；上游删除的 payment 废弃接口按上游处理。

## 5. Fork 不变量

合并结果必须满足：

1. `CLICurrentVersion=2.1.206`，profile 为 `cc-2.1.206-sdk-cli-macos-arm64`。
2. Fable 5 专属 system expansion；operator override 优先；非 Fable 回退通用 prompt。
3. `/messages` 与 `/messages/count_tokens` 使用独立 beta、max_tokens、body defaults；count_tokens 保持 token-counting beta。
4. billing block 保持 `cc_version=2.1.206.{fp}; cc_entrypoint=sdk-cli;`。
5. TLS 常量与 `ResolveTLSProfileForClaudeMimic` 选择路径保持现状。
6. mixed-pool scheduler、runtime block、sticky 清理与 Bedrock guard 保持有效。
7. Claude 5 whitelist/pricing、Anthropic session 批量导入、内容安全、公共 pool-health 保持有效。
8. fast policy 后端原子写与 apicompat namespace tool_choice fallback / tool_search 撞名拒绝保持有效。

## 6. 官方迁移与配置风险

官方新增 migration：`174_add_usage_log_long_context_billing.sql`、`174_add_usage_logs_api_key_latest_ip_index_notx.sql`、`174_group_web_search_price_per_call.sql`、`175_add_ops_system_logs_host.sql`、`175_default_openai_long_context_billing.sql`、`175a_add_ops_system_logs_host_index_notx.sql`、`176_channel_monitor_grok_provider.sql`，以及 3 个迁移测试 go 文件。fork 独有 `154_anthropic_mixed_type_weight_scheduling.sql` 保留，字母序无冲突、无需 renumber。上游未改写任何已发布 migration。`batch_image.enabled`、`queue_enabled`、`vertex_enabled` 继续使用代码默认 false。实施阶段只做静态和测试验证，不在生产执行。

## 7. 验证设计

### 静态与生成门禁

- `git diff --check`
- VERSION 精确为 `0.1.155`
- `go generate ./ent` 后无非预期生成差异
- pricing JSON 可解析
- 保护锚点检索覆盖 2.1.206、Fable、count_tokens、TLS、mixed-pool、Bedrock、sessionKey 导入、pool-health

### 后端门禁

- Claude profile 与 TLS 包测试
- messages/count_tokens route 与 repository cache 测试
- scheduler、429、Bedrock、pricing、GPT-5.6、Grok、namespace、long-context focused tests
- `go test -tags=unit ./internal/service -count=1`
- 最终 `go test -tags=unit ./... -count=1`

Go 缓存使用 `/private/tmp/sub2api-go-build-cache`。如仅因沙箱禁止 `httptest` 绑定回环端口失败，申请沙箱外重跑；不得把环境失败写成代码通过。

### 前端门禁

- model whitelist、批量导入 locale、Claude profile locale、fast policy locale、SettingsView、受 upstream 影响的新增前端测试
- `pnpm test:run`
- `pnpm build`

### 审查门禁

每个冲突解决任务先做规格审查，再做代码质量审查。所有实现完成后生成固定 diff 包，交给全新代理做整分支审查。Critical/Important 问题必须修复并复审。

## 8. 失败处理与回滚

- 所有实施在隔离工作树进行；主分支和生产保持不变。
- 冲突解决、Ent 生成或测试失败时保留工作树与证据，不自动丢弃改动。
- 不使用 `git reset --hard`、`git checkout --ours`、`git checkout --theirs`。
- 合并完成后由用户选择本地合并、推送 PR、保留分支或丢弃；未经授权不推送、不部署。

## 9. 完成标准

1. merge commit 的第二父提交精确为 `7c717365`。
2. VERSION 为 `0.1.155`，migration 174–176 完整且历史 migration 未被改写。
3. 十一个冲突按本设计逐一解决，Ent 生成结果一致，snapshot 版本为 16。
4. 自动合并文件完成语义复核，fork 不变量全部有测试或静态证据。
5. 后端、前端和构建门禁通过，或环境阻断被精确区分并补充可验证证据。
6. 独立整分支审查无 Critical/Important 问题。
7. 主工作区保持干净；无推送、无部署。
