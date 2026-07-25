package claude

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsContainsClaudeSonnet5(t *testing.T) {
	var got *Model
	for i := range DefaultModels {
		if DefaultModels[i].ID == "claude-sonnet-5" {
			got = &DefaultModels[i]
			break
		}
	}

	require.NotNil(t, got)
	require.Equal(t, "model", got.Type)
	require.Equal(t, "Claude Sonnet 5", got.DisplayName)
	require.Equal(t, "2026-07-01T00:00:00Z", got.CreatedAt)
	require.Contains(t, DefaultModelIDs(), "claude-sonnet-5")
}

func TestDefaultModelsContainsClaudeOpus5(t *testing.T) {
	var got *Model
	for i := range DefaultModels {
		if DefaultModels[i].ID == "claude-opus-5" {
			got = &DefaultModels[i]
			break
		}
	}

	require.NotNil(t, got)
	require.Equal(t, "model", got.Type)
	require.Equal(t, "Claude Opus 5", got.DisplayName)
	require.Equal(t, "2026-07-24T00:00:00Z", got.CreatedAt)
	require.Contains(t, DefaultModelIDs(), "claude-opus-5")

	// Opus 5 无日期变体，API ID 与 alias 相同，不应参与短名/长名互转。
	require.Equal(t, "claude-opus-5", NormalizeModelID("claude-opus-5"))
	require.Equal(t, "claude-opus-5", DenormalizeModelID("claude-opus-5"))
}

// Opus 5 官方默认即 adaptive thinking + effort=high，正好是 opus 档 profile 的语义；
// 这里锁定归族结果，避免未来改动子串匹配时把它错分到 sonnet/haiku 档。
func TestClaudeOpus5UsesOpusMimicryProfile(t *testing.T) {
	profile := ResolveClaudeCodeMimicryModelProfile("claude-opus-5")

	require.Equal(t, "opus", profile.ID)
	require.Equal(t, "adaptive", profile.DefaultThinkingType)
	require.Equal(t, "high", profile.DefaultOutputConfigEffort)
	require.Equal(t, "opus", CalibratedFamilyOf("claude-opus-5"))
}

func TestDefaultClaudeCodeMimicryProfileUsesCapturedClaudeCode2211Baseline(t *testing.T) {
	profile := DefaultClaudeCodeMimicryProfile()

	require.Equal(t, DefaultClaudeCodeMimicryProfileID, profile.ID)
	require.Equal(t, "cc-2.1.211-sdk-cli-linux-x64", profile.ID)
	require.Equal(t, "2.1.211", profile.CLIVersion)
	require.Equal(t, "claude-cli/2.1.211 (external, sdk-cli)", profile.Headers["User-Agent"])
	require.Equal(t, "Linux", profile.Headers["X-Stainless-OS"])
	require.Equal(t, "x64", profile.Headers["X-Stainless-Arch"])
	require.Equal(t, "0.94.0", profile.Headers["X-Stainless-Package-Version"])
	require.Equal(t, "v26.3.0", profile.Headers["X-Stainless-Runtime-Version"])
}

func TestResolveClaudeCodeMimicryModelProfileUsesCapturedClaudeCode2211MessageDefaults(t *testing.T) {
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
			name:             "sonnet",
			model:            "claude-sonnet-4-6",
			wantMaxTokens:    32000,
			wantThinkingType: "adaptive",
			wantOutputEffort: "high",
			wantMessageBetas: []string{
				"claude-code-20250219",
				"interleaved-thinking-2025-05-14",
				"tool-search-tool-2025-10-19",
				"effort-2025-11-24",
			},
		},
		{
			name:             "opus",
			model:            "claude-opus-4-6",
			wantMaxTokens:    64000,
			wantThinkingType: "adaptive",
			wantOutputEffort: "high",
			wantMessageBetas: []string{
				"claude-code-20250219",
				"interleaved-thinking-2025-05-14",
				"tool-search-tool-2025-10-19",
				"effort-2025-11-24",
			},
		},
		{
			name:               "haiku",
			model:              "claude-haiku-4-5",
			wantMaxTokens:      32000,
			wantThinkingType:   "enabled",
			wantThinkingBudget: 31999,
			wantMessageBetas: []string{
				"claude-code-20250219",
				"tool-search-tool-2025-10-19",
			},
		},
		{
			name:             "fable",
			model:            "claude-fable-5",
			wantMaxTokens:    64000,
			wantThinkingType: "adaptive",
			wantOutputEffort: "high",
			wantMessageBetas: []string{
				"claude-code-20250219",
				"interleaved-thinking-2025-05-14",
				"tool-search-tool-2025-10-19",
				"effort-2025-11-24",
				"fallback-credit-2026-06-01",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveClaudeCodeMimicryModelProfile(tt.model)

			require.Equal(t, tt.wantMaxTokens, got.DefaultMaxTokens)
			require.Equal(t, tt.wantThinkingType, got.DefaultThinkingType)
			require.Equal(t, tt.wantThinkingBudget, got.DefaultThinkingBudgetTokens)
			require.Equal(t, tt.wantOutputEffort, got.DefaultOutputConfigEffort)
			require.Equal(t, tt.wantMessageBetas, got.MessageBetas)
			require.NotContains(t, got.MessageBetas, BetaOAuth)
		})
	}
}

func TestResolveClaudeCodeMimicryModelProfilePreservesPre2206CountTokensBetas(t *testing.T) {
	tests := []struct {
		name               string
		model              string
		wantCountBetas     []string
		wantCountMaxTokens int
	}{
		{
			name:               "sonnet",
			model:              "claude-sonnet-5",
			wantCountMaxTokens: 64000,
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
			name:               "opus",
			model:              "claude-opus-4-6",
			wantCountMaxTokens: 64000,
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
			name:               "haiku",
			model:              "claude-haiku-4-5",
			wantCountMaxTokens: 32000,
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
			name:               "fable",
			model:              "claude-fable-5",
			wantCountMaxTokens: 64000,
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
			require.Equal(t, tt.wantCountMaxTokens, got.CountTokensDefaultMaxTokens)
		})
	}
}
