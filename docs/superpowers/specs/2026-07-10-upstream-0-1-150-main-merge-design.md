# 合并官方 upstream/main（0.1.150）设计

日期：2026-07-10
状态：设计已确认，等待书面规格复核后进入实施计划

## 1. 背景与证据基线

当前二开基线为：

- 分支：`custom/prod`
- 提交：`498903dea914ea74c8568842fde39ba9158dd188`
- 应用 VERSION：`0.1.146`
- 远端：`origin/custom/prod` 与本地 HEAD 一致

本轮冻结的官方目标为：

- `upstream/main`：`6dd3274aafbc1a7a91304380fb3d7e50406841e0`
- 官方 VERSION：`0.1.150`
- 最近正式 tag：`v0.1.150`，提交 `0dec1ad2922ff8c9d27b67f8a31dfb35bce1902b`

不能只按 tag 名称判断版本。`v0.1.150` 内的 `backend/cmd/server/VERSION` 仍为 `0.1.149`，且缺少 tag 后补入的 migration 173。`upstream/main@6dd3274a` 才同时具备 VERSION `0.1.150`、数据库 `request_type=4` 约束迁移、setup-token 自动刷新、Codex `image_gen` strip 补强和 GPT-5.6 计费完整性修复。

合并基点为 `6f43986c376d76144cb39c7a562c179e19ac7439`。相对该基点，fork 修改 153 个文件，upstream 修改 278 个文件，双方交集 45 个文件。`git merge-tree` 对 tag 与 main 均预报相同的 6 个内容冲突，因此选择 main 不增加文本冲突成本。

## 2. 目标与非目标

### 目标

1. 将冻结的 `upstream/main@6dd3274a` 合并进二开分支。
2. 保留 fork 的 Claude Code 2.1.206、Fable 5、TLS、计费元数据、调度器、Bedrock、批量账号导入和内容安全等定制。
3. 吸收官方 0.1.147–0.1.150 的安全、协议、账单、批量生图、Grok 4.5、GPT-5.6 和版本回退改动。
4. 让代码、Ent schema、生成文件、SQL migration、前后端类型和 Docker 构建定义保持一致。
5. 通过后端、前端、生成器、JSON 和构建门禁，并由独立审查代理审查合并结果。

### 非目标

1. 本轮不推送 `origin`，不部署生产，不执行生产数据库 migration。
2. 不启用批量生图、队列或 Vertex；相关默认开关继续保持关闭。
3. 不顺带更新 Claude HTTP/TLS 抓包资料；HTTP profile 保持 2.1.206，TLS 保持已验证的 2.1.195-shaped ClientHello。
4. 不用 `--ours`、`--theirs` 或整文件覆盖解决冲突。
5. 不改写历史 migration，不修改生产 `.env`、数据库、卷或服务。

## 3. 合并策略

### 3.1 隔离与提交拓扑

从 `custom/prod@498903de` 创建：

- 工作树：`.worktrees/upstream-0.1.150-main-merge`
- 分支：`merge/upstream-0.1.150-main`

使用普通 merge commit 合并冻结 ref：

```bash
git merge --no-ff 6dd3274aafbc1a7a91304380fb3d7e50406841e0
```

不 rebase、不 squash 官方历史，便于以后计算新 merge-base、审计官方来源和增量同步。

### 3.2 六个文本冲突

#### `backend/ent/group.go`

保留双方字段并集：

- fork：三个 Anthropic mixed-pool 字段；
- upstream：五个 Grok video pricing 字段。

结构体、扫描、赋值和 String 输出均需包含两组字段。该文件是生成物，最终结果以合并后的 schema 重新生成结果为准。

#### `backend/ent/mutation.go`

合并后的 `GroupMutation` 必须包含基础 42 个字段、3 个 mixed-pool 字段和 5 个 video pricing 字段，最终容量为 50。所有 Set/Get/Old/Add/Clear/Reset/Field 分派必须由 Ent 生成器统一生成，不能只手改冲突块。

#### `backend/internal/service/api_key_auth_cache_impl.go`

认证快照同时保存：

- 三个 Anthropic mixed-pool 字段；
- 五个 Grok video pricing 字段。

`apiKeyAuthSnapshotVersion` 从双方各自使用的 14 升为 15，避免部署前由任一旧分支写入的 v14 L2 快照被错误接受并静默补零。

#### `backend/internal/service/gateway_forward.go`

保留 fork 的严格 Claude Code 判定、全模型 system rewrite、mimic body defaults、fingerprint、metadata/session 处理和 `ResolveTLSProfileForClaudeMimic`；同时吸收 upstream 对 `identityService` 和 `*gin.Context` 的 nil 防护。不得恢复仅凭 UA 或 metadata fallback 判定真实 Claude Code 的路径。

#### `backend/internal/service/ratelimit_service.go`

保留 fork 的 `applyAnthropic429RuntimeCooldown`：无 reset 的 Anthropic 429 只进入 runtime/temp-cache cooldown，不持久化为数据库长期 rate limit。不得与 upstream `apply429FallbackRateLimit` 叠加。自动合并后的 `rate_limit_429_cooldown_test.go` 中 DB=0 与 DB=1 的互斥断言必须统一为最终 runtime-only 语义。

#### `frontend/src/composables/__tests__/useModelWhitelist.spec.ts`

保留测试并集：fork 的 Sonnet 5 preset，以及 upstream 的 GPT-5.6、Grok 4.5、alias 和 Composer 测试。实现侧同时保留 Claude 5 与官方新模型。

### 3.3 Ent 生成规则

先确认以下生成源取并集：

- `backend/ent/schema/group.go`
- `backend/ent/schema/usage_log.go`
- migration 154 mixed-pool
- migration 170 Grok video pricing

再从 `backend` 运行：

```bash
go generate ./ent
```

检查所有伴生生成文件。SQL migration 不由 Ent 生成器替代，也不得因生成结果而删除。

## 4. 自动合并文件的语义审查

Git 未报告冲突不代表语义安全。45 个重叠文件按以下门禁复核。

### P0：网关运行时顺序

重点文件包括 gateway handlers、`gateway_forward.go`、`gateway_scheduling.go`、`ratelimit_service.go` 和 runtime settings。必须同时成立：

- upstream lenient JSON 限制、SSE heartbeat、in-band error 和 failover；
- fork content safety、pre-slot 不提前 flush、sticky failover；
- Claude mimic body/system/metadata/TLS 的原有执行顺序。

### P0：Group、缓存与调度契约

schema、Ent 生成物、repository、DTO、admin service、cache snapshot、前端 Group 类型和表单必须同时携带 mixed-pool 与 video pricing 字段。调度器必须保留 setup-token/API-key 分池权重、runtime block、sticky 清理和 Bedrock account guard，并吸收 upstream 凭证 hydration 修复。

### P0：模型、定价与构建

以下内容必须取并集：

- Claude Sonnet/Fable 5 whitelist 与定价；
- GPT-5.6 与 Grok 4.5 aliases、pricing 和 usage；
- `deploy/Dockerfile` 的 Go `1.26.5`、`resolve-version.sh`、VERSION/COMMIT ldflags 和 `-trimpath`。

### P1：批量账号导入与路由

保留 sessionKey 批量导入 API、类型、UI 和中英文 i18n，同时吸收 upstream CRS 180 秒 timeout、Grok OAuth 文案、payment/risk-control guard；fork `/pool-health` 路由不得丢失。

## 5. Fork 不变量

合并结果必须满足：

1. `CLICurrentVersion=2.1.206`，profile 为 `cc-2.1.206-sdk-cli-macos-arm64`。
2. Fable 5 仅在 messages synthetic mimic 使用专属 system expansion；operator override 优先；非 Fable 回退通用 prompt。
3. `/messages` 与 `/messages/count_tokens` 使用独立 beta、max_tokens 和 body defaults；count_tokens 保持 token-counting beta 与通用 expansion。
4. billing block 保持 `cc_version=2.1.206.{fp}; cc_entrypoint=sdk-cli;`，最终 UA 与 billing 版本一致。
5. TLS 常量与选择路径保持现状。
6. mixed-pool scheduler、runtime block、sticky 清理和 Bedrock guard 保持有效。
7. Claude 5 whitelist/pricing、批量账号导入、内容安全和公共 pool-health 保持有效。

## 6. 官方迁移与配置风险

官方更新新增 migration 159–173。实施阶段只做静态和测试验证，不在生产执行。重点核对：

- 159 创建 batch image 表；
- 两个 160 按完整文件名独立记录；
- 161–170 增加批量生图、分组和 Grok video 字段；
- 171–173 调整 usage constraint，其中 173 允许 `request_type=4`。

不得修改已经发布 migration 的内容。`batch_image.enabled`、`queue_enabled`、`vertex_enabled` 继续使用代码默认 false；不能把开发 compose 的启用值带入生产配置。

## 7. 验证设计

### 静态与生成门禁

- `git diff --check`
- VERSION 精确为 `0.1.150`
- `go generate ./ent` 后无非预期生成差异
- pricing JSON 可解析
- 保护锚点检索覆盖 2.1.206、Fable、count_tokens、TLS、mixed-pool、Bedrock、批量导入

### 后端门禁

- Claude profile 与 TLS 包测试
- messages/count_tokens route 与 repository cache 测试
- scheduler、429、Bedrock、pricing、GPT-5.6、Codex image-gen focused tests
- `go test -tags=unit ./internal/service -count=1`
- 最终 `go test -tags=unit ./... -count=1`

Go 缓存使用 `/private/tmp/sub2api-go-build-cache`。如仅因沙箱禁止 `httptest` 绑定回环端口失败，申请沙箱外重跑；不得把环境失败写成代码通过。

### 前端门禁

- model whitelist
- 批量导入 locale
- Claude profile locale
- SettingsView
- 受 upstream 影响的新增前端测试
- `npm --prefix frontend run build`

### 审查门禁

每个冲突解决任务先做规格审查，再做代码质量审查。所有实现完成后生成固定 diff 包，交给全新代理做整分支审查。Critical/Important 问题必须修复并复审。

## 8. 失败处理与回滚

- 所有实施在隔离工作树进行；主分支和生产保持不变。
- 冲突解决、Ent 生成或测试失败时保留工作树与证据，不自动丢弃改动。
- 不使用 `git reset --hard`、`git checkout --ours`、`git checkout --theirs`。
- 合并完成后由用户选择本地合并、推送 PR、保留分支或丢弃；未经授权不推送、不部署。

## 9. 完成标准

1. merge commit 的第二父提交精确为 `6dd3274a`。
2. VERSION 为 `0.1.150`，migration 159–173 完整且历史 migration 未被改写。
3. 六个冲突按本设计逐行解决，Ent 生成结果一致，snapshot 版本为 15。
4. 45 个重叠文件完成语义复核，fork 不变量全部有测试或静态证据。
5. 后端、前端和构建门禁通过，或环境阻断被精确区分并补充可验证证据。
6. 独立整分支审查无 Critical/Important 问题。
7. 主工作区保持干净；无推送、无部署。
