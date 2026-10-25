//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// ============================================================================
// 背景
// ============================================================================
//
// Anthropic 上游对 body.context_management 字段实施 Pydantic schema 校验：
// 当且仅当 anthropic-beta header 含 context-management-2025-06-27 时接受。
// 否则报：
//   "context_management: Extra inputs are not permitted"
//
// 本仓采用能力维度对称约束（与 Bedrock 路径的 sanitizeBedrockFieldsForBetaTokens
// 对称）：在所有 Anthropic 直连出口，按最终 anthropic-beta header 是否含上述 token
// 决定 body 是否保留同名字段。
//
// 本文件覆盖：
//   1) sanitizeAnthropicBodyForBetaTokens 纯函数
//   2) anthropicBetaTokensContains 解析辅助函数
//   3) computeFinalAnthropicBeta / computeFinalCountTokensAnthropicBeta 各路径
//   4) normalizeClaudeOAuthRequestBody 的 context_management 补齐行为（不再按 model 短路）

// ============================================================================
// anthropicBetaTokensContains
// ============================================================================

func TestAnthropicBetaTokensContains_EmptyInputs(t *testing.T) {
	require.False(t, anthropicBetaTokensContains("", "context-management-2025-06-27"))
	require.False(t, anthropicBetaTokensContains("oauth-2025-04-20", ""))
}

func TestAnthropicBetaTokensContains_SingleToken(t *testing.T) {
	require.True(t, anthropicBetaTokensContains("context-management-2025-06-27", "context-management-2025-06-27"))
}

func TestAnthropicBetaTokensContains_MultiTokenComma(t *testing.T) {
	header := "oauth-2025-04-20,context-management-2025-06-27,interleaved-thinking-2025-05-14"
	require.True(t, anthropicBetaTokensContains(header, "context-management-2025-06-27"))
	require.True(t, anthropicBetaTokensContains(header, "oauth-2025-04-20"))
	require.False(t, anthropicBetaTokensContains(header, "fast-mode-2026-02-01"))
}

func TestAnthropicBetaTokensContains_ToleratesWhitespace(t *testing.T) {
	header := "oauth-2025-04-20 , context-management-2025-06-27 ,  interleaved-thinking-2025-05-14"
	require.True(t, anthropicBetaTokensContains(header, "context-management-2025-06-27"))
}

func TestAnthropicBetaTokensContains_SubstringNotMatched(t *testing.T) {
	// 严格 token 比较，不应被子串误匹配
	require.False(t, anthropicBetaTokensContains("context-management-2025-06-27-rev2", "context-management-2025-06-27"),
		"必须按 token 边界匹配，不允许 prefix 子串误命中")
}

// ============================================================================
// sanitizeAnthropicBodyForBetaTokens
// ============================================================================

func TestSanitizeAnthropicBodyForBetaTokens_NoFieldNoChange(t *testing.T) {
	body := []byte(`{"model":"claude-haiku-4-5","messages":[]}`)
	out, changed := sanitizeAnthropicBodyForBetaTokens(body, "oauth-2025-04-20")
	require.False(t, changed)
	require.Equal(t, string(body), string(out))
}

func TestSanitizeAnthropicBodyForBetaTokens_FieldKeptWhenBetaPresent(t *testing.T) {
	body := []byte(`{"model":"claude-opus-4-7","context_management":{"edits":[{"type":"clear_thinking_20251015"}]},"messages":[]}`)
	out, changed := sanitizeAnthropicBodyForBetaTokens(body,
		"oauth-2025-04-20,context-management-2025-06-27,interleaved-thinking-2025-05-14")
	require.False(t, changed)
	require.True(t, gjson.GetBytes(out, "context_management").Exists())
	require.Equal(t, "clear_thinking_20251015",
		gjson.GetBytes(out, "context_management.edits.0.type").String())
}

func TestSanitizeAnthropicBodyForBetaTokens_FieldStrippedWhenBetaMissing(t *testing.T) {
	body := []byte(`{"model":"claude-haiku-4-5","context_management":{"edits":[{"type":"clear_thinking_20251015"}]},"messages":[]}`)
	out, changed := sanitizeAnthropicBodyForBetaTokens(body, "oauth-2025-04-20,interleaved-thinking-2025-05-14")
	require.True(t, changed)
	require.False(t, gjson.GetBytes(out, "context_management").Exists(),
		"header 不含 context-management beta 时必须 strip 同名字段")
}

func TestSanitizeAnthropicBodyForBetaTokens_FieldStrippedWhenBetaEmpty(t *testing.T) {
	body := []byte(`{"context_management":{"edits":[]},"messages":[]}`)
	out, changed := sanitizeAnthropicBodyForBetaTokens(body, "")
	require.True(t, changed)
	require.False(t, gjson.GetBytes(out, "context_management").Exists())
}

func TestSanitizeAnthropicBodyForBetaTokens_EmptyBody(t *testing.T) {
	out, changed := sanitizeAnthropicBodyForBetaTokens([]byte{}, "")
	require.False(t, changed)
	require.Empty(t, out)

	out, changed = sanitizeAnthropicBodyForBetaTokens(nil, "")
	require.False(t, changed)
	require.Empty(t, out)
}

// ★ 关键回归断言：能力维度 sanitize 解决了 "真 CC + haiku" 路径的过度删除问题。
// 真实 Claude Code CLI 2.1.87+ 客户端 header 含 context-management beta；
// 即使 model 是 haiku，sanitize 也不应剥离功能字段。
func TestSanitizeAnthropicBodyForBetaTokens_HaikuRealCCClientPreservesField(t *testing.T) {
	body := []byte(`{"model":"claude-haiku-4-5","context_management":{"edits":[{"type":"clear_thinking_20251015","keep":"all"}]},"messages":[]}`)
	// 真 Claude Code CLI 2.1.87+ 客户端 header 含 context-management beta
	clientBeta := "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27"
	out, changed := sanitizeAnthropicBodyForBetaTokens(body, clientBeta)
	require.False(t, changed,
		"真 CC 客户端 header 含 context-management beta 时，haiku body 字段必须保留（功能不丢）")
	require.True(t, gjson.GetBytes(out, "context_management").Exists())
}

// ============================================================================
// computeFinalAnthropicBeta — 关键路径
// ============================================================================

func newTestGatewayServiceForBeta(injectBetaForAPIKey bool) *GatewayService {
	cfg := &config.Config{}
	cfg.Gateway.InjectBetaForAPIKey = injectBetaForAPIKey
	return &GatewayService{cfg: cfg}
}

func TestComputeFinalAnthropicBeta_APIKey_PassesClientBetaThroughDropSet(t *testing.T) {
	s := newTestGatewayServiceForBeta(false)
	hdr := http.Header{}
	hdr.Set("anthropic-beta", "oauth-2025-04-20,custom-beta")
	final, ok := s.computeFinalAnthropicBeta("apikey", "claude-sonnet-4-6", hdr, []byte(`{}`), nil)
	require.True(t, ok)
	require.True(t, anthropicBetaTokensContains(final, "oauth-2025-04-20"))
	require.True(t, anthropicBetaTokensContains(final, "custom-beta"))
}

func TestComputeFinalAnthropicBeta_APIKey_NoClientBetaInjectOff_ShouldNotSet(t *testing.T) {
	s := newTestGatewayServiceForBeta(false)
	final, ok := s.computeFinalAnthropicBeta("apikey", "claude-sonnet-4-6", http.Header{}, []byte(`{}`), nil)
	require.False(t, ok, "API-key + 客户端未传 + InjectBetaForAPIKey 关 → 不应主动设置 anthropic-beta")
	require.Equal(t, "", final)
}

// ============================================================================
// computeFinalCountTokensAnthropicBeta
// ============================================================================

func newAnthropicAPIKeyPassthroughAccountForBetaTest() *Account {
	return &Account{
		ID:       501,
		Name:     "anthropic-apikey-passthrough-ctxmgmt-test",
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "upstream-key",
		},
		Extra:       map[string]any{"anthropic_passthrough": true},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func readUpstreamBodyForTest(t *testing.T, req *http.Request) []byte {
	t.Helper()
	require.NotNil(t, req.Body)
	b, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	return b
}

func TestBuildUpstreamRequestAnthropicAPIKeyPassthrough_StripsContextManagementWhenClientHeaderMissingBeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	// 客户端仅带 oauth beta，不带 context-management-2025-06-27
	c.Request.Header.Set("Anthropic-Beta", "oauth-2025-04-20")

	body := []byte(`{"model":"claude-haiku-4-5","context_management":{"edits":[{"type":"clear_thinking_20251015"}]},"messages":[]}`)
	svc := &GatewayService{cfg: &config.Config{}}
	req, _, err := svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(
		context.Background(), c, newAnthropicAPIKeyPassthroughAccountForBetaTest(), body, "token",
	)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(readUpstreamBodyForTest(t, req), "context_management").Exists(),
		"API-key passthrough + 客户端未带 context-management beta → strip body 字段")
}

func TestBuildUpstreamRequestAnthropicAPIKeyPassthrough_PreservesContextManagementWhenClientHeaderHasBeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("Anthropic-Beta", "oauth-2025-04-20,context-management-2025-06-27")

	body := []byte(`{"model":"claude-haiku-4-5","context_management":{"edits":[{"type":"clear_thinking_20251015"}]},"messages":[]}`)
	svc := &GatewayService{cfg: &config.Config{}}
	req, _, err := svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(
		context.Background(), c, newAnthropicAPIKeyPassthroughAccountForBetaTest(), body, "token",
	)
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(readUpstreamBodyForTest(t, req), "context_management").Exists(),
		"API-key passthrough + 客户端带 context-management beta → 字段保留（不过度删除）")
}

func TestBuildCountTokensRequestAnthropicAPIKeyPassthrough_StripsContextManagementWhenClientHeaderMissingBeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	c.Request.Header.Set("Anthropic-Beta", "oauth-2025-04-20,token-counting-2024-11-01")

	body := []byte(`{"model":"claude-haiku-4-5","context_management":{"edits":[]},"messages":[]}`)
	svc := &GatewayService{cfg: &config.Config{}}
	req, err := svc.buildCountTokensRequestAnthropicAPIKeyPassthrough(
		context.Background(), c, newAnthropicAPIKeyPassthroughAccountForBetaTest(), body, "token",
	)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(readUpstreamBodyForTest(t, req), "context_management").Exists(),
		"count_tokens passthrough + 客户端未带 context-management beta → strip")
}

// ============================================================================
// 集成测试：buildUpstreamRequest
// 全路径验证上游 outgoing body 与 anthropic-beta header 严格对称。
// 这个测试能挡住未来某人忘调 sanitize / 将 sanitize 挪到 CCH 之后 等 regression。
// ============================================================================

func TestBuildCountTokensRequest_APIKeyHaiku_StripsContextManagementEndToEnd(t *testing.T) {
	// API-key + haiku + 客户端 header 不带 context-management beta → final beta 不含 → strip
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	c.Request.Header.Set("Anthropic-Beta", "interleaved-thinking-2025-05-14")

	account := &Account{ID: 412, Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-ant-xxx"},
		Status:      StatusActive, Schedulable: true,
	}
	body := []byte(`{"model":"claude-haiku-4-5","context_management":{"edits":[]},"messages":[]}`)
	svc := &GatewayService{cfg: &config.Config{}}
	req, _, err := svc.buildCountTokensRequest(
		context.Background(), c, account, body,
		"sk-ant-xxx", "apikey", "claude-haiku-4-5",
	)
	require.NoError(t, err)

	outBody := readUpstreamBodyForTest(t, req)
	require.False(t, gjson.GetBytes(outBody, "context_management").Exists(),
		"count_tokens API-key + 客户端未带 beta token → body strip")
}

// count_tokens passthrough preserve 测试
func TestBuildCountTokensRequestAnthropicAPIKeyPassthrough_PreservesContextManagementWhenClientHeaderHasBeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	c.Request.Header.Set("Anthropic-Beta", "oauth-2025-04-20,context-management-2025-06-27,token-counting-2024-11-01")

	body := []byte(`{"model":"claude-haiku-4-5","context_management":{"edits":[{"type":"clear_thinking_20251015"}]},"messages":[]}`)
	svc := &GatewayService{cfg: &config.Config{}}
	req, err := svc.buildCountTokensRequestAnthropicAPIKeyPassthrough(
		context.Background(), c, newAnthropicAPIKeyPassthroughAccountForBetaTest(), body, "token",
	)
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(readUpstreamBodyForTest(t, req), "context_management").Exists(),
		"count_tokens passthrough + 客户端带 context-management beta → 字段保留")
}

func TestBuildUpstreamRequest_APIKeyHaikuWithContextManagement_StripsField(t *testing.T) {
	// API-key + haiku + body 带 context_management + 客户端 header 未带 context-management beta
	// → final beta 不含 → body 字段被 strip
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("Anthropic-Beta", "interleaved-thinking-2025-05-14")

	account := &Account{ID: 404, Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-ant-xxx"},
		Status:      StatusActive, Schedulable: true,
	}
	body := []byte(`{"model":"claude-haiku-4-5","context_management":{"edits":[]},"messages":[]}`)
	svc := &GatewayService{cfg: &config.Config{}}
	req, _, err := svc.buildUpstreamRequest(
		context.Background(), c, account, body,
		"sk-ant-xxx", "apikey", "claude-haiku-4-5", false,
	)
	require.NoError(t, err)

	outBody := readUpstreamBodyForTest(t, req)
	require.False(t, gjson.GetBytes(outBody, "context_management").Exists(),
		"API-key + haiku + 客户端未带 beta token → body 字段必须被 strip")
}
