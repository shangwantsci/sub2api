//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveGroupPolicy(t *testing.T) {
	require.True(t, ResolveGroupPolicy(true, GroupPolicyInherit))
	require.False(t, ResolveGroupPolicy(false, GroupPolicyInherit))
	require.True(t, ResolveGroupPolicy(false, GroupPolicyEnabled))
	require.False(t, ResolveGroupPolicy(true, GroupPolicyDisabled))
	require.True(t, ResolveGroupPolicy(true, "unknown"))
}

func TestAnthropicGroupPoliciesIgnoreOtherPlatforms(t *testing.T) {
	group := &Group{
		Platform:                      PlatformOpenAI,
		ContentReviewPolicy:           GroupPolicyDisabled,
		ClaudeOAuthSystemPromptPolicy: GroupPolicyDisabled,
	}

	require.Equal(t, GroupPolicyInherit, group.AnthropicContentReviewPolicy())
	require.Equal(t, GroupPolicyInherit, group.AnthropicClaudeOAuthSystemPromptPolicy())
}

func TestAnthropicGroupPoliciesNormalizeStoredValues(t *testing.T) {
	group := &Group{
		Platform:                      PlatformAnthropic,
		ContentReviewPolicy:           " ENABLED ",
		ClaudeOAuthSystemPromptPolicy: "disabled",
	}

	require.Equal(t, GroupPolicyEnabled, group.AnthropicContentReviewPolicy())
	require.Equal(t, GroupPolicyDisabled, group.AnthropicClaudeOAuthSystemPromptPolicy())
}
