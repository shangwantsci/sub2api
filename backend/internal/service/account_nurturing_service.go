package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/util/httputil"
	"github.com/imroc/req/v3"
)

var (
	errNurturingAuth = errors.New("nurturing: auth failure")
	errNurturingCF   = errors.New("nurturing: cloudflare challenge")
)

// AccountNurturingService 周期性地给号池 Anthropic OAuth 账号制造正常的
// claude.ai 网页活动痕迹（页面浏览、设置查看、轻量 Chat 对话）以及用量查询，
// 降低「只有 Claude Code /v1/messages、零网页活动」的风控信号。
//
// 所有外部调用都是 best-effort：失败只记日志，不影响主流程和其他账号。
type AccountNurturingService struct {
	accountRepo   AccountRepository
	tokenProvider *ClaudeTokenProvider // 用于获取 OAuth token 查询 usage
	interval      time.Duration        // 扫描间隔，建议 1 小时
	stopCh        chan struct{}
	stopOnce      sync.Once
	wg            sync.WaitGroup

	// nurturedToday 记录当天已养过的账号，key = "{accountID}:{YYYYMMDD}"。
	nurturedMu    sync.Mutex
	nurturedToday map[string]struct{}
}

// NewAccountNurturingService 构造养号服务。interval <= 0 时 Start 为空操作。
func NewAccountNurturingService(
	accountRepo AccountRepository,
	tokenProvider *ClaudeTokenProvider,
	interval time.Duration,
) *AccountNurturingService {
	return &AccountNurturingService{
		accountRepo:   accountRepo,
		tokenProvider: tokenProvider,
		interval:      interval,
		stopCh:        make(chan struct{}),
		nurturedToday: make(map[string]struct{}),
	}
}

// Start 启动后台扫描 goroutine（ticker + stopCh + wg 模式）。
func (s *AccountNurturingService) Start() {
	if s == nil || s.accountRepo == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
	logger.LegacyPrintf("service.nurturing", "[Nurturing] Started (interval: %v)", s.interval)
}

// Stop 优雅停止后台 goroutine。
func (s *AccountNurturingService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
	logger.LegacyPrintf("service.nurturing", "[Nurturing] Stopped")
}

// runOnce 一次扫描循环：列出可调度 Anthropic OAuth/setup_token 账号，
// 按「跟随真实使用」策略养号——只对近期有 API 调用活动的账号执行养号动作，
// 使养号活动与真实使用穿插在一起，行为模式更自然。
func (s *AccountNurturingService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	accounts, err := s.accountRepo.ListSchedulableByPlatform(ctx, PlatformAnthropic)
	if err != nil {
		logger.LegacyPrintf("service.nurturing", "[Nurturing] ListSchedulableByPlatform failed: %v", err)
		return
	}

	today := time.Now().Format("20060102")
	s.pruneNurturedMap(today)

	for i := range accounts {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		account := &accounts[i]
		if account.Type != AccountTypeOAuth && account.Type != AccountTypeSetupToken {
			continue
		}

		// 预设为 off 时不养号
		if account.GetPersonaPreset() == PersonaPresetOff {
			continue
		}

		// 跟随真实使用：只对近期有 API 调用活动的账号养号。
		// 这样养号活动与真实请求穿插，Anthropic 看到的是一个人在密集使用
		// 间隙浏览网页、查额度，比固定时段养号更自然。
		// 未启用 persona/preset 的账号也参与（向后兼容旧行为）。
		if !s.hasRecentActivity(account) {
			continue
		}

		dayKey := s.nurtureDayKey(account.ID, today)
		if s.hasNurtured(dayKey) {
			continue
		}

		// 账号间随机延迟 0-30 秒，避免批量并发。
		if !s.sleepOrStop(ctx, time.Duration(rand.Intn(31))*time.Second) {
			return
		}

		s.nurture(ctx, account)
		s.markNurtured(dayKey)
	}
}

// hasRecentActivity 判断账号近期是否有 API 调用活动。
// 有 persona/preset 配置的账号：检查 LastUsedAt 是否在最近 2 小时内。
// 未配置 persona 的账号：保持旧行为（旧的 IsWithinActiveHours 逻辑，默认放行）。
func (s *AccountNurturingService) hasRecentActivity(account *Account) bool {
	if account == nil {
		return false
	}

	// 有 preset 或 persona 配置：基于真实使用活动判断
	if account.IsPersonaEnabled() {
		if account.LastUsedAt == nil {
			return false // 从未使用过，不需要养号
		}
		return time.Since(*account.LastUsedAt) < 2*time.Hour
	}

	// 未配置 persona/preset：保持旧行为（IsWithinActiveHours 默认放行）
	return account.GetPersonaEnvelope().IsWithinActiveHours(time.Now())
}

// nurture 对单个账号执行养号动作（浏览 + Chat 对话 + 用量查询）。
// 动作编排模拟真人使用间隙的行为：浏览网页→偶尔聊一句→用多了查额度。
// browseClaudeAI 充当 SK 预检：如果 SK 失效或遭 CF 拦截，立即熔断并标记冷却。
func (s *AccountNurturingService) nurture(ctx context.Context, account *Account) {
	if account == nil {
		return
	}

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	webClient, err := createNurturingWebClient(proxyURL)
	if err != nil {
		logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d create web client failed: %v", account.ID, err)
		return
	}

	if err := s.browseClaudeAI(ctx, account, webClient); err != nil {
		logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d browse failed: %v", account.ID, err)
		s.handleNurturingError(ctx, account, err)
		return
	}

	// 随机延迟 5-15 秒
	if !s.sleepOrStop(ctx, time.Duration(5000+rand.Intn(10000))*time.Millisecond) {
		return
	}

	// Chat 对话痕迹（每次养号有 50% 概率发 Chat，避免每天固定模式）
	if rand.Intn(2) == 0 {
		if err := s.sendNurturingChat(ctx, account, webClient); err != nil {
			logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d chat failed: %v", account.ID, err)
			s.handleNurturingError(ctx, account, err)
			return
		}
		if !s.sleepOrStop(ctx, time.Duration(3000+rand.Intn(7000))*time.Millisecond) {
			return
		}
	}

	// 查用量：真人在大量使用后会查"我还剩多少额度"。
	// 利用率 > 30% 时一定查（模拟用多了自然关心额度），否则 30% 概率查。
	env := account.GetPersonaEnvelope()
	shouldQueryUsage := env.Utilization5h > 0.30 || rand.Intn(100) < 30
	if shouldQueryUsage {
		if err := s.queryUsage(ctx, account); err != nil {
			logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d usage query failed: %v", account.ID, err)
		}
	}
}

// handleNurturingError 根据养号错误类型标记账号冷却，防止失效 SK 反复请求。
func (s *AccountNurturingService) handleNurturingError(ctx context.Context, account *Account, err error) {
	switch {
	case errors.Is(err, errNurturingAuth):
		if setErr := s.accountRepo.SetTempUnschedulable(ctx, account.ID, time.Now().Add(1*time.Hour), "nurturing: session key invalid"); setErr != nil {
			logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d SetTempUnschedulable failed: %v", account.ID, setErr)
		} else {
			logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d marked unschedulable 1h (auth failure)", account.ID)
		}
	case errors.Is(err, errNurturingCF):
		if setErr := s.accountRepo.SetTempUnschedulable(ctx, account.ID, time.Now().Add(2*time.Hour), "nurturing: cloudflare challenge"); setErr != nil {
			logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d SetTempUnschedulable failed: %v", account.ID, setErr)
		} else {
			logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d marked unschedulable 2h (cloudflare challenge)", account.ID)
		}
	}
}

// browseClaudeAI 用 sessionKey 访问 claude.ai 组织与设置接口，制造网页活动痕迹。
// 同时充当 SK 预检——如果 GET /api/organizations 返回 401/403，说明 SK 已失效或
// 遭遇 CF 拦截，返回带标记的 error 供 nurture() 熔断。
func (s *AccountNurturingService) browseClaudeAI(ctx context.Context, account *Account, client *req.Client) error {
	sessionKey := strings.TrimSpace(account.GetCredential("session_key"))
	if sessionKey == "" {
		return nil
	}

	cookie := &http.Cookie{Name: "sessionKey", Value: sessionKey}

	resp, err := client.R().
		SetContext(ctx).
		SetCookies(cookie).
		Get("https://claude.ai/api/organizations")
	if err != nil {
		return fmt.Errorf("browse organizations: %w", err)
	}
	if !resp.IsSuccessState() {
		return classifyNurturingHTTPError(resp.StatusCode, resp.Header, resp.Bytes(),
			fmt.Sprintf("browse /api/organizations: status %d", resp.StatusCode))
	}

	if !s.sleepOrStop(ctx, time.Duration(2000+rand.Intn(3000))*time.Millisecond) {
		return ctx.Err()
	}

	orgUUID := strings.TrimSpace(account.GetExtraString("org_uuid"))
	if orgUUID != "" {
		resp2, err := client.R().
			SetContext(ctx).
			SetCookies(cookie).
			Get(fmt.Sprintf("https://claude.ai/api/organizations/%s/settings", orgUUID))
		if err == nil && resp2.IsSuccessState() {
			logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d browsed settings OK", account.ID)
		}
	}

	return nil
}

// queryUsage 用 OAuth access token 查询用量，制造正常的 API 侧活动痕迹。
func (s *AccountNurturingService) queryUsage(ctx context.Context, account *Account) error {
	if s.tokenProvider == nil {
		return nil
	}

	token, err := s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	client, err := createNurturingClient(proxyURL)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+token).
		SetHeader("anthropic-beta", claude.BetaOAuth).
		SetHeader("User-Agent", "claude-code/"+claude.CLICurrentVersion).
		Get("https://api.anthropic.com/api/oauth/usage")
	if err != nil {
		return fmt.Errorf("query usage: %w", err)
	}

	if resp.IsSuccessState() {
		logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d usage query OK", account.ID)
	} else {
		logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d usage query failed: %d", account.ID, resp.StatusCode)
	}

	return nil
}

// createNurturingWebClient 创建面向 claude.ai web 的 HTTP 客户端。
// 启用默认 CookieJar（Cloudflare 下发的 __cf_bm 等 cookie 自动存储并回传），
// 设置与 XHR API 调用一致的浏览器 header（覆盖 ImpersonateChrome 的导航默认值）。
func createNurturingWebClient(proxyURL string) (*req.Client, error) {
	client := req.C().
		SetTimeout(30 * time.Second).
		ImpersonateChrome().
		SetCommonHeaders(map[string]string{
			"accept-language": "en-US,en;q=0.9",
			"sec-fetch-site":  "same-origin",
			"sec-fetch-mode":  "cors",
			"sec-fetch-dest":  "empty",
			"origin":          "https://claude.ai",
			"referer":         "https://claude.ai/",
		})

	trimmed, _, err := proxyurl.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	if trimmed != "" {
		client.SetProxyURL(trimmed)
	}

	return client, nil
}

// createNurturingClient 创建面向 api.anthropic.com 的 HTTP 客户端（queryUsage 专用）。
func createNurturingClient(proxyURL string) (*req.Client, error) {
	client := req.C().
		SetTimeout(30 * time.Second).
		ImpersonateChrome().
		SetCookieJar(nil)

	trimmed, _, err := proxyurl.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	if trimmed != "" {
		client.SetProxyURL(trimmed)
	}

	return client, nil
}

// classifyNurturingHTTPError 根据 HTTP 状态码和响应体分类养号错误。
func classifyNurturingHTTPError(statusCode int, headers http.Header, body []byte, msg string) error {
	if httputil.IsCloudflareChallengeResponse(statusCode, headers, body) {
		ray := httputil.ExtractCloudflareRayID(headers, body)
		if ray != "" {
			msg += " (cf-ray: " + ray + ")"
		}
		return fmt.Errorf("%s: %w", msg, errNurturingCF)
	}

	if statusCode == 401 || statusCode == 403 {
		if isDefinitiveClaudeSessionRejection(string(body)) {
			return fmt.Errorf("%s: %w", msg, errNurturingAuth)
		}
		return fmt.Errorf("%s: %w", msg, errNurturingAuth)
	}

	return fmt.Errorf("%s", msg)
}

func (s *AccountNurturingService) nurtureDayKey(accountID int64, today string) string {
	return fmt.Sprintf("%d:%s", accountID, today)
}

func (s *AccountNurturingService) hasNurtured(key string) bool {
	s.nurturedMu.Lock()
	defer s.nurturedMu.Unlock()
	_, ok := s.nurturedToday[key]
	return ok
}

func (s *AccountNurturingService) markNurtured(key string) {
	s.nurturedMu.Lock()
	defer s.nurturedMu.Unlock()
	s.nurturedToday[key] = struct{}{}
}

// pruneNurturedMap 丢掉非今日键，避免内存 map 无限增长。
func (s *AccountNurturingService) pruneNurturedMap(today string) {
	suffix := ":" + today
	s.nurturedMu.Lock()
	defer s.nurturedMu.Unlock()
	for k := range s.nurturedToday {
		if !strings.HasSuffix(k, suffix) {
			delete(s.nurturedToday, k)
		}
	}
}

// sleepOrStop 可中断睡眠：收到 stop 或 ctx 取消时返回 false。
func (s *AccountNurturingService) sleepOrStop(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-s.stopCh:
		return false
	case <-ctx.Done():
		return false
	}
}
