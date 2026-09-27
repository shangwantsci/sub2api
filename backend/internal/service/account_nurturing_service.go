package service

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/imroc/req/v3"
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
// 在人格活跃时段内、按「每天一次」规则依次养号。
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

		// 只在人格活跃时段养号；未启用 persona 时 IsWithinActiveHours 默认放行。
		if !account.GetPersonaEnvelope().IsWithinActiveHours(time.Now()) {
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

// nurture 对单个账号执行养号动作（浏览 + Chat 对话 + 用量查询）。
func (s *AccountNurturingService) nurture(ctx context.Context, account *Account) {
	if account == nil {
		return
	}

	if err := s.browseClaudeAI(ctx, account); err != nil {
		logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d browse failed: %v", account.ID, err)
	}

	// 随机延迟 5-15 秒
	if !s.sleepOrStop(ctx, time.Duration(5000+rand.Intn(10000))*time.Millisecond) {
		return
	}

	// Chat 对话痕迹（每次养号有 50% 概率发 Chat，避免每天固定模式）
	if rand.Intn(2) == 0 {
		if err := s.sendNurturingChat(ctx, account); err != nil {
			logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d chat failed: %v", account.ID, err)
		}
		if !s.sleepOrStop(ctx, time.Duration(3000+rand.Intn(7000))*time.Millisecond) {
			return
		}
	}

	if err := s.queryUsage(ctx, account); err != nil {
		logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d usage query failed: %v", account.ID, err)
	}
}

// browseClaudeAI 用 sessionKey 访问 claude.ai 组织与设置接口，制造网页活动痕迹。
// 无 sessionKey 时跳过（仍可由 nurture 继续做 usage 查询）。
func (s *AccountNurturingService) browseClaudeAI(ctx context.Context, account *Account) error {
	sessionKey := strings.TrimSpace(account.GetCredential("session_key"))
	if sessionKey == "" {
		return nil
	}

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	client, err := createNurturingClient(proxyURL)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	cookie := &http.Cookie{Name: "sessionKey", Value: sessionKey}

	// 1. GET /api/organizations（正常用户每次开页面都会触发）
	resp, err := client.R().
		SetContext(ctx).
		SetCookies(cookie).
		Get("https://claude.ai/api/organizations")
	if err != nil {
		return fmt.Errorf("browse organizations: %w", err)
	}
	if !resp.IsSuccessState() {
		logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d browse /api/organizations failed: %d", account.ID, resp.StatusCode)
		return nil
	}

	// 随机延迟 2-5 秒，模拟用户看页面。
	if !s.sleepOrStop(ctx, time.Duration(2000+rand.Intn(3000))*time.Millisecond) {
		return ctx.Err()
	}

	// 2. GET /api/organizations/{org}/settings（查看设置页）
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

// createNurturingClient 创建带 Chrome TLS 仿真的 HTTP 客户端，可选走账号代理。
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
