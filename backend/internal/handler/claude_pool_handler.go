package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ClaudePoolHandler struct {
	service *service.ClaudePoolStatusService
}

func NewClaudePoolHandler(service *service.ClaudePoolStatusService) *ClaudePoolHandler {
	return &ClaudePoolHandler{service: service}
}

// GET /api/v1/claude-pool/status
func (h *ClaudePoolHandler) GetStatus(c *gin.Context) {
	response.Success(c, h.service.PublicStatus())
}
