package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ClaudeSessionKeyInvalidError marks a definitive claude.ai login-session
// rejection. Callers may permanently quarantine the account only for this
// typed error; transport, proxy, Cloudflare, rate-limit, and 5xx failures must
// remain retryable.
type ClaudeSessionKeyInvalidError struct {
	err error
}

// ClaudeSessionKeyReauthorizationError marks a failed Chrome sessionKey
// fallback that did not contain definitive evidence that the browser session
// itself is invalid. String markers such as invalid_grant from the secondary
// authorization-code exchange must remain retryable.
type ClaudeSessionKeyReauthorizationError struct {
	err error
}

func (e *ClaudeSessionKeyInvalidError) Error() string {
	if e == nil || e.err == nil {
		return "CLAUDE_SESSION_KEY_INVALID"
	}
	return "CLAUDE_SESSION_KEY_INVALID: " + e.err.Error()
}

func (e *ClaudeSessionKeyInvalidError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func NewClaudeSessionKeyInvalidError(err error) error {
	if err == nil {
		err = errors.New("claude.ai session is no longer valid")
	}
	return &ClaudeSessionKeyInvalidError{err: err}
}

func IsClaudeSessionKeyInvalidError(err error) bool {
	var target *ClaudeSessionKeyInvalidError
	return errors.As(err, &target)
}

func (e *ClaudeSessionKeyReauthorizationError) Error() string {
	if e == nil || e.err == nil {
		return "Claude sessionKey re-authorization failed"
	}
	return "Claude sessionKey re-authorization failed: " + e.err.Error()
}

func (e *ClaudeSessionKeyReauthorizationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func NewClaudeSessionKeyReauthorizationError(err error) error {
	if err == nil {
		err = errors.New("unknown Claude sessionKey re-authorization error")
	}
	return &ClaudeSessionKeyReauthorizationError{err: err}
}

func IsClaudeSessionKeyReauthorizationError(err error) bool {
	var target *ClaudeSessionKeyReauthorizationError
	return errors.As(err, &target)
}

// IsClaudeRefreshCredentialError reports account-scoped refresh-token
// failures that are eligible for a sessionKey re-authorization fallback.
// OAuth client/scope configuration failures are intentionally excluded.
func IsClaudeRefreshCredentialError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"invalid_grant",
		"invalid_refresh_token",
		"refresh_token_reused",
		"refresh_token_invalidated",
		"no refresh token available",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func NewClaudeOAuthUpstreamError(operation string, status int, body string) error {
	detail := fmt.Sprintf("%s failed: status %d, body: %s", operation, status, body)
	if isDefinitiveClaudeSessionRejection(body) {
		return NewClaudeSessionKeyInvalidError(errors.New(detail))
	}
	return errors.New(detail)
}

func isDefinitiveClaudeSessionRejection(body string) bool {
	var response struct {
		Error struct {
			Details struct {
				ErrorCode string `json:"error_code"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &response); err == nil {
		switch strings.ToLower(strings.TrimSpace(response.Error.Details.ErrorCode)) {
		case "account_session_invalid", "session_stale_relogin", "invalid_session", "session_expired":
			return true
		}
	}

	message := strings.ToLower(body)
	for _, marker := range []string{
		"account_session_invalid",
		"session_stale_relogin",
		"invalid_session",
		"session_expired",
		"session key is invalid",
		"sessionkey is invalid",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
