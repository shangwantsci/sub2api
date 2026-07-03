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
	writeContentSafetyErrorResponse(c, decision)
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

func writeContentSafetyErrorResponse(c *gin.Context, decision *service.ContentSafetyDecision) {
	code := contentSafetyErrorCode(decision)
	finding := service.ContentSafetyFinding{}
	if decision != nil {
		finding = decision.PrimaryFinding
	}
	requestID := ""
	if c != nil && c.Request != nil {
		requestID = contentModerationRequestID(c.Request.Context())
	}
	message := "请求违反使用政策，已被拦截。"
	if decision != nil && strings.TrimSpace(decision.Message) != "" {
		message = decision.Message
	}
	policy := gin.H{
		"category":       finding.Category,
		"category_label": service.ContentSafetyCategoryLabel(finding.Category),
		"severity":       finding.Severity,
		"confidence":     finding.Confidence,
		"action":         finding.Action,
		"request_id":     requestID,
	}
	if strings.TrimSpace(finding.Evidence.Excerpt) != "" {
		policy["evidence"] = gin.H{
			"source":  finding.Evidence.Source,
			"excerpt": finding.Evidence.Excerpt,
			"hash":    finding.Evidence.Hash,
		}
	}
	c.JSON(contentSafetyStatus(decision), gin.H{
		"type": "error",
		"error": gin.H{
			"type":    code,
			"code":    code,
			"message": message,
			"policy":  policy,
		},
	})
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
		zap.String("confidence", finding.Confidence),
		zap.String("endpoint", endpoint),
		zap.String("protocol", protocol),
		zap.String("model", strings.TrimSpace(model)),
		zap.String("mode", decision.Mode),
		zap.String("action", decision.Action),
		zap.Bool("blocked", decision.Blocked),
		zap.Int("finding_count", len(decision.Findings)),
	}
	if decision.LogRedactedEvidence && strings.TrimSpace(finding.Evidence.Excerpt) != "" {
		fields = append(fields,
			zap.String("evidence_source", finding.Evidence.Source),
			zap.String("evidence_excerpt", finding.Evidence.Excerpt),
			zap.String("evidence_hash", finding.Evidence.Hash),
		)
	}
	if decision.Blocked {
		reqLog.Warn("content_safety_filter.blocked", fields...)
		return
	}
	if decision.Action == service.ContentSafetyActionObserve {
		reqLog.Warn("content_safety_filter.observe", fields...)
		return
	}
	reqLog.Warn("content_safety_filter.warn", fields...)
}
