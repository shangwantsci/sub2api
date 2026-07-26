//go:build unit

package provider

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// 供号商侧的账号视图是整套功能的脱敏边界。这个测试盯的是「平台对账号做了什么处理」
// 这类信息绝不能出现在响应里：凭据、代理、人格、TLS 指纹、调度参数、分组策略。
//
// 用序列化后的 JSON 全文做断言而不是逐字段比对，是为了让将来给 service.Account
// 加字段、或有人图省事改回复用 dto.Account 时，这里能立刻炸掉。
func TestAccountViewNeverLeaksInternalProcessing(t *testing.T) {
	now := time.Now()
	tier := "3"
	notes := "supplier note"
	proxyID := int64(77)
	providerID := int64(42)
	rateMultiplier := 1.25
	loadFactor := 8
	poolWeight := 3

	account := &service.Account{
		ID:       1,
		Name:     "supplier-account",
		Notes:    &notes,
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":              "SECRET-ACCESS-TOKEN",
			"refresh_token":             "SECRET-REFRESH-TOKEN",
			"session_key":               "SECRET-SESSION-KEY",
			"oauth_client":              "claude_chrome",
			"email_address":             "supplier@example.com",
			"intercept_warmup_requests": true,
		},
		Extra: map[string]any{
			"persona_enabled":            true,
			"persona_timezone":           "America/New_York",
			"enable_tls_fingerprint":     true,
			"session_id_masking_enabled": true,
			"base_rpm":                   30,
			"window_cost_limit":          60,
			"privacy_mode":               "on",
		},
		ProxyID:        &proxyID,
		Concurrency:    3,
		Priority:       50,
		RateMultiplier: &rateMultiplier,
		LoadFactor:     &loadFactor,
		PoolWeight:     &poolWeight,
		Status:         service.StatusActive,
		ErrorMessage:   "upstream 429 from proxy 23",
		Schedulable:    true,
		CreatedAt:      now,
		GroupIDs:       []int64{11},
		ProviderUserID: &providerID,
		ProviderTier:   &tier,
	}

	settings := service.ProviderSettings{
		HostingTypes: []service.ProviderHostingType{
			{GroupID: 11, Label: "稳健型", Description: "寿命最长", Enabled: true},
		},
		CapacityTiers: service.DefaultProviderCapacityTiers(),
	}

	view := AccountViewFromService(account, settings, &service.ProviderPeriodTotals{
		Requests: 10, Tokens: 2000,
		StandardCost: decimal.RequireFromString("1.2345678901"),
	})

	raw, err := json.Marshal(view)
	require.NoError(t, err)
	payload := string(raw)

	// 凭据与授权方式
	forbiddenSubstrings := []string{
		"SECRET-ACCESS-TOKEN", "SECRET-REFRESH-TOKEN", "SECRET-SESSION-KEY",
		"access_token", "refresh_token", "session_key", "credentials",
		"oauth_client", "claude_chrome",
		// 内部处理配置
		"persona", "America/New_York",
		"enable_tls_fingerprint", "session_id_masking",
		"intercept_warmup_requests", "privacy_mode",
		"extra",
		// 代理与调度
		"proxy", "load_factor", "pool_weight", "priority", "rate_multiplier",
		"concurrency", "base_rpm", "window_cost_limit",
		// 分组与内部错误细节
		"group_ids", "content_review_policy", "claude_oauth_system_prompt_policy",
		"error_message", "upstream 429",
		// 平台与账号类型属于内部实现
		"platform", "anthropic",
	}
	for _, needle := range forbiddenSubstrings {
		require.NotContains(t, payload, needle,
			"provider account view must not expose %q; payload=%s", needle, payload)
	}

	// 应当出现的字段：只有对外文案与用量。
	require.Equal(t, int64(1), view.ID)
	require.Equal(t, "supplier-account", view.Name)
	require.Equal(t, "active", view.Status)
	require.Equal(t, "稳健型", view.HostingTypeLabel)
	require.Equal(t, "3 档", view.TierLabel)
	require.Equal(t, int64(10), view.PeriodRequests)

	// 金额必须序列化成十进制字符串并保住全部有效位。序列化成 JSON 数字的话，
	// JS 侧解析成 float64 就当场丢精度，对账时两边对不上。
	require.Equal(t, "1.2345678901", view.PeriodCost.String())
	require.Contains(t, payload, `"period_cost":"1.2345678901"`,
		"amount must be a JSON string, not a number; payload=%s", payload)
}

// 账号 type 是 oauth 还是 setup-token 属于内部处理方式，视图里不能出现该字段。
func TestAccountViewOmitsAccountType(t *testing.T) {
	for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeSetupToken} {
		view := AccountViewFromService(
			&service.Account{ID: 1, Name: "a", Type: accountType, Status: service.StatusActive, Schedulable: true},
			service.ProviderSettings{},
			nil,
		)
		raw, err := json.Marshal(view)
		require.NoError(t, err)
		require.NotContains(t, strings.ToLower(string(raw)), "setup-token")
		require.NotContains(t, string(raw), `"type"`)
	}
}

// 上号选项只暴露对外文案，真实策略与倍率永不下发。
func TestOnboardOptionsOmitGroupPolicies(t *testing.T) {
	settings := service.ProviderSettings{
		HostingTypes: []service.ProviderHostingType{
			{GroupID: 11, Label: "稳健型", Description: "寿命最长", Enabled: true, Sort: 2},
			{GroupID: 12, Label: "直连型", Description: "吞吐最大", Enabled: true, Sort: 1},
			{GroupID: 13, Label: "隐藏", Enabled: false, Sort: 0},
		},
		DefaultGroupID:    11,
		CapacityTiers:     service.DefaultProviderCapacityTiers(),
		DefaultTier:       "3",
		CustomTierEnabled: true,
		CustomTierCaps:    service.DefaultProviderCustomTierCaps(),
	}

	options := OnboardOptionsFromSettings(settings)

	// 只返回已启用项，且按 sort 排序。
	require.Len(t, options.HostingTypes, 2)
	require.Equal(t, int64(12), options.HostingTypes[0].ID)
	require.Equal(t, int64(11), options.HostingTypes[1].ID)

	raw, err := json.Marshal(options)
	require.NoError(t, err)
	payload := string(raw)

	// 档位的并发/会话/RPM 是刻意展示给供号商的（上号页要让他们看清各档区别），
	// custom_tier.enabled 也是对外可见的开关。真正不能泄露的是分组的底层策略与倍率。
	for _, needle := range []string{
		"content_review_policy", "claude_oauth_system_prompt_policy",
		"rate_multiplier", "group_id", "inherit", "identity_only",
		"disabled", "sort",
	} {
		require.NotContains(t, payload, needle,
			"onboard options must not expose %q; payload=%s", needle, payload)
	}
}
