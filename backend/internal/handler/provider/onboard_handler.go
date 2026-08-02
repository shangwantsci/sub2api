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
	// Name 允许留空：批量上号时供号商一次粘贴多行 session key，逐个手填名字不现实。
	// 留空时按换票返回的邮箱自动命名，见 resolveOnboardAccountName。
	Name  string  `json:"name"`
	Notes *string `json:"notes"`
	// Method 决定账号 type：oauth 或 setup-token。
	Method string `json:"method" binding:"required,oneof=oauth setup-token"`

	SessionKey string `json:"session_key"`
	SessionID  string `json:"session_id"`
	Code       string `json:"code"`

	// ProxyMode 决定出口来自平台代理池（auto）还是供号商自带（manual）。
	// 实际是否放行还要过站点策略，见 service.AssertProviderProxyModeAllowed。
	ProxyMode string `json:"proxy_mode" binding:"required,oneof=auto manual"`
	// ProxyURL 只在 manual 模式使用，接受多种粘贴写法，见 service.ParseProviderProxyURL。
	// 刻意不收 protocol/host/port 这些分离字段：供号商手上就是一整行连接串。
	ProxyURL string `json:"proxy_url"`

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

	if err := service.AssertProviderProxyModeAllowed(settings.ProxyModePolicy, req.ProxyMode); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 先定出口：换 token 就要走它，避免用平台默认出口换到的凭据与后续调度出口不一致。
	proxyID, createdProxy, err := h.resolveOnboardProxy(ctx, providerID, req, settings)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// 只有本次新建的供号商私有代理才允许回收。auto 模式拿到的是共享的平台代理，
	// 删掉会波及别人的账号 —— DeleteProxy 仅在还有账号引用时才拒绝，
	// 恰好选中一条当前零绑定的平台代理就会被真的删掉。
	cleanup := func(reason string) {
		if createdProxy {
			h.cleanupOrphanProxy(ctx, proxyID, reason)
		}
	}

	tokenInfo, err := h.exchangeCredentials(ctx, req, proxyID)
	if err != nil {
		cleanup("credential exchange failed")
		response.ErrorFrom(c, err)
		return
	}

	// 同一个 Anthropic 账号不能上两次。
	//
	// 这不是「顺手做个校验」：前端 axios 超时是 30 秒，而 CookieAuth 内部要串行打三次
	// claude.ai、最坏 180 秒，于是「前端已超时报错、后端仍然把号建成了」是常态，
	// 供号商看到失败必然重试。不去重的话同一个 Anthropic 账号会变成两条账号记录各自
	// 参与调度，共用一份上游配额互相打架，结算时也会把同一份用量算成两个号。
	existing, err := h.findOnboardedAccount(ctx, providerID, tokenInfo)
	if err != nil {
		cleanup("duplicate lookup failed")
		response.ErrorFrom(c, err)
		return
	}
	if existing != nil {
		// 已下线（软删除）的账号查不到，所以下线后重新上同一个号仍然走正常创建流程。
		cleanup("account already onboarded")
		response.Success(c, OnboardResultView{
			Account:   AccountViewFromService(existing, settings, nil),
			Duplicate: true,
		})
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
		cleanup("group priority lookup failed")
		response.ErrorFrom(c, err)
		return
	}

	credentials := buildCredentials(tokenInfo)
	input, err := service.BuildProviderAccountInput(settings, service.ProviderOnboardInput{
		ProviderUserID: providerID,
		Name:           resolveOnboardAccountName(req.Name, tokenInfo),
		Notes:          req.Notes,
		AccountType:    req.Method,
		Credentials:    credentials,
		HostingType:    req.HostingTypeID,
		Tier:           req.Tier,
		CustomTier:     toServiceCustomTier(req.CustomTier),
	}, proxyID, groupMin, groupHasAccounts)
	if err != nil {
		cleanup("account input build failed")
		response.ErrorFrom(c, err)
		return
	}

	account, err := h.adminService.CreateAccount(ctx, input)
	if err != nil {
		// CreateAccount 内部账号与分组绑定是同一事务，失败即整体回滚，
		// 所以这里只需回收代理，不会有引用它的残留账号。
		cleanup("account creation failed")
		response.ErrorFrom(c, err)
		return
	}

	// 只回脱敏视图，不回 tokenInfo。
	response.Success(c, OnboardResultView{Account: AccountViewFromService(account, settings, nil)})
}

// resolveOnboardAccountName 决定落库的账号名。
//
// 供号商自己填了就用他填的；批量上号时留空，按换票拿到的邮箱命名 —— 这正是供号商
// 用来对照「手上哪些号已经上了」的标识，比 `账号1/账号2` 有用得多。
// 邮箱也拿不到时退回账号 UUID 前 8 位，仍然为空则交给 BuildProviderAccountInput 报
// NAME_REQUIRED，不编造一个无从对照的名字。
func resolveOnboardAccountName(name string, t *service.TokenInfo) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	if t == nil {
		return ""
	}
	if email := strings.TrimSpace(t.EmailAddress); email != "" {
		return email
	}
	if uuid := strings.TrimSpace(t.AccountUUID); len(uuid) >= 8 {
		return "Anthropic " + uuid[:8]
	}
	return ""
}

// findOnboardedAccount 在该供号商名下按 Anthropic 账号 UUID 找已上过的号。
//
// 只认 account_uuid：名称由供号商自填可以重复，邮箱在部分换票结果里为空，
// 都不足以判定「是同一个 Anthropic 账号」。
// 已下线的账号被 Ent 软删除拦截器过滤掉，所以下线后重新上同一个号不会被误判成重复。
func (h *Handler) findOnboardedAccount(
	ctx context.Context,
	providerID int64,
	t *service.TokenInfo,
) (*service.Account, error) {
	if t == nil {
		return nil, nil
	}
	uuid := strings.TrimSpace(t.AccountUUID)
	if uuid == "" {
		return nil, nil
	}
	accounts, err := h.accountRepo.ListByProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		acc := &accounts[i]
		if strings.EqualFold(providerAccountUUID(acc), uuid) {
			return acc, nil
		}
	}
	return nil, nil
}

// resolveOnboardProxy 定出本次上号使用的出口代理。
//
// 返回的 created 表示这条代理是本次新建的供号商私有代理 —— 只有它才允许在后续失败时
// 回收。auto 模式选中的是共享的平台代理，任何情况下都不能删。
func (h *Handler) resolveOnboardProxy(
	ctx context.Context,
	providerID int64,
	req OnboardRequest,
	settings service.ProviderSettings,
) (proxyID int64, created bool, err error) {
	if strings.TrimSpace(strings.ToLower(req.ProxyMode)) == service.ProviderProxyModeAuto {
		candidates, listErr := h.adminService.GetAllProxiesWithAccountCount(ctx)
		if listErr != nil {
			return 0, false, listErr
		}
		selected, selErr := service.SelectAutoAssignProxy(candidates, settings.AutoProxyMaxAccounts, time.Now())
		if selErr != nil {
			return 0, false, selErr
		}
		return selected.ID, false, nil
	}

	proxyInput, err := service.ParseProviderProxyURL(req.ProxyURL)
	if err != nil {
		return 0, false, err
	}

	// 同一个供号商反复用同一条代理上号是常态。不复用的话代理池会被同参数记录撑爆，
	// 而且每条记录各绑一个账号，绑定数统计跟着失真。
	existing, err := h.findProviderProxy(ctx, providerID, proxyInput)
	if err != nil {
		return 0, false, err
	}
	if existing != nil {
		return existing.ID, false, nil
	}

	proxy, err := h.adminService.CreateProxy(ctx, &service.CreateProxyInput{
		Name:     service.ProviderProxyName(providerID, proxyInput.Host, proxyInput.Port),
		Protocol: proxyInput.Protocol,
		Host:     proxyInput.Host,
		Port:     proxyInput.Port,
		Username: proxyInput.Username,
		Password: proxyInput.Password,
		// 归属必须落到列上。只靠记录名前缀的话，自动分配会把这条代理当成平台自有
		// 而分给别的供号商，两家账号就共用同一个出口 IP 了。
		ProviderUserID: &providerID,
	})
	if err != nil {
		return 0, false, err
	}
	return proxy.ID, true, nil
}

// findProviderProxy 查这个供号商名下是否已有一条参数完全相同的代理。
func (h *Handler) findProviderProxy(
	ctx context.Context,
	providerID int64,
	in service.ProviderProxyInput,
) (*service.Proxy, error) {
	proxies, err := h.adminService.GetAllProxies(ctx)
	if err != nil {
		return nil, err
	}
	for i := range proxies {
		p := &proxies[i]
		if p.ProviderUserID == nil || *p.ProviderUserID != providerID {
			continue
		}
		if p.Protocol == in.Protocol &&
			p.Host == in.Host &&
			p.Port == in.Port &&
			p.Username == in.Username &&
			p.Password == in.Password {
			return p, nil
		}
	}
	return nil, nil
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

// GenerateReauthURL 为已有账号生成重新授权链接。
// POST /api/v1/provider/accounts/:id/auth-url
//
// 与上号时的 /onboard/auth-url 分成两个端点，是因为链接类型必须与账号原有类型一致：
// Setup Token 的号拿 OAuth 链接换回来的凭据 scope 对不上账号类型。而账号是 oauth
// 还是 setup-token 属于内部处理方式、不下发给供号商，前端没有这个信息也不该有，
// 所以只能由后端按账号自己定，不接受请求参数。
func (h *Handler) GenerateReauthURL(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	account, ok := h.ownedAccount(ctx, c, providerID)
	if !ok {
		return
	}

	var (
		result *service.GenerateAuthURLResult
		err    error
	)
	if account.Type == service.AccountTypeSetupToken {
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
