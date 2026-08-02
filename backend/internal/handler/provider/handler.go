package provider

import (
	"context"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// Handler 承载供号商站点的全部接口。
type Handler struct {
	authService       *service.AuthService
	oauthService      *service.OAuthService
	adminService      service.AdminService
	settingService    *service.SettingService
	settlementService *service.ProviderSettlementService
	accountRepo       service.AccountRepository
	usageReader       service.ProviderUsageReader
	userReader        service.ProviderUserReader
	// accountUsageService 供「账号在 Anthropic 侧的额度用量」使用，与结算无关。
	accountUsageService *service.AccountUsageService
}

func NewHandler(
	authService *service.AuthService,
	oauthService *service.OAuthService,
	adminService service.AdminService,
	settingService *service.SettingService,
	settlementService *service.ProviderSettlementService,
	accountRepo service.AccountRepository,
	usageReader service.ProviderUsageReader,
	userReader service.ProviderUserReader,
	accountUsageService *service.AccountUsageService,
) *Handler {
	return &Handler{
		authService:         authService,
		oauthService:        oauthService,
		adminService:        adminService,
		settingService:      settingService,
		settlementService:   settlementService,
		accountRepo:         accountRepo,
		usageReader:         usageReader,
		userReader:          userReader,
		accountUsageService: accountUsageService,
	}
}

// currentProviderID 取当前登录供号商的用户 ID。
// ProviderOnly 中间件已保证身份，这里只做取值。
func currentProviderID(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Unauthorized")
		return 0, false
	}
	return subject.UserID, true
}

// loadSettings 读取供号商站点设置。
func (h *Handler) loadSettings(ctx context.Context) (service.ProviderSettings, error) {
	return h.settingService.GetProviderSettings(ctx)
}

// ownedAccount 按归属取账号。
//
// 不属于当前供号商时一律返回 404 而不是 403：403 会告诉调用方「这个 id 存在但不是你的」，
// 等于泄露了其他供号商的账号 id 空间。
func (h *Handler) ownedAccount(ctx context.Context, c *gin.Context, providerID int64) (*service.Account, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.NotFound(c, "Account not found")
		return nil, false
	}
	account, err := h.adminService.GetAccount(ctx, id)
	if err != nil || account == nil {
		response.NotFound(c, "Account not found")
		return nil, false
	}
	if account.ProviderUserID == nil || *account.ProviderUserID != providerID {
		response.NotFound(c, "Account not found")
		return nil, false
	}
	return account, true
}

// GetHostingTypes 返回可选托管类型（只含对外文案）。
// GET /api/v1/provider/hosting-types
func (h *Handler) GetHostingTypes(c *gin.Context) {
	settings, err := h.loadSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]HostingTypeView, 0, len(settings.HostingTypes))
	for _, ht := range settings.EnabledHostingTypes() {
		out = append(out, HostingTypeView{ID: ht.GroupID, Label: ht.Label, Description: ht.Description})
	}
	response.Success(c, gin.H{"hosting_types": out, "default_hosting_type_id": settings.DefaultGroupID})
}

// GetOnboardOptions 返回上号页需要的托管类型与档位选项。
// GET /api/v1/provider/onboard/options
func (h *Handler) GetOnboardOptions(c *gin.Context) {
	ctx := c.Request.Context()
	settings, err := h.loadSettings(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := OnboardOptionsFromSettings(settings)
	// manual_only 下这个标志不会被上号页用到，省掉一次全表聚合。
	if out.ProxyModePolicy != service.ProviderProxyPolicyManualOnly {
		candidates, listErr := h.adminService.GetAllProxiesWithAccountCount(ctx)
		if listErr != nil {
			response.ErrorFrom(c, listErr)
			return
		}
		out.AutoProxyAvailable = service.HasAutoAssignableProxy(
			candidates, settings.AutoProxyMaxAccounts, time.Now())
	}
	response.Success(c, out)
}

// GetProfile 返回当前供号商的基本信息。
// GET /api/v1/provider/profile
func (h *Handler) GetProfile(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	user, err := h.userReader.GetByID(c.Request.Context(), providerID)
	if err != nil || user == nil {
		response.ErrorFrom(c, infraerrors.NotFound("USER_NOT_FOUND", "user not found"))
		return
	}
	response.Success(c, gin.H{
		"id":         user.ID,
		"email":      user.Email,
		"username":   user.Username,
		"created_at": user.CreatedAt,
	})
}
