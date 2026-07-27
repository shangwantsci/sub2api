package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
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

// 无 wire marker 时绝不能靠固定 ack 猜 synthetic pair。合法真实对话可能逐字命中该
// 文本，任何结构识别都会让 fp 与 metadata session 静默改用第二轮。
func TestExtractFirstUserText_NeverGuessesSyntheticPairFromAck(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "assistant reply is ordinary conversation",
			body: `{"messages":[
			  {"role":"user","content":[{"type":"text","text":"first turn"}]},
			  {"role":"assistant","content":[{"type":"text","text":"Sure, here you go."}]},
			  {"role":"user","content":"second turn"}
			]}`,
			want: "first turn",
		},
		{
			name: "assistant reply exactly equals migration ack",
			body: `{"messages":[
			  {"role":"user","content":[{"type":"text","text":"first turn"}]},
			  {"role":"assistant","content":[{"type":"text","text":"Understood. I will follow these instructions."}]},
			  {"role":"user","content":"second turn"}
			]}`,
			want: "first turn",
		},
		{
			name: "ack without a preceding user message",
			body: `{"messages":[
			  {"role":"assistant","content":[{"type":"text","text":"Understood. I will follow these instructions."}]},
			  {"role":"user","content":"first turn"}
			]}`,
			want: "first turn",
		},
		{
			name: "unlabeled pair is not inferred without explicit context",
			body: `{"messages":[
			  {"role":"user","content":[{"type":"text","text":"project rules"}]},
			  {"role":"assistant","content":[{"type":"text","text":"Understood. I will follow these instructions."}]}
			]}`,
			want: "project rules",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, extractFirstUserText([]byte(tt.body)))
		})
	}
}

// 端到端锁定：rewrite 在注入 synthetic pair 前保存真实首轮，billing block 必须继续
// 使用它；事后兼容推断也应能用已经写入的 fp 找回同一文本。
func TestRewriteSystemForNonClaudeCode_MigrationKeepsFingerprintAndDropsLabel(t *testing.T) {
	const userTurn = "Read the file /etc/hostname and tell me its exact contents"
	original := []byte(`{"model":"claude-sonnet-4-6","system":"Customer instructions","messages":[{"role":"user","content":"` + userTurn + `"}]}`)

	migrated := rewriteSystemForNonClaudeCode(original, "Customer instructions")

	require.NotContains(t, string(migrated), legacyMigratedSystemPromptLabel)
	require.Equal(t, "Customer instructions", gjson.GetBytes(migrated, "messages.0.content.0.text").String())
	require.Equal(t, "Customer instructions", extractFirstUserText(migrated),
		"无 marker body 不允许靠 ack 猜 synthetic pair")
	inferred, ok := inferBillingFingerprintSourceText(migrated)
	require.True(t, ok)
	require.Equal(t, userTurn, inferred)
	require.Contains(t,
		gjson.GetBytes(migrated, "system.0.text").String(),
		"cc_version="+claude.CLICurrentVersion+"."+computeClaudeCodeFingerprint(original, claude.CLICurrentVersion),
	)
}

func TestRewriteSystemForNonClaudeCode_NaturalAckConversationUsesFirstTurn(t *testing.T) {
	const firstTurn = "first turn must drive billing fingerprint"
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[
	  {"role":"user","content":[{"type":"text","text":"` + firstTurn + `"}]},
	  {"role":"assistant","content":[{"type":"text","text":"Understood. I will follow these instructions."}]},
	  {"role":"user","content":"second turn"}
	]}`)

	rewritten := rewriteSystemForNonClaudeCode(body, nil)
	wantFP := computeClaudeCodeFingerprintFromText(firstTurn, claude.CLICurrentVersion)
	require.Contains(t, gjson.GetBytes(rewritten, "system.0.text").String(),
		"cc_version="+claude.CLICurrentVersion+"."+wantFP)
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

func TestSyncBillingHeaderVersion_MigratedBodyUsesExplicitOriginalFirstTurn(t *testing.T) {
	const userTurn = "Read the file /etc/hostname and tell me its exact contents"
	original := []byte(`{"model":"claude-sonnet-4-6","system":"Customer instructions","messages":[{"role":"user","content":"` + userTurn + `"}]}`)
	migrated := rewriteSystemForNonClaudeCode(original, "Customer instructions")
	const nextVersion = "9.9.9"

	got := syncBillingHeaderVersionWithFirstUserText(
		migrated,
		"claude-cli/"+nextVersion+" (external, sdk-cli)",
		userTurn,
	)

	wantFP := computeClaudeCodeFingerprintFromText(userTurn, nextVersion)
	require.Contains(t, gjson.GetBytes(got, "system.0.text").String(),
		"cc_version="+nextVersion+"."+wantFP)
	require.NotContains(t, gjson.GetBytes(got, "system.0.text").String(),
		"cc_version="+nextVersion+"."+computeClaudeCodeFingerprintFromText("Customer instructions", nextVersion))
}
