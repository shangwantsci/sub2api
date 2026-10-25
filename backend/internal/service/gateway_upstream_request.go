package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/tidwall/gjson"

	"github.com/gin-gonic/gin"
)

func (s *GatewayService) buildUpstreamRequest(ctx context.Context, c *gin.Context, account *Account, body []byte, token, tokenType, modelID string, reqStream bool) (*http.Request, []byte, error) {
	if account == nil {
		return nil, nil, fmt.Errorf("build upstream request: nil account")
	}
	if account.Transport() == TransportClaudeCode || tokenType == "oauth" {
		return nil, nil, fmt.Errorf("build upstream request: OAuth accounts must use Claude Code transport")
	}
	if account.Platform == PlatformAnthropic && account.Type == AccountTypeServiceAccount {
		req, err := s.buildUpstreamRequestAnthropicVertex(ctx, c, account, body, token, modelID, reqStream)
		return req, body, err
	}

	// 确定目标URL：API Key 账号可配 base_url，其余走默认 Anthropic 端点。
	targetURL := claudeAPIURL
	if account.Type == AccountTypeAPIKey {
		if baseURL := account.GetBaseURL(); baseURL != "" {
			validatedURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, nil, err
			}
			targetURL = validatedURL + "/v1/messages?beta=true"
		}
	} else if account.IsCustomBaseURLEnabled() {
		customURL := account.GetCustomBaseURL()
		if customURL == "" {
			return nil, nil, fmt.Errorf("custom_base_url is enabled but not configured for account %d", account.ID)
		}
		validatedURL, err := s.validateUpstreamBaseURL(customURL)
		if err != nil {
			return nil, nil, err
		}
		targetURL = s.buildCustomRelayURL(validatedURL, "/v1/messages", account)
	}

	clientHeaders := http.Header{}
	if c != nil && c.Request != nil {
		clientHeaders = c.Request.Header
	}

	// beta 策略先于 body sanitize；API Key 路径不注入 OAuth/billing/metadata 相关 beta。
	policyFilterSet := s.getBetaPolicyFilterSet(ctx, c, account, modelID)
	effectiveDropSet := mergeDropSets(policyFilterSet)
	finalBetaHeader, finalBetaShouldSet := s.computeFinalAnthropicBeta(
		tokenType, modelID, clientHeaders, body, effectiveDropSet,
	)
	if beta, ok := account.HeaderOverrideValue("anthropic-beta"); ok {
		finalBetaHeader, finalBetaShouldSet = beta, true
	}
	if sanitized, changed := sanitizeAnthropicBodyForBetaTokens(body, finalBetaHeader); changed {
		body = sanitized
	}
	body = sanitizeAnthropicThinkingCitations(body, modelID)

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}

	setAnthropicAPIKeyAuthHeader(req.Header, account, token)

	for key, values := range clientHeaders {
		lowerKey := strings.ToLower(key)
		if !allowedHeaders[lowerKey] {
			continue
		}
		wireKey := resolveWireCasing(key)
		for _, v := range values {
			addHeaderRaw(req.Header, wireKey, v)
		}
	}

	if getHeaderRaw(req.Header, "content-type") == "" {
		setHeaderRaw(req.Header, "content-type", "application/json")
	}
	if getHeaderRaw(req.Header, "anthropic-version") == "" {
		setHeaderRaw(req.Header, "anthropic-version", "2023-06-01")
	}

	deleteHeaderAllForms(req.Header, "anthropic-beta")
	if finalBetaShouldSet {
		setHeaderRaw(req.Header, "anthropic-beta", finalBetaHeader)
	}

	// Accept-Language 统一设为美式英语，与美国出口 IP 一致。
	setHeaderRaw(req.Header, "Accept-Language", "en-US,en;q=0.9")

	// 账号级请求头覆写（仅 anthropic/openai api_key 账号启用时生效）。
	account.ApplyHeaderOverrides(req.Header)

	s.debugLogGatewaySnapshot("UPSTREAM_FORWARD", req.Header, body, map[string]string{
		"url":        req.URL.String(),
		"token_type": tokenType,
	})

	return req, body, nil
}

// vertexSupportedBetaTokens 是 Vertex AI 的 Anthropic 端点接受的 anthropic-beta
// 白名单。Vertex 对任何未知 token 直接 HTTP 400，故采用白名单（与 Bedrock 的
// bedrockSupportedBetaTokens 同思路）而非黑名单：未来 Claude Code 新增的、Vertex 尚未
// 支持的 token 天然被剥离。当 Vertex 新增支持某 beta 时在此补充。
//
// 明确排除（issue #3358 中 Vertex 报 400 的 token）：advisor-tool-2026-03-01、
// prompt-caching-scope-2026-01-05、redact-thinking-2026-02-12、
// thinking-token-count-2026-05-13；以及 claude-code-20250219 / oauth-2025-04-20 等
// 客户端身份 beta——Vertex service_account 走 Bearer 鉴权，不需要它们。
var vertexSupportedBetaTokens = map[string]bool{
	"context-1m-2025-08-07":                  true,
	"context-management-2025-06-27":          true,
	"fine-grained-tool-streaming-2025-05-14": true,
	"interleaved-thinking-2025-05-14":        true,
}

// filterVertexBetaTokens 解析 client 的 anthropic-beta header，先剔除 drop 集合中的
// token（BetaPolicy filter + 默认 drop），再只保留 Vertex 支持的 token，去重后逗号拼接。
// 返回最终 header（可能为空字符串）。
func filterVertexBetaTokens(header string, drop map[string]struct{}) string {
	tokens := parseAnthropicBetaHeader(header)
	if len(tokens) == 0 {
		return ""
	}
	out := make([]string, 0, len(tokens))
	seen := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		if _, dropped := drop[t]; dropped {
			continue
		}
		if !vertexSupportedBetaTokens[t] {
			continue
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return strings.Join(out, ",")
}

func (s *GatewayService) buildUpstreamRequestAnthropicVertex(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	token string,
	modelID string,
	reqStream bool,
) (*http.Request, error) {
	vertexBody, err := buildVertexAnthropicRequestBody(body)
	if err != nil {
		return nil, err
	}

	// 计算最终 outgoing anthropic-beta。Vertex AI 的 Anthropic 端点只接受一小撮
	// beta token，未知 token 会直接 HTTP 400——近期 Claude Code CLI 透传的
	// advisor-tool-2026-03-01 / prompt-caching-scope-2026-01-05 /
	// redact-thinking-2026-02-12 / thinking-token-count-2026-05-13 都不被 Vertex 接受
	// （issue #3358）。这里复用 BetaPolicy 的 block 检查（与 Bedrock 的
	// resolveBedrockBetaTokensForRequest 对称），再按 vertexSupportedBetaTokens 白名单
	// 剥离其余 token，使该路径与 Anthropic 直连 / Bedrock 路径行为一致。
	clientBeta := ""
	if c != nil && c.Request != nil {
		clientBeta = getHeaderRaw(c.Request.Header, "anthropic-beta")
	}
	policy := s.evaluateBetaPolicy(ctx, clientBeta, account, modelID)
	if policy.blockErr != nil {
		return nil, policy.blockErr
	}
	finalBeta := filterVertexBetaTokens(clientBeta, mergeDropSets(policy.filterSet))

	// 能力维度 sanitize：基于最终 beta（而非原始 client 值）决定是否保留 body 中的
	// context_management，与 Anthropic 直连 / Bedrock 路径对称。
	if sanitized, changed := sanitizeAnthropicBodyForBetaTokens(vertexBody, finalBeta); changed {
		vertexBody = sanitized
	}
	fullURL, err := buildVertexAnthropicURL(account.VertexProjectID(), account.VertexLocation(modelID), modelID, reqStream)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(vertexBody))
	if err != nil {
		return nil, err
	}

	if c != nil && c.Request != nil {
		for key, values := range c.Request.Header {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if !allowedHeaders[lowerKey] || lowerKey == "anthropic-version" {
				continue
			}
			wireKey := resolveWireCasing(key)
			for _, v := range values {
				addHeaderRaw(req.Header, wireKey, v)
			}
		}
	}

	req.Header.Del("authorization")
	req.Header.Del("x-api-key")
	req.Header.Del("x-goog-api-key")
	req.Header.Del("cookie")
	req.Header.Del("anthropic-version")
	setHeaderRaw(req.Header, "authorization", "Bearer "+token)
	setHeaderRaw(req.Header, "content-type", "application/json")

	// 覆盖上面白名单 loop 写入的原始 client anthropic-beta，使用过滤后的最终值。
	// finalBeta 为空（全部被剥离）时不下发该 header，与 Vertex 无 beta 请求一致。
	deleteHeaderAllForms(req.Header, "anthropic-beta")
	if finalBeta != "" {
		setHeaderRaw(req.Header, "anthropic-beta", finalBeta)
	}

	s.debugLogGatewaySnapshot("UPSTREAM_FORWARD_VERTEX_ANTHROPIC", req.Header, vertexBody, map[string]string{
		"url":        req.URL.String(),
		"token_type": "service_account",
		"model":      modelID,
		"stream":     strconv.FormatBool(reqStream),
	})

	return req, nil
}

// getBetaHeader 处理 anthropic-beta header。
// 真实 Claude Code CLI 2.1.206 默认不携带 oauth-2025-04-20；该函数不再自动追加它。
func (s *GatewayService) getBetaHeader(modelID string, clientBetaHeader string) string {
	if strings.TrimSpace(clientBetaHeader) != "" {
		return clientBetaHeader
	}
	if strings.Contains(strings.ToLower(modelID), "haiku") {
		return claude.HaikuBetaHeader
	}

	return claude.DefaultBetaHeader
}

// bodyUsesStructuredOutputs 报告请求体是否使用了 output_config.format json_schema
// （真身 2.1.211 在此类请求上会追加 structured-outputs beta）。
func bodyUsesStructuredOutputs(body []byte) bool {
	return gjson.GetBytes(body, "output_config.format.type").String() == "json_schema"
}

func requestNeedsBetaFeatures(body []byte) bool {
	tools := gjson.GetBytes(body, "tools")
	if tools.Exists() && tools.IsArray() && len(tools.Array()) > 0 {
		return true
	}
	thinkingType := gjson.GetBytes(body, "thinking.type").String()
	if strings.EqualFold(thinkingType, "enabled") || strings.EqualFold(thinkingType, "adaptive") {
		return true
	}
	return false
}

func defaultAPIKeyBetaHeader(body []byte) string {
	modelID := gjson.GetBytes(body, "model").String()
	if strings.Contains(strings.ToLower(modelID), "haiku") {
		return claude.APIKeyHaikuBetaHeader
	}
	return claude.APIKeyBetaHeader
}

func applyClaudeOAuthHeaderDefaults(req *http.Request) {
	if req == nil {
		return
	}
	if getHeaderRaw(req.Header, "Accept") == "" {
		setHeaderRaw(req.Header, "Accept", "application/json")
	}
	for key, value := range claude.DefaultHeaders {
		if value == "" {
			continue
		}
		if getHeaderRaw(req.Header, key) == "" {
			setHeaderRaw(req.Header, resolveWireCasing(key), value)
		}
	}
}

func mergeAnthropicBeta(required []string, incoming string) string {
	seen := make(map[string]struct{}, len(required)+8)
	out := make([]string, 0, len(required)+8)

	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	for _, r := range required {
		add(r)
	}
	for _, p := range strings.Split(incoming, ",") {
		add(p)
	}
	return strings.Join(out, ",")
}

func ensureClaudeChromeOAuthBeta(account *Account, tokenType, current string, shouldSet bool) (string, bool) {
	if tokenType != "oauth" ||
		account == nil ||
		account.GetCredential("oauth_client") != oauth.OAuthClientClaudeChrome {
		return current, shouldSet
	}
	return mergeAnthropicBeta([]string{claude.BetaOAuth}, current), true
}

func mergeAnthropicBetaDropping(required []string, incoming string, drop map[string]struct{}) string {
	merged := mergeAnthropicBeta(required, incoming)
	if merged == "" || len(drop) == 0 {
		return merged
	}
	out := make([]string, 0, 8)
	for _, p := range strings.Split(merged, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := drop[p]; ok {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, ",")
}

// computeFinalAnthropicBeta 计算 API Key 账号发往上游的最终 anthropic-beta header 值。
// OAuth/SetupToken 走 Claude Code，不再由 Go 网关组装 beta/mimic。
func (s *GatewayService) computeFinalAnthropicBeta(
	tokenType string,
	modelID string,
	clientHeaders http.Header,
	body []byte,
	effectiveDropSet map[string]struct{},
) (string, bool) {
	if tokenType == "oauth" {
		return "", false
	}
	clientBeta := ""
	if clientHeaders != nil {
		clientBeta = getHeaderRaw(clientHeaders, "anthropic-beta")
	}
	needsExtendedCacheTTL := bodyUsesAnthropicCacheTTL1h(body)

	if clientBeta != "" {
		if needsExtendedCacheTTL {
			return mergeAnthropicBetaDropping([]string{claude.BetaExtendedCacheTTL}, clientBeta, effectiveDropSet), true
		}
		return stripBetaTokensWithSet(clientBeta, effectiveDropSet), true
	}
	if needsExtendedCacheTTL {
		return mergeAnthropicBetaDropping([]string{claude.BetaExtendedCacheTTL}, "", effectiveDropSet), true
	}
	if s.cfg != nil && s.cfg.Gateway.InjectBetaForAPIKey {
		if requestNeedsBetaFeatures(body) {
			if beta := defaultAPIKeyBetaHeader(body); beta != "" {
				return beta, true
			}
		}
	}
	return "", false
}

// computeFinalCountTokensAnthropicBeta 是 count_tokens 路径上的 API Key 版本。
func (s *GatewayService) computeFinalCountTokensAnthropicBeta(
	tokenType string,
	modelID string,
	clientHeaders http.Header,
	body []byte,
	effectiveDropSet map[string]struct{},
) (string, bool) {
	if tokenType == "oauth" {
		return "", false
	}
	clientBeta := ""
	if clientHeaders != nil {
		clientBeta = getHeaderRaw(clientHeaders, "anthropic-beta")
	}
	needsExtendedCacheTTL := bodyUsesAnthropicCacheTTL1h(body)

	if clientBeta != "" {
		if needsExtendedCacheTTL {
			return mergeAnthropicBetaDropping([]string{claude.BetaExtendedCacheTTL}, clientBeta, effectiveDropSet), true
		}
		return stripBetaTokensWithSet(clientBeta, effectiveDropSet), true
	}
	if needsExtendedCacheTTL {
		return mergeAnthropicBetaDropping([]string{claude.BetaExtendedCacheTTL}, "", effectiveDropSet), true
	}
	if s.cfg != nil && s.cfg.Gateway.InjectBetaForAPIKey {
		if requestNeedsBetaFeatures(body) {
			if beta := defaultAPIKeyBetaHeader(body); beta != "" {
				return beta, true
			}
		}
	}
	return "", false
}

// stripBetaTokens removes the given beta tokens from a comma-separated header value.
func stripBetaTokens(header string, tokens []string) string {
	if header == "" || len(tokens) == 0 {
		return header
	}
	return stripBetaTokensWithSet(header, buildBetaTokenSet(tokens))
}

func stripBetaTokensWithSet(header string, drop map[string]struct{}) string {
	if header == "" || len(drop) == 0 {
		return header
	}
	parts := strings.Split(header, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := drop[p]; ok {
			continue
		}
		out = append(out, p)
	}
	if len(out) == len(parts) {
		return header // no change, avoid allocation
	}
	return strings.Join(out, ",")
}

// BetaBlockedError indicates a request was blocked by a beta policy rule.
type BetaBlockedError struct {
	Message string
}

func (e *BetaBlockedError) Error() string { return e.Message }

// betaPolicyResult holds the evaluated result of beta policy rules for a single request.
type betaPolicyResult struct {
	blockErr  *BetaBlockedError   // non-nil if a block rule matched
	filterSet map[string]struct{} // tokens to filter (may be nil)
}

// evaluateBetaPolicy loads settings once and evaluates all rules against the given request.
func (s *GatewayService) evaluateBetaPolicy(ctx context.Context, betaHeader string, account *Account, model string) betaPolicyResult {
	if s.settingService == nil {
		return betaPolicyResult{}
	}
	settings, err := s.settingService.GetBetaPolicySettings(ctx)
	if err != nil || settings == nil {
		return betaPolicyResult{}
	}
	isOAuth := account.IsOAuth()
	isBedrock := account.IsBedrock()
	var result betaPolicyResult
	for _, rule := range settings.Rules {
		if !betaPolicyScopeMatches(rule.Scope, isOAuth, isBedrock) {
			continue
		}
		effectiveAction, effectiveErrMsg := resolveRuleAction(rule, model)
		switch effectiveAction {
		case BetaPolicyActionBlock:
			if result.blockErr == nil && betaHeader != "" && containsBetaToken(betaHeader, rule.BetaToken) {
				msg := effectiveErrMsg
				if msg == "" {
					msg = "beta feature " + rule.BetaToken + " is not allowed"
				}
				result.blockErr = &BetaBlockedError{Message: msg}
			}
		case BetaPolicyActionFilter:
			if result.filterSet == nil {
				result.filterSet = make(map[string]struct{})
			}
			result.filterSet[rule.BetaToken] = struct{}{}
		}
	}
	return result
}

// mergeDropSets merges the static defaultDroppedBetasSet with dynamic policy filter tokens.
// Returns defaultDroppedBetasSet directly when policySet is empty (zero allocation).
func mergeDropSets(policySet map[string]struct{}, extra ...string) map[string]struct{} {
	if len(policySet) == 0 && len(extra) == 0 {
		return defaultDroppedBetasSet
	}
	m := make(map[string]struct{}, len(defaultDroppedBetasSet)+len(policySet)+len(extra))
	for t := range defaultDroppedBetasSet {
		m[t] = struct{}{}
	}
	for t := range policySet {
		m[t] = struct{}{}
	}
	for _, t := range extra {
		m[t] = struct{}{}
	}
	return m
}

// betaPolicyFilterSetKey is the gin.Context key for caching the policy filter set within a request.
const betaPolicyFilterSetKey = "betaPolicyFilterSet"

// getBetaPolicyFilterSet returns the beta policy filter set, using the gin context cache if available.
// In the /v1/messages path, Forward() evaluates the policy first and caches the result;
// buildUpstreamRequest reuses it (zero extra DB calls). In the count_tokens path, this
// evaluates on demand (one DB call).
func (s *GatewayService) getBetaPolicyFilterSet(ctx context.Context, c *gin.Context, account *Account, model string) map[string]struct{} {
	if c != nil {
		if v, ok := c.Get(betaPolicyFilterSetKey); ok {
			if fs, ok := v.(map[string]struct{}); ok {
				return fs
			}
		}
	}
	return s.evaluateBetaPolicy(ctx, "", account, model).filterSet
}

// betaPolicyScopeMatches checks whether a rule's scope matches the current account type.
func betaPolicyScopeMatches(scope string, isOAuth bool, isBedrock bool) bool {
	switch scope {
	case BetaPolicyScopeAll:
		return true
	case BetaPolicyScopeOAuth:
		return isOAuth
	case BetaPolicyScopeAPIKey:
		return !isOAuth && !isBedrock
	case BetaPolicyScopeBedrock:
		return isBedrock
	default:
		return true // unknown scope → match all (fail-open)
	}
}

// matchModelWhitelist checks if a model matches any pattern in the whitelist.
// Reuses matchModelPattern from group.go which supports exact and wildcard prefix matching.
func matchModelWhitelist(model string, whitelist []string) bool {
	for _, pattern := range whitelist {
		if matchModelPattern(pattern, model) {
			return true
		}
	}
	return false
}

// resolveRuleAction determines the effective action and error message for a rule given the request model.
// When ModelWhitelist is empty, the rule's primary Action/ErrorMessage applies unconditionally.
// When non-empty, Action applies to matching models; FallbackAction/FallbackErrorMessage applies to others.
func resolveRuleAction(rule BetaPolicyRule, model string) (action, errorMessage string) {
	if len(rule.ModelWhitelist) == 0 {
		return rule.Action, rule.ErrorMessage
	}
	if matchModelWhitelist(model, rule.ModelWhitelist) {
		return rule.Action, rule.ErrorMessage
	}
	if rule.FallbackAction != "" {
		return rule.FallbackAction, rule.FallbackErrorMessage
	}
	return BetaPolicyActionPass, "" // default fallback: pass (fail-open)
}

// droppedBetaSet returns claude.DroppedBetas as a set, with optional extra tokens.
func droppedBetaSet(extra ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(defaultDroppedBetasSet)+len(extra))
	for t := range defaultDroppedBetasSet {
		m[t] = struct{}{}
	}
	for _, t := range extra {
		m[t] = struct{}{}
	}
	return m
}

// containsBetaToken checks if a comma-separated header value contains the given token.
func containsBetaToken(header, token string) bool {
	if header == "" || token == "" {
		return false
	}
	for _, p := range strings.Split(header, ",") {
		if strings.TrimSpace(p) == token {
			return true
		}
	}
	return false
}

func filterBetaTokens(tokens []string, filterSet map[string]struct{}) []string {
	if len(tokens) == 0 || len(filterSet) == 0 {
		return tokens
	}
	kept := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if _, filtered := filterSet[token]; !filtered {
			kept = append(kept, token)
		}
	}
	return kept
}

func (s *GatewayService) resolveBedrockBetaTokensForRequest(
	ctx context.Context,
	account *Account,
	betaHeader string,
	body []byte,
	modelID string,
) ([]string, error) {
	// 1. 对原始 header 中的 beta token 做 block 检查（快速失败）
	policy := s.evaluateBetaPolicy(ctx, betaHeader, account, modelID)
	if policy.blockErr != nil {
		return nil, policy.blockErr
	}

	// 2. 解析 header + body 自动注入 + Bedrock 转换/过滤
	betaTokens := ResolveBedrockBetaTokens(betaHeader, body, modelID)

	// 3. 对最终 token 列表再做 block 检查，捕获通过 body 自动注入绕过 header block 的情况。
	//    例如：管理员 block 了 interleaved-thinking，客户端不在 header 中带该 token，
	//    但请求体中包含 thinking 字段 → autoInjectBedrockBetaTokens 会自动补齐 →
	//    如果不做此检查，block 规则会被绕过。
	if blockErr := s.checkBetaPolicyBlockForTokens(ctx, betaTokens, account, modelID); blockErr != nil {
		return nil, blockErr
	}

	return filterBetaTokens(betaTokens, policy.filterSet), nil
}

// checkBetaPolicyBlockForTokens 检查 token 列表中是否有被管理员 block 规则命中的 token。
// 用于补充 evaluateBetaPolicy 对 header 的检查，覆盖 body 自动注入的 token。
func (s *GatewayService) checkBetaPolicyBlockForTokens(ctx context.Context, tokens []string, account *Account, model string) *BetaBlockedError {
	if s.settingService == nil || len(tokens) == 0 {
		return nil
	}
	settings, err := s.settingService.GetBetaPolicySettings(ctx)
	if err != nil || settings == nil {
		return nil
	}
	isOAuth := account.IsOAuth()
	isBedrock := account.IsBedrock()
	tokenSet := buildBetaTokenSet(tokens)
	for _, rule := range settings.Rules {
		effectiveAction, effectiveErrMsg := resolveRuleAction(rule, model)
		if effectiveAction != BetaPolicyActionBlock {
			continue
		}
		if !betaPolicyScopeMatches(rule.Scope, isOAuth, isBedrock) {
			continue
		}
		if _, present := tokenSet[rule.BetaToken]; present {
			msg := effectiveErrMsg
			if msg == "" {
				msg = "beta feature " + rule.BetaToken + " is not allowed"
			}
			return &BetaBlockedError{Message: msg}
		}
	}
	return nil
}

func buildBetaTokenSet(tokens []string) map[string]struct{} {
	m := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		if t == "" {
			continue
		}
		m[t] = struct{}{}
	}
	return m
}

var defaultDroppedBetasSet = buildBetaTokenSet(claude.DroppedBetas)

func truncateForLog(b []byte, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 2048
	}
	if len(b) > maxBytes {
		b = b[:maxBytes]
	}
	s := string(b)
	// 保持一行，避免污染日志格式
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}

// buildCustomRelayURL 构建自定义中继转发 URL
// 在 path 后附加 beta=true 和可选的 proxy 查询参数
func (s *GatewayService) buildCustomRelayURL(baseURL, path string, account *Account) string {
	u := strings.TrimRight(baseURL, "/") + path + "?beta=true"
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL := account.Proxy.URL()
		if proxyURL != "" {
			u += "&proxy=" + url.QueryEscape(proxyURL)
		}
	}
	return u
}

func (s *GatewayService) validateUpstreamBaseURL(raw string) (string, error) {
	if s.cfg != nil && !s.cfg.Security.URLAllowlist.Enabled {
		normalized, err := urlvalidator.ValidateURLFormat(raw, s.cfg.Security.URLAllowlist.AllowInsecureHTTP)
		if err != nil {
			return "", fmt.Errorf("invalid base_url: %w", err)
		}
		return normalized, nil
	}
	normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     s.cfg.Security.URLAllowlist.UpstreamHosts,
		RequireAllowlist: true,
		AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	return normalized, nil
}
