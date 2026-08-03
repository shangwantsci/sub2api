package middleware

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func newUnlicensedDeploymentService(t *testing.T) *service.DeploymentLicenseService {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.NewDeploymentLicenseService(&config.Config{
		DeploymentLicense: config.DeploymentLicenseConfig{
			Enabled:                true,
			ServerURL:              "http://127.0.0.1:3900",
			PublicKey:              base64.RawStdEncoding.EncodeToString(publicKey),
			DataDir:                t.TempDir(),
			MachineID:              "test-host",
			RenewalIntervalSeconds: 3600,
			RequestTimeoutSeconds:  1,
			GracePeriodSeconds:     60,
		},
	}, service.BuildInfo{Version: "test", BuildType: "release"})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestDeploymentLicenseEnforcementBlocksGatewayWhenUnlicensed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(DeploymentLicenseEnforcement(newUnlicensedDeploymentService(t)))
	router.POST("/v1/messages", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestDeploymentLicenseEnforcementKeepsManagementPlaneAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(DeploymentLicenseEnforcement(newUnlicensedDeploymentService(t)))
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestDeploymentLicenseExpansionPathClassification(t *testing.T) {
	accountPaths := []string{
		"/api/v1/admin/accounts",
		"/api/v1/admin/accounts/batch",
		"/api/v1/admin/accounts/123/duplicate",
		"/api/v1/provider/onboard/submit",
	}
	for _, path := range accountPaths {
		if !isAccountExpansion(http.MethodPost, path) {
			t.Fatalf("expected account expansion path: %s", path)
		}
	}
	if isAccountExpansion(http.MethodPost, "/api/v1/admin/accounts/123/refresh") {
		t.Fatal("refresh must not consume account capacity")
	}
	for _, path := range []string{"/api/v1/admin/users", "/api/v1/auth/register", "/api/v1/provider/auth/register"} {
		if !isUserExpansion(http.MethodPost, path) {
			t.Fatalf("expected user expansion path: %s", path)
		}
	}
}

// The existing community production build has no license configuration at all.
// Its gateway and admin traffic must pass through untouched.
func TestDeploymentLicenseEnforcementIsTransparentForCommunityBuild(t *testing.T) {
	svc, err := service.NewDeploymentLicenseService(&config.Config{},
		service.BuildInfo{Version: "0.1.156", BuildType: "release", ManagedCustomerID: "community"})
	if err != nil {
		t.Fatalf("community build failed to construct: %v", err)
	}
	if svc.Enabled() {
		t.Fatal("licensing must stay off for a community build")
	}
	for _, licenseService := range map[string]*service.DeploymentLicenseService{
		"community build": svc,
		"nil service":     nil,
	} {
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		engine.Use(DeploymentLicenseEnforcement(licenseService))
		engine.POST("/v1/messages", func(c *gin.Context) { c.String(http.StatusOK, "upstream") })
		engine.POST("/api/v1/admin/accounts", func(c *gin.Context) { c.String(http.StatusOK, "created") })

		for _, path := range []string{"/v1/messages", "/api/v1/admin/accounts"} {
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
			if recorder.Code != http.StatusOK {
				t.Errorf("%s was blocked with %d: %s", path, recorder.Code, recorder.Body.String())
			}
		}
	}
}

// Chrome cookie auth is gated per customer, but the neighbouring cookie
// features are separate products and must never be caught by the same rule.
func TestChromeCookieAuthPathClassification(t *testing.T) {
	gated := []string{
		"/api/v1/admin/accounts/chrome-cookie-auth",
		"/api/v1/admin/accounts/42/refresh-cookie-auth",
	}
	for _, path := range gated {
		if !isChromeCookieAuthPath(http.MethodPost, path) {
			t.Errorf("%s should be gated by the Chrome cookie capability", path)
		}
	}

	// Regression guard: these are plain cookie auto-auth and setup-token auth.
	// Gating them would break account onboarding for every customer.
	open := []string{
		"/api/v1/admin/accounts/cookie-auth",
		"/api/v1/admin/accounts/setup-token-cookie-auth",
		"/api/v1/admin/accounts",
		"/api/v1/admin/accounts/batch",
		"/api/v1/admin/accounts/42/duplicate",
		"/v1/messages",
	}
	for _, path := range open {
		if isChromeCookieAuthPath(http.MethodPost, path) {
			t.Errorf("%s must not be gated by the Chrome cookie capability", path)
		}
	}

	// Reads are never gated by this rule.
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		if isChromeCookieAuthPath(method, "/api/v1/admin/accounts/chrome-cookie-auth") {
			t.Errorf("%s must not be gated", method)
		}
	}
}

// An unlicensed customer image has no lease, so the capability is absent and
// the endpoint must be refused - while unrelated admin reads still work.
func TestDeploymentLicenseEnforcementBlocksChromeCookieAuthWithoutCapability(t *testing.T) {
	svc := newUnlicensedDeploymentService(t)
	if svc.HasCapability(service.CapabilityChromeCookieAuth) {
		t.Fatal("an unlicensed deployment must not hold the Chrome cookie capability")
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(DeploymentLicenseEnforcement(svc))
	engine.POST("/api/v1/admin/accounts/chrome-cookie-auth", func(c *gin.Context) {
		c.String(http.StatusOK, "authorized")
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodPost, "/api/v1/admin/accounts/chrome-cookie-auth", nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "deployment_capability_disabled") {
		t.Errorf("unexpected error body: %s", recorder.Body.String())
	}
}
