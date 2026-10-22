package admin

import (
	"log/slog"
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
//
// 受主设置控制：pool_health_enabled 关闭时返回 404；统计的分组名取自 pool_health_group_name。
func (h *OpsHandler) GetPublicPoolHealth(c *gin.Context) {
	if h == nil || h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}

	// 开关判断放在参数校验之前：关闭时无论请求带什么参数都只返回 404，不暴露该入口存在。
	// 读设置出错按关闭处理（fail-closed）。用 404 而不是 403，文案也不提「功能被关闭」。
	poolSettings, err := h.opsService.GetPublicPoolHealthSettings(c.Request.Context())
	if err != nil {
		slog.Warn("public_pool_health_settings_read_failed", "error", err)
		response.NotFound(c, "Not found")
		return
	}
	if !poolSettings.Enabled {
		response.NotFound(c, "Not found")
		return
	}

	horizon, ok := parsePublicPoolHealthHorizon(c.Query("horizon"))
	if !ok {
		response.BadRequest(c, "Invalid horizon")
		return
	}

	req := service.PublicPoolHealthRequest{
		Platform:    service.PlatformAnthropic,
		GroupName:   poolSettings.GroupName,
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
