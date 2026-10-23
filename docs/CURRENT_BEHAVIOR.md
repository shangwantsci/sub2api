# 当前行为说明（CURRENT_BEHAVIOR）

只写「代码现在怎么做」，不写计划与历史；读者是晓宇和之后的 AI 协作者。**以代码为准**：本文与代码冲突时，改本文。改了下面任何一节描述的行为，要同步改本文。
引用写法是「`文件路径` · `函数名`」，不写行号（行号一改就失效）。功能清单见 `docs/FEATURES.md`，协作规则见根目录 `CLAUDE.md`。
名词约定：「设置」= 数据库 `settings` 表里的键（管理端系统设置页可改）；「配置」= `config.yaml` 里的键（样例见 `deploy/config.example.yaml`）；`extra` = `accounts.extra` 这一列 JSON；「槽」= Redis 里的计数名额，占一个、用完释放。

## 1. 一个 `/v1/messages` 请求的旅程

路由 `backend/internal/server/routes/gateway.go` · `RegisterGatewayRoutes`：body 大小限制 → 请求 ID → Ops 错误日志 → 端点归一 → API Key 鉴权 → `RequireGroupAssignment`（Key 没绑分组、且设置 `allow_ungrouped_key_scheduling` 为关，就 403）→ 按分组平台分流。Claude 分组进 `backend/internal/handler/gateway_handler.go` · `GatewayHandler.Messages`，下面第 2–10 步都在这个函数里按序发生。
总览：鉴权 → 内容审查 → 用户并发槽 → 计费资格 → 选号 → 预热拦截/排队等槽 → 用户消息限速 → 转发 → 失败处理 → 收尾记账。

1. **鉴权**（`backend/internal/server/middleware/api_key_auth.go` · `apiKeyAuthWithSubscription`）。取 Key（`Authorization: Bearer` → `x-api-key` → `x-goog-api-key`；URL 里带 `key` 直接 400）→ Key 存在 → Key 状态（`disabled` 等直接 401；`expired`/`quota_exhausted` 留给本函数的计费段）→ Key 的 IP 白/黑名单（403）→ 用户存在且 active → 分组未删除且启用 → 专属分组仍允许该用户（订阅分组免查）→ 订阅分组取有效订阅（没有就 403）→ 计费段：Key 已过期（403）、Key 总额度 `quota` 用尽（429）→ 订阅日/周/月限额（429），非订阅分组则余额 ≤ 0（403）。`run_mode=simple` 时过了前几关就放行、不查计费。
2. **内容审查**（`backend/internal/handler/content_moderation_helper.go` · `checkContentModeration` → `backend/internal/service/content_moderation.go` · `ContentModerationService.Check`）。上游自带的「风控中心」。读：设置 `risk_control_enabled`（总开关）与 `content_moderation_config`（enabled、模式 off/observe/pre_block、分组与模型范围、关键词、审核 API Key）、分组自己的审查策略。总开关关着就什么都不做；observe 只异步记录；pre_block 才同步拦截（关键词 → 历史命中 hash → 审核 API），拦截返回 `content_policy_violation`（状态码取配置，默认 403）；审查自身出错一律放行。
3. **用户并发槽**（`backend/internal/handler/gateway_helper.go` · `ConcurrencyHelper.AcquireUserSlotWithWait`）。读该用户的 `users.concurrency`（鉴权时放进 `AuthSubject`）。占满就排队（排队人数也有上限，满了直接报错），流式请求等待期间发 SSE ping（心跳帧，防客户端等得断开），超时报错；请求结束释放。
4. **计费资格检查**（`backend/internal/service/billing_cache_service.go` · `BillingCacheService.CheckBillingEligibility`；排完队再查一遍）。顺序：熔断器 → 余额（> 0 且不低于配置 `billing.minimum_balance_reserve`）或订阅状态与限额 → （余额模式）用户×平台日/周/月额度 → Key 的 5h/1d/7d 限速窗口 → RPM（每分钟请求数）。RPM 放最后（避免给注定失败的请求计数），且几项**并行生效、任一超限即 429**（带 `Retry-After`）：用户×分组覆盖值 `rpm_override` 存在时替代分组的 `rpm_limit`（覆盖值 0 = 该用户在该分组免检），用户自己的 `rpm_limit` 始终另外生效；Redis 出错放行。
5. **选号**（`backend/internal/service/gateway_scheduling.go` · `GatewayService.SelectAccountWithLoadAwareness`）。
   - 前置：`checkClaudeCodeRestriction`（分组开了 `claude_code_only` 而客户端不是 Claude Code → 改走降级分组或拒绝）、`checkChannelPricingRestriction`（渠道定价限制）；取该会话绑定的账号 id（handler 预取，否则读 Redis）。
   - 候选集：`listSchedulableAccounts` → `backend/internal/service/scheduler_snapshot_service.go` · `ListSchedulableAccounts`，读 Redis 里「分组×平台×模式」的**调度快照**（可调度账号的精简副本，`extra` 只留白名单键，见第 2 节）；没命中才查库并回填。随后批量预取各账号 5h 窗口花费与 RPM 计数、读运行时冷却表（`RateLimitService.GetAccountRuntimeSchedulingBlocks`）。选中的账号最后会被「水合」（`hydrateSelectedAccount`：用完整账号替换精简副本，之后转发/计费/UMQ 读的都是完整账号）。
   - 三层按序尝试，命中即返回：
     - **模型路由层**（仅 anthropic 分组，且分组开了 `model_routing_enabled`、规则匹配所请求的模型）：候选只限规则指定的账号，**优先于粘性复用**；路由列表内先看粘性绑定账号，没有再排序抢槽；路由账号全不可用才落到负载感知层（此时不再做粘性复用，候选扩大到整个分组）。
     - **粘性复用层**（没有模型路由时）：绑定账号仍在候选集且过关口（粘性模式放宽：5h 费用与 RPM 的黄区允许「仅老会话」），抢到槽 → 会话数 → 续约绑定（TTL `stickySessionTTL` = 1 小时）；槽满且排队未满（配置 `gateway.scheduling.sticky_session_max_waiting`）→ 返回等待计划；否则落到下一层。
     - **负载感知层**：对全部候选做下面的逐账号过滤 → 批量查负载（分母 `LoadBalanceCapacity`），负载率 ≥ 100% 的剔除 → 逐轮选一个：优先级数值小者先 →（请求 Fable 系列模型时：有新鲜「可用」证据者先）→（配置 `gateway.scheduling.prefer_soonest_reset` 开着：5h 窗口最早重置者先，默认关）→ 负载率最低 → 最久未用（并列随机）→ 抢并发槽（`EffectiveConcurrency`）→ 会话数 → `SetSessionAccountID` 写粘性；任何一步失败就剔除该账号再选下一个。分组开了「混合类型权重」时改为先按 Setup Token / API Key 两个池的权重抽池，再在池内选（`tryAcquireMixedTypeWeighted`）。
     - **兜底排队**：全部槽都满时，按配置 `gateway.scheduling.fallback_selection_mode`（last_used / random）排序，取第一个过得了会话数的账号，返回等待计划（`fallback_wait_timeout`、`fallback_max_waiting`）。
   - 负载感知层逐账号过滤的**实际顺序**（模型路由层同一套，范围只是路由账号）：① 本次请求已失败/排除的账号（第 9 步 failover 即换号重试，累积下来的失败名单）② 运行时冷却（429/529 触发）③ 账号自身状态 `Account.IsSchedulable`（active、`schedulable` 开、未到过期自动暂停、未过载、未到限流重置时间、未临时不可调度）④ 平台匹配（混合调度下 antigravity 账号需开 `mixed_scheduling`）⑤ 账号支持该模型（模型映射/白名单）⑥ 模型级限流 ⑦ API Key/Bedrock 账号额度 ⑧ 5h 窗口花费门（`window_cost_limit`，仅 Claude OAuth/SetupToken）⑨ RPM 门（`base_rpm`，同上，用降速后的值）⑩ 分组开了「混合类型权重」时的类型策略过滤。**会话数门不在过滤里**：它在抢到槽之后才查，过不了就放槽、换下一个账号。
6. **选中之后**（`Messages`）。账号开了 `intercept_warmup_requests`（credentials；供货商账号强制开）且请求是预热/建议类探针 → 本地回假响应、放槽、不打上游。选号返回的若是等待计划：先进该账号的等待队列（超过 `MaxWaiting` 直接 429），再带超时等槽，拿到槽后写粘性绑定。
7. **用户消息限速 UMQ**（User Message Queue，让「真实用户消息」按账号节奏发出；`backend/internal/handler/gateway_handler.go` · `getUserMsgQueueMode`、`backend/internal/handler/user_msg_queue_helper.go`）。**在选号之后**，因为要读水合后的完整账号。仅 Claude OAuth/SetupToken 账号，且请求最后一条是真实用户消息（`IsRealUserMessage`，tool_result 续跑不算）。模式：账号 `extra.user_msg_queue_mode`（serialize / throttle）优先，否则配置 `gateway.user_message_queue.mode`（默认关）。serialize = 账号级串行锁 + 延迟，throttle = 只延迟；延迟由「当前 RPM ÷ 降速后的 `EffectiveBaseRPM()`」决定（比值 < 0.5 取下限，≥ 0.8 取上限，之间线性插值，±15% 抖动；`base_rpm` 为 0 时恒取下限），范围 `min_delay_ms`–`max_delay_ms`（默认 200–2000ms）；拿锁失败只告警放行（fail-open）；串行锁在上游接受请求时就提前释放。
8. **转发**（`backend/internal/service/gateway_forward.go` · `GatewayService.Forward` → `backend/internal/service/gateway_upstream_request.go` · `buildUpstreamRequest`）。
   - Beta 策略（`evaluateBetaPolicy`，读设置 `beta_policy_settings`）：命中 block 规则 → 直接 400、不换号；命中 filter 规则 → 从 `anthropic-beta` 头里剔除对应 token。
   - 指纹统一（所有 OAuth/SetupToken 账号，设置 `enable_fingerprint_unification`，默认开）：账号固定一份指纹（User-Agent、X-Stainless-* 等），并改写 `metadata.user_id`（设置 `enable_metadata_passthrough` 开则透传不改）。
   - 伪装（mimicry，让第三方客户端的请求看起来像官方 Claude Code CLI）：仅当账号是 OAuth/SetupToken、且客户端**不是**通过 handler 严格校验的真 Claude Code 时才做（`Forward` 里的 `shouldMimicClaudeCode` 判定）：改写 system、`metadata.user_id`、工具名、`cache_control`，并写入 Claude Code 的指纹头；TLS 指纹取账号自己的配置（`enable_tls_fingerprint` / 指纹模板），没配置时用内置的 Claude Code TLS profile。真 Claude Code 客户端不做这些 body 级改写（模型映射、`cache_control` 限 4 块、dateline 归一等通用整理照常执行）。读设置 `claude_code_mimicry_profile`（`tools/cc-calibrate` 标定出的 `claude_code_calibrated_profile` 存在且守卫通过时，优先于内置常量）、`claude_oauth_system_prompt` 与 `claude_oauth_system_prompt_blocks`。
   - 伪装守卫（`backend/internal/service/claude_mimicry_guard.go` · `enforceClaudeMimicryGuard`）：请求组装完、发出之前，对照 profile 审计请求头、`anthropic-beta`、system 块（含计费块里的 `cc_version`）、`metadata.user_id` 与会话头，读设置 `claude_mimicry_guard_mode`：`off` 不查；`warn`（默认）只记 `[ClaudeMimicryGuard]` 日志；`block` 发现不一致就本地拒绝、不发上游。只在 OAuth 伪装路径执行。
9. **失败处理**（由里到外三层）。
   - ① `Forward` 内部同号重试：最多 5 次、总耗时 ≤ 10 秒，退避 0.3 秒起翻倍、封顶 3 秒。Claude OAuth/SetupToken 账号只对 403 重试；API Key 账号只有开了「自定义错误码」、且该状态码不在清单里才重试（默认不重试）；400 的 thinking 签名/预算错误由整流器（失败后自动改写请求体再试一次的容错逻辑）重试一次（设置里有开关）。
   - ② 重试用完、或不可重试的 401/403/429/529/5xx：先 `handleFailoverSideEffects` 把账号状态改好（冷却见下），再返回 `UpstreamFailoverError` 给 handler。
   - ③ handler 的 `backend/internal/handler/failover_loop.go` · `FailoverState.HandleFailoverError`：只有「池模式」API Key 账号（`pool_mode`，本地不标记账号状态）会先同号重试（次数取账号 `pool_mode_retry_count`，间隔 0.5 秒）；其余直接把账号加进本次请求的排除名单、**换号重新选号**，最多换 `gateway.max_account_switches` 次（默认 10）；Anthropic 的 429 另有更宽预算：最多 `gateway.max_account_switches_anthropic_429`（默认 20）次，且不超过 `gateway.anthropic_429_failover_timeout_seconds`（默认 20 秒）。429 换号后该会话的粘性绑定被清掉。已经向客户端写出流式内容就**不再换号**（防止流拼接损坏）。换完仍失败，`mapUpstreamError` 映射成客户端错误（429→429，529→503，其余→502）。
   - 冷却（`backend/internal/service/ratelimit_service.go` · `HandleUpstreamError`）：429 → 解析 5h/7d 窗口重置时间写 `rate_limit_reset_at`（该时间前账号不可调度；Fable 的 7d 窗口只记模型级限流）；没有重置时间头的 429 → 固定 1 分钟运行时冷却（设置 `rate_limit_429_cooldown_settings` 可关）。529 → 写 `overload_until`，默认冷却 10 分钟（设置 `overload_cooldown_settings`）。401：`type=oauth` 的账号清 token 缓存并临时不可调度 `rate_limit.oauth_401_cooldown_minutes`（默认 10 分钟），没有 refresh_token 则直接标 error；其它类型（setup-token、API Key）的 401 直接标 error。冷却期内账号会被第 5 步的 ②③ 过滤掉。
10. **成功收尾**（`Messages`）。RPM 计数 +1（`IncrementAccountRPM`，仅 Claude OAuth/SetupToken 且配了 `base_rpm`；读-加之间允许短暂超额）→ 写/续粘性绑定（`BindStickySession`；首次，或选中的就是原绑定账号时才写，原绑定账号只是被临时跳过时不覆盖）→ 把用量交给有界 worker 池记账（`RecordUsage`，不阻塞响应）。

## 2. 容量模型（统一后的唯一模型）

容量 = 一个账号「同时能扛几个请求、每分钟能发多少、同时开几个会话、5 小时能花多少钱」。数值只存在账号自己的字段里，没有第二份。0 的含义见表（并发 / RPM / 会话数 / 5h 额度的 0 都是不限）。

| 字段（位置） | 作用 | 0 或缺省 |
|---|---|---|
| `accounts.concurrency`（列） | 同时在飞请求上限，选号时拿并发槽用 | 0 = 不限 |
| `extra.base_rpm` | 每分钟请求数门（RPM） | 0 = 不限 |
| `extra.rpm_strategy` | `tiered`（三区，默认）或 `sticky_exempt`（没有红区） | 缺省 tiered |
| `extra.rpm_sticky_buffer` | 黄区宽度（RPM 超过上限后还允许老会话多少） | 缺省 = 并发 + 会话数，下限 base_rpm ÷ 5 |
| `extra.max_sessions` | 同时活跃会话数上限 | 0 = 不限 |
| `extra.session_idle_timeout_minutes` | 会话空闲多久算结束 | 缺省 5 分钟 |
| `extra.window_cost_limit` | 5h 窗口花费上限（美元，标准成本） | 0 = 不限 |
| `extra.window_cost_sticky_reserve` | 超过上限后，只许老会话继续用的额外额度 | 缺省 10 美元 |
| `extra.adaptive_throttle` | 动态降速开关；必须是严格 JSON 布尔（字符串 "true"、数字 1 都不认） | 缺省关 |

另有 `accounts.load_factor`（负载排序的分母，空 = 取并发）；`extra.nurturing_enabled`（养号：账号空闲时自动发少量正常请求保持活跃）与容量同在一张卡里，但不参与容量计算，还要全局设置 `account_nurturing_enabled` 同时开。RPM 三区见 `backend/internal/service/account.go` · `CheckRPMSchedulability`：当前 RPM < 上限正常；< 上限 + 黄区宽度只许老会话；再往上不可调度。5h 窗口花费门同理（`CheckWindowCostSchedulability`）。

- **动态降速**（`backend/internal/service/account_throttle.go`）：只对开了 `adaptive_throttle` 的账号生效。系数 f(利用率)：< 50% → 1（全速）；50%→80% 线性降到 0.5；80%→95% 线性降到 0.2；≥ 95% 直接 0.1；利用率 > 1 钳到 0.1；没数据或 NaN 当 1。账号系数 = min(f(5h 利用率), f(7d 利用率))，利用率来自 `extra.session_window_utilization` 与 `extra.passive_usage_7d_utilization`（由 `ratelimit_service` 被动采样写入，取值 0–1）。**有效值 = 数值 × 系数，向下取整，下限 1；数值 ≤ 0（不限）原样不动，不会被降成 1。**
- **谁用有效值**：`EffectiveConcurrency` → 拿并发槽与等待计划（选号每条路径都用）；`EffectiveBaseRPM` → RPM 门（`CheckRPMSchedulability`）与 UMQ 延迟（第 1 节第 7 步）；`LoadBalanceCapacity`（`EffectiveLoadFactor` × 系数）→ 只做负载率分母，影响排序、不是硬上限。管理端账号列表的容量列（`frontend/src/components/account/AccountCapacityCell.vue`）显示的 `effective_concurrency` / `effective_base_rpm` / `throttle_factor` 也来自它们（`backend/internal/handler/dto/mappers.go` 输出）。
- **刻意不缩放的**：`GetRPMStickyBuffer`（黄区宽度）。已建立的会话要靠这段缓冲继续被服务，缩小它会把老会话饿死。`max_sessions` 与 `window_cost_limit` 也不随降速变化。
- **档位模板**（`backend/internal/service/capacity_presets.go`，设置 `capacity_presets`）：一张表，每行 = 名称 + 并发 / 会话数 / RPM / 5h 额度 / 动态降速 5 项（种子 1–5 档，默认第 3 档）。**选档位 = 把这 5 项复制写进账号**，账号上不存档位名；所以改模板只影响之后套用的账号，存量要靠「应用到存量」（第 3 节）或批量编辑。**标签由 4 个数值反推**（并发、会话数、RPM、5h 额度，**不含**降速开关；后端 `backend/internal/service/provider_onboard.go` · `providerTierMatchesAccount`，前端 `frontend/src/components/account/capacityBuilder.ts` · `matchPreset`，两处必须同口径）：对得上某个模板就显示它的名字，对不上就是「自定义」（供货商面板与账号表单的档位下拉显示「自定义」，管理端账号列表则不显示标签）。写入校验 `ValidateCapacityPresets`：key ≤ 20 字符、不含 `/`、不能叫 `custom`、不重复；并发 1–50（并发 0 在槽位逻辑里是「不限」，所以下限是 1），会话数 0–50，RPM 0–500，5h 额度 0–1000；4 个数值两两不同（否则反推分不清）；供货商账号还在用的 key 不能改名或删除（`CAPACITY_PRESET_IN_USE`）。
- **选号快照契约**（改选号相关字段前必读）：
  - 选号读的是调度快照里的**精简副本**（`backend/internal/repository/scheduler_cache.go` · `buildSchedulerMetadataAccount` / `filterSchedulerExtra`），副本的 `extra` 只保留 `service.SchedulerProjectedExtraKeys()`（`backend/internal/service/scheduler_projection_keys.go`）登记的键。没登记的键在选号阶段读出来是空值；对限流类配置，空值 = 没配置，RPM 门、动态降速会**静默放行**。
  - 缓存命中读精简副本，未命中走库读完整账号，所以漏登记时现象是**间歇性**的；单测直接构造完整账号，抓不到。
  - **规则**：选号阶段（过滤 / 拿槽 / 排序）要读的新 `extra` 键，必须 ① 登记进 `schedulerProjectedExtraKeys`，② 在 `backend/internal/repository/scheduler_projection_contract_unit_test.go` 的 `contractScenarios` 里加一行用例。选中之后才读的（转发、计费、UMQ 延迟）读的是水合后的完整账号，不用登记。
  - 绕过应用层直接改库（迁移、手工 SQL）改了会影响选号的字段，必须同时写 `scheduler_outbox`（发件箱表：记一条「账号变了」，后台任务据此刷新快照），见第 4 节，否则快照不会及时刷新。

## 3. 供货商流程

供货商（代码里叫 `provider` / 供号商）= 把自己的 Claude 账号交给本站调度、按用量结算的外部人员。账号落库后就是一条普通 `accounts` 记录，没有单独的「供货商账号」表。`/api/v1/provider/*` 只对供货商开放（`ProviderOnly` 中间件；管理员也不放行，管理员用 `/api/v1/admin/providers` 那套），站点总开关 `provider_portal_enabled` 关闭时，需登录的接口返回 404（公开的注册接口返回 403 `PROVIDER_PORTAL_DISABLED`）；反过来供货商也用不了消费者接口（`ProviderDenyConsumerRoutes`）。

1. **注册**（`backend/internal/service/auth_provider_register.go` · `AuthService.RegisterProvider`，`POST /api/v1/provider/auth/register`）。要求设置 `provider_portal_enabled` 为开（不看 `registration_enabled`），并提供一枚类型为 `provider_invite` 的邀请码（与建用户同一事务里消费）。建出的是 `users.is_provider=true`、role=user、余额 0、不送额度不分配订阅的用户。
2. **上号**（`backend/internal/handler/provider/onboard_handler.go` · `Onboard`，`/api/v1/provider/onboard/{auth-url,submit}`）。凭据三选一：手动 OAuth、Setup Token、Cookie（session key），全走 claude_code profile，Chrome OAuth 在这条链路上不可达。顺序：先定出口代理（`auto` = 从平台代理池挑绑定账号最少的，单个代理上限 `provider_auto_proxy_max_accounts`（种子 2）；`manual` = 供货商自带；允许哪种由 `provider_proxy_mode_policy` 决定）→ 换凭据 → 按 Anthropic `account_uuid` 去重（同一个号重复提交返回已存在）→ 调度优先级与目标托管分组里现有最小 priority 对齐 → `BuildProviderAccountInput` → `adminService.CreateAccount`。
3. **档位**（`backend/internal/service/provider_tier.go` · `ResolveProviderTier`）。固定档取自档位模板（第 2 节），只能选 `provider_enabled_tier_keys`（JSON 字段名 `enabled_tier_keys`）里开放的，不填则用 `provider_default_tier`。**自定义档**需 `provider_custom_tier_enabled`，并发 / 会话 / RPM / 5h 额度都必须 > 0 且不超过护栏 `provider_custom_tier_caps`（种子 10 / 10 / 100 / 200 美元），空闲超时 0–120 分钟；**自定义档一律开启动态降速**（供货商填的只是裸数值，不能借自定义档绕开降速）。供货商提交的 `extra` 一律被白名单丢弃（白名单目前为空），容量只能通过选档位写入；`intercept_warmup_requests`（credentials）、`enable_tls_fingerprint` 与 `session_id_masking_enabled`（extra）强制开启，供货商看不到也改不了。
4. **账号落库**。一条普通 `accounts` 记录：`platform=anthropic`，type 为 oauth / setup-token，`concurrency` 与 `load_factor` 都取档位并发，`extra` 写 `max_sessions` / `base_rpm` / `window_cost_limit` / `adaptive_throttle`；另有 `provider_user_id`（归属，结算按它聚合）与 `provider_tier`（档位 key 或 `custom`，只用于「应用到存量」的筛选和占用保护）。账号的分组由托管类型唯一决定，绝不回落默认分组。供货商面板显示的档位标签**不读** `provider_tier`，而是按账号当前 4 个数值反推（`ResolveProviderAccountTier`），所以管理员手改数值后标签仍然诚实。
5. **应用到存量**（管理端「供货商设置」；`backend/internal/handler/admin/provider_handler.go` · `PreviewTierApplication` / `ApplyTierToExisting`，计划算法 `backend/internal/service/provider_preset_plan.go` · `PlanPresetApplication`）。先 `GET /api/v1/admin/provider-settings/tiers/:tier/apply-preview`（只读：该档位账号总数 `matched`、将被改动的 `will_change`、至多 20 个样例及逐项「当前值 → 模板值」；比较并发、会话数、RPM、5h 额度、动态降速、`load_factor` 共 6 项）；确认后再 `POST …/apply-existing`，必须带 `{"expected_changed": will_change}`。执行时重算一次，数量对不上就 409 `PRESET_APPLY_CONFLICT` 且一行都不写。写入是一条 `BulkUpdate`：并发列与 `load_factor` 写成档位并发，`extra` 只合并那 4 个键，不碰 `window_cost_sticky_reserve`、`nurturing_enabled` 等其它设置。`custom` 档不能回填。
6. **停用 / 启用**（`backend/internal/service/admin_user_provider_status.go` · `syncProviderAccountsWithUserStatus`）。管理员把供货商停用 → 其名下 active 且可调度的账号，在一条 SQL 里 `schedulable=false` 并打 `extra.provider_suspended=true`；启用 → 只恢复带这个标记的账号（并把标记写成 false）。停用前就被管理员手动暂停的账号保持暂停。可重试、幂等；级联失败返回 500 并说明用户状态已落库。该标记是纯内部状态：不在容量键里，不在供货商可提交键里，也不进调度快照白名单（暂停靠 `schedulable` 列）。
7. **结算（只读概览）**（`backend/internal/service/provider_settlement.go` · `ProviderSettlementService`；管理端 `/api/v1/admin/providers/*`，供货商侧只读 `/api/v1/provider/billing/*`）。「封账」不删改 `usage_logs`，只在 `provider_settlements` 插一条不可变结算单（连同 `provider_settlement_items` 的分账号明细快照）：周期 = 上一条已结算单的 `period_end` → 当前时刻减冷却期（设置 `provider_settlement_cooldown_seconds`，种子 600 秒，等异步写入的用量落定）；金额 = 周期内供货商名下账号 `usage_logs.total_cost` 之和（1 倍率标准成本，不含分组/账号倍率；账号下线后本期用量仍计入）；`last_usage_id` 水位兜住「时间早、提交晚」的迟到记录；封账在该供货商的 advisory lock（PostgreSQL 咨询锁，一把按供货商加的互斥锁）内进行，防止重复结算；作废（`Void`）后该期金额回到待结算；按日明细的时区取设置 `provider_settlement_timezone`（种子 Asia/Shanghai）。

## 4. 迁移彩排方法

迁移 = `backend/migrations/NNN_*.sql`，服务启动时由 `backend/internal/repository/migrations_runner.go` · `applyMigrationsFS` 自动执行（`backend/internal/repository/ent.go` 初始化数据库时调用）。runner 的规则：以文件名为主键写入 `schema_migrations`；记录文件内容（去首尾空白）的 SHA256，**已应用的迁移文件内容变了就拒绝启动**（只有 runner 里白名单列出的历史误改例外），所以**已应用的迁移不能改，要改就新写一个**；普通 `.sql` 在一个事务里执行，`*_notx.sql`（只许 `CREATE/DROP INDEX CONCURRENTLY`）逐条非事务执行；执行时持 PostgreSQL advisory lock，多实例只有一个在迁。`backend/migrations/migrations.go` 与 `backend/migrations/README.md` 要求迁移**必须幂等**（重复执行不报错、结果不变：`IF NOT EXISTS`、`ON CONFLICT DO NOTHING`，或像 227 那样让第二次执行命中 0 行）；runner 不检查这一点，要靠下面的彩排验证。下一个编号是 **228**。README 里的 `make migrate-up/migrate-down` 与「必须写 Down 迁移」是上游残留，本仓库没有这两个 make 目标，迁移只向前、启动时自动应用，别照着做。迁移的单元测试（如 `backend/migrations/capacity_adaptive_throttle_migration_test.go`）只对 SQL **文本**做断言、不会真执行，所以写了新迁移必须彩排。

一旦要写新迁移，先在临时 PostgreSQL 上彩排：

1. 起临时库（`$S` = 会话 scratchpad 目录；只走 TCP，因为 macOS 的 Unix socket 路径上限 104 字节、scratchpad 路径太长；**不要动 brew 服务**）：
   ```bash
   export PATH=/opt/homebrew/bin:$PATH
   initdb -D $S/pgdata -U postgres --auth=trust -E UTF8
   pg_ctl -D $S/pgdata -o "-p 55434 -c listen_addresses=127.0.0.1 -c unix_socket_directories=''" -l $S/pg.log -w start
   createdb -h 127.0.0.1 -p 55434 -U postgres mig
   ```
2. 写一个带构建标签的临时测试 `backend/internal/repository/zz_mig_rehearsal_test.go`（**不入库**），用 `lib/pq` 连 `MIG_DSN`，调用同包里的 `applyMigrationsFS`：
   ```go
   //go:build mig_rehearsal

   package repository

   import (
   	"context"
   	"database/sql"
   	"os"
   	"testing"

   	_ "github.com/lib/pq"
   	"github.com/stretchr/testify/require"
   )

   func TestMigRehearsal(t *testing.T) {
   	db, err := sql.Open("postgres", os.Getenv("MIG_DSN"))
   	require.NoError(t, err)
   	defer func() { _ = db.Close() }()
   	require.NoError(t, applyMigrationsFS(context.Background(), db, os.DirFS(os.Getenv("MIG_DIR"))))
   }
   ```
3. 先把「待测迁移之前的所有 `.sql`」拷到临时目录，`MIG_DIR` 指向它跑一遍（应用到待测迁移的上一个编号；现在待测的是 228，所以到 227 为止）；用 `psql` 造有代表性的数据（含脏值、非对象 `extra`、非 anthropic 账号、软删账号、JSON 类型错位的值）；再把 `MIG_DIR` 指向 `backend/migrations` 跑一遍，此时只会新应用待测迁移：
   ```bash
   cd backend && MIG_DSN='postgres://postgres@127.0.0.1:55434/mig?sslmode=disable' MIG_DIR=<目录> \
     go test -tags 'unit mig_rehearsal' ./internal/repository -run TestMigRehearsal -count=1
   ```
4. 逐行断言结果（对每个造的账号 `SELECT` 看新值；没命中的「对照行」必须原样）；再用 `psql -v ON_ERROR_STOP=1 -f` 把迁移 SQL **重放**一次，比较重放前后 `accounts` / `settings` / `scheduler_outbox` 的 md5，必须不变（幂等）：
   ```sql
   SELECT 'accounts', md5(coalesce(string_agg(t::text,'|' ORDER BY id),'')) FROM accounts t
   UNION ALL SELECT 'settings', md5(coalesce(string_agg(t::text,'|' ORDER BY key),'')) FROM settings t
   UNION ALL SELECT 'scheduler_outbox', md5(coalesce(string_agg(t::text,'|' ORDER BY id),'')) FROM scheduler_outbox t;
   ```
5. 另建空库（`createdb … fresh`），对它把完整链（`MIG_DIR=backend/migrations`）跑**两次**：第一次建全表，第二次应是纯跳过且校验和通过。
6. 收尾：`pg_ctl -D $S/pgdata -m fast stop`；`rm -rf $S/pgdata`；删掉 `zz_mig_rehearsal_test.go`；`lsof -iTCP:55434` 确认端口已释放。

**绕过应用层的 UPDATE / INSERT（动了账号或分组）必须同时写 `scheduler_outbox`**（`event_type='account_changed'` 与 `account_id`），调度快照才会刷新。写法见 `backend/migrations/227_capacity_adaptive_throttle.sql`：`UPDATE … RETURNING a.id` 之后 `INSERT INTO scheduler_outbox (event_type, account_id) SELECT 'account_changed', u.id FROM upd`，`dedup_key` 留空（部分唯一索引只约束非空值），并且只为真正被改的行写。

## 5. 测试地图

| 不变量 | 守卫测试 |
|---|---|
| 选号快照契约：精简副本与完整账号对同一账号做出相同判定（含经真实 Redis 读写；覆盖 RPM / 窗口费用 / 会话数 / 动态降速 / API Key 自设额度 / OpenAI 透传开关）；登记表无重复、键都能穿过投影、关键键不被误删 | `backend/internal/repository/scheduler_projection_contract_unit_test.go` |
| 公开号池健康页取数：真实 repository 在 SQLite 内存库上喂给真实 `OpsService`，每个取数列（type、extra、过期、会话窗口）各有一个只靠它才能判对的场景。**不能用 stub 代替**：stub 绕过了真实 SELECT 选了哪些列 | `backend/internal/repository/ops_pool_health_repo_unit_test.go` |
| 动态降速曲线、有效值公式、严格布尔开关、`EffectiveConcurrency` / `EffectiveBaseRPM` / `LoadBalanceCapacity`、RPM 门用降速后的上限 | `backend/internal/service/account_throttle_test.go` |
| 选号每条路径（拿槽、等待计划、负载分母、RPM 三区、模型路由层）都用有效值 | `backend/internal/service/gateway_scheduling_capacity_test.go` |
| 批量更新：容量键只作用于 Claude OAuth/SetupToken；非布尔的降速/养号开关被拒 | `backend/internal/service/admin_account_capacity_test.go` |
| 模板表校验与存取（数值范围、4 元组唯一、保留 key、被供货商账号占用时不能改名删除、读路径宽松） | `backend/internal/service/capacity_presets_test.go`；admin 接口 `backend/internal/handler/admin/provider_handler_capacity_presets_test.go` |
| 供货商档位解析与护栏（自定义档不许 0、恒开降速）、extra 白名单、强制伪装项、建号输入 | `backend/internal/service/provider_tier_test.go` |
| 应用到存量：计划比较 6 项且顺序固定、预览与执行同源、数量不符 409 不写 | `backend/internal/service/provider_preset_plan_test.go`、`backend/internal/service/provider_apply_tier_test.go`；`backend/internal/handler/admin/provider_handler_apply_preview_test.go` |
| 停用级联：只暂停 active 且可调度的账号，启用只恢复带标记的 | `backend/internal/service/admin_service_provider_status_cascade_test.go`；前端 `frontend/src/views/admin/__tests__/ProvidersView.disable.spec.ts` |
| 迁移 227 的 SQL 文本契约（只清 9 个 persona 键、翻译规则、写 outbox、幂等写法、纯数据迁移）。**只断言文本，不执行 SQL** | `backend/migrations/capacity_adaptive_throttle_migration_test.go` |
| 前端三个弹窗共用一个容量漏斗：容量键字面量只出现在 `capacityBuilder.ts`；旧实现残留（persona、启用开关、魔法默认 RPM）不许回来；新建弹窗 `buildAnthropicOAuthExtra(` 恰好 3 处调用 | `frontend/src/components/account/__tests__/accountCapacity.guard.spec.ts` |
| 各提交路径的 payload（新建 OAuth / Cookie / 批量导入、编辑、批量编辑）与 builder 纯函数 | `frontend/src/components/account/__tests__/CreateAccountModal.capacity.spec.ts`、`frontend/src/components/account/__tests__/EditAccountModal.capacity.spec.ts`、`frontend/src/components/account/__tests__/BulkEditAccountModal.capacity.spec.ts`、`frontend/src/components/account/__tests__/capacityBuilder.spec.ts` |
| i18n 键覆盖：`t('…')` 引用的键在 zh / en 都存在、zh / en 子树一一对应、已删除词条不回来、全部文案可编译、顶层键不撞车 | `frontend/src/i18n/__tests__/capacityI18nKeys.spec.ts`、`frontend/src/i18n/__tests__/providerI18nKeys.spec.ts`、`frontend/src/i18n/__tests__/adminLargeFilesI18nKeys.spec.ts`、`frontend/src/i18n/__tests__/messageCompilation.spec.ts`、`frontend/src/i18n/__tests__/localesNoKeyCollision.spec.ts` |
| 前端关键清单：`Makefile` 的 `FRONTEND_CRITICAL_VITEST`（27 个文件：登录回调、支付、系统设置、容量与三个弹窗、供货商、公开号池健康页等），`make test-frontend-critical` 只跑它 | `Makefile` |

怎么跑：
- 后端：`cd backend && go test -tags unit ./... -count=1`（`service` 包必须带 `-tags unit`，否则编译不过，是上游既有问题）；静态检查 `go vet -tags unit ./...`。
- 前端（本机没有 pnpm 时直接用本地二进制）：`cd frontend && ./node_modules/.bin/vue-tsc --noEmit && ./node_modules/.bin/eslint . --ext .vue,.js,.jsx,.cjs,.mjs,.ts,.tsx,.cts,.mts && ./node_modules/.bin/vitest run`。
- 已知既有失败（P6 时点，与容量无关；修好后删掉这条）：后端 `internal/handler` 的 `TestGatewayHandlerMessages_InterceptWarmup_AntigravityAccount_MixedSchedulingV1` 与 `..._ForcePlatform`；前端 `frontend/src/features/channel-monitor-v2/__tests__/designSystem.structure.spec.ts` 的 2 个用例。

## 6. 本地全栈冒烟（临时 PostgreSQL + miniredis + 真实后端）

本机没有 docker 和 redis-server，testcontainers 集成测试跑不了，但能凑出一套真实的栈：临时 PostgreSQL（同第 4 节）、用 Go 库 miniredis 起的 Redis 兼容服务、真实后端进程。2026-10-22 用它对着整理后的代码把管理端接口与设置页走了一遍，抓到一个单测抓不到的 bug（公开号池健康页在真实库里账号数恒为 0，见 `docs/FEATURES.md` 第 3 节）。改了接口形状、设置、账号字段、迁移、供货商流程之后建议再跑一遍。

只验证，不往仓库里写文件。下面的 `$S` 是一个临时目录，用完整个删掉。

1. **PostgreSQL**：照第 4 节起一个临时实例，端口换成别的高位端口（下面用 55435）。库不用建，后端的 `AUTO_SETUP` 会自己创建。
2. **Redis**：在 `$S/miniredis-srv` 建一个独立的临时 Go module（不要放进仓库），用 `miniredis.NewMiniRedis().StartAddr("127.0.0.1:56379")` 起服务，再开一个 goroutine 每秒 `FastForward(1s)`，让 TTL 真实流逝。`go.mod` 写 `go 1.24`（写成本机 Go 的小版本号会触发 toolchain 下载而失败）。构建：`GOTOOLCHAIN=local GOFLAGS=-mod=mod GOPROXY=off GOSUMDB=off go build -o $S/miniredis-bin .`。miniredis 自带 Lua（`EVAL` / `EVALSHA`），调度快照、锁、运维采集这几条路径都跑通了；网关转发路径里的并发槽、等待队列、限流脚本没被压到过，不要外推。
3. **后端**：`cd backend && go build -o $S/sub2api-server ./cmd/server`，在 `$S/data` 目录下运行（`config.yaml` 会生成在那里，不会落进仓库）。环境变量（名字都在 `backend/internal/setup/setup.go`）：`AUTO_SETUP=true`、`DATA_DIR=$S/data`、`DATABASE_HOST=127.0.0.1`、`DATABASE_PORT=55435`、`DATABASE_USER=postgres`、`DATABASE_PASSWORD`（**必须非空**，任意值；空值会让向导把迁移和管理员建进默认库，见 `docs/FEATURES.md` 第 10 节）、`DATABASE_DBNAME`、`DATABASE_SSLMODE=disable`、`REDIS_HOST=127.0.0.1`、`REDIS_PORT=56379`、`ADMIN_EMAIL`、`ADMIN_PASSWORD`（随机值）、`SERVER_HOST=127.0.0.1`、`SERVER_PORT=18081`、`SERVER_MODE=release`、`JWT_SECRET`（64 位十六进制）、`TZ=UTC`。不设 `TOTP_ENCRYPTION_KEY` 即可（自动生成，只有一条警告）；若要设，必须是 64 位十六进制，字母数字混合会让进程反复重启。
4. **合规确认**：管理员登录后先 `POST /api/v1/admin/compliance/accept`，body 为 `{"phrase":"I have read, understood, and agree to the Sub2API Deployment and Operation Compliance Commitment","language":"en"}`（确认语常量在 `backend/internal/service/admin_compliance.go`），否则所有 `/admin` 接口返回 423 `ADMIN_COMPLIANCE_ACK_REQUIRED`。
5. **前端（可选）**：必须 `cd frontend` 再运行，否则 tailwind 找不到配置。`VITE_DEV_PROXY_TARGET=http://127.0.0.1:18081 VITE_DEV_PORT=15173 node_modules/.bin/vite .`。**不要运行 `vite build`**：产物会写进 `backend/internal/web/dist`。仓库的 `vite.config.ts` 默认监听 `0.0.0.0` 并把缓存写进 `node_modules/.vite`，本地冒烟建议用一份放在 `$S` 里的包装配置，复用它并把 `host` 改成 `127.0.0.1`、`cacheDir` 放到 `$S`。用浏览器自动化时需要一个开着 `--remote-debugging-port=9222`、使用独立 `--user-data-dir` 的无头 Chrome。
6. **收尾**：停掉 vite、Chrome、后端、miniredis、PostgreSQL，删掉 `$S`（里面有 PostgreSQL 数据目录、`config.yaml`、令牌和密码文件），用 `lsof` 确认端口已释放。
