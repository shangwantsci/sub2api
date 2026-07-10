package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"

	"github.com/stretchr/testify/require"
)

func TestMergeAnthropicBeta(t *testing.T) {
	got := mergeAnthropicBeta(
		[]string{"oauth-2025-04-20", "interleaved-thinking-2025-05-14"},
		"foo, oauth-2025-04-20,bar, foo",
	)
	require.Equal(t, "oauth-2025-04-20,interleaved-thinking-2025-05-14,foo,bar", got)
}

func TestMergeAnthropicBeta_EmptyIncoming(t *testing.T) {
	got := mergeAnthropicBeta(
		[]string{"oauth-2025-04-20", "interleaved-thinking-2025-05-14"},
		"",
	)
	require.Equal(t, "oauth-2025-04-20,interleaved-thinking-2025-05-14", got)
}

func TestStripBetaTokens(t *testing.T) {
	tests := []struct {
		name   string
		header string
		tokens []string
		want   string
	}{
		{
			name:   "single token in middle",
			header: "oauth-2025-04-20,context-1m-2025-08-07,interleaved-thinking-2025-05-14",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "single token at start",
			header: "context-1m-2025-08-07,oauth-2025-04-20,interleaved-thinking-2025-05-14",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "single token at end",
			header: "oauth-2025-04-20,interleaved-thinking-2025-05-14,context-1m-2025-08-07",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "token not present",
			header: "oauth-2025-04-20,interleaved-thinking-2025-05-14",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "empty header",
			header: "",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "",
		},
		{
			name:   "with spaces",
			header: "oauth-2025-04-20, context-1m-2025-08-07 , interleaved-thinking-2025-05-14",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "only token",
			header: "context-1m-2025-08-07",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "",
		},
		{
			name:   "nil tokens",
			header: "oauth-2025-04-20,interleaved-thinking-2025-05-14",
			tokens: nil,
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "multiple tokens removed",
			header: "oauth-2025-04-20,context-1m-2025-08-07,interleaved-thinking-2025-05-14,fast-mode-2026-02-01",
			tokens: []string{"context-1m-2025-08-07", "fast-mode-2026-02-01"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "DroppedBetas is empty (filtering moved to configurable beta policy)",
			header: "oauth-2025-04-20,context-1m-2025-08-07,fast-mode-2026-02-01,interleaved-thinking-2025-05-14",
			tokens: claude.DroppedBetas,
			want:   "oauth-2025-04-20,context-1m-2025-08-07,fast-mode-2026-02-01,interleaved-thinking-2025-05-14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripBetaTokens(tt.header, tt.tokens)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMergeAnthropicBetaDropping_Context1M(t *testing.T) {
	required := []string{"oauth-2025-04-20", "interleaved-thinking-2025-05-14"}
	incoming := "context-1m-2025-08-07,foo-beta,oauth-2025-04-20"
	drop := map[string]struct{}{"context-1m-2025-08-07": {}}

	got := mergeAnthropicBetaDropping(required, incoming, drop)
	require.Equal(t, "oauth-2025-04-20,interleaved-thinking-2025-05-14,foo-beta", got)
	require.NotContains(t, got, "context-1m-2025-08-07")
}

func TestMergeAnthropicBetaDropping_DroppedBetas(t *testing.T) {
	required := []string{"oauth-2025-04-20", "interleaved-thinking-2025-05-14"}
	incoming := "context-1m-2025-08-07,fast-mode-2026-02-01,foo-beta,oauth-2025-04-20"
	// DroppedBetas is now empty — filtering moved to configurable beta policy.
	// Without a policy filter set, nothing gets dropped from the static set.
	drop := droppedBetaSet()

	got := mergeAnthropicBetaDropping(required, incoming, drop)
	require.Equal(t, "oauth-2025-04-20,interleaved-thinking-2025-05-14,context-1m-2025-08-07,fast-mode-2026-02-01,foo-beta", got)
	require.Contains(t, got, "context-1m-2025-08-07")
	require.Contains(t, got, "fast-mode-2026-02-01")
}

func TestFullClaudeCodeMimicryBetas_DoesNotDefaultRedactThinking(t *testing.T) {
	required := claude.FullClaudeCodeMimicryBetas()

	require.NotContains(t, required, claude.BetaRedactThinking)
	require.Contains(t, required, claude.BetaClaudeCode)
	require.Contains(t, required, claude.BetaInterleavedThinking)
	require.Contains(t, required, claude.BetaToolSearchTool)
	require.NotContains(t, required, claude.BetaThinkingTokenCount)
	require.NotContains(t, required, claude.BetaOAuth)
	require.NotContains(t, required, claude.BetaExtendedCacheTTL)
	require.NotContains(t, required, claude.BetaFineGrainedToolStreaming)
}

func TestDefaultClaudeCodeMimicryProfile_UsesCapturedClaudeCode2206Baseline(t *testing.T) {
	profile := claude.DefaultClaudeCodeMimicryProfile()

	require.Equal(t, claude.DefaultClaudeCodeMimicryProfileID, profile.ID)
	require.Equal(t, "cc-2.1.206-sdk-cli-macos-arm64", profile.ID)
	require.Equal(t, "2.1.206", profile.CLIVersion)
	require.Equal(t, "sdk-cli", profile.BillingEntrypoint)
	require.Equal(t, "You are a Claude agent, built on Anthropic's Claude Agent SDK.", profile.SystemPrompt)
	require.Equal(t, "claude-cli/2.1.206 (external, sdk-cli)", profile.Headers["User-Agent"])
	require.Equal(t, "0.94.0", profile.Headers["X-Stainless-Package-Version"])
	require.Equal(t, "v26.3.0", profile.Headers["X-Stainless-Runtime-Version"])
	require.Equal(t, "MacOS", profile.Headers["X-Stainless-OS"])
	require.Equal(t, "arm64", profile.Headers["X-Stainless-Arch"])
	require.Equal(t, strings.Join(profile.MessageBetas, ","), strings.Join(claude.FullClaudeCodeMimicryBetas(), ","))
}

func TestComputeFinalAnthropicBeta_OAuthMimicUsesCapturedClaudeCode2206MessageBetas(t *testing.T) {
	svc := &GatewayService{}
	tests := []struct {
		name       string
		model      string
		wantHeader string
	}{
		{
			name:       "sonnet",
			model:      "claude-sonnet-4-6",
			wantHeader: "claude-code-20250219,interleaved-thinking-2025-05-14,tool-search-tool-2025-10-19,effort-2025-11-24",
		},
		{
			name:       "opus",
			model:      "claude-opus-4-6",
			wantHeader: "claude-code-20250219,interleaved-thinking-2025-05-14,tool-search-tool-2025-10-19,effort-2025-11-24",
		},
		{
			name:       "haiku",
			model:      "claude-haiku-4-5",
			wantHeader: "claude-code-20250219,tool-search-tool-2025-10-19",
		},
		{
			name:       "fable",
			model:      "claude-fable-5",
			wantHeader: "claude-code-20250219,interleaved-thinking-2025-05-14,tool-search-tool-2025-10-19,effort-2025-11-24,fallback-credit-2026-06-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, shouldSet := svc.computeFinalAnthropicBeta("oauth", true, tt.model, nil, []byte(`{"model":"`+tt.model+`"}`), nil)

			require.True(t, shouldSet)
			require.Equal(t, tt.wantHeader, got)
		})
	}
}

func TestComputeFinalCountTokensAnthropicBeta_OAuthMimicPreservesPre2206Betas(t *testing.T) {
	svc := &GatewayService{}
	tests := []struct {
		name       string
		model      string
		wantHeader string
	}{
		{
			name:  "sonnet",
			model: "claude-sonnet-5",
			wantHeader: "claude-code-20250219,interleaved-thinking-2025-05-14,thinking-token-count-2026-05-13," +
				"context-management-2025-06-27,prompt-caching-scope-2026-01-05,mid-conversation-system-2026-04-07," +
				"advanced-tool-use-2025-11-20,effort-2025-11-24,token-counting-2024-11-01",
		},
		{
			name:  "opus",
			model: "claude-opus-4-6",
			wantHeader: "claude-code-20250219,interleaved-thinking-2025-05-14,thinking-token-count-2026-05-13," +
				"context-management-2025-06-27,prompt-caching-scope-2026-01-05,mid-conversation-system-2026-04-07," +
				"advanced-tool-use-2025-11-20,effort-2025-11-24,token-counting-2024-11-01",
		},
		{
			name:  "haiku",
			model: "claude-haiku-4-5",
			wantHeader: "interleaved-thinking-2025-05-14,thinking-token-count-2026-05-13,context-management-2025-06-27," +
				"prompt-caching-scope-2026-01-05,claude-code-20250219,advanced-tool-use-2025-11-20,token-counting-2024-11-01",
		},
		{
			name:  "fable",
			model: "claude-fable-5",
			wantHeader: "claude-code-20250219,interleaved-thinking-2025-05-14,thinking-token-count-2026-05-13," +
				"context-management-2025-06-27,prompt-caching-scope-2026-01-05,mid-conversation-system-2026-04-07," +
				"advanced-tool-use-2025-11-20,effort-2025-11-24,server-side-fallback-2026-06-01," +
				"fallback-credit-2026-06-01,token-counting-2024-11-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, shouldSet := svc.computeFinalCountTokensAnthropicBeta(
				"oauth", true, tt.model, nil, []byte(`{"model":"`+tt.model+`"}`), nil,
			)

			require.True(t, shouldSet)
			require.Equal(t, tt.wantHeader, got)
		})
	}
}

func TestComputeFinalAnthropicBeta_RealClaudeCodeDoesNotAppendOAuth(t *testing.T) {
	svc := &GatewayService{}
	headers := http.Header{}
	headers.Set("anthropic-beta", strings.Join([]string{
		claude.BetaClaudeCode,
		claude.BetaInterleavedThinking,
		claude.BetaThinkingTokenCount,
	}, ","))

	got, shouldSet := svc.computeFinalAnthropicBeta("oauth", false, "claude-sonnet-4-6", headers, []byte(`{"model":"claude-sonnet-4-6"}`), nil)

	require.True(t, shouldSet)
	require.NotContains(t, got, claude.BetaOAuth)
	require.Contains(t, got, claude.BetaThinkingTokenCount)
}

func TestApplyClaudeCodeMimicHeaders_UsesCapturedProfileHeaders(t *testing.T) {
	profile := claude.DefaultClaudeCodeMimicryProfile()
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages?beta=true", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "curl/8")
	req.Header.Set("X-Stainless-OS", "Linux")

	applyClaudeCodeMimicHeaders(req, true)

	for key, want := range profile.Headers {
		require.Equal(t, want, getHeaderRaw(req.Header, key), key)
	}
	require.Equal(t, "application/json", getHeaderRaw(req.Header, "Accept"))
	require.Equal(t, "gzip, deflate, br, zstd", getHeaderRaw(req.Header, "Accept-Encoding"))
	require.Empty(t, getHeaderRaw(req.Header, "x-stainless-helper-method"))
	require.NotEmpty(t, getHeaderRaw(req.Header, "x-client-request-id"))
}

func TestMergeAnthropicBetaDropping_PreservesIncomingRedactThinking(t *testing.T) {
	required := claude.FullClaudeCodeMimicryBetas()
	incoming := claude.BetaRedactThinking

	got := mergeAnthropicBetaDropping(required, incoming, droppedBetaSet())

	require.Contains(t, got, claude.BetaRedactThinking)
}

func TestDroppedBetaSet(t *testing.T) {
	// Base set contains DroppedBetas (now empty — filtering moved to configurable beta policy)
	base := droppedBetaSet()
	require.Len(t, base, len(claude.DroppedBetas))

	// With extra tokens
	extended := droppedBetaSet(claude.BetaClaudeCode)
	require.Contains(t, extended, claude.BetaClaudeCode)
	require.Len(t, extended, len(claude.DroppedBetas)+1)
}

func TestBuildBetaTokenSet(t *testing.T) {
	got := buildBetaTokenSet([]string{"foo", "", "bar", "foo"})
	require.Len(t, got, 2)
	require.Contains(t, got, "foo")
	require.Contains(t, got, "bar")
	require.NotContains(t, got, "")

	empty := buildBetaTokenSet(nil)
	require.Empty(t, empty)
}

func TestContainsBetaToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		token  string
		want   bool
	}{
		{"present in middle", "oauth-2025-04-20,fast-mode-2026-02-01,interleaved-thinking-2025-05-14", "fast-mode-2026-02-01", true},
		{"present at start", "fast-mode-2026-02-01,oauth-2025-04-20", "fast-mode-2026-02-01", true},
		{"present at end", "oauth-2025-04-20,fast-mode-2026-02-01", "fast-mode-2026-02-01", true},
		{"only token", "fast-mode-2026-02-01", "fast-mode-2026-02-01", true},
		{"not present", "oauth-2025-04-20,interleaved-thinking-2025-05-14", "fast-mode-2026-02-01", false},
		{"with spaces", "oauth-2025-04-20, fast-mode-2026-02-01 , interleaved-thinking-2025-05-14", "fast-mode-2026-02-01", true},
		{"empty header", "", "fast-mode-2026-02-01", false},
		{"empty token", "fast-mode-2026-02-01", "", false},
		{"partial match", "fast-mode-2026-02-01-extra", "fast-mode-2026-02-01", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsBetaToken(tt.header, tt.token)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestStripBetaTokensWithSet_EmptyDropSet(t *testing.T) {
	header := "oauth-2025-04-20,interleaved-thinking-2025-05-14"
	got := stripBetaTokensWithSet(header, map[string]struct{}{})
	require.Equal(t, header, got)
}

func TestIsCountTokensUnsupported404(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       bool
	}{
		{
			name:       "exact endpoint not found",
			statusCode: 404,
			body:       `{"error":{"message":"Not found: /v1/messages/count_tokens","type":"not_found_error"}}`,
			want:       true,
		},
		{
			name:       "contains count_tokens and not found",
			statusCode: 404,
			body:       `{"error":{"message":"count_tokens route not found","type":"not_found_error"}}`,
			want:       true,
		},
		{
			name:       "generic 404",
			statusCode: 404,
			body:       `{"error":{"message":"resource not found","type":"not_found_error"}}`,
			want:       false,
		},
		{
			name:       "404 with empty error message",
			statusCode: 404,
			body:       `{"error":{"message":"","type":"not_found_error"}}`,
			want:       false,
		},
		{
			name:       "non-404 status",
			statusCode: 400,
			body:       `{"error":{"message":"Not found: /v1/messages/count_tokens","type":"invalid_request_error"}}`,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCountTokensUnsupported404(tt.statusCode, []byte(tt.body))
			require.Equal(t, tt.want, got)
		})
	}
}

// TestDefaultBetaPolicy_Context1M_Sonnet5Whitelist 验证默认策略下 context-1m-2025-08-07 的分模型行为：
//   - claude-sonnet-5 及后续版本：pass（放行），保留 1M 上下文能力
//   - 其他 sonnet 版本（4.x 及以下）、opus、haiku：filter（过滤），因为上游不支持
func TestDefaultBetaPolicy_Context1M_Sonnet5Whitelist(t *testing.T) {
	settings := DefaultBetaPolicySettings()

	// 找到 context-1m-2025-08-07 规则
	var rule *BetaPolicyRule
	for i := range settings.Rules {
		if settings.Rules[i].BetaToken == "context-1m-2025-08-07" {
			rule = &settings.Rules[i]
			break
		}
	}
	require.NotNil(t, rule, "default policy must include context-1m-2025-08-07 rule")
	require.Equal(t, BetaPolicyActionPass, rule.Action, "primary action for whitelisted models is pass")
	require.Equal(t, BetaPolicyActionFilter, rule.FallbackAction, "non-whitelisted models must be filtered")
	require.NotEmpty(t, rule.ModelWhitelist, "context-1m must be scoped to sonnet-5+ via whitelist")

	// 表驱动：模型 → 期望 action
	// 覆盖每种上游路径下的模型 ID 变形：直连 Anthropic API、Vertex AI（"@YYYYMMDD" 后缀）、
	// AWS Bedrock 跨区域推理（us./eu./apac./jp./au./us-gov./global./anthropic. 前缀）。
	cases := []struct {
		model      string
		wantAction string
		desc       string
	}{
		// —— 直连 Anthropic API —— sonnet-5 系列应放行
		{"claude-sonnet-5", BetaPolicyActionPass, "sonnet-5 canonical"},
		{"claude-sonnet-5-20260701", BetaPolicyActionPass, "sonnet-5 dated variant matches wildcard"},
		{"claude-sonnet-5-thinking", BetaPolicyActionPass, "sonnet-5 thinking variant matches wildcard"},
		// —— Vertex AI 归一化后的 sonnet-5 —— 也应放行
		{"claude-sonnet-5@20260701", BetaPolicyActionPass, "sonnet-5 Vertex-normalized dated form"},
		// —— AWS Bedrock 各跨区域前缀 sonnet-5 —— 也应放行
		{"us.anthropic.claude-sonnet-5-v1", BetaPolicyActionPass, "bedrock us. sonnet-5"},
		{"eu.anthropic.claude-sonnet-5-20260701-v1:0", BetaPolicyActionPass, "bedrock eu. sonnet-5 dated"},
		{"apac.anthropic.claude-sonnet-5-v1", BetaPolicyActionPass, "bedrock apac. sonnet-5"},
		{"jp.anthropic.claude-sonnet-5-v1", BetaPolicyActionPass, "bedrock jp. sonnet-5"},
		{"au.anthropic.claude-sonnet-5-v1", BetaPolicyActionPass, "bedrock au. sonnet-5"},
		{"us-gov.anthropic.claude-sonnet-5-v1", BetaPolicyActionPass, "bedrock us-gov. sonnet-5"},
		{"global.anthropic.claude-sonnet-5-v1", BetaPolicyActionPass, "bedrock global. sonnet-5"},
		{"anthropic.claude-sonnet-5-v1", BetaPolicyActionPass, "bedrock no-region sonnet-5"},

		// —— sonnet-4.x 及以下必须过滤 ——
		{"claude-sonnet-4-6", BetaPolicyActionFilter, "sonnet-4.6 must be filtered"},
		{"claude-sonnet-4-5-20250929", BetaPolicyActionFilter, "sonnet-4.5 dated must be filtered"},
		{"claude-sonnet-4", BetaPolicyActionFilter, "sonnet-4 must be filtered"},
		{"claude-sonnet-4-5@20250929", BetaPolicyActionFilter, "sonnet-4.5 Vertex format must be filtered"},
		{"us.anthropic.claude-sonnet-4-6", BetaPolicyActionFilter, "bedrock us. sonnet-4.6 must be filtered"},
		{"us.anthropic.claude-sonnet-4-5-20250929-v1:0", BetaPolicyActionFilter, "bedrock us. sonnet-4.5 must be filtered"},
		// —— Opus / Haiku 必须过滤（无 1M） ——
		{"claude-opus-4-8", BetaPolicyActionFilter, "opus must be filtered"},
		{"claude-opus-4-7", BetaPolicyActionFilter, "opus 4.7 must be filtered"},
		{"us.anthropic.claude-opus-4-8-v1", BetaPolicyActionFilter, "bedrock opus 4.8 must be filtered"},
		{"claude-haiku-4-5", BetaPolicyActionFilter, "haiku must be filtered"},
		{"us.anthropic.claude-haiku-4-5-20251001-v1:0", BetaPolicyActionFilter, "bedrock haiku must be filtered"},
		{"claude-3-5-sonnet-20241022", BetaPolicyActionFilter, "legacy sonnet 3.5 must be filtered"},
		// —— 特殊边界：不应把 "claude-sonnet-50" / "claude-sonnet-5.1" 之类意外命名误放行 ——
		{"claude-sonnet-50", BetaPolicyActionFilter, "must not over-match a hypothetical sonnet-50"},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			action, _ := resolveRuleAction(*rule, tc.model)
			require.Equal(t, tc.wantAction, action, tc.desc)
		})
	}
}
