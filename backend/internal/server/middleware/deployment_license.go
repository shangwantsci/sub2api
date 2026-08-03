package middleware

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const deploymentLicenseStatusPath = "/api/v1/admin/deployment-license/status"
const deploymentLicenseRefreshPath = "/api/v1/admin/deployment-license/refresh"

// DeploymentLicenseEnforcement performs only an in-memory state read. It never
// contacts the license server or database, so it does not add network latency
// to gateway requests or touch Claude request bytes.
func DeploymentLicenseEnforcement(license *service.DeploymentLicenseService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if license == nil || !license.Enabled() {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if path == deploymentLicenseStatusPath || path == deploymentLicenseRefreshPath {
			c.Next()
			return
		}
		if isAccountExpansion(c.Request.Method, path) {
			allowed, err := license.CanCreateAccount(c.Request.Context())
			if err != nil {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
					"type":    "deployment_license_limit_unavailable",
					"message": "Unable to verify deployment account capacity.",
				}})
				return
			}
			if !allowed {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{
					"type":    "deployment_account_limit_reached",
					"message": "Deployment account limit has been reached.",
				}})
				return
			}
		}
		if isUserExpansion(c.Request.Method, path) {
			allowed, err := license.CanCreateUser(c.Request.Context())
			if err != nil {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
					"type":    "deployment_license_limit_unavailable",
					"message": "Unable to verify deployment user capacity.",
				}})
				return
			}
			if !allowed {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{
					"type":    "deployment_user_limit_reached",
					"message": "Deployment user limit has been reached.",
				}})
				return
			}
		}
		if isChromeCookieAuthPath(c.Request.Method, path) &&
			!license.HasCapability(service.CapabilityChromeCookieAuth) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{
				"type":    "deployment_capability_disabled",
				"message": "Claude for Chrome cookie authorization is not enabled for this deployment.",
			}})
			return
		}
		if isGatewayBusinessPath(path) && !license.CanServeGateway() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{
					"type":    "deployment_license_unavailable",
					"message": "Deployment license is not active. Contact the deployment operator.",
				},
			})
			return
		}
		if isAdminMutation(c.Request.Method, path) && !license.CanMutateAdmin() {
			c.AbortWithStatusJSON(http.StatusLocked, gin.H{
				"error": gin.H{
					"type":    "deployment_license_read_only",
					"message": "Deployment is in license grace/read-only mode.",
				},
			})
			return
		}
		c.Next()
	}
}

func isAccountExpansion(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	switch path {
	case "/api/v1/admin/accounts",
		"/api/v1/admin/accounts/batch",
		"/api/v1/admin/accounts/data",
		"/api/v1/admin/accounts/import/codex-session",
		"/api/v1/admin/accounts/import/anthropic-session",
		"/api/v1/admin/accounts/sync/crs",
		"/api/v1/admin/accounts/exchange-code",
		"/api/v1/admin/accounts/exchange-setup-token-code",
		"/api/v1/admin/accounts/cookie-auth",
		"/api/v1/admin/accounts/setup-token-cookie-auth",
		"/api/v1/admin/accounts/chrome-cookie-auth",
		"/api/v1/provider/onboard/submit":
		return true
	default:
		return strings.HasPrefix(path, "/api/v1/admin/accounts/") &&
			(strings.HasSuffix(path, "/duplicate") || strings.HasSuffix(path, "/shadow"))
	}
}

// isChromeCookieAuthPath matches only the Claude for Chrome cookie OAuth entry
// points: the authorization endpoint and the rotating-token refresh that exists
// solely for Chrome accounts. Plain cookie auto-auth (/cookie-auth) and setup
// token auth (/setup-token-cookie-auth) are different features and stay open.
// Blocking here means the capability can be granted per customer from the
// license server, without a separate build or a stripped-down branch.
func isChromeCookieAuthPath(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	if path == "/api/v1/admin/accounts/chrome-cookie-auth" {
		return true
	}
	return strings.HasPrefix(path, "/api/v1/admin/accounts/") &&
		strings.HasSuffix(path, "/refresh-cookie-auth")
}

func isUserExpansion(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	switch path {
	case "/api/v1/admin/users", "/api/v1/auth/register", "/api/v1/provider/auth/register":
		return true
	default:
		return false
	}
}

func isAdminMutation(method, path string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return strings.HasPrefix(path, "/api/v1/admin/") || strings.HasPrefix(path, "/api/v1/provider/")
}

func isGatewayBusinessPath(path string) bool {
	if strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/v1beta/") ||
		strings.HasPrefix(path, "/backend-api/codex/") {
		return true
	}
	switch path {
	case "/responses", "/alpha/search", "/models", "/chat/completions", "/embeddings":
		return true
	default:
		return strings.HasPrefix(path, "/responses/") ||
			strings.HasPrefix(path, "/images/") ||
			strings.HasPrefix(path, "/videos/")
	}
}
