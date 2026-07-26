//go:build unit

package service

import (
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/stretchr/testify/require"
)

func testProviderSettings() ProviderSettings {
	return ProviderSettings{
		PortalEnabled: true,
		HostingTypes: []ProviderHostingType{
			{GroupID: 11, Label: "稳健型", Description: "寿命最长", Enabled: true, Sort: 1},
			{GroupID: 12, Label: "直连型", Description: "吞吐最大", Enabled: true, Sort: 2},
			{GroupID: 13, Label: "停用的", Enabled: false, Sort: 3},
		},
		DefaultGroupID:     11,
		CapacityTiers:      DefaultProviderCapacityTiers(),
		DefaultTier:        "3",
		CustomTierEnabled:  true,
		CustomTierCaps:     DefaultProviderCustomTierCaps(),
		SettlementTimezone: DefaultProviderSettlementTimezone,
	}
}

// provider 链路必须彻底排除 Chrome OAuth。这不是 UI 隐藏，是 service 层硬约束：
// 空值（即默认 claude_code）放行，任何其它 profile 一律拒绝。
func TestAssertProviderOAuthClientAllowed(t *testing.T) {
	require.NoError(t, AssertProviderOAuthClientAllowed(""))
	require.NoError(t, AssertProviderOAuthClientAllowed(oauth.OAuthClientClaudeCode))

	for _, forbidden := range []string{
		oauth.OAuthClientClaudeChrome,
		"CLAUDE_CHROME",
		"  claude_chrome  ",
		"something_else",
	} {
		err := AssertProviderOAuthClientAllowed(forbidden)
		require.Error(t, err, "oauth client %q must be rejected", forbidden)
		require.True(t, infraerrors.IsBadRequest(err))
	}
}

func TestResolveProviderTierFixed(t *testing.T) {
	settings := testProviderSettings()

	resolved, err := ResolveProviderTier(settings, "1", nil)
	require.NoError(t, err)
	require.Equal(t, "1", resolved.Tier)
	require.Equal(t, 1, resolved.Concurrency)
	require.Equal(t, 1, resolved.MaxSessions)
	require.Equal(t, 10, resolved.BaseRPM)

	// 空档位回落默认档。
	resolved, err = ResolveProviderTier(settings, "", nil)
	require.NoError(t, err)
	require.Equal(t, "3", resolved.Tier)
}

func TestResolveProviderTierRejectsUnknownAndDisabled(t *testing.T) {
	settings := testProviderSettings()
	settings.CapacityTiers[4].Enabled = false // 停用 5 档

	_, err := ResolveProviderTier(settings, "5", nil)
	require.Error(t, err)
	require.True(t, infraerrors.IsBadRequest(err))

	_, err = ResolveProviderTier(settings, "99", nil)
	require.Error(t, err)
}

// 自定义档必须受护栏约束，否则供号商可以给自己开出远超账号承载能力的并发。
func TestResolveProviderTierCustomRespectsCaps(t *testing.T) {
	settings := testProviderSettings()
	caps := settings.CustomTierCaps

	ok, err := ResolveProviderTier(settings, ProviderTierCustom, &ProviderCustomTierInput{
		Concurrency: caps.Concurrency, MaxSessions: caps.MaxSessions,
		BaseRPM: caps.BaseRPM, WindowCostLimit: caps.WindowCostLimit,
	})
	require.NoError(t, err)
	require.Equal(t, ProviderTierCustom, ok.Tier)

	overLimits := []ProviderCustomTierInput{
		{Concurrency: caps.Concurrency + 1, MaxSessions: 1, BaseRPM: 1, WindowCostLimit: 1},
		{Concurrency: 1, MaxSessions: caps.MaxSessions + 1, BaseRPM: 1, WindowCostLimit: 1},
		{Concurrency: 1, MaxSessions: 1, BaseRPM: caps.BaseRPM + 1, WindowCostLimit: 1},
		{Concurrency: 1, MaxSessions: 1, BaseRPM: 1, WindowCostLimit: caps.WindowCostLimit + 1},
	}
	for i := range overLimits {
		_, err := ResolveProviderTier(settings, ProviderTierCustom, &overLimits[i])
		require.Error(t, err, "case %d must be rejected", i)
		require.True(t, infraerrors.IsBadRequest(err))
	}

	settings.CustomTierEnabled = false
	_, err = ResolveProviderTier(settings, ProviderTierCustom, &ProviderCustomTierInput{Concurrency: 1})
	require.Error(t, err)
}

// 0 在运行时表示「不启用该限制」（见 Account.GetMaxSessions / GetBaseRPM /
// GetWindowCostLimit），所以自定义档必须拒绝 0，否则供号商填 0 就绕过了全部护栏，
// 拿到无限会话、无限 RPM 与无限窗口额度。
func TestResolveProviderTierCustomRejectsZeroAsUnlimited(t *testing.T) {
	settings := testProviderSettings()
	valid := ProviderCustomTierInput{Concurrency: 2, MaxSessions: 2, BaseRPM: 20, WindowCostLimit: 40}

	zeroCases := map[string]ProviderCustomTierInput{
		"concurrency":       {Concurrency: 0, MaxSessions: 2, BaseRPM: 20, WindowCostLimit: 40},
		"max_sessions":      {Concurrency: 2, MaxSessions: 0, BaseRPM: 20, WindowCostLimit: 40},
		"base_rpm":          {Concurrency: 2, MaxSessions: 2, BaseRPM: 0, WindowCostLimit: 40},
		"window_cost_limit": {Concurrency: 2, MaxSessions: 2, BaseRPM: 20, WindowCostLimit: 0},
		"all zero":          {},
	}
	for name := range zeroCases {
		input := zeroCases[name]
		_, err := ResolveProviderTier(settings, ProviderTierCustom, &input)
		require.Error(t, err, "%s = 0 must be rejected (0 means unlimited at runtime)", name)
		require.True(t, infraerrors.IsBadRequest(err))
	}

	// 合法输入仍然通过，确认上面拒的是 0 而不是把整条路径堵死。
	resolved, err := ResolveProviderTier(settings, ProviderTierCustom, &valid)
	require.NoError(t, err)
	require.Equal(t, 2, resolved.MaxSessions)
	require.Equal(t, 20, resolved.BaseRPM)
	require.Equal(t, 40.0, resolved.WindowCostLimit)
	// rpm_strategy 属于平台调度策略，不接受供号商输入，解析结果里恒为空。
	require.Equal(t, "", resolved.RPMStrategy)
}

// 固定档由管理员配置，允许用 0 表达「不限」（种子值里 5 档的窗口上限就是 0）。
// 这与自定义档拒绝 0 的规则并不冲突：前者是平台自己的决定，后者是外部输入。
func TestFixedTierMayUseZeroAsUnlimited(t *testing.T) {
	settings := testProviderSettings()
	resolved, err := ResolveProviderTier(settings, "5", nil)
	require.NoError(t, err)
	require.Equal(t, 0.0, resolved.WindowCostLimit)
}

// persona 属于平台内部处理方式，供号商既不能设置也不该察觉。
// extra 走白名单过滤，任何未列入的键都会被丢弃。
func TestSanitizeProviderExtraDropsPersonaAndUnknownKeys(t *testing.T) {
	kept, dropped := SanitizeProviderExtra(map[string]any{
		"max_sessions":               5,
		"base_rpm":                   30,
		"window_cost_limit":          60,
		"persona_enabled":            true,
		"persona_timezone":           "America/New_York",
		"enable_tls_fingerprint":     false,
		"session_id_masking_enabled": false,
		"privacy_mode":               "off",
		"quota_daily_limit":          100,
	})

	require.Equal(t, map[string]any{
		"max_sessions":      5,
		"base_rpm":          30,
		"window_cost_limit": 60,
	}, kept)
	require.False(t, ContainsPersonaKey(kept))
	require.Subset(t, dropped, []string{
		"persona_enabled", "persona_timezone",
		"enable_tls_fingerprint", "session_id_masking_enabled",
		"privacy_mode", "quota_daily_limit",
	})
}

// 三个伪装项强制开启，且供号商提交的相反取值必须被覆盖。
// 注意 intercept_warmup_requests 在 credentials，另外两个在 extra。
func TestProviderForcedMimicryFlags(t *testing.T) {
	extra := ApplyProviderForcedExtra(map[string]any{"enable_tls_fingerprint": false})
	require.Equal(t, true, extra["enable_tls_fingerprint"])
	require.Equal(t, true, extra["session_id_masking_enabled"])

	creds := ApplyProviderForcedCredentials(map[string]any{"intercept_warmup_requests": false})
	require.Equal(t, true, creds["intercept_warmup_requests"])
	// extra 里不该出现 credentials 的键，反之亦然。
	require.NotContains(t, extra, "intercept_warmup_requests")
	require.NotContains(t, creds, "enable_tls_fingerprint")
}

// 「应用到存量」只增量合并档位相关的键，绝不整体覆盖 extra，
// 否则会清掉 persona_*、window_cost_sticky_reserve 等另行配置的持久设置。
func TestApplyProviderTierToExtraPreservesOtherKeys(t *testing.T) {
	existing := map[string]any{
		"persona_enabled":            true,
		"persona_timezone":           "Asia/Tokyo",
		"window_cost_sticky_reserve": 10,
		"quota_daily_limit":          500,
		"privacy_mode":               "on",
		"max_sessions":               1,
		"base_rpm":                   10,
	}
	merged := ApplyProviderTierToExtra(existing, ResolvedProviderTier{
		Tier: "4", Concurrency: 5, MaxSessions: 5, BaseRPM: 50, WindowCostLimit: 100,
	})

	require.Equal(t, 5, merged["max_sessions"])
	require.Equal(t, 50, merged["base_rpm"])
	require.Equal(t, 100.0, merged["window_cost_limit"])

	require.Equal(t, true, merged["persona_enabled"], "persona must survive tier backfill")
	require.Equal(t, "Asia/Tokyo", merged["persona_timezone"])
	require.Equal(t, 10, merged["window_cost_sticky_reserve"])
	require.Equal(t, 500, merged["quota_daily_limit"])
	require.Equal(t, "on", merged["privacy_mode"])

	// 不修改入参。
	require.Equal(t, 1, existing["max_sessions"])
}

func TestValidateProviderProxy(t *testing.T) {
	ok, err := ValidateProviderProxy(ProviderProxyInput{Protocol: "HTTP", Host: " proxy.example.com ", Port: 8080})
	require.NoError(t, err)
	require.Equal(t, "http", ok.Protocol)
	require.Equal(t, "proxy.example.com", ok.Host)

	bad := []ProviderProxyInput{
		{Protocol: "ftp", Host: "h", Port: 1},
		{Protocol: "", Host: "h", Port: 1},
		{Protocol: "http", Host: "", Port: 1},
		{Protocol: "http", Host: "http://h", Port: 1},
		{Protocol: "http", Host: "h:8080", Port: 1},
		{Protocol: "http", Host: "user@h", Port: 1},
		{Protocol: "http", Host: "h", Port: 0},
		{Protocol: "http", Host: "h", Port: 70000},
	}
	for i := range bad {
		_, err := ValidateProviderProxy(bad[i])
		require.Error(t, err, "case %d must be rejected", i)
	}
}

// 身份字段必须从 credentials 复刻进 extra，否则伪装静默失效。
func TestMirrorProviderIdentityToExtra(t *testing.T) {
	creds := map[string]any{
		"access_token":  "tok",
		"account_uuid":  "acc-1",
		"org_uuid":      "org-1",
		"email_address": "a@b.com",
	}

	got := MirrorProviderIdentityToExtra(map[string]any{"max_sessions": 3}, creds)
	require.Equal(t, "acc-1", got["account_uuid"])
	require.Equal(t, "org-1", got["org_uuid"])
	require.Equal(t, "a@b.com", got["email_address"])
	require.Equal(t, 3, got["max_sessions"], "既有键不能被覆盖")
	// access_token 是凭据不是身份，绝不能漏进 extra。
	require.NotContains(t, got, "access_token")

	// 空值不写，避免用空串覆盖掉真实值。
	empty := MirrorProviderIdentityToExtra(nil, map[string]any{"account_uuid": "   "})
	require.NotContains(t, empty, "account_uuid")

	// extra 已有值时保留，不被 credentials 覆盖。
	kept := MirrorProviderIdentityToExtra(map[string]any{"account_uuid": "existing"}, creds)
	require.Equal(t, "existing", kept["account_uuid"])
}

// 重新授权换到另一个 Anthropic 账号时，extra 里的旧身份必须被替换而不是保留。
// 用错身份比没有身份更糟。
func TestRefreshProviderIdentityExtraReplacesStaleUUID(t *testing.T) {
	stale := map[string]any{
		"account_uuid":      "old-uuid",
		"org_uuid":          "old-org",
		"max_sessions":      5,
		"enable_tls_finger": true,
		"window_cost_limit": 20.0,
	}
	newCreds := map[string]any{"account_uuid": "new-uuid", "org_uuid": "new-org"}

	refreshed := MirrorProviderIdentityToExtra(
		RefreshProviderIdentityExtra(stale, newCreds), newCreds)

	require.Equal(t, "new-uuid", refreshed["account_uuid"])
	require.Equal(t, "new-org", refreshed["org_uuid"])
	// 档位与伪装开关不能被这一步带走。
	require.Equal(t, 5, refreshed["max_sessions"])
	require.Equal(t, true, refreshed["enable_tls_finger"])
	require.Equal(t, 20.0, refreshed["window_cost_limit"])

	// credentials 里没有的键不动，避免把仍然有效的旧值清掉。
	partial := MirrorProviderIdentityToExtra(
		RefreshProviderIdentityExtra(stale, map[string]any{"account_uuid": "only-acc"}),
		map[string]any{"account_uuid": "only-acc"})
	require.Equal(t, "only-acc", partial["account_uuid"])
	require.Equal(t, "old-org", partial["org_uuid"])
}

// 上号入口必须校验托管类型在已启用白名单内，并把归属与档位写进账号。
func TestBuildProviderAccountInput(t *testing.T) {
	settings := testProviderSettings()

	input, err := BuildProviderAccountInput(settings, ProviderOnboardInput{
		ProviderUserID: 42,
		Name:           "acct-1",
		AccountType:    AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "tok",
			"account_uuid":  "acc-uuid-1",
			"org_uuid":      "org-uuid-1",
			"email_address": "seller@example.com",
		},
		Extra:       map[string]any{"persona_enabled": true},
		HostingType: 12,
		Tier:        "2",
	}, 77)
	require.NoError(t, err)

	require.Equal(t, PlatformAnthropic, input.Platform)
	require.Equal(t, AccountTypeOAuth, input.Type)
	require.Equal(t, []int64{12}, input.GroupIDs)
	require.True(t, input.SkipDefaultGroupBind, "hosting type must fully determine the group")
	require.NotNil(t, input.ProviderUserID)
	require.Equal(t, int64(42), *input.ProviderUserID)
	require.NotNil(t, input.ProviderTier)
	require.Equal(t, "2", *input.ProviderTier)
	require.Equal(t, 2, input.Concurrency)

	require.False(t, ContainsPersonaKey(input.Extra), "persona keys must never reach the account")
	require.Equal(t, true, input.Extra["enable_tls_fingerprint"])
	require.Equal(t, true, input.Credentials["intercept_warmup_requests"])

	// 身份字段必须同时出现在 extra 里。网关走的是 GetExtraString("account_uuid")
	// （gateway_upstream_request.go），并且以非空为硬前提——为空时
	// RewriteUserIDWithMasking 整段跳过，会话 ID 伪装开关为 true 也不会执行，
	// 且 metadata.user_id 会少一段账号 UUID。只写 credentials 等于伪装静默失效。
	require.Equal(t, "acc-uuid-1", input.Extra["account_uuid"],
		"gateway reads account_uuid from extra, not credentials")
	require.Equal(t, "org-uuid-1", input.Extra["org_uuid"])
	require.Equal(t, "seller@example.com", input.Extra["email_address"])

	// Priority 必须显式给值。createAccountRecord 无条件 SetPriority，留零值不会回落
	// schema 的 default(50)，而是真写 0；调度里 priority 是硬门槛
	// （filterByMinPriority 只保留数值最小的那批），0 会让供号商账号把自有账号
	// 完全挤出候选。
	require.Equal(t, DefaultProviderAccountPriority, input.Priority)
	require.NotZero(t, input.Priority, "zero priority would starve every in-house account")

	// 种子值必须与管理端新建账号表单的默认值一致。两者不同的话，混合分组里
	// 数值大的一边会一个请求都拿不到，而且完全没有报错——供号商只会看到
	// 「账号正常但用量恒为 0」。
	require.Equal(t, 1, DefaultProviderAccountPriority,
		"must match the admin account form default (CreateAccountModal.vue priority: 1)")

	// 设置里的非法值（含 0）必须回落种子值，绝不能原样写进账号。
	require.Equal(t, DefaultProviderAccountPriority,
		resolveProviderAccountPriority(ProviderSettings{AccountPriority: 0}))
	require.Equal(t, DefaultProviderAccountPriority,
		resolveProviderAccountPriority(ProviderSettings{AccountPriority: -5}))
	require.Equal(t, DefaultProviderAccountPriority,
		resolveProviderAccountPriority(ProviderSettings{AccountPriority: 9999}))
	require.Equal(t, 7, resolveProviderAccountPriority(ProviderSettings{AccountPriority: 7}))

	// 未启用的托管类型必须拒绝。
	_, err = BuildProviderAccountInput(settings, ProviderOnboardInput{
		ProviderUserID: 42, Name: "x", AccountType: AccountTypeOAuth, HostingType: 13, Tier: "1",
	}, 77)
	require.Error(t, err)

	// 只允许 oauth / setup-token。
	_, err = BuildProviderAccountInput(settings, ProviderOnboardInput{
		ProviderUserID: 42, Name: "x", AccountType: AccountTypeAPIKey, HostingType: 11, Tier: "1",
	}, 77)
	require.Error(t, err)
}

func TestValidateProviderSettings(t *testing.T) {
	t.Run("accepts a well formed config", func(t *testing.T) {
		out, err := ValidateProviderSettings(testProviderSettings())
		require.NoError(t, err)
		require.Len(t, out.EnabledHostingTypes(), 2)
	})

	t.Run("requires an enabled tier", func(t *testing.T) {
		s := testProviderSettings()
		for i := range s.CapacityTiers {
			s.CapacityTiers[i].Enabled = false
		}
		_, err := ValidateProviderSettings(s)
		require.Error(t, err)
	})

	t.Run("default tier must be enabled", func(t *testing.T) {
		s := testProviderSettings()
		s.DefaultTier = "5"
		s.CapacityTiers[4].Enabled = false
		_, err := ValidateProviderSettings(s)
		require.Error(t, err)
	})

	// 并发 0 在 ConcurrencyService.AcquireAccountSlot 里等于「不限并发」，
	// 而不是「没有容量」。允许管理员填 0 会让本该最小的档位悄悄变成最大。
	t.Run("rejects zero concurrency because zero means unlimited", func(t *testing.T) {
		s := testProviderSettings()
		s.CapacityTiers[0].Concurrency = 0
		_, err := ValidateProviderSettings(s)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unlimited",
			"错误信息必须说明 0 的真实含义，否则管理员会以为是范围写错了")
	})

	// 其余三项的 0 是既有约定：表示不启用该限制。不要顺手一起改掉。
	t.Run("still allows zero for the limits where zero means disabled", func(t *testing.T) {
		s := testProviderSettings()
		s.CapacityTiers[0].MaxSessions = 0
		s.CapacityTiers[0].BaseRPM = 0
		s.CapacityTiers[0].WindowCostLimit = 0
		_, err := ValidateProviderSettings(s)
		require.NoError(t, err)
	})

	t.Run("open portal requires an enabled default hosting type", func(t *testing.T) {
		s := testProviderSettings()
		s.DefaultGroupID = 13 // 停用的分组
		_, err := ValidateProviderSettings(s)
		require.Error(t, err)
	})

	t.Run("enabled hosting type needs a public label", func(t *testing.T) {
		s := testProviderSettings()
		s.HostingTypes[0].Label = "  "
		_, err := ValidateProviderSettings(s)
		require.Error(t, err)
	})

	t.Run("rejects an unknown timezone", func(t *testing.T) {
		s := testProviderSettings()
		s.SettlementTimezone = "Not/AZone"
		_, err := ValidateProviderSettings(s)
		require.Error(t, err)
	})

	t.Run("rejects out of range tier numbers", func(t *testing.T) {
		s := testProviderSettings()
		s.CapacityTiers[0].Concurrency = providerMaxConcurrency + 1
		_, err := ValidateProviderSettings(s)
		require.Error(t, err)
	})
}

// 供号商侧接口只能看到 label，绝不能拿到底层策略。
func TestProviderLabelHelpers(t *testing.T) {
	settings := testProviderSettings()
	require.Equal(t, "稳健型", ProviderHostingLabel(settings, []int64{11}))
	require.Equal(t, "直连型", ProviderHostingLabel(settings, []int64{99, 12}))
	require.Equal(t, "", ProviderHostingLabel(settings, []int64{404}))

	tier := "2"
	require.Equal(t, "2 档", ProviderTierLabel(settings, &tier))
	custom := ProviderTierCustom
	require.Equal(t, "自定义", ProviderTierLabel(settings, &custom))
	require.Equal(t, "", ProviderTierLabel(settings, nil))
}

// 内部调度细节（限流、过载、临时不可调度）不下发给供号商，统一压成四态。
func TestProviderAccountDisplayStatus(t *testing.T) {
	require.Equal(t, "unknown", ProviderAccountDisplayStatus(nil))
	require.Equal(t, "active", ProviderAccountDisplayStatus(&Account{Status: StatusActive, Schedulable: true}))
	require.Equal(t, "paused", ProviderAccountDisplayStatus(&Account{Status: StatusActive, Schedulable: false}))
	require.Equal(t, "error", ProviderAccountDisplayStatus(&Account{Status: StatusError, Schedulable: true}))
}
