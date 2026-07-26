package service

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
)

// ProviderProxyName 生成供号商代理的记录名，带归属标记，避免污染管理员代理池视图。
func ProviderProxyName(providerUserID int64, host string, port int) string {
	return fmt.Sprintf("provider-%d-%s:%d", providerUserID, host, port)
}

// 供号商上号。
//
// 与管理员上号最重要的两点差别：
//  1. 彻底排除 Claude for Chrome OAuth。这里是 service 层硬约束而非 UI 隐藏：
//     构造 CookieAuthInput 时永不设置 OAuthClient（留空即 claude_code），
//     且入口断言拒绝任何显式传 claude_chrome 的请求。
//  2. extra 走白名单过滤，persona_* 一律丢弃。人格属于内部处理方式，
//     由管理员事后单独配置。

// ErrProviderChromeOAuthForbidden 供号商链路禁用 Chrome OAuth。
var ErrProviderChromeOAuthForbidden = infraerrors.BadRequest(
	"PROVIDER_OAUTH_CLIENT_FORBIDDEN",
	"this authorization method is not available",
)

// 供号商账号的调度优先级取自设置 provider_account_priority，
// 种子值见 DefaultProviderAccountPriority（provider_settings.go）。
//
// 必须显式赋值而不能留零值：账号创建走 createAccountRecord 的
// SetPriority(account.Priority)，无条件写入结构体里的值，
// 零值不会回落 Ent schema 的 default(50)，而是真的落库成 0。

// ProviderProxyInput 是供号商自带的代理。上号必填。
type ProviderProxyInput struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ProviderOnboardInput 是落库一个供号商账号所需的全部输入。
type ProviderOnboardInput struct {
	ProviderUserID int64
	Name           string
	Notes          *string
	// AccountType 只允许 oauth 或 setup-token。
	AccountType string
	Credentials map[string]any
	Extra       map[string]any
	Proxy       ProviderProxyInput
	HostingType int64
	Tier        string
	CustomTier  *ProviderCustomTierInput
}

// AssertProviderOAuthClientAllowed 拒绝供号商链路上的 Chrome OAuth。
//
// 空值合法（即默认 claude_code）。这是彻底排除 chrome 的最后一道闸，
// 上号入口与重新授权入口都必须调用。
func AssertProviderOAuthClientAllowed(oauthClient string) error {
	client := strings.TrimSpace(strings.ToLower(oauthClient))
	if client == "" || client == oauth.OAuthClientClaudeCode {
		return nil
	}
	return ErrProviderChromeOAuthForbidden
}

// ValidateProviderProxy 校验供号商填写的代理。
func ValidateProviderProxy(in ProviderProxyInput) (ProviderProxyInput, error) {
	out := in
	out.Protocol = strings.ToLower(strings.TrimSpace(in.Protocol))
	out.Host = strings.TrimSpace(in.Host)
	out.Username = strings.TrimSpace(in.Username)

	switch out.Protocol {
	case "http", "https", "socks5", "socks5h":
	case "":
		return out, infraerrors.BadRequest("PROXY_PROTOCOL_REQUIRED", "proxy protocol is required")
	default:
		return out, infraerrors.BadRequest("INVALID_PROXY_PROTOCOL",
			"proxy protocol must be one of http, https, socks5, socks5h")
	}
	if out.Host == "" {
		return out, infraerrors.BadRequest("PROXY_HOST_REQUIRED", "proxy host is required")
	}
	// host 允许域名或 IP，但不能夹带 scheme/端口/路径，否则拼出来的 URL 会是错的。
	if strings.ContainsAny(out.Host, " /\\@") || strings.Contains(out.Host, "://") {
		return out, infraerrors.BadRequest("INVALID_PROXY_HOST", "proxy host must not contain a scheme, path, or credentials")
	}
	if host, _, err := net.SplitHostPort(out.Host); err == nil && host != "" {
		return out, infraerrors.BadRequest("INVALID_PROXY_HOST", "proxy host must not include a port; use the port field")
	}
	if out.Port < 1 || out.Port > 65535 {
		return out, infraerrors.BadRequest("INVALID_PROXY_PORT", "proxy port must be between 1 and 65535")
	}
	return out, nil
}

// resolveProviderAccountPriority 从设置取调度优先级，非法值回落种子值。
// 绝不返回 0：0 是最高优先级，会让供号商账号独占整个分组的流量。
func resolveProviderAccountPriority(settings ProviderSettings) int {
	if settings.AccountPriority <= 0 || settings.AccountPriority > providerMaxAccountPriority {
		return DefaultProviderAccountPriority
	}
	return settings.AccountPriority
}

// BuildProviderAccountInput 把上号输入翻译成 CreateAccountInput。
//
// 顺序很重要：先过滤 extra（丢掉 persona 等），再套档位，最后写强制伪装项，
// 这样供号商无论怎么构造请求都覆盖不了强制项。
func BuildProviderAccountInput(
	settings ProviderSettings,
	in ProviderOnboardInput,
	proxyID int64,
) (*CreateAccountInput, error) {
	if in.AccountType != AccountTypeOAuth && in.AccountType != AccountTypeSetupToken {
		return nil, infraerrors.BadRequest("INVALID_ACCOUNT_TYPE",
			"account type must be either oauth or setup-token")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, infraerrors.BadRequest("NAME_REQUIRED", "account name is required")
	}

	// 托管类型必须在已启用白名单内。
	hostingID := in.HostingType
	if hostingID == 0 {
		hostingID = settings.DefaultGroupID
	}
	if _, ok := settings.FindHostingType(hostingID); !ok {
		return nil, infraerrors.BadRequest("INVALID_HOSTING_TYPE", "selected hosting type is not available")
	}

	tier, err := ResolveProviderTier(settings, in.Tier, in.CustomTier)
	if err != nil {
		return nil, err
	}

	// extra 白名单过滤 → 套档位 → 写强制伪装项。
	extra, dropped := SanitizeProviderExtra(in.Extra)
	if len(dropped) > 0 {
		slog.Info("provider onboarding dropped disallowed extra keys",
			"provider_user_id", in.ProviderUserID, "keys", dropped)
	}
	extra = ApplyProviderTierToExtra(extra, tier)
	extra = ApplyProviderForcedExtra(extra)

	// intercept_warmup_requests 在 credentials，不在 extra。
	creds := ApplyProviderForcedCredentials(in.Credentials)

	// 身份字段从 credentials 复刻进 extra。必须在白名单过滤之后：
	// 这些值来自我们自己的换票流程，不是供号商提交的，不该被丢弃。
	// 网关读的是 extra，缺了 account_uuid 会让会话 ID 伪装整段跳过（详见函数注释）。
	extra = MirrorProviderIdentityToExtra(extra, creds)

	loadFactor := tier.Concurrency
	tierName := tier.Tier

	return &CreateAccountInput{
		Name:        strings.TrimSpace(in.Name),
		Notes:       in.Notes,
		Platform:    PlatformAnthropic,
		Type:        in.AccountType,
		Credentials: creds,
		Extra:       extra,
		ProxyID:     &proxyID,
		Concurrency: tier.Concurrency,
		LoadFactor:  &loadFactor,
		// Priority 由设置控制且必须显式给值：createAccountRecord 无条件
		// SetPriority(account.Priority)，留零值不会回落 schema 默认值而是真的写 0；
		// 调度里 priority 是硬门槛（filterByMinPriority 只保留最小值那批），
		// 0 会让所有供号商账号把自有账号完全挤出候选。
		Priority: resolveProviderAccountPriority(settings),
		GroupIDs: []int64{hostingID},
		// 分组由托管类型唯一决定，绝不回落平台默认分组。
		SkipDefaultGroupBind: true,
		ProviderUserID:       &in.ProviderUserID,
		ProviderTier:         &tierName,
	}, nil
}

// ProviderTierApplyResult 是「应用到存量」的结果。
type ProviderTierApplyResult struct {
	Matched int `json:"matched"`
	Updated int `json:"updated"`
}

// ApplyTierToExistingProviderAccounts 把某档位的最新参数回填到该档现有账号。
//
// 只增量合并档位相关的 extra 键，绝不整体覆盖 extra，否则会清掉 persona_*、
// window_cost_sticky_reserve、quota_* 、privacy_mode 等另行配置的持久设置。
func (s *adminServiceImpl) ApplyTierToExistingProviderAccounts(
	ctx context.Context,
	tier ResolvedProviderTier,
) (ProviderTierApplyResult, error) {
	result := ProviderTierApplyResult{}
	accounts, err := s.accountRepo.ListByProviderTier(ctx, tier.Tier)
	if err != nil {
		return result, err
	}
	result.Matched = len(accounts)

	for i := range accounts {
		acc := accounts[i]
		merged := ApplyProviderTierToExtra(acc.Extra, tier)
		concurrency := tier.Concurrency
		loadFactor := tier.Concurrency
		if err := s.accountRepo.UpdateProviderTierParams(ctx, acc.ID, concurrency, loadFactor, merged); err != nil {
			slog.Warn("failed to apply tier to existing provider account",
				"account_id", acc.ID, "tier", tier.Tier, "error", err)
			continue
		}
		result.Updated++
	}
	return result, nil
}

// CountProviderAccountsByTier 返回某档位当前的账号数，供设置页二次确认预览。
func (s *adminServiceImpl) CountProviderAccountsByTier(ctx context.Context, tier string) (int, error) {
	return s.accountRepo.CountByProviderTier(ctx, strings.TrimSpace(tier))
}

// ListAccountsByProvider 返回某供号商名下的账号。
func (s *adminServiceImpl) ListAccountsByProvider(ctx context.Context, providerUserID int64) ([]Account, error) {
	return s.accountRepo.ListByProvider(ctx, providerUserID)
}

// ProviderAccountDisplayStatus 把内部账号状态压成供号商看得懂的四态。
//
// 内部的 rate limit / overload / temp unschedulable 等调度细节不下发，
// 统一归入 error 或 paused，避免泄露调度实现。
func ProviderAccountDisplayStatus(a *Account) string {
	if a == nil {
		return "unknown"
	}
	switch {
	case a.Status == StatusError:
		return "error"
	case !a.Schedulable:
		return "paused"
	case a.Status != StatusActive:
		return "paused"
	default:
		return "active"
	}
}

// ProviderTierLabel 从设置里找档位显示名，找不到就回落档位标识本身。
func ProviderTierLabel(settings ProviderSettings, tier *string) string {
	if tier == nil {
		return ""
	}
	name := strings.TrimSpace(*tier)
	if name == "" {
		return ""
	}
	if name == ProviderTierCustom {
		return "自定义"
	}
	for _, t := range settings.CapacityTiers {
		if t.Tier == name {
			return t.Label
		}
	}
	return name
}

// ProviderHostingLabel 从设置里找托管类型显示名。
// 供号商只应看到 label，绝不返回底层策略。
func ProviderHostingLabel(settings ProviderSettings, groupIDs []int64) string {
	for _, gid := range groupIDs {
		for _, ht := range settings.HostingTypes {
			if ht.GroupID == gid {
				return ht.Label
			}
		}
	}
	return ""
}
