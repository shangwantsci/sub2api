// Package claude provides constants and helpers for Claude API integration.
package claude

import "strings"

// Claude Code 客户端相关常量

// Beta header 常量
//
// 这里的兼容兜底组合沿用 Claude Code CLI 2.1.197 受控抓包结果；
// synthetic mimic messages 的 2.1.206 组合由模型 profile 单独维护。
const (
	BetaOAuth                    = "oauth-2025-04-20"
	BetaClaudeCode               = "claude-code-20250219"
	BetaInterleavedThinking      = "interleaved-thinking-2025-05-14"
	BetaFineGrainedToolStreaming = "fine-grained-tool-streaming-2025-05-14"
	BetaTokenCounting            = "token-counting-2024-11-01"
	BetaContext1M                = "context-1m-2025-08-07"
	BetaFastMode                 = "fast-mode-2026-02-01"

	BetaPromptCachingScope    = "prompt-caching-scope-2026-01-05"
	BetaEffort                = "effort-2025-11-24"
	BetaRedactThinking        = "redact-thinking-2026-02-12"
	BetaContextManagement     = "context-management-2025-06-27"
	BetaExtendedCacheTTL      = "extended-cache-ttl-2025-04-11"
	BetaThinkingTokenCount    = "thinking-token-count-2026-05-13"
	BetaToolSearchTool        = "tool-search-tool-2025-10-19"
	BetaMidConversationSystem = "mid-conversation-system-2026-04-07"
	BetaAdvancedToolUse       = "advanced-tool-use-2025-11-20"
	BetaServerSideFallback    = "server-side-fallback-2026-06-01"
	BetaFallbackCredit        = "fallback-credit-2026-06-01"
	// BetaStructuredOutputs 真身 2.1.211 仅在带 output_config.format.json_schema 的请求上追加，
	// 故不放进静态默认集合，由出站层按请求特性条件追加。
	BetaStructuredOutputs = "structured-outputs-2025-12-15"
)

// DroppedBetas 是转发时需要从 anthropic-beta header 中移除的 beta token 列表。
// 这些 token 是客户端特有的，不应透传给上游 API。
var DroppedBetas = []string{}

// DefaultBetaHeader Claude Code 客户端默认的 anthropic-beta header
const DefaultBetaHeader = BetaClaudeCode + "," + BetaInterleavedThinking + "," + BetaThinkingTokenCount + "," + BetaContextManagement + "," + BetaPromptCachingScope + "," + BetaMidConversationSystem + "," + BetaAdvancedToolUse + "," + BetaEffort

// MessageBetaHeaderNoTools /v1/messages 在无工具时的 beta header
//
// NOTE: Claude Code OAuth credentials are scoped to Claude Code. When we "mimic"
// Claude Code for non-Claude-Code clients, we must include the claude-code beta
// even if the request doesn't use tools.
const MessageBetaHeaderNoTools = BetaClaudeCode + "," + BetaInterleavedThinking + "," + BetaThinkingTokenCount

// MessageBetaHeaderWithTools /v1/messages 在有工具时的 beta header
const MessageBetaHeaderWithTools = BetaClaudeCode + "," + BetaInterleavedThinking + "," + BetaThinkingTokenCount

// CountTokensBetaHeader count_tokens 请求使用的 anthropic-beta header
const CountTokensBetaHeader = DefaultBetaHeader + "," + BetaTokenCounting

// HaikuBetaHeader Haiku 模型使用的 anthropic-beta header。
const HaikuBetaHeader = BetaInterleavedThinking + "," + BetaThinkingTokenCount + "," + BetaContextManagement + "," + BetaPromptCachingScope + "," + BetaClaudeCode + "," + BetaAdvancedToolUse

// APIKeyBetaHeader API-key 账号建议使用的 anthropic-beta header（不包含 oauth）
const APIKeyBetaHeader = BetaClaudeCode + "," + BetaInterleavedThinking + "," + BetaFineGrainedToolStreaming

// APIKeyHaikuBetaHeader Haiku 模型在 API-key 账号下使用的 anthropic-beta header（不包含 oauth / claude-code）
const APIKeyHaikuBetaHeader = BetaInterleavedThinking

// DefaultCacheControlTTL 是网关代理为自己生成的 cache_control 块默认使用的 ttl。
// 真实 Claude Code CLI 当前使用 "1h"，但本仓策略是"客户端透传 ttl 优先；
// 客户端缺省时统一使用 5m"，这样既不浪费 1h 缓存额度，也保留客户端自定义能力。
const DefaultCacheControlTTL = "5m"

// CLICurrentVersion 是 sub2api 当前对外伪装的 Claude Code CLI 版本号（三段 semver）。
// 用于 billing attribution block 中的 cc_version=X.Y.Z.{fp} 前缀以及 fingerprint 计算。
// 必须与 DefaultHeaders["User-Agent"] 中的版本号严格一致；不一致会被 Anthropic 判第三方。
//
// 2.1.211 = tools/cc-calibrate 在服务器上对真身 Claude Code 抓包标定所得（此前手抄的
// 2.1.206 已过期：npm stable=2.1.204 / latest=2.1.211，真身用户无人在 2.1.206）。
// 指纹守卫已用真身输出校验：sha256(salt+chars[4,7,20]+version)[:3] 对齐（MATCH）。
const CLICurrentVersion = "2.1.211"

const (
	// DefaultClaudeCodeMimicryProfileID 是默认 Claude Code 伪装 profile。
	// linux-x64：harness/沙箱均在 Linux 上跑真身，故对外声称 Linux 与实际可标定/可复现的
	// 环境自洽（避免声称 macOS 却只能手造 mac 专属字段而再次漂移）。
	DefaultClaudeCodeMimicryProfileID = "cc-2.1.211-sdk-cli-linux-x64"
	defaultClaudeAgentSDKSystemPrompt = "You are a Claude agent, built on Anthropic's Claude Agent SDK."
)

// ClaudeCodeMimicryProfile 描述一个可复用的 Claude Code wire profile。
type ClaudeCodeMimicryProfile struct {
	ID                string
	CLIVersion        string
	BillingEntrypoint string
	SystemPrompt      string
	Headers           map[string]string
	MessageBetas      []string
	CountTokensBetas  []string
}

// ClaudeCodeMimicryModelProfile 描述 Claude Code 2.1.206 对不同模型族的
// beta/body 默认形态差异。
type ClaudeCodeMimicryModelProfile struct {
	ID                          string
	MessageBetas                []string
	CountTokensBetas            []string
	DefaultMaxTokens            int
	CountTokensDefaultMaxTokens int
	DefaultThinkingType         string
	DefaultThinkingBudgetTokens int
	DefaultOutputConfigEffort   string
}

var defaultClaudeCodeMimicryHeaders = map[string]string{
	"User-Agent":                                "claude-cli/" + CLICurrentVersion + " (external, sdk-cli)",
	"X-Stainless-Lang":                          "js",
	"X-Stainless-Package-Version":               "0.94.0",
	"X-Stainless-OS":                            "Linux",
	"X-Stainless-Arch":                          "x64",
	"X-Stainless-Runtime":                       "node",
	"X-Stainless-Runtime-Version":               "v26.3.0",
	"X-Stainless-Retry-Count":                   "0",
	"X-Stainless-Timeout":                       "600",
	"X-App":                                     "cli",
	"Anthropic-Dangerous-Direct-Browser-Access": "true",
}

// defaultClaudeCodeMimicryMessageBetas 是 sonnet/opus 主对话轮的 anthropic-beta 集合。
//
// 注：tools/cc-calibrate 对真身 2.1.211 的抓包显示真身主轮集合为
// {claude-code, interleaved-thinking, thinking-token-count, context-management,
// prompt-caching-scope, mid-conversation-system, effort}（不含 tool-search-tool）。
// 但把 context-management 加入会联动 body 层 sanitize（保留/剥离 body.context_management），
// 影响面较大，属于需受控灰度的能力维度改动，因此 beta 集合的完整重标定交由标定 profile
// 加载器（plan 阶段二 p2-loader）承载，此处暂保留既有集合以限定改动爆炸半径。
var defaultClaudeCodeMimicryMessageBetas = []string{
	BetaClaudeCode,
	BetaInterleavedThinking,
	BetaToolSearchTool,
	BetaEffort,
}

var defaultClaudeCodeMimicryCountTokensBetas = []string{
	BetaClaudeCode,
	BetaInterleavedThinking,
	BetaThinkingTokenCount,
	BetaContextManagement,
	BetaPromptCachingScope,
	BetaMidConversationSystem,
	BetaAdvancedToolUse,
	BetaEffort,
	BetaTokenCounting,
}

var claudeCodeMimicryHaikuMessageBetas = []string{
	BetaClaudeCode,
	BetaToolSearchTool,
}

var claudeCodeMimicryFableMessageBetas = []string{
	BetaClaudeCode,
	BetaInterleavedThinking,
	BetaToolSearchTool,
	BetaEffort,
	BetaFallbackCredit,
}

var claudeCodeMimicryHaikuCountTokensBetas = []string{
	BetaInterleavedThinking,
	BetaThinkingTokenCount,
	BetaContextManagement,
	BetaPromptCachingScope,
	BetaClaudeCode,
	BetaAdvancedToolUse,
	BetaTokenCounting,
}

var claudeCodeMimicryFableCountTokensBetas = []string{
	BetaClaudeCode,
	BetaInterleavedThinking,
	BetaThinkingTokenCount,
	BetaContextManagement,
	BetaPromptCachingScope,
	BetaMidConversationSystem,
	BetaAdvancedToolUse,
	BetaEffort,
	BetaServerSideFallback,
	BetaFallbackCredit,
	BetaTokenCounting,
}

func cloneStringMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = v
	}
	return out
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

// DefaultClaudeCodeMimicryProfile 返回默认的 Claude Code 2.1.206 profile。
func DefaultClaudeCodeMimicryProfile() ClaudeCodeMimicryProfile {
	return ClaudeCodeMimicryProfile{
		ID:                DefaultClaudeCodeMimicryProfileID,
		CLIVersion:        CLICurrentVersion,
		BillingEntrypoint: "sdk-cli",
		SystemPrompt:      defaultClaudeAgentSDKSystemPrompt,
		Headers:           cloneStringMap(defaultClaudeCodeMimicryHeaders),
		MessageBetas:      cloneStrings(defaultClaudeCodeMimicryMessageBetas),
		CountTokensBetas:  cloneStrings(defaultClaudeCodeMimicryCountTokensBetas),
	}
}

// ResolveClaudeCodeMimicryProfile 解析 profile ID。未知值回退到当前默认 profile。
func ResolveClaudeCodeMimicryProfile(id string) ClaudeCodeMimicryProfile {
	switch strings.TrimSpace(id) {
	case "", DefaultClaudeCodeMimicryProfileID:
		return DefaultClaudeCodeMimicryProfile()
	default:
		return DefaultClaudeCodeMimicryProfile()
	}
}

// ResolveClaudeCodeMimicryModelProfile 返回指定模型在 Claude Code 2.1.206
// synthetic mimic 路径上的模型族 profile。未知模型保守按 Sonnet/Opus profile 处理。
func ResolveClaudeCodeMimicryModelProfile(modelID string) ClaudeCodeMimicryModelProfile {
	normalized := strings.ToLower(strings.TrimSpace(NormalizeModelID(modelID)))
	switch {
	case strings.Contains(normalized, "haiku"):
		return ClaudeCodeMimicryModelProfile{
			ID:                          "haiku",
			MessageBetas:                cloneStrings(claudeCodeMimicryHaikuMessageBetas),
			CountTokensBetas:            cloneStrings(claudeCodeMimicryHaikuCountTokensBetas),
			DefaultMaxTokens:            32000,
			CountTokensDefaultMaxTokens: 32000,
			DefaultThinkingType:         "enabled",
			DefaultThinkingBudgetTokens: 31999,
		}
	case strings.Contains(normalized, "fable"):
		return ClaudeCodeMimicryModelProfile{
			ID:                          "fable",
			MessageBetas:                cloneStrings(claudeCodeMimicryFableMessageBetas),
			CountTokensBetas:            cloneStrings(claudeCodeMimicryFableCountTokensBetas),
			DefaultMaxTokens:            64000,
			CountTokensDefaultMaxTokens: 64000,
			DefaultThinkingType:         "adaptive",
			DefaultOutputConfigEffort:   "high",
		}
	case strings.Contains(normalized, "sonnet"):
		return ClaudeCodeMimicryModelProfile{
			ID:                          "sonnet",
			MessageBetas:                cloneStrings(defaultClaudeCodeMimicryMessageBetas),
			CountTokensBetas:            cloneStrings(defaultClaudeCodeMimicryCountTokensBetas),
			DefaultMaxTokens:            32000,
			CountTokensDefaultMaxTokens: 64000,
			DefaultThinkingType:         "adaptive",
			DefaultOutputConfigEffort:   "high",
		}
	default:
		return ClaudeCodeMimicryModelProfile{
			ID:                          "opus",
			MessageBetas:                cloneStrings(defaultClaudeCodeMimicryMessageBetas),
			CountTokensBetas:            cloneStrings(defaultClaudeCodeMimicryCountTokensBetas),
			DefaultMaxTokens:            64000,
			CountTokensDefaultMaxTokens: 64000,
			DefaultThinkingType:         "adaptive",
			DefaultOutputConfigEffort:   "high",
		}
	}
}

func ClaudeCodeMimicryMessageBetasForModel(modelID string) []string {
	return ResolveClaudeCodeMimicryModelProfile(modelID).MessageBetas
}

func ClaudeCodeMimicryCountTokensBetasForModel(modelID string) []string {
	return ResolveClaudeCodeMimicryModelProfile(modelID).CountTokensBetas
}

// FullClaudeCodeMimicryBetas 返回默认模型族最"像"真实 Claude Code CLI 的 beta 列表。
// 新代码应优先使用 ClaudeCodeMimicryMessageBetasForModel，以便保留 Haiku/Fable
// 等模型族的抓包差异。
//
// 使用建议：
//   - OAuth synthetic mimic：使用 ClaudeCodeMimicryMessageBetasForModel。
//   - API-key 账号：不要使用本函数，参见 APIKeyBetaHeader。
//   - 不默认加入 redact-thinking，避免上游抹除 thinking 内容；客户端显式传入时由合并逻辑保留。
func FullClaudeCodeMimicryBetas() []string {
	return ClaudeCodeMimicryMessageBetasForModel("")
}

// DefaultHeaders 是 Claude Code 客户端默认请求头。
var DefaultHeaders = cloneStringMap(defaultClaudeCodeMimicryHeaders)

// Model 表示一个 Claude 模型
type Model struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

// DefaultModels Claude Code 客户端支持的默认模型列表
var DefaultModels = []Model{
	{
		ID:          "claude-fable-5",
		Type:        "model",
		DisplayName: "Claude Fable 5",
		CreatedAt:   "2026-06-09T00:00:00Z",
	},
	{
		ID:          "claude-opus-4-5-20251101",
		Type:        "model",
		DisplayName: "Claude Opus 4.5",
		CreatedAt:   "2025-11-01T00:00:00Z",
	},
	{
		ID:          "claude-opus-4-6",
		Type:        "model",
		DisplayName: "Claude Opus 4.6",
		CreatedAt:   "2026-02-06T00:00:00Z",
	},
	{
		ID:          "claude-opus-4-7",
		Type:        "model",
		DisplayName: "Claude Opus 4.7",
		CreatedAt:   "2026-04-17T00:00:00Z",
	},
	{
		ID:          "claude-opus-4-8",
		Type:        "model",
		DisplayName: "Claude Opus 4.8",
		CreatedAt:   "2026-05-29T00:00:00Z",
	},
	{
		ID:          "claude-sonnet-5",
		Type:        "model",
		DisplayName: "Claude Sonnet 5",
		CreatedAt:   "2026-07-01T00:00:00Z",
	},
	{
		ID:          "claude-sonnet-4-6",
		Type:        "model",
		DisplayName: "Claude Sonnet 4.6",
		CreatedAt:   "2026-02-18T00:00:00Z",
	},
	{
		ID:          "claude-sonnet-4-5-20250929",
		Type:        "model",
		DisplayName: "Claude Sonnet 4.5",
		CreatedAt:   "2025-09-29T00:00:00Z",
	},
	{
		ID:          "claude-haiku-4-5-20251001",
		Type:        "model",
		DisplayName: "Claude Haiku 4.5",
		CreatedAt:   "2025-10-01T00:00:00Z",
	},
}

// DefaultModelIDs 返回默认模型的 ID 列表
func DefaultModelIDs() []string {
	ids := make([]string, len(DefaultModels))
	for i, m := range DefaultModels {
		ids[i] = m.ID
	}
	return ids
}

// DefaultTestModel 测试时使用的默认模型
const DefaultTestModel = "claude-sonnet-4-5-20250929"

// ModelIDOverrides Claude OAuth 请求需要的模型 ID 映射
var ModelIDOverrides = map[string]string{
	"claude-sonnet-4-5": "claude-sonnet-4-5-20250929",
	"claude-opus-4-5":   "claude-opus-4-5-20251101",
	"claude-haiku-4-5":  "claude-haiku-4-5-20251001",
}

// ModelIDReverseOverrides 用于将上游模型 ID 还原为短名
var ModelIDReverseOverrides = map[string]string{
	"claude-sonnet-4-5-20250929": "claude-sonnet-4-5",
	"claude-opus-4-5-20251101":   "claude-opus-4-5",
	"claude-haiku-4-5-20251001":  "claude-haiku-4-5",
}

// NormalizeModelID 根据 Claude OAuth 规则映射模型
func NormalizeModelID(id string) string {
	if id == "" {
		return id
	}
	if mapped, ok := ModelIDOverrides[id]; ok {
		return mapped
	}
	return id
}

// DenormalizeModelID 将上游模型 ID 转换为短名
func DenormalizeModelID(id string) string {
	if id == "" {
		return id
	}
	if mapped, ok := ModelIDReverseOverrides[id]; ok {
		return mapped
	}
	return id
}
