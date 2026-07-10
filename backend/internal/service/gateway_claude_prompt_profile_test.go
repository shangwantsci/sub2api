package service

import (
	"context"
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

func TestEnsureClaudeOAuthMimicCountTokensSystemBody_FablePreservesGenericExpansion(t *testing.T) {
	body := []byte(`{"model":"claude-fable-5","messages":[{"role":"user","content":"hi"}]}`)
	body = (&GatewayService{}).ensureClaudeOAuthMimicCountTokensSystemBody(context.Background(), body)

	require.Equal(
		t,
		strings.TrimSpace(claudeCodeSystemPromptExpansion),
		strings.TrimSpace(gjson.GetBytes(body, "system.2.text").String()),
	)
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
