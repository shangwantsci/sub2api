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

func TestDefaultModelsContainsClaudeOpus5(t *testing.T) {
	var got *Model
	for i := range DefaultModels {
		if DefaultModels[i].ID == "claude-opus-5" {
			got = &DefaultModels[i]
			break
		}
	}

	require.NotNil(t, got)
	require.Equal(t, "model", got.Type)
	require.Equal(t, "Claude Opus 5", got.DisplayName)
	require.Equal(t, "2026-07-24T00:00:00Z", got.CreatedAt)
	require.Contains(t, DefaultModelIDs(), "claude-opus-5")

	// Opus 5 无日期变体，API ID 与 alias 相同，不应参与短名/长名互转。
	require.Equal(t, "claude-opus-5", NormalizeModelID("claude-opus-5"))
	require.Equal(t, "claude-opus-5", DenormalizeModelID("claude-opus-5"))
}
