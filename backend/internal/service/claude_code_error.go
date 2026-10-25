package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// ClaudeCodeFailureKind 是 CC 运输失败的类别。只有可重试的类别才会转成
// UpstreamFailoverError，交给网关的换号循环；请求形状错误直接回 400。
type ClaudeCodeFailureKind string

const (
	ClaudeCodeFailureRequest    ClaudeCodeFailureKind = "request"
	ClaudeCodeFailureAuth       ClaudeCodeFailureKind = "auth"
	ClaudeCodeFailureRateLimit  ClaudeCodeFailureKind = "rate_limit"
	ClaudeCodeFailureOverloaded ClaudeCodeFailureKind = "overloaded"
	ClaudeCodeFailureUpstream   ClaudeCodeFailureKind = "upstream"
	ClaudeCodeFailureTimeout    ClaudeCodeFailureKind = "timeout"
	ClaudeCodeFailureInternal   ClaudeCodeFailureKind = "internal"
)

// ClaudeCodeError 把 CC 进程/上游错误分成“可换号”和“请求本身不合法”两类。
type ClaudeCodeError struct {
	Kind       ClaudeCodeFailureKind
	StatusCode int
	Message    string
	Retryable  bool
	Err        error
}

func (e *ClaudeCodeError) Error() string {
	if e == nil {
		return "claude code error"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "claude code error"
}

func (e *ClaudeCodeError) Unwrap() error { return e.Err }

// AsFailoverError 把可重试的 CC 错误转成网关现有的换号错误。
// 返回 nil 表示不应换号（请求形状错误、客户端取消等）。
func (e *ClaudeCodeError) AsFailoverError() *UpstreamFailoverError {
	if e == nil || !e.Retryable || e.StatusCode < 400 {
		return nil
	}
	stage := GatewayFailureStageInference
	scope := GatewayFailureScopeAccount
	if e.Kind == ClaudeCodeFailureAuth {
		stage = GatewayFailureStageAccountAuth
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = e.Error()
	}
	body, _ := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    anthropicErrorType(e.StatusCode),
			"message": message,
		},
	})
	return &UpstreamFailoverError{
		StatusCode:       e.StatusCode,
		ResponseBody:     body,
		Stage:            stage,
		Scope:            scope,
		Reason:           GatewayFailureReason("claude_code_" + string(e.Kind)),
		ClientStatusCode: e.StatusCode,
		ClientMessage:    message,
	}
}

func anthropicErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case 529:
		return "overloaded_error"
	default:
		return "api_error"
	}
}

var claudeCodeStatusPattern = regexp.MustCompile(`\b(4\d{2}|5\d{2})\b`)

// classifyClaudeCodeRunError 把 runner/pool 的错误分类成 ClaudeCodeError。
// 401/403/429/529/5xx 与超时可以换号；请求形状错误在 RunRequest 里单独处理。
func classifyClaudeCodeRunError(err error) error {
	if err == nil {
		return nil
	}
	var existing *ClaudeCodeError
	if errors.As(err, &existing) {
		return err
	}
	// 客户端主动取消：不换号，交给上层按“客户端已断开”处理。
	if errors.Is(err, context.Canceled) {
		return err
	}

	message := strings.TrimSpace(err.Error())
	if len(message) > 2000 {
		message = message[:2000]
	}
	lower := strings.ToLower(message)

	failure := &ClaudeCodeError{
		Kind:      ClaudeCodeFailureUpstream,
		Message:   message,
		Retryable: true,
		Err:       err,
	}
	switch {
	case errors.Is(err, ErrClaudeCodePoolBusy):
		failure.Kind = ClaudeCodeFailureOverloaded
		failure.StatusCode = http.StatusServiceUnavailable
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(lower, "timed out") || strings.Contains(lower, "timeout"):
		failure.Kind = ClaudeCodeFailureTimeout
		failure.StatusCode = http.StatusGatewayTimeout
	case strings.Contains(lower, "authentication_error") || strings.Contains(lower, "invalid authentication") ||
		strings.Contains(lower, "oauth token") || strings.Contains(lower, "invalid x-api-key") ||
		strings.Contains(lower, "not logged in") || strings.Contains(lower, "please run /login") ||
		strings.Contains(lower, "authentication_failed") || strings.Contains(lower, "failed to authenticate") ||
		strings.Contains(lower, "oauth session expired") || strings.Contains(lower, "could not be refreshed"):
		failure.Kind = ClaudeCodeFailureAuth
		failure.StatusCode = http.StatusUnauthorized
	case strings.Contains(lower, "permission_error") || strings.Contains(lower, "forbidden"):
		failure.Kind = ClaudeCodeFailureAuth
		failure.StatusCode = http.StatusForbidden
	case strings.Contains(lower, "rate_limit") || strings.Contains(lower, "rate limit"):
		failure.Kind = ClaudeCodeFailureRateLimit
		failure.StatusCode = http.StatusTooManyRequests
	case strings.Contains(lower, "overloaded"):
		failure.Kind = ClaudeCodeFailureOverloaded
		failure.StatusCode = 529
	default:
		status := extractClaudeCodeStatus(lower)
		if status == 0 {
			status = http.StatusBadGateway
		}
		failure.StatusCode = status
		switch status {
		case http.StatusUnauthorized:
			failure.Kind = ClaudeCodeFailureAuth
		case http.StatusForbidden:
			failure.Kind = ClaudeCodeFailureAuth
		case http.StatusTooManyRequests:
			failure.Kind = ClaudeCodeFailureRateLimit
		case 529:
			failure.Kind = ClaudeCodeFailureOverloaded
		case http.StatusGatewayTimeout:
			failure.Kind = ClaudeCodeFailureTimeout
		}
	}
	return failure
}

func extractClaudeCodeStatus(text string) int {
	matches := claudeCodeStatusPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return 0
	}
	// 优先取最像上游错误的码，避免把 200/201 之类混进来。
	priority := []int{401, 403, 429, 529, 500, 502, 503, 504, 400, 404, 408, 409, 413, 422}
	present := make(map[int]bool, len(matches))
	for _, match := range matches {
		if n, err := strconv.Atoi(match); err == nil {
			present[n] = true
		}
	}
	for _, code := range priority {
		if present[code] {
			return code
		}
	}
	if n, err := strconv.Atoi(matches[0]); err == nil {
		return n
	}
	return 0
}

func newClaudeCodeRequestError(err error) *ClaudeCodeError {
	message := "invalid request for Claude Code transport"
	if err != nil {
		message = err.Error()
	}
	return &ClaudeCodeError{
		Kind:       ClaudeCodeFailureRequest,
		StatusCode: http.StatusBadRequest,
		Message:    message,
		Retryable:  false,
		Err:        err,
	}
}

func (e *ClaudeCodeError) String() string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Error())
}
