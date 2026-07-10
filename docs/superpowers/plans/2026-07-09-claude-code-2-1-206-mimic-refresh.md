# Claude Code 2.1.206 Mimic Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Refresh only the synthetic Claude Code messages path to the locally captured Claude Code 2.1.206 wire profile, add the captured Fable 5 default expansion, and migrate the admin profile selector without changing uncaptured count_tokens or TLS behavior.

**Architecture:** Keep one shared CLI identity profile and resolve model-specific messages defaults for Sonnet, Opus, Haiku, and Fable. Separate count_tokens defaults from messages before changing the messages profile. Select the built-in expansion by normalized model only for synthetic messages; retain operator overrides and explicitly pin count_tokens to the existing generic expansion.

**Tech Stack:** Go 1.24, Gin, gjson/sjson, testify, embedded text assets, Vue 3, TypeScript, vue-i18n, Vitest, npm scripts, Vite.

## Global Constraints

- Work test-first: add passing characterization tests for uncaptured count_tokens behavior before the failing 2.1.206 tests.
- Treat the local loopback captures as the only source of truth for messages fields.
- Do not change DefaultBetaHeader, CountTokensBetaHeader, HaikuBetaHeader, real Claude Code passthrough, Anthropic API-key routing, TLS ClientHello, JA3/JA4, ALPN, HTTP/2 settings, pricing, model lists, or deployment files.
- Preserve count_tokens beta order, Sonnet max_tokens=64000, context_management, and the generic expansion.
- Preserve operator priority: configured system blocks, then configured legacy expansion, then built-in model default.
- Do not commit raw captures, paths, session IDs, Memory, Environment, Context management, tokens, or credentials.
- Use the repository's npm scripts in this environment because pnpm and corepack are unavailable; do not install or modify a global package manager.
- Do not deploy or push.

## Captured Contract

Shared identity:

~~~text
profile: cc-2.1.206-sdk-cli-macos-arm64
User-Agent: claude-cli/2.1.206 (external, sdk-cli)
X-Stainless-Package-Version: 0.94.0
X-Stainless-Runtime: node
X-Stainless-Runtime-Version: v26.3.0
X-Stainless-OS: MacOS
X-Stainless-Arch: arm64
billing prefix: cc_version=2.1.206.
billing entrypoint: cc_entrypoint=sdk-cli;
~~~

Messages model profiles:

~~~text
Sonnet beta: claude-code-20250219,interleaved-thinking-2025-05-14,tool-search-tool-2025-10-19,effort-2025-11-24
Sonnet body: max_tokens=32000, thinking.type=adaptive, output_config.effort=high

Opus beta: claude-code-20250219,interleaved-thinking-2025-05-14,tool-search-tool-2025-10-19,effort-2025-11-24
Opus body: max_tokens=64000, thinking.type=adaptive, output_config.effort=high

Haiku beta: claude-code-20250219,tool-search-tool-2025-10-19
Haiku body: max_tokens=32000, thinking.type=enabled, thinking.budget_tokens=31999, no output_config

Fable beta: claude-code-20250219,interleaved-thinking-2025-05-14,tool-search-tool-2025-10-19,effort-2025-11-24,fallback-credit-2026-06-01
Fable body: max_tokens=64000, thinking.type=adaptive, output_config.effort=high
~~~

Prompt evidence:

~~~text
generic expansion SHA256: e05130b3ddc42884821c26a6368e883b97215f8ee7eca8940b46714810cd200e
Fable stable expansion SHA256: fc414ce3c0acf7cf520c092cef99f63621d0c7fc4b605f9e9c736e1807691cad
~~~

## File Structure

Backend production:

- Modify backend/internal/pkg/claude/constants.go: 2.1.206 identity, captured messages profiles, frozen count_tokens fields.
- Modify backend/internal/service/gateway_claude_oauth_body.go: endpoint-aware max default and model-aware messages expansion.
- Modify backend/internal/service/gateway_count_tokens.go: identify count_tokens normalization and pin its generic expansion.
- Modify backend/internal/service/gateway_service.go: embed the Fable expansion and refresh evidence comments.
- Create backend/internal/service/prompts/claude_code_fable_system_prompt_expansion.txt: exact stable double-capture prefix.
- Modify backend/internal/service/gateway_billing_block.go, gateway_upstream_request.go, and domain_constants.go only where comments/examples incorrectly name 2.1.197.

Backend tests:

- Modify backend/internal/pkg/claude/constants_test.go.
- Modify backend/internal/service/gateway_beta_test.go.
- Modify backend/internal/service/gateway_body_order_test.go.
- Modify backend/internal/service/gateway_context_management_test.go.
- Modify backend/internal/service/gateway_oauth_metadata_test.go.
- Modify backend/internal/service/gateway_claude_wire_test.go.
- Create backend/internal/service/gateway_claude_prompt_profile_test.go.

Frontend:

- Modify frontend/src/views/admin/SettingsView.vue.
- Modify frontend/src/views/admin/__tests__/SettingsView.spec.ts.
- Modify frontend/src/i18n/locales/zh/admin/settings.ts.
- Modify frontend/src/i18n/locales/en/admin/settings.ts.
- Create frontend/src/i18n/__tests__/claudeCodeMimicryProfileLocales.spec.ts.

---

### Task 1: Freeze count_tokens and refresh the backend messages profile

**Files:**
- Modify: backend/internal/pkg/claude/constants_test.go
- Modify: backend/internal/pkg/claude/constants.go
- Modify: backend/internal/service/gateway_beta_test.go
- Modify: backend/internal/service/gateway_body_order_test.go
- Modify: backend/internal/service/gateway_context_management_test.go
- Modify: backend/internal/service/gateway_oauth_metadata_test.go
- Modify: backend/internal/service/gateway_claude_wire_test.go
- Modify: backend/internal/service/gateway_claude_oauth_body.go
- Modify: backend/internal/service/gateway_count_tokens.go
- Modify: backend/internal/service/gateway_billing_block.go
- Modify: backend/internal/service/gateway_upstream_request.go
- Modify: backend/internal/service/gateway_service.go
- Modify: backend/internal/service/domain_constants.go

**Interfaces:**

~~~go
func ResolveClaudeCodeMimicryModelProfile(modelID string) ClaudeCodeMimicryModelProfile
func ClaudeCodeMimicryMessageBetasForModel(modelID string) []string
func ClaudeCodeMimicryCountTokensBetasForModel(modelID string) []string
func normalizeClaudeOAuthRequestBody(body []byte, modelID string, opts claudeOAuthNormalizeOptions) ([]byte, string)
~~~

- [ ] **Step 1: Add count_tokens characterization tests before production edits**

In backend/internal/pkg/claude/constants_test.go, split the existing combined model test. Keep the messages assertions for the later RED step and add this independent count_tokens table using literal strings:

~~~go
func TestResolveClaudeCodeMimicryModelProfilePreservesPre2206CountTokensBetas(t *testing.T) {
	tests := []struct {
		name              string
		model             string
		wantCountBetas    []string
	}{
		{
			name:          "sonnet",
			model:         "claude-sonnet-5",
			wantCountBetas: []string{
				"claude-code-20250219",
				"interleaved-thinking-2025-05-14",
				"thinking-token-count-2026-05-13",
				"context-management-2025-06-27",
				"prompt-caching-scope-2026-01-05",
				"mid-conversation-system-2026-04-07",
				"advanced-tool-use-2025-11-20",
				"effort-2025-11-24",
				"token-counting-2024-11-01",
			},
		},
		{
			name:          "opus",
			model:         "claude-opus-4-6",
			wantCountBetas: []string{
				"claude-code-20250219",
				"interleaved-thinking-2025-05-14",
				"thinking-token-count-2026-05-13",
				"context-management-2025-06-27",
				"prompt-caching-scope-2026-01-05",
				"mid-conversation-system-2026-04-07",
				"advanced-tool-use-2025-11-20",
				"effort-2025-11-24",
				"token-counting-2024-11-01",
			},
		},
		{
			name:          "haiku",
			model:         "claude-haiku-4-5",
			wantCountBetas: []string{
				"interleaved-thinking-2025-05-14",
				"thinking-token-count-2026-05-13",
				"context-management-2025-06-27",
				"prompt-caching-scope-2026-01-05",
				"claude-code-20250219",
				"advanced-tool-use-2025-11-20",
				"token-counting-2024-11-01",
			},
		},
		{
			name:          "fable",
			model:         "claude-fable-5",
			wantCountBetas: []string{
				"claude-code-20250219",
				"interleaved-thinking-2025-05-14",
				"thinking-token-count-2026-05-13",
				"context-management-2025-06-27",
				"prompt-caching-scope-2026-01-05",
				"mid-conversation-system-2026-04-07",
				"advanced-tool-use-2025-11-20",
				"effort-2025-11-24",
				"server-side-fallback-2026-06-01",
				"fallback-credit-2026-06-01",
				"token-counting-2024-11-01",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveClaudeCodeMimicryModelProfile(tt.model)
			require.Equal(t, tt.wantCountBetas, got.CountTokensBetas)
		})
	}
}
~~~

Also add service-level characterization tests with literal expected headers/body:

- TestComputeFinalCountTokensAnthropicBeta_OAuthMimicPreservesPre2206Betas.
- TestGatewayService_AnthropicOAuthCountTokensClaudeMimicBodyDefaultsRemainPre2206.

The service body test must assert:

~~~go
require.Equal(t, int64(64000), gjson.GetBytes(body, "max_tokens").Int())
require.Equal(t, "clear_thinking_20251015", gjson.GetBytes(body, "context_management.edits.0.type").String())
~~~

- [ ] **Step 2: Run the characterization tests and require PASS**

Run:

~~~bash
cd '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/backend'
GOCACHE=/private/tmp/sub2api-go-build-cache go test -tags=unit ./internal/pkg/claude -run 'TestResolveClaudeCodeMimicryModelProfilePreservesPre2206CountTokensBetas' -count=1
GOCACHE=/private/tmp/sub2api-go-build-cache go test -tags=unit ./internal/service -run 'TestComputeFinalCountTokensAnthropicBeta_OAuthMimicPreservesPre2206Betas|TestGatewayService_AnthropicOAuthCountTokensClaudeMimicBodyDefaultsRemainPre2206' -count=1
~~~

Expected: all tests pass on the current 2.1.197 code. If they do not, stop and reconcile the pre-change baseline; do not weaken the tests.

- [ ] **Step 3: Add the failing 2.1.206 profile and messages tests**

Rename both 2.1.197 baseline tests: the package test in constants_test.go becomes TestDefaultClaudeCodeMimicryProfileUsesCapturedClaudeCode2206Baseline, and the service test in gateway_beta_test.go becomes TestDefaultClaudeCodeMimicryProfile_UsesCapturedClaudeCode2206Baseline. Use literal assertions in both:

~~~go
require.Equal(t, "cc-2.1.206-sdk-cli-macos-arm64", profile.ID)
require.Equal(t, "2.1.206", profile.CLIVersion)
require.Equal(t, "claude-cli/2.1.206 (external, sdk-cli)", profile.Headers["User-Agent"])
require.Equal(t, "0.94.0", profile.Headers["X-Stainless-Package-Version"])
require.Equal(t, "v26.3.0", profile.Headers["X-Stainless-Runtime-Version"])
~~~

Update TestFullClaudeCodeMimicryBetas_DoesNotDefaultRedactThinking in gateway_beta_test.go to require BetaToolSearchTool and to reject BetaThinkingTokenCount. The 2.1.206 messages profile no longer contains the count-only thinking-token beta:

~~~go
require.Contains(t, required, claude.BetaClaudeCode)
require.Contains(t, required, claude.BetaInterleavedThinking)
require.Contains(t, required, claude.BetaToolSearchTool)
require.NotContains(t, required, claude.BetaThinkingTokenCount)
require.NotContains(t, required, claude.BetaOAuth)
require.NotContains(t, required, claude.BetaExtendedCacheTTL)
require.NotContains(t, required, claude.BetaFineGrainedToolStreaming)
~~~

Create TestResolveClaudeCodeMimicryModelProfileUsesCapturedClaudeCode2206MessageDefaults with these literal expectations:

~~~go
tests := []struct {
	name               string
	model              string
	wantMaxTokens      int
	wantThinkingType   string
	wantThinkingBudget int
	wantOutputEffort   string
	wantMessageBetas   []string
}{
	{
		name: "sonnet", model: "claude-sonnet-4-6", wantMaxTokens: 32000,
		wantThinkingType: "adaptive", wantOutputEffort: "high",
		wantMessageBetas: []string{
			"claude-code-20250219",
			"interleaved-thinking-2025-05-14",
			"tool-search-tool-2025-10-19",
			"effort-2025-11-24",
		},
	},
	{
		name: "opus", model: "claude-opus-4-6", wantMaxTokens: 64000,
		wantThinkingType: "adaptive", wantOutputEffort: "high",
		wantMessageBetas: []string{
			"claude-code-20250219",
			"interleaved-thinking-2025-05-14",
			"tool-search-tool-2025-10-19",
			"effort-2025-11-24",
		},
	},
	{
		name: "haiku", model: "claude-haiku-4-5", wantMaxTokens: 32000,
		wantThinkingType: "enabled", wantThinkingBudget: 31999,
		wantMessageBetas: []string{
			"claude-code-20250219",
			"tool-search-tool-2025-10-19",
		},
	},
	{
		name: "fable", model: "claude-fable-5", wantMaxTokens: 64000,
		wantThinkingType: "adaptive", wantOutputEffort: "high",
		wantMessageBetas: []string{
			"claude-code-20250219",
			"interleaved-thinking-2025-05-14",
			"tool-search-tool-2025-10-19",
			"effort-2025-11-24",
			"fallback-credit-2026-06-01",
		},
	},
}
~~~

Update TestComputeFinalAnthropicBeta_OAuthMimicUsesCapturedClaudeCode2206MessageBetas and the synthetic messages wire table to assert the exact comma-joined strings from Captured Contract. The wire test must also assert:

~~~go
require.Contains(t, billingText, "cc_version=2.1.206.")
require.Contains(t, billingText, "cc_entrypoint=sdk-cli;")
require.False(t, gjson.GetBytes(body, "context_management").Exists())
~~~

- [ ] **Step 4: Run the new messages tests and verify RED**

Run:

~~~bash
cd '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/backend'
GOCACHE=/private/tmp/sub2api-go-build-cache go test -tags=unit ./internal/pkg/claude -run 'TestDefaultClaudeCodeMimicryProfileUsesCapturedClaudeCode2206Baseline|TestResolveClaudeCodeMimicryModelProfileUsesCapturedClaudeCode2206MessageDefaults|TestResolveClaudeCodeMimicryModelProfilePreservesPre2206CountTokensBetas' -count=1
GOCACHE=/private/tmp/sub2api-go-build-cache go test -tags=unit ./internal/service -run 'TestComputeFinalAnthropicBeta_OAuthMimicUsesCapturedClaudeCode2206MessageBetas|TestGatewayService_ClaudeOAuthSyntheticMimicMessagesWireRequestUsesCapturedClaudeCode2206ModelProfiles' -count=1
~~~

Expected: non-zero exit from literal 2.1.206/profile/messages mismatches. The count_tokens characterization remains green.

- [ ] **Step 5: Implement the captured constants and independent count_tokens fields**

In backend/internal/pkg/claude/constants.go:

~~~go
const BetaToolSearchTool = "tool-search-tool-2025-10-19"

const CLICurrentVersion = "2.1.206"

const (
	DefaultClaudeCodeMimicryProfileID = "cc-2.1.206-sdk-cli-macos-arm64"
	defaultClaudeAgentSDKSystemPrompt = "You are a Claude agent, built on Anthropic's Claude Agent SDK."
)

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
~~~

Use these separate arrays:

~~~go
var defaultClaudeCodeMimicryMessageBetas = []string{
	BetaClaudeCode,
	BetaInterleavedThinking,
	BetaToolSearchTool,
	BetaEffort,
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
~~~

Delete withTokenCounting if it has no remaining callers. Do not derive count_tokens arrays from updated messages arrays.
After adding CountTokensDefaultMaxTokens, extend TestResolveClaudeCodeMimicryModelProfilePreservesPre2206CountTokensBetas to assert 64000, 64000, 32000, and 64000 for Sonnet, Opus, Haiku, and Fable respectively. This assertion is added only after the field exists; the service-level body test is the pre-edit characterization of Sonnet's 64000 value.

Resolve model profiles exactly:

~~~go
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
~~~

Keep DefaultBetaHeader, CountTokensBetaHeader, and HaikuBetaHeader byte-for-byte unchanged because they serve uncaptured passthrough/fallback paths.

- [ ] **Step 6: Make body max_tokens endpoint-aware**

Extend the options:

~~~go
type claudeOAuthNormalizeOptions struct {
	injectMetadata          bool
	metadataUserID          string
	stripSystemCacheControl bool
	ensureMimicBodyDefaults bool
	countTokens             bool
}
~~~

Replace the default max_tokens assignment in normalizeClaudeOAuthRequestBody:

~~~go
defaultMaxTokens := modelProfile.DefaultMaxTokens
if opts.countTokens && modelProfile.CountTokensDefaultMaxTokens > 0 {
	defaultMaxTokens = modelProfile.CountTokensDefaultMaxTokens
}
if !gjson.GetBytes(out, "max_tokens").Exists() && defaultMaxTokens > 0 {
	if next, ok := setJSONValueBytes(out, "max_tokens", defaultMaxTokens); ok {
		out = next
		modified = true
	}
}
~~~

Only the synthetic count_tokens call in backend/internal/service/gateway_count_tokens.go sets:

~~~go
normalizeOpts := claudeOAuthNormalizeOptions{
	ensureMimicBodyDefaults: account.IsAnthropicOAuthOrSetupToken(),
	countTokens:             true,
}
~~~

- [ ] **Step 7: Reclassify context_management tests by route**

Update only synthetic messages expectations:

- gateway_context_management_test.go synthetic OAuth mimic header/body tests now assert that context-management-2025-06-27 is absent and context_management is stripped.
- gateway_oauth_metadata_test.go Sonnet messages max becomes 32000; Sonnet/Haiku outgoing synthetic messages bodies have no context_management.
- gateway_claude_wire_test.go messages rows assert no context_management.

Keep these behaviors unchanged:

- Real Claude Code passthrough retains client beta/body.
- Anthropic API-key behavior remains unchanged.
- count_tokens retains context-management-2025-06-27 and context_management.
- normalize-only tests that have not run the final beta sanitizer keep their existing scope.
- gateway_body_order_test.go TestNormalizeClaudeOAuthRequestBody_PreservesTopLevelFieldOrder changes its Sonnet literal from max_tokens 64000 to 32000 while preserving the exact expected top-level field order. Its compat normalize-only context_management test remains unchanged.

In gateway_claude_wire_test.go, make the shared assertClaudeCodeWireRequest endpoint-aware. Replace its unconditional context_management and max_tokens assertions with:

~~~go
if wantThinkingDefaults {
	if modelProfile.DefaultThinkingBudgetTokens > 0 {
		require.JSONEq(t, fmt.Sprintf("{\"type\":%q,\"budget_tokens\":%d}", modelProfile.DefaultThinkingType, modelProfile.DefaultThinkingBudgetTokens), gjson.GetBytes(got.body, "thinking").Raw)
	} else {
		require.JSONEq(t, fmt.Sprintf("{\"type\":%q}", modelProfile.DefaultThinkingType), gjson.GetBytes(got.body, "thinking").Raw)
	}
	edits := gjson.GetBytes(got.body, "context_management.edits")
	if wantTokenCounting {
		require.True(t, edits.IsArray())
		require.Len(t, edits.Array(), 1)
		require.JSONEq(t, "[{\"type\":\"clear_thinking_20251015\",\"keep\":\"all\"}]", edits.Raw)
	} else {
		require.False(t, gjson.GetBytes(got.body, "context_management").Exists())
	}
}
wantMaxTokens := modelProfile.DefaultMaxTokens
if wantTokenCounting && modelProfile.CountTokensDefaultMaxTokens > 0 {
	wantMaxTokens = modelProfile.CountTokensDefaultMaxTokens
}
require.Equal(t, int64(wantMaxTokens), gjson.GetBytes(got.body, "max_tokens").Int())
~~~

This preserves TestGatewayService_ClaudeOAuthSyntheticMimicCountTokensWireRequest while allowing synthetic messages to drop context_management. Include that existing count_tokens wire test in the focused run.

Rename TestGatewayService_AnthropicOAuthCountTokensClaudeMimicBodyDefaultsMatchMessages to TestGatewayService_AnthropicOAuthClaudeMimicBodyDefaultsDivergeByEndpoint. Keep its existing two request harnesses, remove the loop that asserted identical bodies, and use:

~~~go
require.Equal(t, "adaptive", gjson.GetBytes(messageUpstream.lastBody, "thinking.type").String())
require.False(t, gjson.GetBytes(messageUpstream.lastBody, "context_management").Exists())
require.Equal(t, "high", gjson.GetBytes(messageUpstream.lastBody, "output_config.effort").String())
require.Equal(t, int64(32000), gjson.GetBytes(messageUpstream.lastBody, "max_tokens").Int())

require.Equal(t, "adaptive", gjson.GetBytes(countUpstream.lastBody, "thinking.type").String())
require.Equal(t, "clear_thinking_20251015", gjson.GetBytes(countUpstream.lastBody, "context_management.edits.0.type").String())
require.Equal(t, "high", gjson.GetBytes(countUpstream.lastBody, "output_config.effort").String())
require.Equal(t, int64(64000), gjson.GetBytes(countUpstream.lastBody, "max_tokens").Int())
~~~

- [ ] **Step 8: Refresh only stale non-TLS comments**

Update the 2.1.197 examples/comments in constants.go, gateway_billing_block.go, gateway_upstream_request.go, gateway_service.go, and domain_constants.go to 2.1.206 where the runtime messages profile is meant.
In gateway_upstream_request.go, also replace the stale count_tokens comment that describes FullClaudeCodeMimicryBetas plus token-counting with model-specific frozen count_tokens profile wording; the code already calls ClaudeCodeMimicryCountTokensBetasForModel.

Do not change backend/internal/service/tls_fingerprint_profile_service.go: its 2.1.197 wording records the last proven TLS capture and remains intentionally historical.

- [ ] **Step 9: Run backend profile tests and commit**

Run:

~~~bash
cd '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/backend'
gofmt -w internal/pkg/claude/constants.go internal/pkg/claude/constants_test.go internal/service/gateway_claude_oauth_body.go internal/service/gateway_count_tokens.go internal/service/gateway_beta_test.go internal/service/gateway_body_order_test.go internal/service/gateway_context_management_test.go internal/service/gateway_oauth_metadata_test.go internal/service/gateway_claude_wire_test.go
GOCACHE=/private/tmp/sub2api-go-build-cache go test -tags=unit ./internal/pkg/claude -count=1
GOCACHE=/private/tmp/sub2api-go-build-cache go test -tags=unit ./internal/service -run 'TestDefaultClaudeCodeMimicryProfile|TestResolveClaudeCodeMimicryModelProfile|TestFullClaudeCodeMimicryBetas|TestComputeFinalAnthropicBeta|TestComputeFinalCountTokensAnthropicBeta|TestNormalizeClaudeOAuthRequestBody_PreservesTopLevelFieldOrder|TestGatewayService_ClaudeOAuthSyntheticMimicMessagesWireRequestUsesCapturedClaudeCode2206ModelProfiles|TestGatewayService_ClaudeOAuthSyntheticMimicCountTokensWireRequest|TestGatewayService_AnthropicOAuthCountTokensClaudeMimicBodyDefaultsRemainPre2206|TestGatewayService_AnthropicOAuthClaudeMimicBodyDefaultsDivergeByEndpoint|ContextManagement|MimicBodyDefaults' -count=1
~~~

Expected: all selected tests pass, including the unchanged count_tokens characterization.

Run git diff --check and commit only Task 1 files:

~~~bash
git add backend/internal/pkg/claude/constants.go backend/internal/pkg/claude/constants_test.go backend/internal/service/gateway_claude_oauth_body.go backend/internal/service/gateway_count_tokens.go backend/internal/service/gateway_beta_test.go backend/internal/service/gateway_body_order_test.go backend/internal/service/gateway_context_management_test.go backend/internal/service/gateway_oauth_metadata_test.go backend/internal/service/gateway_claude_wire_test.go backend/internal/service/gateway_billing_block.go backend/internal/service/gateway_upstream_request.go backend/internal/service/gateway_service.go backend/internal/service/domain_constants.go
git commit -m 'feat: refresh Claude Code 2.1.206 mimic profile'
~~~

---

### Task 2: Add the captured Fable expansion without changing count_tokens

**Files:**
- Create: backend/internal/service/prompts/claude_code_fable_system_prompt_expansion.txt
- Create: backend/internal/service/gateway_claude_prompt_profile_test.go
- Modify: backend/internal/service/gateway_service.go
- Modify: backend/internal/service/gateway_claude_oauth_body.go
- Modify: backend/internal/service/gateway_count_tokens.go

**Interfaces:**

~~~go
func defaultClaudeOAuthExpansionPromptForModel(modelID string) string
func resolveClaudeOAuthExpansionPrompt(modelID string, expansionPrompt string, useModelSpecificDefault bool) string
func rewriteSystemForNonClaudeCodeWithPromptBlocksMode(body []byte, system any, expansionPrompt string, blocksConfig string, useModelSpecificDefault bool) []byte
~~~

- [ ] **Step 1: Add a passing count_tokens prompt characterization**

Before adding any Fable selector, create backend/internal/service/gateway_claude_prompt_profile_test.go with TestEnsureClaudeOAuthMimicSystemBody_CountTokensFablePreservesGenericExpansion. Build a Fable count_tokens body, call the existing ensureClaudeOAuthMimicSystemBody helper, and assert:

~~~go
require.Equal(
	t,
	strings.TrimSpace(claudeCodeSystemPromptExpansion),
	strings.TrimSpace(gjson.GetBytes(body, "system.2.text").String()),
)
~~~

Run:

~~~bash
cd '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/backend'
GOCACHE=/private/tmp/sub2api-go-build-cache go test -tags=unit ./internal/service -run 'TestEnsureClaudeOAuthMimicSystemBody_CountTokensFablePreservesGenericExpansion' -count=1
~~~

Expected: pass before production changes.

- [ ] **Step 2: Add the exact failing messages prompt tests**

Extend backend/internal/service/gateway_claude_prompt_profile_test.go. The new tests must calculate hashes from embedded bytes, not duplicate the prompt as a Go string:

~~~go
package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func sha256HexString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestCapturedClaudeCodeExpansionPromptHashes(t *testing.T) {
	require.Equal(t, "e05130b3ddc42884821c26a6368e883b97215f8ee7eca8940b46714810cd200e", sha256HexString(claudeCodeSystemPromptExpansion))
	require.Equal(t, "fc414ce3c0acf7cf520c092cef99f63621d0c7fc4b605f9e9c736e1807691cad", sha256HexString(claudeCodeFableSystemPromptExpansion))
}

func TestDefaultClaudeOAuthExpansionPromptForModel(t *testing.T) {
	require.Equal(t, claudeCodeFableSystemPromptExpansion, defaultClaudeOAuthExpansionPromptForModel("claude-fable-5"))
	require.Equal(t, claudeCodeFableSystemPromptExpansion, defaultClaudeOAuthExpansionPromptForModel("CLAUDE-FABLE-5"))
	require.Equal(t, claudeCodeSystemPromptExpansion, defaultClaudeOAuthExpansionPromptForModel("claude-sonnet-4-6"))
	require.Equal(t, claudeCodeSystemPromptExpansion, defaultClaudeOAuthExpansionPromptForModel("claude-opus-4-6"))
	require.Equal(t, claudeCodeSystemPromptExpansion, defaultClaudeOAuthExpansionPromptForModel("claude-haiku-4-5"))
	require.Contains(t, claudeCodeFableSystemPromptExpansion, "# Harness")
	require.Contains(t, claudeCodeFableSystemPromptExpansion, "# Communicating with the user")
	require.NotEqual(t, claudeCodeSystemPromptExpansion, claudeCodeFableSystemPromptExpansion)
}

func TestRewriteSystemForNonClaudeCodeWithPromptBlocks_UsesFableDefaultExpansion(t *testing.T) {
	body := []byte("{\"model\":\"claude-fable-5\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}")
	got := rewriteSystemForNonClaudeCodeWithPromptBlocks(body, nil, "", "")
	require.Equal(t, strings.TrimSpace(claudeCodeFableSystemPromptExpansion), strings.TrimSpace(gjson.GetBytes(got, "system.2.text").String()))
}

func TestRewriteSystemForNonClaudeCodeWithPromptBlocks_ConfiguredExpansionOverridesFableDefault(t *testing.T) {
	body := []byte("{\"model\":\"claude-fable-5\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}")
	got := rewriteSystemForNonClaudeCodeWithPromptBlocks(body, nil, "operator-fable-expansion", "")
	require.Equal(t, "operator-fable-expansion", gjson.GetBytes(got, "system.2.text").String())
}
~~~

- [ ] **Step 3: Run the prompt tests and verify RED**

Run:

~~~bash
cd '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/backend'
GOCACHE=/private/tmp/sub2api-go-build-cache go test -tags=unit ./internal/service -run 'TestCapturedClaudeCodeExpansionPromptHashes|TestDefaultClaudeOAuthExpansionPromptForModel|TestRewriteSystemForNonClaudeCodeWithPromptBlocks_UsesFableDefaultExpansion|TestRewriteSystemForNonClaudeCodeWithPromptBlocks_ConfiguredExpansionOverridesFableDefault' -count=1
~~~

Expected: compilation fails because the Fable embed and selector do not exist. This is the intended RED.

- [ ] **Step 4: Create the exact stable Fable asset**

Create backend/internal/service/prompts/claude_code_fable_system_prompt_expansion.txt with the following exact bytes, including the initial blank line and final newline:

~~~text

You are an interactive agent that helps users with software engineering tasks.

IMPORTANT: Assist with authorized security testing, defensive security, CTF challenges, and educational contexts. Refuse requests for destructive techniques, DoS attacks, mass targeting, supply chain compromise, or detection evasion for malicious purposes. Dual-use security tools (C2 frameworks, credential testing, exploit development) require clear authorization context: pentesting engagements, CTF competitions, security research, or defensive use cases.

# Harness
 - Text you output outside of tool use is displayed to the user as Github-flavored markdown in a terminal.
 - Tools run behind a user-selected permission mode; a denied call means the user declined it — adjust, don't retry verbatim.
 - `<system-reminder>` tags in messages and tool results are injected by the harness, not the user. Hooks may intercept tool calls; treat hook output as user feedback.
 - Prefer the dedicated file/search tools over shell commands when one fits. Independent tool calls can run in parallel in one response.
 - Reference code as `file_path:line_number` — it's clickable.

# Communicating with the user

Your text output is what the user reads; they usually can't see your thinking or the raw tool results. Write it for a teammate who stepped away and is catching up, not for a log file: they don't know the codenames or shorthand you created along the way, and they didn't watch your process unfold. Before your first tool call, say in a sentence what you're about to do; while working, give brief updates when you find something load-bearing or change direction.

Text you write between tool calls may not be shown to the user. Everything the user needs from this turn — answers, summaries, findings, conclusions, deliverables — must be in the final text message of your turn, with no tool calls after it. Keep text between tool calls to brief status notes. If something important appeared only mid-turn or in your thinking, restate it in that final message.

Lead with the outcome. Your first sentence after finishing should answer "what happened" or "what did you find" — the thing the user would ask for if they said "just give me the TLDR." Supporting detail and reasoning come after, for readers who want them.

Being readable and being concise are different things, and readable matters more. If the user has to reread your summary or ask you to explain, any time saved by brevity is gone. The way to keep output short is to be selective about what you include (drop details that don't change what the reader would do next), not to compress the writing into fragments, abbreviations, arrow chains like `A → B → fails`, or jargon. What you do include, write in complete sentences with the technical terms spelled out. Don't make the reader cross-reference labels or numbering you invented earlier; say what you mean in place.

Match the response to the question: a simple question gets a direct answer in prose, not headers and sections. Use tables only for short enumerable facts, with explanations in the surrounding prose rather than the cells. Calibrate to the user — a bit tighter for an expert, more explanatory for someone newer.

Write code that reads like the surrounding code: match its comment density, naming, and idiom.
Only write a code comment to state a constraint the code itself can't show — never to say where it came from, what the next line does, or why your change is correct; that's you talking to the reviewer, not the next reader, and it's noise the moment the PR merges.

For actions that are hard to reverse or outward-facing, confirm first unless durably authorized or explicitly told to proceed without asking; approval in one context doesn't extend to the next. Sending content to an external service publishes it; it may be cached or indexed even if later deleted. Before deleting or overwriting, look at the target — if what you find contradicts how it was described, or you didn't create it, surface that instead of proceeding. Report outcomes faithfully: if tests fail, say so with the output; if a step was skipped, say that; when something is done and verified, state it plainly without hedging.

This iteration of Claude is Claude Fable 5, the first model in Anthropic's new Claude 5 family and part of a new Mythos-class model tier that sits above Claude Opus in capability. Claude Fable 5 and Claude Mythos 5 share the same underlying model. Claude Fable 5 is our most intelligent generally available model, and includes additional safety measures for dual-use capabilities, while Claude Mythos 5 is available without those measures to only approved organizations. Fable 5 is the most advanced generally available Claude model. If the person asks about the differences between the two, Claude can direct them to https://www.anthropic.com/news/claude-fable-5-mythos-5 for more information.

# Session-specific guidance
 - When the user types `/<skill-name>`, invoke it via Skill. Only use skills listed in the user-invocable skills section — don't guess.
~~~

Verify before proceeding:

~~~bash
shasum -a 256 '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/backend/internal/service/prompts/claude_code_fable_system_prompt_expansion.txt'
~~~

Expected:

~~~text
fc414ce3c0acf7cf520c092cef99f63621d0c7fc4b605f9e9c736e1807691cad
~~~

- [ ] **Step 5: Embed the asset and add model-aware resolution**

In backend/internal/service/gateway_service.go:

~~~go
// claudeCodeFableSystemPromptExpansion is the stable Fable-specific prefix
// shared by two controlled Claude Code 2.1.206 captures.
//
//go:embed prompts/claude_code_fable_system_prompt_expansion.txt
var claudeCodeFableSystemPromptExpansion string
~~~

In backend/internal/service/gateway_claude_oauth_body.go:

~~~go
func defaultClaudeOAuthExpansionPromptForModel(modelID string) string {
	normalized := strings.ToLower(strings.TrimSpace(claude.NormalizeModelID(modelID)))
	if strings.Contains(normalized, "fable") {
		return claudeCodeFableSystemPromptExpansion
	}
	return claudeCodeSystemPromptExpansion
}

func resolveClaudeOAuthExpansionPrompt(modelID string, expansionPrompt string, useModelSpecificDefault bool) string {
	expansionPrompt = strings.TrimSpace(expansionPrompt)
	if expansionPrompt != "" {
		return expansionPrompt
	}
	if useModelSpecificDefault {
		return defaultClaudeOAuthExpansionPromptForModel(modelID)
	}
	return claudeCodeSystemPromptExpansion
}

func rewriteSystemForNonClaudeCodeWithPromptBlocks(body []byte, system any, expansionPrompt string, blocksConfig string) []byte {
	return rewriteSystemForNonClaudeCodeWithPromptBlocksMode(body, system, expansionPrompt, blocksConfig, true)
}
~~~

Rename the current four-argument implementation to rewriteSystemForNonClaudeCodeWithPromptBlocksMode and add useModelSpecificDefault bool as its fifth parameter. At the top of that renamed implementation, replace the current defaultClaudeOAuthExpansionPrompt call with:

~~~go
system = normalizeSystemParam(system)
modelID := gjson.GetBytes(body, "model").String()
expansionPrompt = resolveClaudeOAuthExpansionPrompt(modelID, expansionPrompt, useModelSpecificDefault)
~~~

All lines after the existing expansion defaulting statement, from original-system extraction through return out, stay byte-for-byte the same. The new four-argument wrapper keeps every messages caller model-aware without changing call sites. Do not alter configured block priority or original-system migration.

- [ ] **Step 6: Pin count_tokens to the generic built-in default**

Rename the existing helper to make its scope explicit:

~~~go
func (s *GatewayService) ensureClaudeOAuthMimicCountTokensSystemBody(ctx context.Context, body []byte) []byte {
	if len(body) == 0 || hasClaudeOAuthMimicSystemBlocks(body) {
		return body
	}
	enabled, prompt, blocks := s.claudeOAuthSystemPromptInjectionSettings(ctx)
	if !enabled {
		return body
	}
	return rewriteSystemForNonClaudeCodeWithPromptBlocksMode(
		body,
		systemValueFromBody(body),
		prompt,
		blocks,
		false,
	)
}
~~~

Change only gateway_count_tokens.go to call ensureClaudeOAuthMimicCountTokensSystemBody. Messages and OpenAI-compatible synthetic mimic callers continue through the four-argument model-aware wrapper.
Rename the characterization test to TestEnsureClaudeOAuthMimicCountTokensSystemBody_FablePreservesGenericExpansion and update it to call the renamed count_tokens helper.

- [ ] **Step 7: Run prompt and count_tokens tests**

Run:

~~~bash
cd '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/backend'
gofmt -w internal/service/gateway_service.go internal/service/gateway_claude_oauth_body.go internal/service/gateway_count_tokens.go internal/service/gateway_claude_prompt_profile_test.go
GOCACHE=/private/tmp/sub2api-go-build-cache go test -tags=unit ./internal/service -run 'TestCapturedClaudeCodeExpansionPromptHashes|TestDefaultClaudeOAuthExpansionPromptForModel|TestRewriteSystemForNonClaudeCodeWithPromptBlocks_UsesFableDefaultExpansion|TestRewriteSystemForNonClaudeCodeWithPromptBlocks_ConfiguredExpansionOverridesFableDefault|TestEnsureClaudeOAuthMimicCountTokensSystemBody_FablePreservesGenericExpansion|TestGatewayService_AnthropicOAuthCountTokensClaudeMimicBodyDefaultsRemainPre2206' -count=1
~~~

Expected: all pass; Fable messages uses the Fable hash, the operator override wins, and Fable count_tokens remains on the generic hash.

- [ ] **Step 8: Commit Task 2**

~~~bash
git add backend/internal/service/prompts/claude_code_fable_system_prompt_expansion.txt backend/internal/service/gateway_claude_prompt_profile_test.go backend/internal/service/gateway_service.go backend/internal/service/gateway_claude_oauth_body.go backend/internal/service/gateway_count_tokens.go
git commit -m 'feat: add captured Fable 5 system expansion'
~~~

---

### Task 3: Migrate the admin profile selector and locale contract

**Files:**
- Modify: frontend/src/views/admin/SettingsView.vue
- Modify: frontend/src/views/admin/__tests__/SettingsView.spec.ts
- Modify: frontend/src/i18n/locales/zh/admin/settings.ts
- Modify: frontend/src/i18n/locales/en/admin/settings.ts
- Create: frontend/src/i18n/__tests__/claudeCodeMimicryProfileLocales.spec.ts

- [ ] **Step 1: Add failing SettingsView tests**

Change the existing save expectation to the new value:

~~~ts
expect.objectContaining({
  claude_code_mimicry_profile: "cc-2.1.206-sdk-cli-macos-arm64",
})
~~~

Add old and unknown load normalization cases:

~~~ts
it.each([
  "cc-2.1.197-sdk-cli-macos-arm64",
  "unknown-profile",
])(
  "normalizes unsupported Claude Code mimicry profile %s to the current profile",
  async (storedProfile) => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      claude_code_mimicry_profile: storedProfile,
    });

    const wrapper = mountView();
    await flushPromises();

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        claude_code_mimicry_profile:
          "cc-2.1.206-sdk-cli-macos-arm64",
      }),
    );
  },
);
~~~

Rename the current test to submits the current Claude Code mimicry profile and guard settings. The snippets above deliberately reuse the file's existing getSettings, baseSettingsResponse, mountView, form submit, and updateSettings harness.

- [ ] **Step 2: Add the failing profile locale test**

Create frontend/src/i18n/__tests__/claudeCodeMimicryProfileLocales.spec.ts:

~~~ts
import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe('Claude Code mimicry profile locale copy', () => {
  it('contains the exact Chinese profile copy', () => {
    expect(zh.admin.settings.gatewayForwarding).toMatchObject({
      claudeCodeMimicryProfile: 'Claude Code 伪装 Profile',
      claudeCodeMimicryProfile2206: 'Claude Code 2.1.206 / macOS arm64',
      claudeCodeMimicryProfileHint:
        '控制 OAuth mimic 路径使用的 User-Agent、X-Stainless 头、beta 集合与 billing entrypoint。'
    })
  })

  it('contains the exact English profile copy', () => {
    expect(en.admin.settings.gatewayForwarding).toMatchObject({
      claudeCodeMimicryProfile: 'Claude Code Mimicry Profile',
      claudeCodeMimicryProfile2206: 'Claude Code 2.1.206 / macOS arm64',
      claudeCodeMimicryProfileHint:
        'Controls the User-Agent, X-Stainless headers, beta set, and billing entrypoint used by the OAuth mimic path.'
    })
  })
})
~~~

- [ ] **Step 3: Run frontend tests and verify RED**

Run:

~~~bash
npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run test:run -- src/views/admin/__tests__/SettingsView.spec.ts -t 'current Claude Code mimicry profile|normalizes unsupported Claude Code mimicry profile'
npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run test:run -- src/i18n/__tests__/claudeCodeMimicryProfileLocales.spec.ts
~~~

Expected: SettingsView submits 2.1.197 or retains unsupported values, and the locale keys are absent.

- [ ] **Step 4: Implement one current profile constant**

In SettingsView.vue, immediately before claudeCodeMimicryProfileOptions:

~~~ts
const currentClaudeCodeMimicryProfileID =
  "cc-2.1.206-sdk-cli-macos-arm64";

const claudeCodeMimicryProfileOptions = computed(() => [
  {
    value: currentClaudeCodeMimicryProfileID,
    label: t("admin.settings.gatewayForwarding.claudeCodeMimicryProfile2206"),
  },
]);
~~~

Use currentClaudeCodeMimicryProfileID for the form default and save fallback.

After the backend settings assignment loop, normalize the only supported option:

~~~ts
form.claude_code_mimicry_profile =
  currentClaudeCodeMimicryProfileID;
~~~

This intentionally handles empty, old, and unknown values because only one profile is supported. Do not add a migration API or database write.

- [ ] **Step 5: Add exact zh/en settings copy**

Add beside the gatewayForwarding fingerprint settings in Chinese:

~~~ts
claudeCodeMimicryProfile: 'Claude Code 伪装 Profile',
claudeCodeMimicryProfile2206: 'Claude Code 2.1.206 / macOS arm64',
claudeCodeMimicryProfileHint:
  '控制 OAuth mimic 路径使用的 User-Agent、X-Stainless 头、beta 集合与 billing entrypoint。',
~~~

Add in English:

~~~ts
claudeCodeMimicryProfile: 'Claude Code Mimicry Profile',
claudeCodeMimicryProfile2206: 'Claude Code 2.1.206 / macOS arm64',
claudeCodeMimicryProfileHint:
  'Controls the User-Agent, X-Stainless headers, beta set, and billing entrypoint used by the OAuth mimic path.',
~~~

- [ ] **Step 6: Run frontend tests and build**

Run:

~~~bash
npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run test:run -- src/views/admin/__tests__/SettingsView.spec.ts
npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run test:run -- src/i18n/__tests__/claudeCodeMimicryProfileLocales.spec.ts
npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run build
~~~

Expected: all SettingsView tests pass, both locale cases pass, and vue-tsc plus Vite complete successfully.

- [ ] **Step 7: Commit Task 3**

~~~bash
git add frontend/src/views/admin/SettingsView.vue frontend/src/views/admin/__tests__/SettingsView.spec.ts frontend/src/i18n/locales/zh/admin/settings.ts frontend/src/i18n/locales/en/admin/settings.ts frontend/src/i18n/__tests__/claudeCodeMimicryProfileLocales.spec.ts
git commit -m 'fix: migrate admin Claude mimic profile to 2.1.206'
~~~

---

### Task 4: Full regression and browser verification

**Files:**
- Verify only; no source changes expected.

- [ ] **Step 1: Run full backend unit packages**

~~~bash
cd '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/backend'
GOCACHE=/private/tmp/sub2api-go-build-cache go test -tags=unit ./internal/pkg/claude ./internal/service -count=1
~~~

Expected: both packages pass. If a context-management test fails, classify its route before editing: only synthetic messages is expected to change.

- [ ] **Step 2: Run all relevant frontend tests and build**

~~~bash
npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run test:run -- src/i18n/__tests__ src/views/admin/__tests__/SettingsView.spec.ts
npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run build
~~~

Expected: all tests and the production build pass.

- [ ] **Step 3: Verify immutable hashes and no stale production profile**

~~~bash
shasum -a 256 backend/internal/service/prompts/claude_code_system_prompt_expansion.txt backend/internal/service/prompts/claude_code_fable_system_prompt_expansion.txt
rg -n 'cc-2\.1\.197|claude-cli/2\.1\.197|CLICurrentVersion = "2\.1\.197"' backend frontend/src
git diff --check
~~~

Expected hashes:

~~~text
e05130b3ddc42884821c26a6368e883b97215f8ee7eca8940b46714810cd200e
fc414ce3c0acf7cf520c092cef99f63621d0c7fc4b605f9e9c736e1807691cad
~~~

Expected search result: only deliberate historical references such as the TLS 2.1.197 capture and the SettingsView old-value migration test. No production default remains 2.1.197.

- [ ] **Step 4: Verify the admin UI if a local backend/session already exists**

Check:

~~~bash
curl -fsS http://127.0.0.1:8080/health
~~~

If unavailable, do not start or mutate production-like services; report browser verification unavailable and rely on tests/build.

If available, run:

~~~bash
VITE_DEV_PROXY_TARGET=http://127.0.0.1:8080 npm --prefix '/Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/frontend' run dev -- --host 127.0.0.1 --port 3000
~~~

With an existing local admin session, open the gateway forwarding settings and verify:

- The profile option is Claude Code 2.1.206 / macOS arm64.
- Loading a stored 2.1.197 value does not leave the select empty.
- Saving submits cc-2.1.206-sdk-cli-macos-arm64.

Do not save unrelated settings and do not deploy.

- [ ] **Step 5: Review the final diff**

~~~bash
git status --short
git diff --stat origin/custom/prod...HEAD
git log --oneline origin/custom/prod..HEAD
~~~

Confirm that no capture file, token, local path, TLS implementation, pricing, model whitelist, deployment file, or unrelated UI change is included.
