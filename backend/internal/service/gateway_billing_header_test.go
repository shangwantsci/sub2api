package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeClaudeCodeFingerprint_Official211Vectors(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "tools canary",
			text: "Read the file /etc/hostname and tell me its exact contents",
			want: "882",
		},
		{
			name: "plain canary",
			text: "In one short sentence, what is 2+2?",
			want: "65c",
		},
		{
			name: "Chinese uses JS UTF-16 indexing, not UTF-8 bytes",
			text: "你好，这是一个用于测试指纹的中文提示词",
			want: "b72",
		},
		{
			name: "selected lone surrogate encodes as replacement rune like Node",
			text: "abc😀defghijklmnopqrstuvwxyz",
			want: "74c",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"messages":[{"role":"user","content":` + mustJSONMarshalString(t, tt.text) + `}]}`)
			require.Equal(t, tt.want, computeClaudeCodeFingerprint(body, "2.1.211"))
		})
	}
}

func TestComputeClaudeCodeFingerprint_SkipsMigratedSystemInstruction(t *testing.T) {
	body := []byte(`{
	  "messages": [
	    {"role":"user","content":[{"type":"text","text":"[System Instructions]\nproject rules"}]},
	    {"role":"assistant","content":[{"type":"text","text":"Understood. I will follow these instructions."}]},
	    {"role":"user","content":"Read the file /etc/hostname and tell me its exact contents"}
	  ]
	}`)
	require.Equal(t, "Read the file /etc/hostname and tell me its exact contents", extractFirstUserText(body))
	require.Equal(t, "882", computeClaudeCodeFingerprint(body, "2.1.211"))
}

func mustJSONMarshalString(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}

func TestSyncBillingHeaderVersion(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		userAgent string
		wantSub   string // substring expected in result
		unchanged bool   // expect body to remain the same
	}{
		{
			// The fp suffix DEPENDS on the version, so a version change must recompute it
			// (not preserve the old one) — otherwise the block carries a fp for the wrong
			// version and gets flagged. See syncBillingHeaderVersion.
			name:      "rewrites cc_version and recomputes fingerprint for the new version",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81.df2; cc_entrypoint=cli; cch=00000;"},{"type":"text","text":"You are Claude Code.","cache_control":{"type":"ephemeral"}}],"messages":[]}`,
			userAgent: "claude-cli/2.1.22 (external, cli)",
			wantSub:   "cc_version=2.1.22.",
		},
		{
			name:      "no billing header in system",
			body:      `{"system":[{"type":"text","text":"You are Claude Code."}],"messages":[]}`,
			userAgent: "claude-cli/2.1.22",
			unchanged: true,
		},
		{
			name:      "no system field",
			body:      `{"messages":[]}`,
			userAgent: "claude-cli/2.1.22",
			unchanged: true,
		},
		{
			name:      "user-agent without version",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81; cc_entrypoint=cli; cch=00000;"}],"messages":[]}`,
			userAgent: "Mozilla/5.0",
			unchanged: true,
		},
		{
			name:      "empty user-agent",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81; cc_entrypoint=cli; cch=00000;"}],"messages":[]}`,
			userAgent: "",
			unchanged: true,
		},
		{
			name:      "version already matches",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.22; cc_entrypoint=cli; cch=00000;"}],"messages":[]}`,
			userAgent: "claude-cli/2.1.22",
			unchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := syncBillingHeaderVersion([]byte(tt.body), tt.userAgent)
			if tt.unchanged {
				assert.Equal(t, tt.body, string(result), "body should remain unchanged")
			} else {
				assert.Contains(t, string(result), tt.wantSub)
				// Ensure old semver is gone
				assert.NotContains(t, string(result), "cc_version=2.1.81")
			}
		})
	}
}
