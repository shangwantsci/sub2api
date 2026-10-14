package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsIncludeBareGPT56Alias(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-5.6")
}

func TestDefaultModelsIncludeGPT6Family(t *testing.T) {
	ids := DefaultModelIDs()
	for _, id := range []string{"gpt-6", "gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna"} {
		require.Contains(t, ids, id)
	}
}

func TestGPT61SolRejectsDisabledReasoning(t *testing.T) {
	require.Error(t, ValidateGPT61SolReasoningEffort("gpt-6.1-sol", "none"))
	require.Error(t, ValidateGPT61SolReasoningEffort("openai/gpt-6.1-sol-minimal", "minimal"))
	require.NoError(t, ValidateGPT61SolReasoningEffort("gpt-6.1-sol", "max"))
	require.NoError(t, ValidateGPT61SolReasoningEffort("gpt-6-sol", "none"))
	require.True(t, IsGPT6SolOrLunaModelSpelling("gpt-6-luna-max"))
	require.False(t, IsGPT61SolModelSpelling("gpt-6-sol"))
}

func TestGPT61SolInstructionsDifferFromAstra(t *testing.T) {
	astra := CodexBaseInstructionsForModel("gpt-6-astra")
	sol := CodexBaseInstructionsForModel("gpt-6.1-sol")
	require.NotEqual(t, astra, sol)
	require.Contains(t, sol, "unless given explicit instructions")
	require.Contains(t, astra, "unless explicit authorization is already provided")
}
