package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// OpenAIOAuthClient interface for OpenAI OAuth operations
type OpenAIOAuthClient interface {
	ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error)
	RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error)
}

// GrokOAuthClient interface for xAI/Grok OAuth operations.
type GrokOAuthClient interface {
	ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*xai.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken, proxyURL, clientID string) (*xai.TokenResponse, error)
	ConvertSSOToBuild(ctx context.Context, ssoToken, proxyURL string) (*xai.TokenResponse, error)
}

// GrokOAuthTokenService is the narrow refresh port used by Grok token providers.
type GrokOAuthTokenService interface {
	RefreshAccountToken(ctx context.Context, account *Account) (*GrokTokenInfo, error)
	BuildAccountCredentials(tokenInfo *GrokTokenInfo) map[string]any
}

// ClaudeOAuthClient handles HTTP requests for Claude OAuth flows
type ClaudeOAuthClient interface {
	GetOrganizationUUID(ctx context.Context, sessionKey, proxyURL string, profile oauth.ClaudeOAuthProfile) (string, error)
	GetAuthorizationCode(ctx context.Context, sessionKey, orgUUID, scope, codeChallenge, state, proxyURL string, profile oauth.ClaudeOAuthProfile) (string, error)
	ExchangeCodeForToken(ctx context.Context, code, codeVerifier, state, proxyURL string, profile oauth.ClaudeOAuthProfile) (*oauth.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken, proxyURL string, profile oauth.ClaudeOAuthProfile) (*oauth.TokenResponse, error)
}

// OAuthService handles OAuth authentication flows
type OAuthService struct {
	sessionStore *oauth.SessionStore
	proxyRepo    ProxyRepository
	oauthClient  ClaudeOAuthClient
}

// NewOAuthService creates a new OAuth service
func NewOAuthService(proxyRepo ProxyRepository, oauthClient ClaudeOAuthClient) *OAuthService {
	return &OAuthService{
		sessionStore: oauth.NewSessionStore(),
		proxyRepo:    proxyRepo,
		oauthClient:  oauthClient,
	}
}

// GenerateAuthURLResult contains the authorization URL and session info
type GenerateAuthURLResult struct {
	AuthURL   string `json:"auth_url"`
	SessionID string `json:"session_id"`
}

// GenerateAuthURL generates an OAuth authorization URL with full scope
func (s *OAuthService) GenerateAuthURL(ctx context.Context, proxyID *int64) (*GenerateAuthURLResult, error) {
	return s.generateAuthURLWithScope(ctx, oauth.ScopeOAuth, proxyID)
}

// GenerateSetupTokenURL generates an OAuth authorization URL for setup token (inference only)
func (s *OAuthService) GenerateSetupTokenURL(ctx context.Context, proxyID *int64) (*GenerateAuthURLResult, error) {
	scope := oauth.ScopeInference
	return s.generateAuthURLWithScope(ctx, scope, proxyID)
}

func (s *OAuthService) generateAuthURLWithScope(ctx context.Context, scope string, proxyID *int64) (*GenerateAuthURLResult, error) {
	// Generate PKCE values
	state, err := oauth.GenerateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	codeVerifier, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to generate code verifier: %w", err)
	}

	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	// Generate session ID
	sessionID, err := oauth.GenerateSessionID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session ID: %w", err)
	}

	// Get proxy URL if specified
	var proxyURL string
	if proxyID != nil {
		proxy, proxyErr := s.proxyRepo.GetByID(ctx, *proxyID)
		if proxyErr == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	// Store session
	session := &oauth.OAuthSession{
		State:        state,
		CodeVerifier: codeVerifier,
		Scope:        scope,
		ProxyURL:     proxyURL,
		CreatedAt:    time.Now(),
	}
	s.sessionStore.Set(sessionID, session)

	// Build authorization URL
	authURL := oauth.BuildAuthorizationURL(state, codeChallenge, scope)

	return &GenerateAuthURLResult{
		AuthURL:   authURL,
		SessionID: sessionID,
	}, nil
}

// ExchangeCodeInput represents the input for code exchange
type ExchangeCodeInput struct {
	SessionID string
	Code      string
	ProxyID   *int64
}

// TokenInfo represents the token information stored in credentials
type TokenInfo struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	OrgUUID      string `json:"org_uuid,omitempty"`
	AccountUUID  string `json:"account_uuid,omitempty"`
	EmailAddress string `json:"email_address,omitempty"`
	OAuthClient  string `json:"oauth_client,omitempty"`
	SessionKey   string `json:"session_key,omitempty"`
}

// ExchangeCode exchanges authorization code for tokens
func (s *OAuthService) ExchangeCode(ctx context.Context, input *ExchangeCodeInput) (*TokenInfo, error) {
	// Get session
	session, ok := s.sessionStore.Get(input.SessionID)
	if !ok {
		return nil, fmt.Errorf("session not found or expired")
	}

	// Get proxy URL
	proxyURL := session.ProxyURL
	if input.ProxyID != nil {
		proxy, proxyErr := s.proxyRepo.GetByID(ctx, *input.ProxyID)
		if proxyErr == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	// Exchange code for token
	tokenInfo, err := s.exchangeCodeForToken(ctx, input.Code, session.CodeVerifier, session.State, proxyURL, oauth.ClaudeCodeProfile)
	if err != nil {
		return nil, err
	}

	// Delete session after successful exchange
	s.sessionStore.Delete(input.SessionID)

	return tokenInfo, nil
}

// CookieAuthInput represents the input for cookie-based authentication
type CookieAuthInput struct {
	SessionKey  string
	ProxyID     *int64
	Scope       string // "full" or "inference"
	OAuthClient string
}

// CookieAuth performs OAuth using sessionKey (cookie-based auto-auth)
func (s *OAuthService) CookieAuth(ctx context.Context, input *CookieAuthInput) (*TokenInfo, error) {
	if input == nil {
		return nil, fmt.Errorf("cookie auth input is nil")
	}
	profile := oauth.ClaudeProfile(input.OAuthClient)
	sessionKey := input.SessionKey
	if profile.Name == oauth.OAuthClientClaudeChrome {
		sessionKey = strings.TrimSpace(sessionKey)
		if sessionKey == "" {
			return nil, fmt.Errorf("sessionKey is required")
		}
	}
	var proxyURL string
	if profile.Name == oauth.OAuthClientClaudeChrome {
		var err error
		proxyURL, err = s.resolveProxyURL(ctx, input.ProxyID)
		if err != nil {
			return nil, err
		}
	} else if input.ProxyID != nil {
		proxy, proxyErr := s.proxyRepo.GetByID(ctx, *input.ProxyID)
		if proxyErr == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	// Determine scope.
	// Internal API call uses ScopeAPI (org:create_api_key not supported)
	scope := oauth.ScopeAPI
	if input.Scope == "inference" {
		scope = oauth.ScopeInference
	}
	if profile.Name == oauth.OAuthClientClaudeChrome {
		scope = oauth.ScopeChrome
	}

	// Step 1: Get organization info using sessionKey
	orgUUID, err := s.getOrganizationUUID(ctx, sessionKey, proxyURL, profile)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization info: %w", err)
	}

	// Step 2: Generate PKCE values
	codeVerifier, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to generate code verifier: %w", err)
	}
	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	state, err := oauth.GenerateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	// Step 3: Get authorization code using cookie
	authCode, err := s.getAuthorizationCode(ctx, sessionKey, orgUUID, scope, codeChallenge, state, proxyURL, profile)
	if err != nil {
		return nil, fmt.Errorf("failed to get authorization code: %w", err)
	}

	// Step 4: Exchange code for token
	tokenInfo, err := s.exchangeCodeForToken(ctx, authCode, codeVerifier, state, proxyURL, profile)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Ensure org_uuid is set (from step 1 if not from token response)
	if tokenInfo.OrgUUID == "" && orgUUID != "" {
		tokenInfo.OrgUUID = orgUUID
		log.Printf("[OAuth] Set org_uuid from cookie auth")
	}
	if profile.Name == oauth.OAuthClientClaudeChrome {
		tokenInfo.OAuthClient = profile.Name
		tokenInfo.SessionKey = sessionKey
	}

	return tokenInfo, nil
}

// getOrganizationUUID gets the organization UUID from claude.ai using sessionKey
func (s *OAuthService) getOrganizationUUID(ctx context.Context, sessionKey, proxyURL string, profile oauth.ClaudeOAuthProfile) (string, error) {
	return s.oauthClient.GetOrganizationUUID(ctx, sessionKey, proxyURL, profile)
}

// getAuthorizationCode gets the authorization code using sessionKey
func (s *OAuthService) getAuthorizationCode(ctx context.Context, sessionKey, orgUUID, scope, codeChallenge, state, proxyURL string, profile oauth.ClaudeOAuthProfile) (string, error) {
	return s.oauthClient.GetAuthorizationCode(ctx, sessionKey, orgUUID, scope, codeChallenge, state, proxyURL, profile)
}

// exchangeCodeForToken exchanges authorization code for tokens
func (s *OAuthService) exchangeCodeForToken(ctx context.Context, code, codeVerifier, state, proxyURL string, profile oauth.ClaudeOAuthProfile) (*TokenInfo, error) {
	tokenResp, err := s.oauthClient.ExchangeCodeForToken(ctx, code, codeVerifier, state, proxyURL, profile)
	if err != nil {
		return nil, err
	}

	tokenInfo := &TokenInfo{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
		ExpiresAt:    time.Now().Unix() + tokenResp.ExpiresIn,
		RefreshToken: tokenResp.RefreshToken,
		Scope:        tokenResp.Scope,
	}

	if tokenResp.Organization != nil && tokenResp.Organization.UUID != "" {
		tokenInfo.OrgUUID = tokenResp.Organization.UUID
		log.Printf("[OAuth] Got org_uuid")
	}
	if tokenResp.Account != nil {
		if tokenResp.Account.UUID != "" {
			tokenInfo.AccountUUID = tokenResp.Account.UUID
			log.Printf("[OAuth] Got account_uuid")
		}
		if tokenResp.Account.EmailAddress != "" {
			tokenInfo.EmailAddress = tokenResp.Account.EmailAddress
			log.Printf("[OAuth] Got email_address")
		}
	}

	return tokenInfo, nil
}

// RefreshToken refreshes an OAuth token
func (s *OAuthService) RefreshToken(ctx context.Context, refreshToken string, proxyURL string, profile oauth.ClaudeOAuthProfile) (*TokenInfo, error) {
	tokenResp, err := s.oauthClient.RefreshToken(ctx, refreshToken, proxyURL, profile)
	if err != nil {
		return nil, err
	}

	tokenInfo := &TokenInfo{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
		ExpiresAt:    time.Now().Unix() + tokenResp.ExpiresIn,
		RefreshToken: tokenResp.RefreshToken,
		Scope:        tokenResp.Scope,
	}
	if profile.Name == oauth.OAuthClientClaudeChrome {
		tokenInfo.OAuthClient = profile.Name
	}
	return tokenInfo, nil
}

// RefreshAccountToken refreshes token for an account
func (s *OAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*TokenInfo, error) {
	refreshToken := account.GetCredential("refresh_token")
	if refreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	profile := oauth.ClaudeProfile(account.GetCredential("oauth_client"))
	var proxyURL string
	if profile.Name == oauth.OAuthClientClaudeChrome {
		var err error
		proxyURL, err = s.resolveProxyURL(ctx, account.ProxyID)
		if err != nil {
			return nil, err
		}
	} else if account.ProxyID != nil {
		proxy, proxyErr := s.proxyRepo.GetByID(ctx, *account.ProxyID)
		if proxyErr == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}
	return s.RefreshToken(ctx, refreshToken, proxyURL, profile)
}

func (s *OAuthService) resolveProxyURL(ctx context.Context, proxyID *int64) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	if s.proxyRepo == nil {
		return "", fmt.Errorf("proxy repository is not configured for proxy %d", *proxyID)
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil {
		return "", fmt.Errorf("resolve proxy %d: %w", *proxyID, err)
	}
	if proxy == nil {
		return "", fmt.Errorf("resolve proxy %d: proxy not found", *proxyID)
	}
	proxyURL := proxy.URL()
	if proxyURL == "" {
		return "", fmt.Errorf("resolve proxy %d: proxy URL is empty", *proxyID)
	}
	return proxyURL, nil
}

// Stop stops the session store cleanup goroutine
func (s *OAuthService) Stop() {
	s.sessionStore.Stop()
}
