package handler

import (
	"net/http"
	"strings"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *GatewayHandler) checkContentSafety(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol string, model string, body []byte) bool {
	decision := h.contentSafetyDecision(c, reqLog, apiKey, subject, protocol, model, body)
	if decision == nil || !decision.Blocked {
		return false
	}
	h.errorResponse(c, contentSafetyStatus(decision), contentSafetyErrorCode(decision), decision.Message)
	return true
}

func (h *GatewayHandler) contentSafetyDecision(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol string, model string, body []byte) *service.ContentSafetyDecision {
	if h == nil || h.contentSafetyGuard == nil || c == nil || c.Request == nil {
		return nil
	}
	decision := h.contentSafetyGuard.Check(c.Request.Context(), service.ContentSafetyCheckInput{Protocol: protocol, Body: body})
	logContentSafetyDecision(c, reqLog, apiKey, subject, protocol, model, decision)
	return decision
}

func (h *OpenAIGatewayHandler) contentSafetyDecision(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol string, model string, body []byte) *service.ContentSafetyDecision {
	if h == nil || h.contentSafetyGuard == nil || c == nil || c.Request == nil {
		return nil
	}
	decision := h.contentSafetyGuard.Check(c.Request.Context(), service.ContentSafetyCheckInput{Protocol: protocol, Body: body})
	logContentSafetyDecision(c, reqLog, apiKey, subject, protocol, model, decision)
	return decision
}

func contentSafetyStatus(decision *service.ContentSafetyDecision) int {
	if decision == nil {
		return http.StatusForbidden
	}
	return http.StatusForbidden
}

func contentSafetyErrorCode(decision *service.ContentSafetyDecision) string {
	return "content_policy_violation"
}

func logContentSafetyDecision(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol string, model string, decision *service.ContentSafetyDecision) {
	if decision == nil || !decision.Flagged || reqLog == nil {
		return
	}
	endpoint := GetInboundEndpoint(c)
	if endpoint == "" && c != nil && c.Request != nil && c.Request.URL != nil {
		endpoint = c.Request.URL.Path
	}
	finding := decision.PrimaryFinding
	fields := []zap.Field{
		zap.String("request_id", contentModerationRequestID(c.Request.Context())),
		zap.String("category", finding.Category),
		zap.String("severity", finding.Severity),
		zap.String("endpoint", endpoint),
		zap.String("protocol", protocol),
		zap.String("model", strings.TrimSpace(model)),
		zap.String("mode", decision.Mode),
		zap.String("action", decision.Action),
		zap.Bool("blocked", decision.Blocked),
		zap.Int("finding_count", len(decision.Findings)),
	}
	if decision.Blocked {
		reqLog.Warn("content_safety_filter.blocked", fields...)
		return
	}
	reqLog.Warn("content_safety_filter.warn", fields...)
}
