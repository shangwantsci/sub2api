package routes

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 「应用到存量」先预览再确认：路径是与前端的契约。
// 预览（apply-preview）已经包含匹配数量，旧的「只回一个匹配数量」的 GET 端点被它取代并删除，
// 不能留着让人绕过预览直接确认。
func TestRegisterProviderAdminRoutes_ApplyPreviewReplacesOldCountEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminGroup := router.Group("/api/v1/admin")

	registerProviderAdminRoutes(adminGroup, &handler.Handlers{Admin: &handler.AdminHandlers{
		Provider: &admin.ProviderHandler{},
	}})

	routes := routeSet(router)
	require.Contains(t, routes, http.MethodGet+" /api/v1/admin/provider-settings/tiers/:tier/apply-preview")
	require.Contains(t, routes, http.MethodPost+" /api/v1/admin/provider-settings/tiers/:tier/apply-existing")

	require.NotContains(t, routes, http.MethodGet+" /api/v1/admin/provider-settings/tiers/:tier/affected-count")
	for route := range routes {
		require.False(t, strings.HasSuffix(route, "/affected-count"), "旧的匹配数量端点已删除: %s", route)
	}
}
