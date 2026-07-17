package claude

import (
	"encoding/json"
	"testing"
)

func validCalibratedProfileJSON(t *testing.T) []byte {
	t.Helper()
	p := CalibratedProfile{
		SchemaVersion: CalibratedProfileSchemaVersion,
		CLIVersion:    "2.1.211",
		CapturedAt:    "2026-07-16T00:00:00Z",
		Source:        "cc-calibrate",
		Headers: CalibratedHeaders{
			Template: map[string]string{
				"User-Agent":        "claude-cli/2.1.211 (external, sdk-cli)",
				"X-Stainless-OS":    "Linux",
				"X-Stainless-Arch":  "x64",
				"anthropic-version": "2023-06-01",
			},
			Absent: []string{"x-client-request-id"},
		},
		BetaRules: map[string][]string{
			"messages|sonnet|":                  {"claude-code-20250219", "interleaved-thinking-2025-05-14"},
			"messages|sonnet|tools":             {"claude-code-20250219", "interleaved-thinking-2025-05-14", "context-management-2025-06-27"},
			"messages|opus|":                    {"claude-code-20250219", "effort-2025-11-24"},
			"messages|haiku|":                   {"claude-code-20250219"},
			"count_tokens|sonnet|":              {"claude-code-20250219", "token-counting-2024-11-01"},
			"messages|sonnet|json_schema+tools": {"claude-code-20250219", "structured-outputs-2025-12-15"},
		},
		Guard: CalibratedGuard{SaltVerified: true, Checked: 6, OK: 6},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestParseCalibratedProfile_Valid(t *testing.T) {
	p, err := ParseCalibratedProfile(validCalibratedProfileJSON(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Version() != "2.1.211" {
		t.Fatalf("version = %q, want 2.1.211", p.Version())
	}
	if p.UserAgent() != "claude-cli/2.1.211 (external, sdk-cli)" {
		t.Fatalf("ua = %q", p.UserAgent())
	}
	if got := p.AbsentHeaders(); len(got) != 1 || got[0] != "x-client-request-id" {
		t.Fatalf("absent = %v", got)
	}
}

func TestParseCalibratedProfile_Rejects(t *testing.T) {
	base := func(t *testing.T) *CalibratedProfile {
		t.Helper()
		var p CalibratedProfile
		if err := json.Unmarshal(validCalibratedProfileJSON(t), &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return &p
	}

	tests := []struct {
		name   string
		mutate func(p *CalibratedProfile)
	}{
		{"empty", func(p *CalibratedProfile) { *p = CalibratedProfile{} }},
		{"bad schema", func(p *CalibratedProfile) { p.SchemaVersion = 999 }},
		{"bad version", func(p *CalibratedProfile) { p.CLIVersion = "2.1" }},
		{"ua version mismatch", func(p *CalibratedProfile) {
			p.Headers.Template["User-Agent"] = "claude-cli/2.1.999 (external, sdk-cli)"
		}},
		{"missing ua", func(p *CalibratedProfile) { delete(p.Headers.Template, "User-Agent") }},
		{"guard not verified", func(p *CalibratedProfile) { p.Guard.SaltVerified = false }},
		{"guard never checked", func(p *CalibratedProfile) { p.Guard.Checked = 0 }},
		{"no beta rules", func(p *CalibratedProfile) { p.BetaRules = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := base(t)
			tt.mutate(p)
			data, err := json.Marshal(p)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if _, err := ParseCalibratedProfile(data); err == nil {
				t.Fatalf("expected rejection for %s, got nil error", tt.name)
			}
		})
	}
}

func TestCalibratedFamilyOf(t *testing.T) {
	cases := map[string]string{
		"claude-haiku-4-5-20251001": "haiku",
		"claude-haiku-4-5":          "haiku",
		"claude-fable-5":            "fable",
		"claude-sonnet-4-6":         "sonnet",
		"claude-sonnet-4-5":         "sonnet",
		"claude-opus-4-8":           "opus",
		"weird-unknown-model":       "opus",
	}
	for model, want := range cases {
		if got := CalibratedFamilyOf(model); got != want {
			t.Errorf("family(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestCalibratedProfile_MessageBetas_ExactAndFallback(t *testing.T) {
	p, err := ParseCalibratedProfile(validCalibratedProfileJSON(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Exact tools match.
	betas, ok := p.MessageBetas("claude-sonnet-4-6", true, false)
	if !ok {
		t.Fatalf("expected sonnet|tools hit")
	}
	if !contains(betas, "context-management-2025-06-27") {
		t.Fatalf("sonnet|tools betas = %v", betas)
	}

	// No tools -> base bucket.
	betas, ok = p.MessageBetas("claude-sonnet-4-6", false, false)
	if !ok {
		t.Fatalf("expected sonnet| hit")
	}
	if contains(betas, "context-management-2025-06-27") {
		t.Fatalf("sonnet| base should not include context-management: %v", betas)
	}

	// json_schema+tools exact hit.
	betas, ok = p.MessageBetas("claude-sonnet-4-6", true, true)
	if !ok {
		t.Fatalf("expected sonnet|json_schema+tools hit")
	}
	if !contains(betas, "structured-outputs-2025-12-15") {
		t.Fatalf("sonnet|json_schema+tools betas = %v", betas)
	}

	// opus tools not captured -> falls back to opus| base.
	betas, ok = p.MessageBetas("claude-opus-4-8", true, false)
	if !ok {
		t.Fatalf("expected opus fallback to base")
	}
	if !contains(betas, "effort-2025-11-24") {
		t.Fatalf("opus fallback betas = %v", betas)
	}

	// Unknown family with no rules for it: fable not captured -> not found.
	if _, ok := p.MessageBetas("claude-fable-5", false, false); ok {
		t.Fatalf("expected fable miss (no fable rules), got hit")
	}
}

func TestCalibratedProfile_CountTokens_NoCrossEndpointFallback(t *testing.T) {
	p, err := ParseCalibratedProfile(validCalibratedProfileJSON(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// count_tokens sonnet base exists.
	if _, ok := p.CountTokensBetas("claude-sonnet-4-6", false, false); !ok {
		t.Fatalf("expected count_tokens sonnet hit")
	}
	// count_tokens opus not captured, and must NOT fall back to messages|opus.
	if _, ok := p.CountTokensBetas("claude-opus-4-8", false, false); ok {
		t.Fatalf("expected count_tokens opus miss (no cross-endpoint fallback)")
	}
}

func TestNilCalibratedProfile_SafeAccessors(t *testing.T) {
	var p *CalibratedProfile
	if p.Version() != "" || p.UserAgent() != "" || p.HeaderTemplate() != nil || p.AbsentHeaders() != nil {
		t.Fatalf("nil profile accessors should be zero")
	}
	if _, ok := p.MessageBetas("claude-sonnet-4-6", true, true); ok {
		t.Fatalf("nil profile should miss")
	}
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
