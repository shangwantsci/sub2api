package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

// RegisterPublicRoutes registers unauthenticated public API routes.
func RegisterPublicRoutes(v1 *gin.RouterGroup, h *handler.Handlers) {
	public := v1.Group("/public")
	if h != nil && h.Admin != nil && h.Admin.Ops != nil {
		public.GET("/pool-health", h.Admin.Ops.GetPublicPoolHealth)
	}
}
