package admin

import (
	"io"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// maxCalibratedProfileBytes 限制发布 body 大小（标定 profile 只有 headers + 分档 beta，
// 几 KB 量级；给足余量防止误传大文件）。
const maxCalibratedProfileBytes = 1 << 20 // 1 MiB

// GetClaudeCalibratedProfile 返回当前已发布标定 profile 的只读状态。
// GET /api/v1/admin/settings/claude-calibrated-profile
func (h *SettingHandler) GetClaudeCalibratedProfile(c *gin.Context) {
	status := h.settingService.GetClaudeCalibratedProfileStatus(c.Request.Context())
	response.Success(c, status)
}

// PublishClaudeCalibratedProfile 校验并发布一个标定 profile JSON（body 即 tools/cc-calibrate
// 产出的 profile-<version>.json）。校验不通过（守卫未过 / cc_version 与 UA 不一致 /
// schema 漂移）直接 400，不写入——保证错误字节永不进入热路径。
// POST /api/v1/admin/settings/claude-calibrated-profile
func (h *SettingHandler) PublishClaudeCalibratedProfile(c *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxCalibratedProfileBytes+1))
	if err != nil {
		response.BadRequest(c, "failed to read request body: "+err.Error())
		return
	}
	if len(raw) > maxCalibratedProfileBytes {
		response.BadRequest(c, "calibrated profile too large")
		return
	}
	if _, err := h.settingService.PublishClaudeCalibratedProfile(c.Request.Context(), raw); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, h.settingService.GetClaudeCalibratedProfileStatus(c.Request.Context()))
}

// ClearClaudeCalibratedProfile 清除已发布的标定 profile，网关回退到编译内置常量。
// DELETE /api/v1/admin/settings/claude-calibrated-profile
func (h *SettingHandler) ClearClaudeCalibratedProfile(c *gin.Context) {
	if err := h.settingService.ClearClaudeCalibratedProfile(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, h.settingService.GetClaudeCalibratedProfileStatus(c.Request.Context()))
}
