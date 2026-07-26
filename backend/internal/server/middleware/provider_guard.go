package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ProviderOnly 守住 /api/v1/provider/*：必须挂在 jwtAuth 之后。
//
// 两道检查：站点总开关必须开着，且当前用户必须是供号商。管理员刻意不放行——
// 供号商侧接口全部按 provider_user_id 归属过滤，管理员走这里没有意义，
// 反而会让「归属校验」的语义变模糊。管理员用 /admin/providers 那套接口。
func ProviderOnly(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settingService == nil || !settingService.IsProviderPortalEnabled(c.Request.Context()) {
			// 站点关停时返回 404 而非 403，不暴露该入口是否存在。
			response.NotFound(c, "Not found")
			c.Abort()
			return
		}
		if !IsProviderFromContext(c) {
			AbortWithError(c, 403, "FORBIDDEN", "Provider access required")
			return
		}
		c.Next()
	}
}

// ProviderDenyConsumerRoutes 是反向隔离：供号商是纯供货方，不得访问消费侧接口
// （/user、/keys、/usage、订阅、支付等）。必须挂在 jwtAuth 之后。
//
// 供号商没有余额也没有 API Key，即便放行这些接口也只会看到空数据；显式拒绝是为了
// 避免将来有人给这些接口加上跨用户能力时，供号商顺带获得了不该有的可见性。
func ProviderDenyConsumerRoutes() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsProviderFromContext(c) {
			AbortWithError(c, 403, "PROVIDER_SCOPE_ONLY",
				"Provider accounts can only use the provider portal")
			return
		}
		c.Next()
	}
}
