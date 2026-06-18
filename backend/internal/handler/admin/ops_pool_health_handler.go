package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	defaultPoolHealthHorizon = 4 * time.Hour
	maxPoolHealthHorizon     = 24 * time.Hour
)

// GetPublicPoolHealth returns redacted aggregate health data for a public pool monitor.
// GET /api/v1/public/pool-health
func (h *OpsHandler) GetPublicPoolHealth(c *gin.Context) {
	if h == nil || h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}

	horizon, ok := parsePublicPoolHealthHorizon(c.Query("horizon"))
	if !ok {
		response.BadRequest(c, "Invalid horizon")
		return
	}

	req := service.PublicPoolHealthRequest{
		Platform:    service.PlatformAnthropic,
		GroupName:   "ccmax",
		AccountType: service.AccountTypeSetupToken,
		Horizon:     horizon,
	}

	snapshot, err := h.opsService.GetPublicPoolHealth(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, snapshot)
}

func parsePublicPoolHealthHorizon(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultPoolHealthHorizon, true
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < time.Minute || d > maxPoolHealthHorizon {
		return 0, false
	}
	return d, true
}
