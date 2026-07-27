package provider

import (
	"context"
	"log/slog"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// 供号商上号。四种方式全部走 claude_code profile：
//   - 手动 OAuth       : /onboard/auth-url  → /onboard/submit（携带 session_id + code）
//   - 手动 Setup Token : 同上，method=setup-token
//   - Cookie 授权      : /onboard/submit 直接带 session_key
//
// Chrome OAuth 在本包完全不可达：既没有对应的入口，构造 CookieAuthInput 时也永不设置
// OAuthClient（留空即 claude_code），并且请求体里若显式带了 oauth_client 会被断言拒绝。

// AuthURLRequest 请求授权链接。
type AuthURLRequest struct {
	// Method 只接受 oauth 或 setup-token。
	Method string `json:"method" binding:"required,oneof=oauth setup-token"`
	// OAuthClient 仅用于拒绝：供号商链路不允许指定 profile。
	OAuthClient string `json:"oauth_client"`
}

// GenerateAuthURL 生成 Anthropic 授权链接。
// POST /api/v1/provider/onboard/auth-url
//
// 刻意不接受 proxy_id：供号商的代理在提交阶段才创建，授权阶段走平台默认出口即可。
func (h *Handler) GenerateAuthURL(c *gin.Context) {
	var req AuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := service.AssertProviderOAuthClientAllowed(req.OAuthClient); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	ctx := c.Request.Context()
	var (
		result *service.GenerateAuthURLResult
		err    error
	)
	if req.Method == service.AccountTypeSetupToken {
		result, err = h.oauthService.GenerateSetupTokenURL(ctx, nil)
	} else {
		result, err = h.oauthService.GenerateAuthURL(ctx, nil)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ProxyPayload 是供号商自带的代理。
type ProxyPayload struct {
	Protocol string `json:"protocol" binding:"required"`
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// CustomTierPayload 是自定义档参数。
//
// 刻意不含 rpm_strategy：那决定调度器如何处理粘性会话，属于平台调度策略，
// 不能让供号商通过手工构造请求改动。
type CustomTierPayload struct {
	Concurrency     int     `json:"concurrency"`
	MaxSessions     int     `json:"max_sessions"`
	BaseRPM         int     `json:"base_rpm"`
	WindowCostLimit float64 `json:"window_cost_limit"`
	IdleTimeoutMin  int     `json:"session_idle_timeout_minutes"`
}

// OnboardRequest 是上号提交。
//
// 三选一的凭据来源：
//   - SessionKey 非空       → Cookie 授权
//   - SessionID + Code 非空 → 手动授权码
type OnboardRequest struct {
	Name  string  `json:"name" binding:"required"`
	Notes *string `json:"notes"`
	// Method 决定账号 type：oauth 或 setup-token。
	Method string `json:"method" binding:"required,oneof=oauth setup-token"`

	SessionKey string `json:"session_key"`
	SessionID  string `json:"session_id"`
	Code       string `json:"code"`

	Proxy         ProxyPayload       `json:"proxy" binding:"required"`
	HostingTypeID int64              `json:"hosting_type_id"`
	Tier          string             `json:"tier"`
	CustomTier    *CustomTierPayload `json:"custom_tier"`

	// OAuthClient 仅用于拒绝。
	OAuthClient string `json:"oauth_client"`
}

// Onboard 完成上号：换取凭据 → 建代理 → 建账号。
// POST /api/v1/provider/onboard/submit
//
// 响应绝不回显 TokenInfo / access_token / refresh_token / session_key。
func (h *Handler) Onboard(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	var req OnboardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := service.AssertProviderOAuthClientAllowed(req.OAuthClient); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	ctx := c.Request.Context()
	settings, err := h.loadSettings(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	proxyInput, err := service.ValidateProviderProxy(service.ProviderProxyInput{
		Protocol: req.Proxy.Protocol,
		Host:     req.Proxy.Host,
		Port:     req.Proxy.Port,
		Username: req.Proxy.Username,
		Password: req.Proxy.Password,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 先建代理：换 token 就要走它，避免用平台出口换到的凭据与后续调度出口不一致。
	proxy, err := h.adminService.CreateProxy(ctx, &service.CreateProxyInput{
		Name:     service.ProviderProxyName(providerID, proxyInput.Host, proxyInput.Port),
		Protocol: proxyInput.Protocol,
		Host:     proxyInput.Host,
		Port:     proxyInput.Port,
		Username: proxyInput.Username,
		Password: proxyInput.Password,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	tokenInfo, err := h.exchangeCredentials(ctx, req, proxy.ID)
	if err != nil {
		h.cleanupOrphanProxy(ctx, proxy.ID, "credential exchange failed")
		response.ErrorFrom(c, err)
		return
	}

	// 调度优先级与目标托管分组对齐。priority 是硬门槛，取值与分组内在跑的账号
	// 不一致会让其中一边永久拿不到流量，所以这里按分组现值自动决定，不让人配。
	hostingGroupID := req.HostingTypeID
	if hostingGroupID == 0 {
		hostingGroupID = settings.DefaultGroupID
	}
	groupMin, groupHasAccounts, err := h.resolveGroupMinPriority(ctx, hostingGroupID)
	if err != nil {
		h.cleanupOrphanProxy(ctx, proxy.ID, "group priority lookup failed")
		response.ErrorFrom(c, err)
		return
	}

	credentials := buildCredentials(tokenInfo)
	input, err := service.BuildProviderAccountInput(settings, service.ProviderOnboardInput{
		ProviderUserID: providerID,
		Name:           req.Name,
		Notes:          req.Notes,
		AccountType:    req.Method,
		Credentials:    credentials,
		HostingType:    req.HostingTypeID,
		Tier:           req.Tier,
		CustomTier:     toServiceCustomTier(req.CustomTier),
	}, proxy.ID, groupMin, groupHasAccounts)
	if err != nil {
		h.cleanupOrphanProxy(ctx, proxy.ID, "account input build failed")
		response.ErrorFrom(c, err)
		return
	}

	account, err := h.adminService.CreateAccount(ctx, input)
	if err != nil {
		// CreateAccount 内部账号与分组绑定是同一事务，失败即整体回滚，
		// 所以这里只需回收代理，不会有引用它的残留账号。
		h.cleanupOrphanProxy(ctx, proxy.ID, "account creation failed")
		response.ErrorFrom(c, err)
		return
	}

	// 只回脱敏视图，不回 tokenInfo。
	response.Success(c, AccountViewFromService(account, settings, nil))
}

// resolveGroupMinPriority 返回目标分组内现有账号的最小 priority。
// 分组为空时返回 (0, false, nil)，由调用方回落到设置里的默认值。
func (h *Handler) resolveGroupMinPriority(ctx context.Context, groupID int64) (int, bool, error) {
	if groupID <= 0 {
		return 0, false, nil
	}
	minByGroup, err := h.accountRepo.MinPriorityByGroup(ctx)
	if err != nil {
		return 0, false, err
	}
	v, ok := minByGroup[groupID]
	return v, ok, nil
}

// cleanupOrphanProxy 回收上号失败时残留的代理。
//
// 用独立的 context 而不是请求 ctx：上号失败常常正是因为请求超时或客户端断开，
// 此时请求 ctx 已取消，拿它去做清理必然也失败，反而稳定地留下孤儿代理。
func (h *Handler) cleanupOrphanProxy(ctx context.Context, proxyID int64, reason string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := h.adminService.DeleteProxy(cleanupCtx, proxyID); err != nil {
		slog.Warn("failed to clean up provider proxy after onboarding failure",
			"proxy_id", proxyID, "reason", reason, "error", err)
	}
}

// exchangeCredentials 按请求形态换取凭据，全程锁定 claude_code profile。
func (h *Handler) exchangeCredentials(
	ctx context.Context,
	req OnboardRequest,
	proxyID int64,
) (*service.TokenInfo, error) {
	sessionKey := strings.TrimSpace(req.SessionKey)
	code := strings.TrimSpace(req.Code)
	sessionID := strings.TrimSpace(req.SessionID)

	switch {
	case sessionKey != "":
		scope := "full"
		if req.Method == service.AccountTypeSetupToken {
			scope = "inference"
		}
		// OAuthClient 刻意留空 —— 留空即 claude_code。这是排除 chrome 的关键一行，
		// 不要因为「顺手支持一下」而把它改成可配置。
		return h.oauthService.CookieAuth(ctx, &service.CookieAuthInput{
			SessionKey: sessionKey,
			ProxyID:    &proxyID,
			Scope:      scope,
		})
	case sessionID != "" && code != "":
		return h.oauthService.ExchangeCode(ctx, &service.ExchangeCodeInput{
			SessionID: sessionID,
			Code:      code,
			ProxyID:   &proxyID,
		})
	default:
		return nil, infraerrors.BadRequest("MISSING_CREDENTIALS",
			"provide either a session key or an authorization code")
	}
}

// buildCredentials 把 TokenInfo 转成账号凭据。
//
// 刻意不写 oauth_client 与 session_key：供号商链路恒为 claude_code，
// 也不保留 sessionKey 回退能力（那是 chrome 链路特有的）。
func buildCredentials(t *service.TokenInfo) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	creds := map[string]any{
		"access_token": t.AccessToken,
		"token_type":   t.TokenType,
		"expires_at":   t.ExpiresAt,
	}
	if t.RefreshToken != "" {
		creds["refresh_token"] = t.RefreshToken
	}
	if t.Scope != "" {
		creds["scope"] = t.Scope
	}
	if t.OrgUUID != "" {
		creds["org_uuid"] = t.OrgUUID
	}
	if t.AccountUUID != "" {
		creds["account_uuid"] = t.AccountUUID
	}
	if t.EmailAddress != "" {
		creds["email_address"] = t.EmailAddress
	}
	return creds
}

func toServiceCustomTier(in *CustomTierPayload) *service.ProviderCustomTierInput {
	if in == nil {
		return nil
	}
	return &service.ProviderCustomTierInput{
		Concurrency:     in.Concurrency,
		MaxSessions:     in.MaxSessions,
		BaseRPM:         in.BaseRPM,
		WindowCostLimit: in.WindowCostLimit,
		IdleTimeoutMin:  in.IdleTimeoutMin,
	}
}

// ReauthRequest 是重新授权。
type ReauthRequest struct {
	SessionKey  string `json:"session_key"`
	SessionID   string `json:"session_id"`
	Code        string `json:"code"`
	OAuthClient string `json:"oauth_client"`
}

// Reauth 为已存在的账号重新换取凭据，沿用账号原有代理与档位。
// POST /api/v1/provider/accounts/:id/reauth
func (h *Handler) Reauth(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	account, ok := h.ownedAccount(ctx, c, providerID)
	if !ok {
		return
	}

	var req ReauthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := service.AssertProviderOAuthClientAllowed(req.OAuthClient); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account.ProxyID == nil {
		response.ErrorFrom(c, infraerrors.BadRequest("ACCOUNT_MISSING_PROXY",
			"this account has no proxy configured; contact support"))
		return
	}

	tokenInfo, err := h.exchangeCredentials(ctx, OnboardRequest{
		Method:     account.Type,
		SessionKey: req.SessionKey,
		SessionID:  req.SessionID,
		Code:       req.Code,
	}, *account.ProxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 凭据整体替换，但强制伪装项必须重新写回。
	creds := service.ApplyProviderForcedCredentials(buildCredentials(tokenInfo))

	// 身份字段同步回 extra。换票可能返回不同的 account_uuid，只改 credentials 会
	// 让 extra 留着旧值；而网关只认 extra 里的那份。这里传的是账号现有 extra 的副本，
	// 只补身份字段，不动档位与伪装开关。
	// 顺带修复本次改动之前上号、extra 里缺 account_uuid 的存量账号。
	extra := service.MirrorProviderIdentityToExtra(
		service.RefreshProviderIdentityExtra(account.Extra, creds), creds)

	updated, err := h.adminService.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{
		Name:        account.Name,
		Notes:       account.Notes,
		Type:        account.Type,
		Credentials: creds,
		Extra:       extra,
		Status:      service.StatusActive,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	settings, err := h.loadSettings(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, AccountViewFromService(updated, settings, nil))
}
