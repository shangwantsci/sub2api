package routes

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func routeSet(router *gin.Engine) map[string]struct{} {
	out := make(map[string]struct{})
	for _, route := range router.Routes() {
		out[route.Method+" "+route.Path] = struct{}{}
	}
	return out
}

// 容量档位模板的两个专用端点挂在管理员路由组的 /settings 下（仿 beta-policy 的写法），
// 路径是与前端的契约：GET / PUT /api/v1/admin/settings/capacity-presets。
func TestRegisterSettingsRoutes_CapacityPresetsEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminGroup := router.Group("/api/v1/admin")

	registerSettingsRoutes(adminGroup, &handler.Handlers{Admin: &handler.AdminHandlers{
		Setting:  &admin.SettingHandler{},
		Provider: &admin.ProviderHandler{},
	}})

	routes := routeSet(router)
	require.Contains(t, routes, http.MethodGet+" /api/v1/admin/settings/capacity-presets")
	require.Contains(t, routes, http.MethodPut+" /api/v1/admin/settings/capacity-presets")
	// 同一个路由组里已有的专用端点不受影响。
	require.Contains(t, routes, http.MethodGet+" /api/v1/admin/settings/beta-policy")
}

// 没有装配供号商处理器（例如部分测试装配）时不注册这两个端点，也不能因此 panic。
func TestRegisterSettingsRoutes_CapacityPresetsSkippedWithoutProviderHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminGroup := router.Group("/api/v1/admin")

	require.NotPanics(t, func() {
		registerSettingsRoutes(adminGroup, &handler.Handlers{Admin: &handler.AdminHandlers{Setting: &admin.SettingHandler{}}})
	})
	routes := routeSet(router)
	require.NotContains(t, routes, http.MethodGet+" /api/v1/admin/settings/capacity-presets")
	require.Contains(t, routes, http.MethodGet+" /api/v1/admin/settings/beta-policy")
}
