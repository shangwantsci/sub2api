package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func resetGatewayForwardingSettingsCacheForTest(t *testing.T) {
	t.Helper()
	gatewayForwardingSF.Forget("gateway_forwarding")
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
	t.Cleanup(func() {
		gatewayForwardingSF.Forget("gateway_forwarding")
		gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
	})
}

func TestSettingService_GetClaudeOAuthSystemPromptInjectionSettings(t *testing.T) {
	t.Run("defaults to enabled with empty prompt", func(t *testing.T) {
		resetGatewayForwardingSettingsCacheForTest(t)
		svc := NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, &config.Config{})

		enabled, prompt, blocks := svc.GetClaudeOAuthSystemPromptInjectionSettings(context.Background())

		require.True(t, enabled)
		require.Empty(t, prompt)
		require.Empty(t, blocks)
	})

	t.Run("uses configured switch prompt and blocks", func(t *testing.T) {
		resetGatewayForwardingSettingsCacheForTest(t)
		const customPrompt = "custom prompt\n\nkeep spacing"
		const customBlocks = `[{"type":"text","text":"custom block","cache_control":true}]`
		svc := NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
			SettingKeyEnableClaudeOAuthSystemPromptInjection: "false",
			SettingKeyClaudeOAuthSystemPrompt:                customPrompt,
			SettingKeyClaudeOAuthSystemPromptBlocks:          customBlocks,
		}}, &config.Config{})

		enabled, prompt, blocks := svc.GetClaudeOAuthSystemPromptInjectionSettings(context.Background())

		require.False(t, enabled)
		require.Equal(t, customPrompt, prompt)
		require.Equal(t, customBlocks, blocks)
	})
}

func TestSettingService_GetClaudeMimicryRuntimeSettings(t *testing.T) {
	t.Run("defaults to captured profile and warn guard", func(t *testing.T) {
		resetGatewayForwardingSettingsCacheForTest(t)
		svc := NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, &config.Config{})

		settings := svc.GetClaudeMimicryRuntimeSettings(context.Background())

		require.Equal(t, claude.DefaultClaudeCodeMimicryProfileID, settings.ProfileID)
		require.Equal(t, "warn", settings.GuardMode)
	})

	t.Run("uses configured profile and guard mode", func(t *testing.T) {
		resetGatewayForwardingSettingsCacheForTest(t)
		svc := NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
			SettingKeyClaudeCodeMimicryProfile: claude.DefaultClaudeCodeMimicryProfileID,
			SettingKeyClaudeMimicryGuardMode:   "block",
		}}, &config.Config{})

		settings := svc.GetClaudeMimicryRuntimeSettings(context.Background())

		require.Equal(t, claude.DefaultClaudeCodeMimicryProfileID, settings.ProfileID)
		require.Equal(t, "block", settings.GuardMode)
	})
}

func TestGatewayService_ClaudeOAuthSystemPromptInjectionUsesAnthropicGroupPolicy(t *testing.T) {
	tests := []struct {
		name          string
		globalEnabled string
		policy        string
		wantMode      claudeOAuthSystemPromptMode
		wantBlocks    string
	}{
		{name: "inherits enabled global", globalEnabled: "true", policy: GroupPolicyInherit, wantMode: claudeOAuthSystemPromptModeFull},
		{name: "inherits disabled global", globalEnabled: "false", policy: GroupPolicyInherit, wantMode: claudeOAuthSystemPromptModeDisabled},
		{name: "group disables enabled global", globalEnabled: "true", policy: GroupPolicyDisabled, wantMode: claudeOAuthSystemPromptModeDisabled},
		{name: "group enables disabled global", globalEnabled: "false", policy: GroupPolicyEnabled, wantMode: claudeOAuthSystemPromptModeFull},
		{name: "group identity only overrides global", globalEnabled: "false", policy: GroupPolicyIdentityOnly, wantMode: claudeOAuthSystemPromptModeIdentityOnly, wantBlocks: claudeOAuthIdentityOnlyBlocksConfig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGatewayForwardingSettingsCacheForTest(t)
			settingService := NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
				SettingKeyEnableClaudeOAuthSystemPromptInjection: tt.globalEnabled,
			}}, &config.Config{})
			svc := &GatewayService{settingService: settingService}
			group := &Group{
				ID:                            1,
				Platform:                      PlatformAnthropic,
				Status:                        StatusActive,
				Hydrated:                      true,
				ClaudeOAuthSystemPromptPolicy: tt.policy,
			}
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)

			mode, _, blocks := svc.claudeOAuthSystemPromptInjectionSettings(ctx)

			require.Equal(t, tt.wantMode, mode)
			require.Equal(t, tt.wantBlocks, blocks)
		})
	}
}
