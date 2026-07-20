package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/stretchr/testify/require"
)

func TestNewClaudeOAuthUpstreamErrorRequiresExplicitSessionRejection(t *testing.T) {
	t.Parallel()

	genericUnauthorized := NewClaudeOAuthUpstreamError("get organizations", 401, `{"error":"unauthorized"}`)
	require.False(t, IsClaudeSessionKeyInvalidError(genericUnauthorized))

	explicitInvalidSession := NewClaudeOAuthUpstreamError("get organizations", 401, `{"error":"invalid_session"}`)
	require.True(t, IsClaudeSessionKeyInvalidError(explicitInvalidSession))

	productionInvalidSession := NewClaudeOAuthUpstreamError(
		"get organizations",
		403,
		`{"type":"error","error":{"type":"permission_error","message":"Invalid authorization","details":{"error_visibility":"user_facing","error_code":"account_session_invalid"}}}`,
	)
	require.True(t, IsClaudeSessionKeyInvalidError(productionInvalidSession))

	genericPermissionDenied := NewClaudeOAuthUpstreamError(
		"get organizations",
		403,
		`{"type":"error","error":{"type":"permission_error","message":"Invalid authorization"}}`,
	)
	require.False(t, IsClaudeSessionKeyInvalidError(genericPermissionDenied))

	cloudflareChallenge := NewClaudeOAuthUpstreamError(
		"get organizations",
		403,
		`<!DOCTYPE html><html><head><title>Just a moment...</title></head></html>`,
	)
	require.False(t, IsClaudeSessionKeyInvalidError(cloudflareChallenge))
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

func TestClaudeTokenRefresherFallbackTreatsAccountSessionInvalidAsPermanent(t *testing.T) {
	t.Parallel()

	oauthService := NewOAuthService(nil, &mockClaudeOAuthClient{
		getOrgUUIDFunc: func(context.Context, string, string, oauth.ClaudeOAuthProfile) (string, error) {
			return "", NewClaudeOAuthUpstreamError(
				"get organizations",
				403,
				`{"type":"error","error":{"type":"permission_error","message":"Invalid authorization","details":{"error_code":"account_session_invalid"}}}`,
			)
		},
	})
	refresher := NewClaudeTokenRefresher(oauthService)
	account := &Account{
		ID:       42,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"oauth_client": oauth.OAuthClientClaudeChrome,
			"session_key":  "expired-session",
		},
	}

	_, attempted, err := refresher.FallbackRefresh(
		context.Background(),
		account,
		errors.New("invalid_grant"),
	)

	require.True(t, attempted)
	require.Error(t, err)
	require.True(t, IsClaudeSessionKeyInvalidError(err))
	require.False(t, IsClaudeSessionKeyReauthorizationError(err))
	require.True(t, isNonRetryableRefreshError(err))
}
