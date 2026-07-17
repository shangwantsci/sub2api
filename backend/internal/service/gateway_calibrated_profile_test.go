package service

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/stretchr/testify/require"
)

// calibratedProfileForTest builds a valid, guard-passing profile that claims a bumped
// CLI version (2.1.212) and carries a sonnet tools bucket with context-management —
// something the compiled-in constants deliberately do NOT emit — so tests can prove the
// wire bytes actually switch to the calibrated set.
func calibratedProfileForTest(t *testing.T) *claude.CalibratedProfile {
	t.Helper()
	raw := `{
      "schema_version": 1,
      "cli_version": "2.1.212",
      "captured_at": "2026-07-16T00:00:00Z",
      "source": "cc-calibrate",
      "headers": {
        "template": {
          "User-Agent": "claude-cli/2.1.212 (external, sdk-cli)",
          "X-Stainless-OS": "Linux",
          "X-Stainless-Arch": "x64",
          "Accept": "application/json",
          "Accept-Encoding": "gzip, deflate, br, zstd"
        },
        "absent": ["x-client-request-id"]
      },
      "beta_rules": {
        "messages|sonnet|": ["claude-code-20250219", "interleaved-thinking-2025-05-14"],
        "messages|sonnet|tools": ["claude-code-20250219", "interleaved-thinking-2025-05-14", "context-management-2025-06-27"],
        "count_tokens|sonnet|": ["claude-code-20250219", "token-counting-2024-11-01"]
      },
      "guard": {"salt_verified": true, "checked": 4, "ok": 4}
    }`
	p, err := claude.ParseCalibratedProfile([]byte(raw))
	require.NoError(t, err)
	return p
}

func TestComputeFinalAnthropicBeta_UsesCalibratedProfileOverConstants(t *testing.T) {
	svc := &GatewayService{}
	profile := calibratedProfileForTest(t)
	toolsBody := []byte(`{"model":"claude-sonnet-4-6","tools":[{"name":"read_file"}]}`)

	withProfile, ok := svc.computeFinalAnthropicBeta(profile, "oauth", true, "claude-sonnet-4-6", nil, toolsBody, nil)
	require.True(t, ok)
	require.True(t, anthropicBetaTokensContains(withProfile, claude.BetaContextManagement),
		"calibrated sonnet|tools bucket must drive context-management onto the wire")
	require.False(t, anthropicBetaTokensContains(withProfile, claude.BetaToolSearchTool),
		"calibrated bucket replaces the constants beta set entirely (no tool-search-tool)")

	// nil profile -> compiled-in constants (which use tool-search-tool, no context-management).
	fallback, ok := svc.computeFinalAnthropicBeta(nil, "oauth", true, "claude-sonnet-4-6", nil, toolsBody, nil)
	require.True(t, ok)
	require.True(t, anthropicBetaTokensContains(fallback, claude.BetaToolSearchTool))
	require.False(t, anthropicBetaTokensContains(fallback, claude.BetaContextManagement))
}

func TestComputeFinalAnthropicBeta_CalibratedProfileMissingComboFallsBackToConstants(t *testing.T) {
	svc := &GatewayService{}
	profile := calibratedProfileForTest(t) // only has sonnet buckets
	body := []byte(`{"model":"claude-opus-4-8"}`)

	final, ok := svc.computeFinalAnthropicBeta(profile, "oauth", true, "claude-opus-4-8", nil, body, nil)
	require.True(t, ok)
	// opus not captured in the profile -> constants opus betas (tool-search-tool).
	require.True(t, anthropicBetaTokensContains(final, claude.BetaToolSearchTool))
}

func TestComputeFinalCountTokensAnthropicBeta_UsesCalibratedCountTokensBucket(t *testing.T) {
	svc := &GatewayService{}
	profile := calibratedProfileForTest(t)
	body := []byte(`{"model":"claude-sonnet-4-6"}`)

	final, ok := svc.computeFinalCountTokensAnthropicBeta(profile, "oauth", true, "claude-sonnet-4-6", nil, body, nil)
	require.True(t, ok)
	require.True(t, anthropicBetaTokensContains(final, claude.BetaTokenCounting))
	// count_tokens must NOT borrow the messages bucket's context-management.
	require.False(t, anthropicBetaTokensContains(final, claude.BetaContextManagement))
}

func TestComputeFinalAnthropicBeta_StructuredOutputsAppendedOnJSONSchema(t *testing.T) {
	svc := &GatewayService{}
	profile := calibratedProfileForTest(t)
	jsonBody := []byte(`{"model":"claude-sonnet-4-6","output_config":{"format":{"type":"json_schema"}}}`)

	final, ok := svc.computeFinalAnthropicBeta(profile, "oauth", true, "claude-sonnet-4-6", nil, jsonBody, nil)
	require.True(t, ok)
	require.True(t, anthropicBetaTokensContains(final, claude.BetaStructuredOutputs),
		"json_schema requests append structured-outputs on top of the resolved bucket")
}

func TestApplyClaudeCodeMimicHeaders_CalibratedProfileDrivesHeadersAndAbsent(t *testing.T) {
	profile := calibratedProfileForTest(t)
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages?beta=true", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "curl/8")
	req.Header.Set("x-client-request-id", "should-be-removed")

	applyClaudeCodeMimicHeaders(profile, req, true)

	require.Equal(t, "claude-cli/2.1.212 (external, sdk-cli)", getHeaderRaw(req.Header, "User-Agent"),
		"calibrated User-Agent (bumped version) must be emitted")
	require.Equal(t, "application/json", getHeaderRaw(req.Header, "Accept"))
	require.Equal(t, "gzip, deflate, br, zstd", getHeaderRaw(req.Header, "Accept-Encoding"))
	require.Empty(t, getHeaderRaw(req.Header, "x-client-request-id"),
		"headers in profile.absent must be deleted (over-forgery removal)")
}

func TestSyncBillingHeaderVersion_RecomputesFingerprintForCalibratedVersion(t *testing.T) {
	userText := "please refactor the calibration harness loader"
	fpBody := []byte(`{"messages":[{"role":"user","content":"` + userText + `"}]}`)
	fp211 := computeClaudeCodeFingerprint(fpBody, "2.1.211")
	fp212 := computeClaudeCodeFingerprint(fpBody, "2.1.212")
	require.NotEqual(t, fp211, fp212, "sanity: fingerprint depends on version")

	body := []byte(`{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.211.` + fp211 + `; cc_entrypoint=sdk-cli;"}],"messages":[{"role":"user","content":"` + userText + `"}]}`)

	out := syncBillingHeaderVersion(body, "claude-cli/2.1.212 (external, sdk-cli)")
	require.Contains(t, string(out), "cc_version=2.1.212."+fp212,
		"version bump via calibrated UA must recompute the fingerprint for the new version")
	require.NotContains(t, string(out), "2.1.211")
}
