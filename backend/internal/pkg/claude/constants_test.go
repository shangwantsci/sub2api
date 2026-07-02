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

func TestDefaultClaudeCodeMimicryProfileUsesCapturedClaudeCode2197Baseline(t *testing.T) {
	profile := DefaultClaudeCodeMimicryProfile()

	require.Equal(t, DefaultClaudeCodeMimicryProfileID, profile.ID)
	require.Equal(t, "cc-2.1.197-sdk-cli-macos-arm64", profile.ID)
	require.Equal(t, "2.1.197", profile.CLIVersion)
	require.Equal(t, "claude-cli/2.1.197 (external, sdk-cli)", profile.Headers["User-Agent"])
	require.Equal(t, "0.94.0", profile.Headers["X-Stainless-Package-Version"])
	require.Equal(t, "v26.3.0", profile.Headers["X-Stainless-Runtime-Version"])
}

func TestResolveClaudeCodeMimicryModelProfileUsesCapturedClaudeCode2197ModelDefaults(t *testing.T) {
	tests := []struct {
		name                string
		model               string
		wantMaxTokens       int
		wantThinkingType    string
		wantThinkingBudget  int
		wantOutputEffort    string
		wantMessageBetas    []string
		wantCountTokenBetas []string
	}{
		{
			name:             "sonnet",
			model:            "claude-sonnet-5",
			wantMaxTokens:    64000,
			wantThinkingType: "adaptive",
			wantOutputEffort: "high",
			wantMessageBetas: []string{BetaClaudeCode, BetaInterleavedThinking, BetaThinkingTokenCount, BetaContextManagement, BetaPromptCachingScope, BetaMidConversationSystem, BetaAdvancedToolUse, BetaEffort},
			wantCountTokenBetas: []string{
				BetaClaudeCode, BetaInterleavedThinking, BetaThinkingTokenCount, BetaContextManagement,
				BetaPromptCachingScope, BetaMidConversationSystem, BetaAdvancedToolUse, BetaEffort, BetaTokenCounting,
			},
		},
		{
			name:             "opus",
			model:            "claude-opus-4-8",
			wantMaxTokens:    64000,
			wantThinkingType: "adaptive",
			wantOutputEffort: "high",
			wantMessageBetas: []string{BetaClaudeCode, BetaInterleavedThinking, BetaThinkingTokenCount, BetaContextManagement, BetaPromptCachingScope, BetaMidConversationSystem, BetaAdvancedToolUse, BetaEffort},
			wantCountTokenBetas: []string{
				BetaClaudeCode, BetaInterleavedThinking, BetaThinkingTokenCount, BetaContextManagement,
				BetaPromptCachingScope, BetaMidConversationSystem, BetaAdvancedToolUse, BetaEffort, BetaTokenCounting,
			},
		},
		{
			name:               "haiku",
			model:              "claude-haiku-4-5-20251001",
			wantMaxTokens:      32000,
			wantThinkingType:   "enabled",
			wantThinkingBudget: 31999,
			wantMessageBetas:   []string{BetaInterleavedThinking, BetaThinkingTokenCount, BetaContextManagement, BetaPromptCachingScope, BetaClaudeCode, BetaAdvancedToolUse},
			wantCountTokenBetas: []string{
				BetaInterleavedThinking, BetaThinkingTokenCount, BetaContextManagement,
				BetaPromptCachingScope, BetaClaudeCode, BetaAdvancedToolUse, BetaTokenCounting,
			},
		},
		{
			name:             "fable",
			model:            "claude-fable-5",
			wantMaxTokens:    64000,
			wantThinkingType: "adaptive",
			wantOutputEffort: "high",
			wantMessageBetas: []string{
				BetaClaudeCode, BetaInterleavedThinking, BetaThinkingTokenCount, BetaContextManagement,
				BetaPromptCachingScope, BetaMidConversationSystem, BetaAdvancedToolUse, BetaEffort,
				BetaServerSideFallback, BetaFallbackCredit,
			},
			wantCountTokenBetas: []string{
				BetaClaudeCode, BetaInterleavedThinking, BetaThinkingTokenCount, BetaContextManagement,
				BetaPromptCachingScope, BetaMidConversationSystem, BetaAdvancedToolUse, BetaEffort,
				BetaServerSideFallback, BetaFallbackCredit, BetaTokenCounting,
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
			require.Equal(t, tt.wantCountTokenBetas, got.CountTokensBetas)
			require.NotContains(t, got.MessageBetas, BetaOAuth)
			require.NotContains(t, got.CountTokensBetas, BetaOAuth)
		})
	}
}
