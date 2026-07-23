package service

import "strings"

const (
	GroupPolicyInherit      = "inherit"
	GroupPolicyEnabled      = "enabled"
	GroupPolicyDisabled     = "disabled"
	GroupPolicyIdentityOnly = "identity_only"
)

func NormalizeGroupPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case GroupPolicyEnabled:
		return GroupPolicyEnabled
	case GroupPolicyDisabled:
		return GroupPolicyDisabled
	default:
		return GroupPolicyInherit
	}
}

func IsValidGroupPolicy(policy string) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case GroupPolicyInherit, GroupPolicyEnabled, GroupPolicyDisabled:
		return true
	default:
		return false
	}
}

func ResolveGroupPolicy(globalEnabled bool, policy string) bool {
	switch NormalizeGroupPolicy(policy) {
	case GroupPolicyEnabled:
		return true
	case GroupPolicyDisabled:
		return false
	default:
		return globalEnabled
	}
}

func NormalizeClaudeOAuthSystemPromptPolicy(policy string) string {
	if strings.ToLower(strings.TrimSpace(policy)) == GroupPolicyIdentityOnly {
		return GroupPolicyIdentityOnly
	}
	return NormalizeGroupPolicy(policy)
}

func IsValidClaudeOAuthSystemPromptPolicy(policy string) bool {
	if strings.ToLower(strings.TrimSpace(policy)) == GroupPolicyIdentityOnly {
		return true
	}
	return IsValidGroupPolicy(policy)
}

func (g *Group) AnthropicContentReviewPolicy() string {
	if g == nil || g.Platform != PlatformAnthropic {
		return GroupPolicyInherit
	}
	return NormalizeGroupPolicy(g.ContentReviewPolicy)
}

func (g *Group) AnthropicClaudeOAuthSystemPromptPolicy() string {
	if g == nil || g.Platform != PlatformAnthropic {
		return GroupPolicyInherit
	}
	return NormalizeClaudeOAuthSystemPromptPolicy(g.ClaudeOAuthSystemPromptPolicy)
}
