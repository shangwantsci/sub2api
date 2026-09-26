package service

import "testing"

func TestDetectModelPlatformKnownFamilies(t *testing.T) {
	cases := []struct {
		model    string
		platform string
		ok       bool
	}{
		{"claude-opus-5-5", PlatformAnthropic, true},
		{"gpt-5.6-sol", PlatformOpenAI, true},
		{"gemini-2.5-pro", PlatformGemini, true},
		{"grok-4.6", PlatformGrok, true},
		{"anthropic/claude-fable-5-1", PlatformAnthropic, true},
		{"kimi-k2", "", false},
		{"deepseek-v4-pro", "", false},
		{"glm-5", "", false},
		{"minimax-m2", "", false},
		{"not-a-model", "", false},
	}
	for _, tc := range cases {
		got, ok := DetectModelPlatform(tc.model)
		if ok != tc.ok || got != tc.platform {
			t.Fatalf("DetectModelPlatform(%q) = %q, %v; want %q, %v", tc.model, got, ok, tc.platform, tc.ok)
		}
	}
}
