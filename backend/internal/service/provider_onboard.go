package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

// proxyNameMaxLen 与 Ent schema 里 proxies.name 的 MaxLen(100) 对齐。
const proxyNameMaxLen = 100

// ProviderProxyName 生成供号商代理的记录名，带归属标记，避免污染管理员代理池视图。
//
// 归属的权威来源是 proxies.provider_user_id 这一列，名字只是给管理员看的；
// 任何代码都不应该靠解析这个前缀来判断归属。
func ProviderProxyName(providerUserID int64, host string, port int) string {
	name := fmt.Sprintf("provider-%d-%s:%d", providerUserID, host, port)
	if len(name) <= proxyNameMaxLen {
		return name
	}
	// host 列宽 255 而 name 只有 100，长域名拼出来会直接写库失败。
	// 保留归属前缀与端口这两个有辨识度的部分，只截中间的 host。
	prefix := fmt.Sprintf("provider-%d-", providerUserID)
	suffix := fmt.Sprintf(":%d", port)
	room := proxyNameMaxLen - len(prefix) - len(suffix)
	if room <= 0 {
		return truncateUTF8(name, proxyNameMaxLen)
	}
	return prefix + truncateUTF8(host, room) + suffix
}

// ErrProviderProxyModeNotAllowed 当前站点策略不开放这种代理来源。
var ErrProviderProxyModeNotAllowed = infraerrors.BadRequest(
	"PROXY_MODE_NOT_ALLOWED", "this proxy option is not available")

// NormalizeProviderProxyPolicy 把策略值归一到三个合法取值之一，未知值回落种子值。
func NormalizeProviderProxyPolicy(policy string) string {
	switch p := strings.TrimSpace(strings.ToLower(policy)); p {
	case ProviderProxyPolicyBoth, ProviderProxyPolicyAutoOnly, ProviderProxyPolicyManualOnly:
		return p
	default:
		return DefaultProviderProxyModePolicy
	}
}

// AssertProviderProxyModeAllowed 按站点策略校验单次上号请求声明的代理来源。
//
// 与 AssertProviderOAuthClientAllowed 同理，这是 service 层硬约束而不是 UI 隐藏：
// 供号商可以直接构造请求，前端少渲染一个单选框拦不住任何人。
func AssertProviderProxyModeAllowed(policy, mode string) error {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case ProviderProxyModeAuto:
		if NormalizeProviderProxyPolicy(policy) == ProviderProxyPolicyManualOnly {
			return ErrProviderProxyModeNotAllowed
		}
		return nil
	case ProviderProxyModeManual:
		if NormalizeProviderProxyPolicy(policy) == ProviderProxyPolicyAutoOnly {
			return ErrProviderProxyModeNotAllowed
		}
		return nil
	default:
		return infraerrors.BadRequest("INVALID_PROXY_MODE", "proxy mode must be auto or manual")
	}
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
	// 代理不在这里传：它在换票之前就已经落库（或从平台池里选出），
	// 调用方只把 proxyID 作为参数交给 BuildProviderAccountInput。
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

// defaultProviderProxyProtocol 是不含协议头的粘贴格式所补的协议。
//
// 取 socks5h 而不是 socks5：它是 proxyurl.Parse 对 socks5 的升级目标，DNS 由代理端
// 解析，不会把目标域名泄漏到本机出口。供号商说的「socks5 格式」实际就是这个。
const defaultProviderProxyProtocol = "socks5h"

var (
	// ErrProviderProxyURLRequired 手填模式下没给代理串。
	ErrProviderProxyURLRequired = infraerrors.BadRequest("PROXY_URL_REQUIRED", "proxy address is required")
	// ErrProviderProxyURLInvalid 代理串不符合任何一种受支持的写法。
	//
	// 错误信息刻意不回显原串：它可能含代理密码，会进日志和前端 toast。
	ErrProviderProxyURLInvalid = infraerrors.BadRequest("INVALID_PROXY_URL",
		"proxy address must look like socks5://user:pass@host:port, host:port:user:pass, or user:pass@host:port")
)

// ParseProviderProxyURL 把供号商粘贴的一行代理串解析成结构化字段。
//
// 支持三种写法，因为供号商手上拿到的代理格式很杂，逐字段填写体验太差：
//
//	scheme://[user:pass@]host:port   标准 URL，scheme 走 proxyurl 的白名单
//	[user:pass@]host:port            缺协议头，补 defaultProviderProxyProtocol
//	host:port[:user:pass]            代理商常见的导出格式，同样补默认协议
//
// IPv6 只在带方括号时受支持（[::1]:1080 或标准 URL 形式）；冒号分隔的四段格式与
// IPv6 语法天然冲突，无法表达带认证的 IPv6 地址。
//
// 解析结果统一交给 ValidateProviderProxy 做最终校验，协议白名单、host 净化和
// 端口区间只保留一份规则。
func ParseProviderProxyURL(raw string) (ProviderProxyInput, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ProviderProxyInput{}, ErrProviderProxyURLRequired
	}

	var (
		in  ProviderProxyInput
		err error
	)
	switch {
	case strings.Contains(s, "://"):
		in, err = parseProviderProxyStandardURL(s)
	case strings.Contains(s, "@"):
		in, err = parseProviderProxyCredentialForm(s)
	default:
		in, err = parseProviderProxyColonForm(s)
	}
	if err != nil {
		return ProviderProxyInput{}, err
	}
	return ValidateProviderProxy(in)
}

// parseProviderProxyStandardURL 处理 scheme://[user:pass@]host:port。
func parseProviderProxyStandardURL(s string) (ProviderProxyInput, error) {
	// 复用 proxyurl.Parse：它是本项目唯一被允许解析代理 URL 的入口，
	// 自带协议白名单，并把 socks5 升级成 socks5h 防 DNS 泄漏。
	_, parsed, err := proxyurl.Parse(s)
	if err != nil || parsed == nil {
		return ProviderProxyInput{}, ErrProviderProxyURLInvalid
	}
	port, err := parseProviderProxyPort(parsed.Port())
	if err != nil {
		return ProviderProxyInput{}, err
	}
	in := ProviderProxyInput{
		Protocol: parsed.Scheme,
		Host:     parsed.Hostname(),
		Port:     port,
	}
	if parsed.User != nil {
		in.Username = parsed.User.Username()
		in.Password, _ = parsed.User.Password()
	}
	return in, nil
}

// parseProviderProxyCredentialForm 处理 user:pass@host:port（无协议头）。
func parseProviderProxyCredentialForm(s string) (ProviderProxyInput, error) {
	// 从最后一个 @ 切：代理密码里出现 @ 并不罕见，用第一个 @ 会把密码截断。
	at := strings.LastIndex(s, "@")
	credentials, hostPort := s[:at], s[at+1:]
	if credentials == "" {
		return ProviderProxyInput{}, ErrProviderProxyURLInvalid
	}
	host, port, err := splitProviderProxyHostPort(hostPort)
	if err != nil {
		return ProviderProxyInput{}, err
	}
	in := ProviderProxyInput{
		Protocol: defaultProviderProxyProtocol,
		Host:     host,
		Port:     port,
	}
	// 反过来，用户名不允许含冒号，所以凭据用第一个冒号切，密码可以含冒号。
	if colon := strings.Index(credentials, ":"); colon >= 0 {
		in.Username = credentials[:colon]
		in.Password = credentials[colon+1:]
	} else {
		in.Username = credentials
	}
	return in, nil
}

// parseProviderProxyColonForm 处理 host:port 与 host:port:user:pass。
func parseProviderProxyColonForm(s string) (ProviderProxyInput, error) {
	// 先试 host:port。net.SplitHostPort 认识 IPv6 的方括号写法，
	// 所以 [::1]:1080 这种无认证地址在这条分支上也能过。
	if host, port, err := splitProviderProxyHostPort(s); err == nil {
		return ProviderProxyInput{
			Protocol: defaultProviderProxyProtocol,
			Host:     host,
			Port:     port,
		}, nil
	}

	// 否则只接受严格四段。三段（host:port:user）语义歧义太大，直接拒绝，
	// 让供号商补全而不是让我们猜。SplitN 留 4 段是为了让密码能含冒号。
	parts := strings.SplitN(s, ":", 4)
	if len(parts) != 4 {
		return ProviderProxyInput{}, ErrProviderProxyURLInvalid
	}
	port, err := parseProviderProxyPort(parts[1])
	if err != nil {
		return ProviderProxyInput{}, err
	}
	return ProviderProxyInput{
		Protocol: defaultProviderProxyProtocol,
		Host:     parts[0],
		Port:     port,
		Username: parts[2],
		Password: parts[3],
	}, nil
}

func splitProviderProxyHostPort(s string) (string, int, error) {
	host, rawPort, err := net.SplitHostPort(strings.TrimSpace(s))
	if err != nil || host == "" {
		return "", 0, ErrProviderProxyURLInvalid
	}
	port, err := parseProviderProxyPort(rawPort)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func parseProviderProxyPort(raw string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, ErrProviderProxyURLInvalid
	}
	return port, nil
}

// ErrNoAutoAssignableProxy 平台侧当前没有可分配的出口。
//
// 触发原因有两类：管理员一条代理都没开放，或者所有开放的代理都到了绑定上限。
// 对外文案不区分这两者——池子规模属于内部信息，不下发到供号商侧。
var ErrNoAutoAssignableProxy = infraerrors.Conflict("AUTO_PROXY_UNAVAILABLE",
	"no platform IP is currently available")

// SelectAutoAssignProxy 从候选里挑一个绑定账号最少的平台代理。
//
// 入选条件：平台自有（ProviderUserID 为空）、管理员已勾选开放、active 且未过期，
// 且当前绑定账号数没到 maxAccounts 封顶。供号商自带的代理永远不参与——把一家自费
// 的出口分给另一家，两家账号还会共用同一个出口 IP。
//
// 并列时按 ID 升序，保证确定性。不做随机：「最少优先」本身就会轮转，被选中的代理
// 绑定数 +1 后自然排到后面去。
//
// 已知且被接受的竞态：两个供号商同时上号可能选中同一个代理，最终绑定数超出封顶 1 个。
// 选号必须发生在换票之前（换票就要走这个出口），而锁没法横跨 OAuth 换票这段外部慢
// IO，所以不加锁。后果只是某个出口多挂一个账号，且下一次分配会自动跳过它。
func SelectAutoAssignProxy(candidates []ProxyWithAccountCount, maxAccounts int, now time.Time) (*Proxy, error) {
	if maxAccounts <= 0 {
		maxAccounts = DefaultProviderAutoProxyMaxAccounts
	}

	var best *ProxyWithAccountCount
	for i := range candidates {
		c := &candidates[i]
		if c.ProviderUserID != nil || !c.AutoAssignable {
			continue
		}
		if !c.IsActive() || c.IsExpired(now) {
			continue
		}
		if c.AccountCount >= int64(maxAccounts) {
			continue
		}
		if best == nil ||
			c.AccountCount < best.AccountCount ||
			(c.AccountCount == best.AccountCount && c.ID < best.ID) {
			best = c
		}
	}
	if best == nil {
		return nil, ErrNoAutoAssignableProxy
	}
	selected := best.Proxy
	return &selected, nil
}

// HasAutoAssignableProxy 报告当前是否还有可分配的平台出口。
//
// 供上号页在渲染前判断要不要给出「由平台提供 IP」这个选项，只回布尔值，
// 不回可用数量：代理池规模属于平台内部信息。
func HasAutoAssignableProxy(candidates []ProxyWithAccountCount, maxAccounts int, now time.Time) bool {
	_, err := SelectAutoAssignProxy(candidates, maxAccounts, now)
	return err == nil
}

// ResolveProviderAccountPriority 决定新供号商账号的调度优先级。
//
// 优先与目标托管分组内**当前最小的 priority** 对齐，而不是套用一个全局固定值。
// 原因是调度里 priority 是硬门槛：filterByMinPriority 只保留分组内数值最小的那批
// 账号，其余完全不参与选择。取分组最小值能让新号与该分组里真正在跑的账号平起平坐，
// 既不会插队抢走自有账号的流量，也不会被静默饿死。
//
// groupMin 为该分组现有账号的最小 priority；分组为空（ok=false）时回落设置值。
// 结果绝不为 0：0 是最高优先级，会让供号商账号独占整个分组。
func ResolveProviderAccountPriority(settings ProviderSettings, groupMin int, ok bool) int {
	if ok && groupMin > 0 && groupMin <= providerMaxAccountPriority {
		return groupMin
	}
	if settings.AccountPriority <= 0 || settings.AccountPriority > providerMaxAccountPriority {
		return DefaultProviderAccountPriority
	}
	return settings.AccountPriority
}

// BuildProviderAccountInput 把上号输入翻译成 CreateAccountInput。
//
// 顺序很重要：先过滤 extra（丢掉 persona 等），再套档位，最后写强制伪装项，
// 这样供号商无论怎么构造请求都覆盖不了强制项。
// groupMinPriority 是目标托管分组内现有账号的最小 priority，ok=false 表示分组为空。
// 由调用方查好传入，保持本函数无 IO、可单测。
func BuildProviderAccountInput(
	settings ProviderSettings,
	in ProviderOnboardInput,
	proxyID int64,
	groupMinPriority int,
	groupHasAccounts bool,
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
		// Priority 与目标分组自动对齐，且必须显式给值：createAccountRecord 无条件
		// SetPriority(account.Priority)，留零值不会回落 schema 默认值而是真的写 0；
		// 调度里 priority 是硬门槛（filterByMinPriority 只保留最小值那批），
		// 0 会让所有供号商账号把自有账号完全挤出候选。
		Priority: ResolveProviderAccountPriority(settings, groupMinPriority, groupHasAccounts),
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

// MinSchedulablePriorityByGroup 返回每个分组当前的调度优先级门槛。
func (s *adminServiceImpl) MinSchedulablePriorityByGroup(ctx context.Context) (map[int64]int, error) {
	return s.accountRepo.MinPriorityByGroup(ctx)
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

// ResolveProviderAccountTier 根据账号**实际生效的参数**反推档位标识与对外文案。
//
// 刻意不再单纯读 provider_tier 列。那一列只有两条写入路径（建号、供号商自己换档），
// 管理端**既没有改它的 API 也没有 UI**；而管理员在管理端编辑账号时改的并发 /
// 会话数 / RPM / 5 小时上限，恰好就是档位映射的同一组参数。结果是：管理员把一个
// 「3 档」账号的并发从 3 改成 1000，provider_tier 仍写着 3，供号商页面上也仍然
// 显示「3 档」—— 标签与这个号实际生效的参数完全是两回事。
//
// 改成按实际参数反推之后，标签在任何情况下都不会撒谎，也不关心这些参数是谁改的。
//
// 返回的 label 为空表示「不属于任何标准档位」，由前端用 i18n 渲染成「自定义」——
// 固定档的 label 是管理员自己填的文案（不走 i18n），但「自定义」这三个字原先是
// 硬编码中文，英文界面的供号商也会看到中文。
//
// 比对全量 CapacityTiers 而不是 EnabledTiers：档位被管理员停用后，已经在用它的
// 账号参数并没有变，显示原档位名仍然是准确的（停用只影响能不能**换到**该档）。
func ResolveProviderAccountTier(settings ProviderSettings, a *Account) (tier string, label string) {
	if a == nil {
		return "", ""
	}
	for _, t := range settings.CapacityTiers {
		if providerTierMatchesAccount(t, a) {
			return t.Tier, t.Label
		}
	}
	return ProviderTierCustom, ""
}

// providerTierMatchesAccount 判断账号当前参数是否与某个固定档完全一致。
//
// 四项都要匹配：并发在 accounts.concurrency 列上，其余三项在 extra 里。
// 任何一项对不上就不算这个档 —— 宁可显示「自定义」，也不能给一个只对了一半的标签。
func providerTierMatchesAccount(t ProviderCapacityTier, a *Account) bool {
	return a.Concurrency == t.Concurrency &&
		a.GetMaxSessions() == t.MaxSessions &&
		a.GetBaseRPM() == t.BaseRPM &&
		// 金额是 float64，JSON 里 60 与 60.0 都可能出现，用容差比而不是 ==。
		math.Abs(a.GetWindowCostLimit()-t.WindowCostLimit) < 1e-9
}

// ProviderTierLabel 从设置里找档位显示名，找不到就回落档位标识本身。
//
// 已不用于账号视图（那里改用 ResolveProviderAccountTier 按实际参数反推），
// 保留给仍然需要「按档位标识查文案」的场景。
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
