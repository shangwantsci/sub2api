package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type CompositeRouteRequest struct {
	PublicModel    string `json:"public_model" binding:"required"`
	MatchType      string `json:"match_type" binding:"omitempty,oneof=exact prefix"`
	TargetPlatform string `json:"target_platform" binding:"required,oneof=anthropic openai gemini antigravity grok"`
	UpstreamModel  string `json:"upstream_model"`
	Endpoint       string `json:"endpoint" binding:"omitempty,oneof=any messages count_tokens responses chat_completions embeddings images gemini"`
	Priority       int    `json:"priority"`
	Enabled        *bool  `json:"enabled"`
	Notes          string `json:"notes"`
}

type CompositeRoutePreviewRequest struct {
	Model    string `json:"model" binding:"required"`
	Endpoint string `json:"endpoint" binding:"omitempty,oneof=any messages count_tokens responses chat_completions embeddings images gemini"`
}

func (h *GroupHandler) ListCompositeRoutes(c *gin.Context) {
	groupID, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	routes, err := h.adminService.ListCompositeRoutes(c.Request.Context(), groupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, routes)
}

func (h *GroupHandler) CreateCompositeRoute(c *gin.Context) {
	groupID, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	var req CompositeRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}
	route, err := h.adminService.CreateCompositeRoute(c.Request.Context(), groupID, compositeRouteRequestToInput(req, true))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, route)
}

func (h *GroupHandler) UpdateCompositeRoute(c *gin.Context) {
	groupID, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	routeID, ok := parsePositiveIDParam(c, "route_id")
	if !ok {
		return
	}
	var req CompositeRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}
	route, err := h.adminService.UpdateCompositeRoute(c.Request.Context(), groupID, routeID, compositeRouteRequestToInput(req, true))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, route)
}

func (h *GroupHandler) DeleteCompositeRoute(c *gin.Context) {
	groupID, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	routeID, ok := parsePositiveIDParam(c, "route_id")
	if !ok {
		return
	}
	if err := h.adminService.DeleteCompositeRoute(c.Request.Context(), groupID, routeID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Composite route deleted"})
}

func (h *GroupHandler) PreviewCompositeRoute(c *gin.Context) {
	groupID, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	var req CompositeRoutePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}
	decision, err := h.adminService.PreviewCompositeRoute(c.Request.Context(), groupID, service.CompositeRoutePreviewRequest{
		Model:    req.Model,
		Endpoint: req.Endpoint,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, decision)
}

func compositeRouteRequestToInput(req CompositeRouteRequest, defaultEnabled bool) service.CompositeRouteInput {
	enabled := defaultEnabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return service.CompositeRouteInput{
		PublicModel:    req.PublicModel,
		MatchType:      req.MatchType,
		TargetPlatform: req.TargetPlatform,
		UpstreamModel:  req.UpstreamModel,
		Endpoint:       req.Endpoint,
		Priority:       req.Priority,
		Enabled:        enabled,
		Notes:          req.Notes,
	}
}

func parsePositiveIDParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid "+name)
		return 0, false
	}
	return id, true
}
