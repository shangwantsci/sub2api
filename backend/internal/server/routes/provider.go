package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterProviderRoutes 注册供号商站点路由 /api/v1/provider/*。
//
// 注册接口是公开的（供号商还没有账号），其余全部要求 jwtAuth + ProviderOnly。
// 登录复用 /api/v1/auth/login，因为供号商与普通用户共用 users 表与同一套 JWT。
func RegisterProviderRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	settingService *service.SettingService,
) {
	if h == nil || h.Provider == nil {
		return
	}

	// 公开：注册与注册验证码。两者的站点开关都由 service 内部校验，
	// 且看的是 provider_portal_enabled 而非 registration_enabled。
	public := v1.Group("/provider")
	{
		public.POST("/auth/register", h.Provider.Register)
		public.POST("/auth/send-verify-code", h.Provider.SendVerifyCode)
	}

	authed := v1.Group("/provider")
	authed.Use(gin.HandlerFunc(jwtAuth))
	authed.Use(middleware.ProviderOnly(settingService))
	{
		authed.GET("/profile", h.Provider.GetProfile)
		authed.GET("/hosting-types", h.Provider.GetHostingTypes)

		onboard := authed.Group("/onboard")
		{
			onboard.GET("/options", h.Provider.GetOnboardOptions)
			onboard.POST("/auth-url", h.Provider.GenerateAuthURL)
			onboard.POST("/submit", h.Provider.Onboard)
		}

		accounts := authed.Group("/accounts")
		{
			accounts.GET("", h.Provider.ListAccounts)
			accounts.GET("/:id/stats", h.Provider.GetAccountStats)
			accounts.POST("/:id/pause", h.Provider.PauseAccount)
			accounts.POST("/:id/resume", h.Provider.ResumeAccount)
			accounts.POST("/:id/reauth", h.Provider.Reauth)
			accounts.DELETE("/:id", h.Provider.OfflineAccount)
		}

		billing := authed.Group("/billing")
		{
			billing.GET("/current", h.Provider.GetCurrentBilling)
			// 只要周期元信息、不跑聚合的轻量端点，供账号列表等页面使用。
			billing.GET("/period", h.Provider.GetCurrentPeriodInfo)
			billing.GET("/settlements", h.Provider.ListSettlements)
			billing.GET("/export", h.Provider.ExportCurrentBilling)
		}
	}
}
