//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyClaudeCodeRunError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantKind   ClaudeCodeFailureKind
		wantStatus int
		retryable  bool
	}{
		{
			name:       "auth 401",
			err:        errors.New(`claude code reported error: API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"invalid token"}}`),
			wantKind:   ClaudeCodeFailureAuth,
			wantStatus: http.StatusUnauthorized,
			retryable:  true,
		},
		{
			name:       "oauth session expired",
			err:        errors.New("claude code: read stream: claude code reported error: Failed to authenticate: OAuth session expired and could not be refreshed (stderr: )"),
			wantKind:   ClaudeCodeFailureAuth,
			wantStatus: http.StatusUnauthorized,
			retryable:  true,
		},
		{
			name:       "not logged in",
			err:        errors.New("claude code reported error: Not logged in · Please run /login"),
			wantKind:   ClaudeCodeFailureAuth,
			wantStatus: http.StatusUnauthorized,
			retryable:  true,
		},
		{
			name:       "permission 403",
			err:        errors.New("API Error: 403 permission_error"),
			wantKind:   ClaudeCodeFailureAuth,
			wantStatus: http.StatusForbidden,
			retryable:  true,
		},
		{
			name:       "rate limit 429",
			err:        errors.New("API Error: 429 rate_limit_error"),
			wantKind:   ClaudeCodeFailureRateLimit,
			wantStatus: http.StatusTooManyRequests,
			retryable:  true,
		},
		{
			name:       "overloaded 529",
			err:        errors.New("API Error: 529 overloaded_error"),
			wantKind:   ClaudeCodeFailureOverloaded,
			wantStatus: 529,
			retryable:  true,
		},
		{
			name:       "server 500",
			err:        errors.New("API Error: 500 internal server error"),
			wantKind:   ClaudeCodeFailureUpstream,
			wantStatus: http.StatusInternalServerError,
			retryable:  true,
		},
		{
			name:       "timeout",
			err:        fmt.Errorf("claude code: timed out after 2m0s: %w", context.DeadlineExceeded),
			wantKind:   ClaudeCodeFailureTimeout,
			wantStatus: http.StatusGatewayTimeout,
			retryable:  true,
		},
		{
			name:       "no result",
			err:        errors.New("claude code: no result event"),
			wantKind:   ClaudeCodeFailureUpstream,
			wantStatus: http.StatusBadGateway,
			retryable:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classified := classifyClaudeCodeRunError(tt.err)
			var ccErr *ClaudeCodeError
			require.ErrorAs(t, classified, &ccErr)
			assert.Equal(t, tt.wantKind, ccErr.Kind)
			assert.Equal(t, tt.wantStatus, ccErr.StatusCode)
			assert.Equal(t, tt.retryable, ccErr.Retryable)
		})
	}
}

func TestClassifyClaudeCodeRunError_ClientCanceledIsNotFailover(t *testing.T) {
	classified := classifyClaudeCodeRunError(context.Canceled)
	assert.ErrorIs(t, classified, context.Canceled)
	var ccErr *ClaudeCodeError
	assert.False(t, errors.As(classified, &ccErr))
}

func TestClaudeCodeError_AsFailoverError(t *testing.T) {
	ccErr := &ClaudeCodeError{
		Kind:       ClaudeCodeFailureAuth,
		StatusCode: http.StatusUnauthorized,
		Message:    "token expired",
		Retryable:  true,
	}
	failover := ccErr.AsFailoverError()
	require.NotNil(t, failover)
	assert.Equal(t, http.StatusUnauthorized, failover.StatusCode)
	assert.Equal(t, GatewayFailureStageAccountAuth, failover.Stage)
	assert.Equal(t, GatewayFailureScopeAccount, failover.Scope)
	assert.Equal(t, "token expired", failover.ClientMessage)

	var body map[string]any
	require.NoError(t, json.Unmarshal(failover.ResponseBody, &body))
	assert.Equal(t, "error", body["type"])
	inner := body["error"].(map[string]any)
	assert.Equal(t, "authentication_error", inner["type"])

	requestErr := &ClaudeCodeError{Kind: ClaudeCodeFailureRequest, StatusCode: 400}
	assert.Nil(t, requestErr.AsFailoverError())
}

func TestClaudeCodePool_RunRequestMarksBuildErrorAsRequest(t *testing.T) {
	pool := NewClaudeCodePool(0, 2)
	called := false
	pool.NewRunner = func(*Account, func(json.RawMessage) error) (ClaudeCodeTurnRunner, error) {
		called = true
		return nil, nil
	}
	body := []byte(`{"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Bash"}]}`)
	_, err := pool.RunRequest(context.Background(),
		&Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		body, "claude-opus-5-5", nil,
	)
	require.Error(t, err)
	var ccErr *ClaudeCodeError
	require.ErrorAs(t, err, &ccErr)
	assert.Equal(t, ClaudeCodeFailureRequest, ccErr.Kind)
	assert.False(t, ccErr.Retryable)
	assert.False(t, called)
}
