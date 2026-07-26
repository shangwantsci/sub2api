package provider

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// SendVerifyCodeRequest 是供号商注册前的邮箱验证码请求。
type SendVerifyCodeRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// SendVerifyCode 为供号商注册发送邮箱验证码。
// POST /api/v1/provider/auth/send-verify-code （公开路由）
//
// 单独开一个入口而不是复用 /auth/send-verify-code：后者受 registration_enabled 控制，
// 而供号商站点的典型配置就是关闭公开注册、只放邀请码注册。
func (h *Handler) SendVerifyCode(c *gin.Context) {
	var req SendVerifyCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.authService.SendProviderVerifyCodeAsync(c.Request.Context(), req.Email)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// RegisterRequest 是供号商注册。邀请码强制必填。
type RegisterRequest struct {
	Email      string `json:"email" binding:"required,email"`
	Password   string `json:"password" binding:"required,min=6"`
	VerifyCode string `json:"verify_code"`
	InviteCode string `json:"invite_code" binding:"required"`
}

// Register 供号商注册。
// POST /api/v1/provider/auth/register （公开路由）
//
// 登录复用现有 /api/v1/auth/login，因为供号商与普通用户共用一张 users 表和同一套 JWT，
// 区分只在 is_provider 能力位与前端落地路由上。
func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	token, user, err := h.authService.RegisterProvider(
		c.Request.Context(), req.Email, req.Password, req.VerifyCode, req.InviteCode,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{
		"token": token,
		"user": gin.H{
			"id":          user.ID,
			"email":       user.Email,
			"username":    user.Username,
			"role":        user.Role,
			"is_provider": true,
		},
	})
}
