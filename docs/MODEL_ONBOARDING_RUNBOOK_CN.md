# 新模型接入标准流程

本文档用于 sub2api fork 接入新模型时保持固定流程，避免把口头简称、限时促销价或未经核验的客户端特征写入仓库。

## 1. 官方事实核验

新增模型前必须先核验并记录：

- 真实模型 ID：以官方 API 文档、官方 changelog、官方 SDK/包元数据为准，不使用社区简称。
- alias：确认是否存在短别名、快照 ID、云厂商 ID；没有证据时不新增映射。
- API 可用性：确认 Claude API / Bedrock / Vertex / 其它 provider 是否分别可用。
- 上下文与输出：context window、max output tokens、特殊 beta 或能力开关。
- 稳定定价：默认使用官方稳定价，不使用限时促销价；促销信息只写在调研结论中。
- 来源链接：记录官方文档、官方公告、npm/SDK 元数据或 changelog。
- 未确认项：明确列出未确认的信息和原因。

## 2. 代码审计清单

按固定顺序检查这些位置，按需最小改动：

- 后端默认模型目录：`backend/internal/pkg/claude/constants.go`
- 模型定价：`backend/resources/model-pricing/model_prices_and_context_window.json`
- 模型 alias / override：仅当官方 ID 与用户输入 ID 不一致时新增。
- 前端白名单与 preset：`frontend/src/composables/useModelWhitelist.ts`
- provider mapping：Antigravity、Bedrock、Vertex 等必须单独核验证据后再改。
- usage billing：确认新增模型不会被错误模糊匹配到其它 provider。
- tests：先写 RED 测试，再做最小实现。

## 3. 定价原则

- 不修改旧模型定价，除非官方稳定价变更且本轮目标明确包含定价修正。
- 新模型只新增必要精确条目，避免依赖模糊匹配。
- 不把限时促销价写入 pricing JSON。
- 同家族稳定价可复用；例如 Sonnet 新模型默认使用 Sonnet 稳定价。
- 如果稳定价未确认，先不写 pricing JSON，改为记录未确认项并等待官方信息。

## 4. Claude Code mimic 原则

Claude Code 客户端版本变化时，不能默认更新 mimic profile，必须先抓证据：

- 只用本地 dummy base URL / 本地监听代理。
- 使用 dummy token，不访问真实 Anthropic API。
- 不安装证书，不改系统代理，不部署服务器。
- 抓 `/v1/messages`，尽量触发 `/v1/messages/count_tokens`。
- 对比 User-Agent、X-Stainless headers、Accept、Accept-Encoding、anthropic-beta、system block、billing block、metadata.user_id、X-Claude-Code-Session-Id、thinking、context_management、output_config。
- TLS / JA3 只有在安全、可重复、接近真实域名连接条件下才可作为强证据；dummy TLS 不足以单独更新 TLS profile。

## 5. 验证与交付

- 后端至少运行相关 Go 单测。
- 前端改白名单或类型时运行对应 Vitest 和 typecheck。
- 使用代码审查检查是否越界修改 provider mapping、调度、TLS、旧定价。
- 不提交、不部署，除非晓宇明确要求。
