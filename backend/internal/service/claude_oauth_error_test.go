package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewClaudeOAuthUpstreamErrorRequiresExplicitSessionRejection(t *testing.T) {
	t.Parallel()

	genericUnauthorized := NewClaudeOAuthUpstreamError("get organizations", 401, `{"error":"unauthorized"}`)
	require.False(t, IsClaudeSessionKeyInvalidError(genericUnauthorized))

	explicitInvalidSession := NewClaudeOAuthUpstreamError("get organizations", 401, `{"error":"invalid_session"}`)
	require.True(t, IsClaudeSessionKeyInvalidError(explicitInvalidSession))
}

func TestClaudeSessionKeyReauthorizationErrorKeepsExchangeMarkersRetryable(t *testing.T) {
	t.Parallel()

	err := NewClaudeSessionKeyReauthorizationError(
		NewClaudeOAuthUpstreamError("exchange code", 400, `{"error":"invalid_grant"}`),
	)

	require.True(t, IsClaudeSessionKeyReauthorizationError(err))
	require.False(t, isNonRetryableRefreshError(err))
	require.True(t, isNonRetryableRefreshError(NewClaudeSessionKeyInvalidError(nil)))
}
