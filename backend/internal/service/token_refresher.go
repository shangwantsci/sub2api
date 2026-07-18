package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
)

// TokenRefresher 定义平台特定的token刷新策略接口
// 通过此接口可以扩展支持不同平台（Anthropic/OpenAI/Gemini）
type TokenRefresher interface {
	// CanRefresh 检查此刷新器是否能处理指定账号
	CanRefresh(account *Account) bool

	// NeedsRefresh 检查账号的token是否需要刷新
	NeedsRefresh(account *Account, refreshWindow time.Duration) bool

	// Refresh 执行token刷新，返回更新后的credentials
	// 注意：返回的map应该保留原有credentials中的所有字段，只更新token相关字段
	Refresh(ctx context.Context, account *Account) (map[string]any, error)
}

// ClaudeTokenRefresher 处理Anthropic/Claude OAuth token刷新
type ClaudeTokenRefresher struct {
	oauthService *OAuthService
}

// NewClaudeTokenRefresher 创建Claude token刷新器
func NewClaudeTokenRefresher(oauthService *OAuthService) *ClaudeTokenRefresher {
	return &ClaudeTokenRefresher{
		oauthService: oauthService,
	}
}

// CacheKey 返回用于分布式锁的缓存键
func (r *ClaudeTokenRefresher) CacheKey(account *Account) string {
	return ClaudeTokenCacheKey(account)
}

// CanRefresh 检查是否能处理此账号
// 处理 anthropic 平台的 oauth 与 setup-token 类型账号。
// 两者的 access_token 均为短期令牌（expires_in=28800，即 8h），到期都需刷新；
// setup-token 之前被排除会导致其 access_token 过期后请求 401。
// 此处与手动刷新入口（account.IsOAuth()）保持一致，实际是否刷新由 NeedsRefresh
// 基于 expires_at 门控，并在分布式锁保护下执行，不会造成过度刷新。
func (r *ClaudeTokenRefresher) CanRefresh(account *Account) bool {
	return account.Platform == PlatformAnthropic && account.IsOAuth()
}

// NeedsRefresh 检查token是否需要刷新
// 基于 expires_at 字段判断是否在刷新窗口内
func (r *ClaudeTokenRefresher) NeedsRefresh(account *Account, refreshWindow time.Duration) bool {
	expiresAt := account.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		return false
	}
	return time.Until(*expiresAt) < refreshWindow
}

// Refresh 执行token刷新
// 保留原有credentials中的所有字段，只更新token相关字段
func (r *ClaudeTokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	tokenInfo, err := r.oauthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return nil, err
	}

	newCredentials := BuildClaudeAccountCredentials(tokenInfo)
	newCredentials = MergeCredentials(account.Credentials, newCredentials)

	return newCredentials, nil
}

// FallbackRefresh re-authorizes Claude Chrome accounts with the stored
// sessionKey after an account-scoped refresh-token rejection. OAuthRefreshAPI
// invokes this only after refresh-token race recovery and while holding the
// same refresh locks.
func (r *ClaudeTokenRefresher) FallbackRefresh(ctx context.Context, account *Account, primaryErr error) (map[string]any, bool, error) {
	if !IsClaudeRefreshCredentialError(primaryErr) ||
		account == nil ||
		account.GetCredential("oauth_client") != oauth.OAuthClientClaudeChrome ||
		strings.TrimSpace(account.GetCredential("session_key")) == "" {
		return nil, false, nil
	}

	credentials, err := r.refreshWithSessionKey(ctx, account)
	if err != nil && !IsClaudeSessionKeyInvalidError(err) {
		err = NewClaudeSessionKeyReauthorizationError(err)
	}
	return credentials, true, err
}

func (r *ClaudeTokenRefresher) refreshWithSessionKey(ctx context.Context, account *Account) (map[string]any, error) {
	scope := "full"
	if account.Type == AccountTypeSetupToken {
		scope = "inference"
	}
	tokenInfo, err := r.oauthService.CookieAuth(ctx, &CookieAuthInput{
		SessionKey:  account.GetCredential("session_key"),
		ProxyID:     account.ProxyID,
		Scope:       scope,
		OAuthClient: oauth.OAuthClientClaudeChrome,
	})
	if err != nil {
		return nil, err
	}

	return MergeCredentials(account.Credentials, BuildClaudeAccountCredentials(tokenInfo)), nil
}

// ClaudeSessionKeyRefresher forces a stored-sessionKey re-authorization while
// reusing OAuthRefreshAPI's lock, DB reread, persistence, and token-version
// handling. It powers the explicit admin action.
type ClaudeSessionKeyRefresher struct {
	delegate *ClaudeTokenRefresher
}

func NewClaudeSessionKeyRefresher(oauthService *OAuthService) *ClaudeSessionKeyRefresher {
	return &ClaudeSessionKeyRefresher{delegate: NewClaudeTokenRefresher(oauthService)}
}

func (r *ClaudeSessionKeyRefresher) CacheKey(account *Account) string {
	return ClaudeTokenCacheKey(account)
}

func (r *ClaudeSessionKeyRefresher) CanRefresh(account *Account) bool {
	return account != nil &&
		account.Platform == PlatformAnthropic &&
		account.IsOAuth() &&
		account.GetCredential("oauth_client") == oauth.OAuthClientClaudeChrome &&
		strings.TrimSpace(account.GetCredential("session_key")) != ""
}

func (r *ClaudeSessionKeyRefresher) NeedsRefresh(_ *Account, _ time.Duration) bool {
	return true
}

func (r *ClaudeSessionKeyRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	return r.delegate.refreshWithSessionKey(ctx, account)
}

// OpenAITokenRefresher 处理 OpenAI OAuth token刷新
type OpenAITokenRefresher struct {
	openaiOAuthService *OpenAIOAuthService
	accountRepo        AccountRepository
}

// NewOpenAITokenRefresher 创建 OpenAI token刷新器
func NewOpenAITokenRefresher(openaiOAuthService *OpenAIOAuthService, accountRepo AccountRepository) *OpenAITokenRefresher {
	return &OpenAITokenRefresher{
		openaiOAuthService: openaiOAuthService,
		accountRepo:        accountRepo,
	}
}

// CacheKey 返回用于分布式锁的缓存键
func (r *OpenAITokenRefresher) CacheKey(account *Account) string {
	return OpenAITokenCacheKey(account)
}

// CanRefresh 检查是否能处理此账号
func (r *OpenAITokenRefresher) CanRefresh(account *Account) bool {
	if account.IsCredentialShadow() {
		return false
	}
	return account.Platform == PlatformOpenAI && account.Type == AccountTypeOAuth
}

// NeedsRefresh 检查token是否需要刷新
// expires_at 缺失且处于限流状态时需要刷新，防止限流期间 token 静默过期
func (r *OpenAITokenRefresher) NeedsRefresh(account *Account, refreshWindow time.Duration) bool {
	if account.IsOpenAIPersonalAccessToken() {
		return false
	}
	if strings.TrimSpace(account.GetOpenAIRefreshToken()) == "" {
		return false
	}
	expiresAt := account.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		return account.IsRateLimited()
	}

	return time.Until(*expiresAt) < refreshWindow
}

// Refresh 执行token刷新
// 保留原有credentials中的所有字段，只更新token相关字段
func (r *OpenAITokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	tokenInfo, err := r.openaiOAuthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return nil, err
	}

	// 使用服务提供的方法构建新凭证，并保留原有字段
	newCredentials := r.openaiOAuthService.BuildAccountCredentials(tokenInfo)
	newCredentials = MergeCredentials(account.Credentials, newCredentials)
	newCredentials = NormalizeOpenAIPersonalAccessTokenCredentials(account, tokenInfo, newCredentials)

	return newCredentials, nil
}
