package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ClaudeCodeRuntime 是 CC 运输的运行时装配：
//   - 进程池（账号级/全局并发、预刷新钩子）
//   - 每账号独立 HOME（.claude/.credentials.json + .claude.json）
//   - 每账号本地 forwarder 的 ANTHROPIC_BASE_URL
//
// 只有 ProcessPoolConfig.CCTransportEnabled 打开时才启用；默认关，线上行为不变。
type ClaudeCodeRuntime struct {
	cfg            *config.Config
	accountRepo    AccountRepository
	tokenProvider  *ClaudeTokenProvider
	processPool    *ProcessPool
	cache          GatewayCache
	ownerID        string
	pool           *ClaudeCodePool
	sessions       *ClaudeCodeSessionStore
	runs           *ClaudeCodeRunManager
	stickyMu       sync.Mutex
	stickyFirst    map[string]time.Time
	leaseMu        sync.Mutex
	leaseStops     map[string]chan struct{}
	leaseHeartbeat time.Duration
	leaseFailures  int
	events         *claudeCodeEventLog
}

func NewClaudeCodeRuntime(
	cfg *config.Config,
	accountRepo AccountRepository,
	tokenProvider *ClaudeTokenProvider,
	processPool *ProcessPool,
	cache GatewayCache,
) *ClaudeCodeRuntime {
	runtime := &ClaudeCodeRuntime{
		cfg:            cfg,
		accountRepo:    accountRepo,
		tokenProvider:  tokenProvider,
		processPool:    processPool,
		cache:          cache,
		ownerID:        newClaudeCodeOwnerID(),
		sessions:       NewClaudeCodeSessionStore(),
		runs:           NewClaudeCodeRunManager(),
		stickyFirst:    make(map[string]time.Time),
		leaseStops:     make(map[string]chan struct{}),
		leaseHeartbeat: claudeCodeLeaseHeartbeat,
		leaseFailures:  2,
		events:         newClaudeCodeEventLog(200),
	}
	globalLimit := 0
	perAccountLimit := defaultClaudeCodePerAccountLimit
	if cfg != nil {
		globalLimit = cfg.ProcessPool.CCMaxConcurrent
		if cfg.ProcessPool.CCPerAccountConcurrent > 0 {
			perAccountLimit = cfg.ProcessPool.CCPerAccountConcurrent
		}
	}
	runtime.pool = NewClaudeCodePool(globalLimit, perAccountLimit)
	if cfg != nil {
		runtime.pool.SetQueueLimits(cfg.ProcessPool.CCQueueLimit, time.Duration(cfg.ProcessPool.CCQueueWaitSeconds)*time.Second)
	}
	runtime.pool.EnsureFresh = runtime.ensureFresh
	runtime.pool.NewRunner = runtime.newRunner
	runtime.runs.NewProcess = runtime.newPersistentProcess
	runtime.runs.OnRelease = runtime.releaseRunLease
	runtime.runs.OnStart = runtime.recordRunStarted
	return runtime
}

// Enabled 报告 CC 运输是否可以工作。任何一个前置条件缺失都返回 false，
// 调用方应继续走原来的 Go 路径。
func (r *ClaudeCodeRuntime) Enabled() bool {
	if r == nil || r.cfg == nil || r.pool == nil {
		return false
	}
	if !r.cfg.ProcessPool.CCTransportEnabled {
		return false
	}
	if strings.TrimSpace(r.cfg.ProcessPool.ClaudeBinary) == "" {
		return false
	}
	return r.processPool != nil && r.processPool.ForwarderAvailable()
}

// Pool 返回进程池（网关层直接用它）。
func (r *ClaudeCodeRuntime) Pool() *ClaudeCodePool {
	if r == nil {
		return nil
	}
	return r.pool
}

// RunManager 返回槽所有权管理器。
func (r *ClaudeCodeRuntime) RunManager() *ClaudeCodeRunManager {
	if r == nil {
		return nil
	}
	return r.runs
}

// BindSessionSlot 把 scheduler 预占的池槽转交给 session run。
func (r *ClaudeCodeRuntime) BindSessionSlot(sessionKey string, accountID int64, release func()) {
	if r == nil || r.runs == nil {
		if release != nil {
			release()
		}
		return
	}
	r.runs.Bind(sessionKey, accountID, release, time.Now())
}

// LookupSessionAccount 返回 session 当前持有的账号。
func (r *ClaudeCodeRuntime) LookupSessionAccount(sessionKey string) (int64, bool) {
	if r == nil || r.runs == nil {
		return 0, false
	}
	id, ok := r.runs.Lookup(sessionKey)
	if !ok {
		return 0, false
	}
	if !r.refreshRunLease(context.Background(), sessionKey) {
		return 0, false
	}
	return id, true
}

// RunRequest 是网关的入口：解析 body → 校验/映射 → 进程池执行（可流式）→ 归一化 usage。
func (r *ClaudeCodeRuntime) RunRequest(
	ctx context.Context,
	account *Account,
	body []byte,
	model string,
	onEvent func(json.RawMessage) error,
) (*ClaudeCodeForwardResult, error) {
	return r.RunRequestWithSession(ctx, account, body, model, "", onEvent, false)
}

// RunRequestReserved 供 scheduler 已预占池槽位的路径使用。
func (r *ClaudeCodeRuntime) RunRequestReserved(
	ctx context.Context,
	account *Account,
	body []byte,
	model string,
	onEvent func(json.RawMessage) error,
) (*ClaudeCodeForwardResult, error) {
	return r.RunRequestWithSession(ctx, account, body, model, "", onEvent, true)
}

const (
	claudeCodeRunLeaseTTL    = 10 * time.Minute
	claudeCodeLeaseHeartbeat = 3 * time.Minute
)

// ErrClaudeCodeSessionOwned 表示同一 session 的持久 run 由其它实例持有。
var ErrClaudeCodeSessionOwned = errors.New("claude code session is owned by another instance")

type claudeCodeLeaseCache interface {
	AcquireClaudeCodeRunLease(ctx context.Context, sessionKey, owner string, ttl time.Duration) (bool, error)
	RefreshClaudeCodeRunLease(ctx context.Context, sessionKey, owner string, ttl time.Duration) (bool, error)
	ReleaseClaudeCodeRunLease(ctx context.Context, sessionKey, owner string) error
}

func newClaudeCodeOwnerID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("cc-%d", time.Now().UnixNano())
	}
	return "cc-" + hex.EncodeToString(buf)
}

func (r *ClaudeCodeRuntime) leaseCache() claudeCodeLeaseCache {
	if r == nil {
		return nil
	}
	cache, _ := r.cache.(claudeCodeLeaseCache)
	return cache
}

func (r *ClaudeCodeRuntime) acquireRunLease(ctx context.Context, sessionKey string) error {
	cache := r.leaseCache()
	if cache == nil || sessionKey == "" {
		return nil
	}
	ok, err := cache.AcquireClaudeCodeRunLease(ctx, sessionKey, r.ownerID, claudeCodeRunLeaseTTL)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	refreshed, err := cache.RefreshClaudeCodeRunLease(ctx, sessionKey, r.ownerID, claudeCodeRunLeaseTTL)
	if err != nil {
		return err
	}
	if !refreshed {
		return ErrClaudeCodeSessionOwned
	}
	return nil
}

func (r *ClaudeCodeRuntime) refreshRunLease(ctx context.Context, sessionKey string) bool {
	cache := r.leaseCache()
	if cache == nil || sessionKey == "" {
		return true
	}
	ok, err := cache.RefreshClaudeCodeRunLease(ctx, sessionKey, r.ownerID, claudeCodeRunLeaseTTL)
	return err == nil && ok
}

func (r *ClaudeCodeRuntime) startLeaseHeartbeat(sessionKey string) {
	if r == nil || sessionKey == "" || r.leaseCache() == nil {
		return
	}
	r.leaseMu.Lock()
	if r.leaseStops == nil {
		r.leaseStops = make(map[string]chan struct{})
	}
	if _, exists := r.leaseStops[sessionKey]; exists {
		r.leaseMu.Unlock()
		return
	}
	stop := make(chan struct{})
	r.leaseStops[sessionKey] = stop
	r.leaseMu.Unlock()
	interval := r.leaseHeartbeat
	if interval <= 0 {
		interval = claudeCodeLeaseHeartbeat
	}
	threshold := r.leaseFailures
	if threshold <= 0 {
		threshold = 2
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		failures := 0
		for {
			select {
			case <-ticker.C:
				if r.refreshRunLease(context.Background(), sessionKey) {
					failures = 0
					continue
				}
				failures++
				// 容忍一次瞬时 Redis 抖动；连续两次（约 6 分钟）才认为 owner 丢失。
				if failures >= threshold {
					r.RecordEvent("lease_lost", sessionKey, 0, "lease refresh failed")
					r.runs.Release(sessionKey)
					return
				}
			case <-stop:
				return
			}
		}
	}()
}

func (r *ClaudeCodeRuntime) stopLeaseHeartbeat(sessionKey string) {
	if r == nil || sessionKey == "" {
		return
	}
	r.leaseMu.Lock()
	stop := r.leaseStops[sessionKey]
	delete(r.leaseStops, sessionKey)
	r.leaseMu.Unlock()
	if stop != nil {
		close(stop)
	}
}

func (r *ClaudeCodeRuntime) releaseRunLease(sessionKey string) {
	r.stopLeaseHeartbeat(sessionKey)
	cache := r.leaseCache()
	if cache == nil || sessionKey == "" {
		return
	}
	_ = cache.ReleaseClaudeCodeRunLease(context.Background(), sessionKey, r.ownerID)
}

// prepareMCPRun 为带 tools 的持久 run 写 tools/MCP config，并预注册回调 token。
func (r *ClaudeCodeRuntime) prepareMCPRun(sessionKey string, body []byte) (string, []string, string, error) {
	var envelope struct {
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Tools) == 0 {
		return "", nil, "", nil
	}
	var tools []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(envelope.Tools, &tools); err != nil {
		return "", nil, "", err
	}
	if len(tools) == 0 {
		return "", nil, "", nil
	}
	base := ""
	if r.cfg != nil {
		base = strings.TrimRight(strings.TrimSpace(r.cfg.ProcessPool.CCCallbackBaseURL), "/")
	}
	if base == "" {
		return "", nil, "", errors.New("MCP callback base URL is not configured")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", nil, "", err
	}
	hash := sha256.Sum256([]byte(sessionKey))
	dir := filepath.Join(r.homeDirRoot(), "runs", hex.EncodeToString(hash[:]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, "", err
	}
	toolsPath := filepath.Join(dir, "tools.json")
	if err := os.WriteFile(toolsPath, envelope.Tools, 0o600); err != nil {
		return "", nil, "", err
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", nil, "", err
	}
	token := hex.EncodeToString(tokenBytes)
	config, err := BuildClaudeCodeMCPConfig(executable, base+"/internal/cc/tools", token, toolsPath)
	if err != nil {
		return "", nil, "", err
	}
	configPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		return "", nil, "", err
	}
	allowed := make([]string, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		allowed = append(allowed, "mcp__sub2api__"+tool.Name)
	}
	r.runs.RegisterToolToken(sessionKey, token)
	return configPath, allowed, token, nil
}

func (r *ClaudeCodeRuntime) homeDirRoot() string {
	root := "/tmp/sub2api-process-pool"
	if r != nil && r.cfg != nil && strings.TrimSpace(r.cfg.ProcessPool.DataDir) != "" {
		root = r.cfg.ProcessPool.DataDir
	}
	return filepath.Join(root, "cc")
}

// RunRequestWithSessionManaged 是持久 run 的入口。开关关闭或没有预占槽时回退到一次性 runner。
// slotOwned=true 表示 release 已交给 RunManager，handler 不得再释放。
func (r *ClaudeCodeRuntime) RunRequestWithSessionManaged(
	ctx context.Context,
	account *Account,
	body []byte,
	model string,
	sessionKey string,
	onEvent func(json.RawMessage) error,
	release func(),
	reserved bool,
) (*ClaudeCodeForwardResult, bool, error) {
	if r == nil || !r.Enabled() {
		return nil, false, errors.New("claude code runtime is not enabled")
	}
	if !r.persistentRunsEnabled() || sessionKey == "" || release == nil || !reserved {
		result, err := r.RunRequestWithSession(ctx, account, body, model, sessionKey, onEvent, reserved)
		return result, false, err
	}

	mcpConfig, allowedTools, toolToken, mcpErr := r.prepareMCPRun(sessionKey, body)
	if mcpErr != nil {
		release()
		return nil, true, newClaudeCodeRequestError(mcpErr)
	}
	turn, err := BuildClaudeCodeTurnWithTools(body, model, len(allowedTools) > 0)
	if err != nil {
		if toolToken != "" {
			r.runs.UnregisterToolToken(toolToken)
		}
		return nil, false, newClaudeCodeRequestError(err)
	}
	turn.MCPConfigPath = mcpConfig
	turn.AllowedTools = allowedTools
	if sessionID, ok := r.sessions.Lookup(sessionKey, account.ID, time.Now()); ok {
		turn.ResumeSessionID = sessionID
		turn, err = latestClaudeCodeUserTurn(turn)
		if err != nil {
			return nil, false, newClaudeCodeRequestError(err)
		}
	}

	if leaseErr := r.acquireRunLease(ctx, sessionKey); leaseErr != nil {
		if toolToken != "" {
			r.runs.UnregisterToolToken(toolToken)
		}
		release()
		return nil, true, leaseErr
	}
	r.startLeaseHeartbeat(sessionKey)

	fresh := account
	if r.tokenProvider != nil {
		refreshed, freshErr := r.ensureFresh(ctx, account)
		if freshErr != nil {
			release()
			return nil, true, freshErr
		}
		if refreshed != nil {
			fresh = refreshed
		}
	}
	result, runErr := r.runs.RunTurn(ctx, sessionKey, fresh, release, turn, onEvent)
	if runErr != nil {
		if toolToken != "" {
			r.runs.UnregisterToolToken(toolToken)
		}
		r.persistRefreshedCredentials(ctx, account)
		return nil, true, classifyClaudeCodeRunError(runErr)
	}
	if result != nil && strings.TrimSpace(sessionKey) != "" {
		r.sessions.Record(sessionKey, account.ID, result.SessionID, time.Now())
	}
	r.persistRefreshedCredentials(ctx, account)
	return &ClaudeCodeForwardResult{Turn: turn, Result: result, Usage: NormalizeClaudeCodeUsage(turn, result)}, true, nil
}

func (r *ClaudeCodeRuntime) persistentRunsEnabled() bool {
	return r != nil && r.cfg != nil && r.cfg.ProcessPool.CCPersistentRuns
}

// ContinueToolSession 回填工具结果后继续读同一个持久进程。
func (r *ClaudeCodeRuntime) ContinueToolSession(ctx context.Context, sessionKey string, onEvent func(json.RawMessage) error) (*ClaudeCodeForwardResult, error) {
	if r == nil || !r.Enabled() || r.runs == nil {
		return nil, errors.New("claude code runtime is not enabled")
	}
	if !r.refreshRunLease(ctx, sessionKey) {
		return nil, errors.New("claude code session is owned by another instance")
	}
	result, err := r.runs.ContinueSession(ctx, sessionKey, onEvent)
	if err != nil {
		return nil, classifyClaudeCodeRunError(err)
	}
	return &ClaudeCodeForwardResult{
		Result: result,
		Usage:  NormalizeClaudeCodeUsage(ClaudeCodeTurn{}, result),
	}, nil
}

// StickySessionAllowed 实施 2h 账号绑定绝对上限。返回 false 时调用方应清理旧绑定；
// 过期记录会被删除，下一次绑定重新计时。当前为进程内实现，多实例需换 Redis。
func (r *ClaudeCodeRuntime) StickySessionAllowed(sessionKey string) bool {
	if r == nil || sessionKey == "" {
		return true
	}
	const max = 2 * time.Hour
	now := time.Now()
	r.stickyMu.Lock()
	first, ok := r.stickyFirst[sessionKey]
	if !ok {
		r.stickyFirst[sessionKey] = now
		r.stickyMu.Unlock()
		return true
	}
	if now.Sub(first) < max {
		r.stickyMu.Unlock()
		return true
	}
	delete(r.stickyFirst, sessionKey)
	r.stickyMu.Unlock()
	return false
}

// newPersistentProcess 启动一个不受单次 HTTP 请求 ctx 影响的长期进程。
func (r *ClaudeCodeRuntime) newPersistentProcess(_ context.Context, account *Account, first ClaudeCodeTurn, onEvent func(json.RawMessage) error) (ClaudeCodeTurnProcess, error) {
	runner, err := r.newRunner(account, onEvent)
	if err != nil {
		return nil, err
	}
	concrete, ok := runner.(*ClaudeCodeRunner)
	if !ok {
		return nil, errors.New("claude code runtime: unexpected runner type")
	}
	return StartClaudeCodeProcess(context.Background(), concrete, first)
}

// RunRequestWithSession 在 RunRequest 基础上按 sessionKey 恢复 CC session。
// 命中时只发送最新 user turn，并带 --resume；未命中时发送完整对话。
func (r *ClaudeCodeRuntime) RunRequestWithSession(
	ctx context.Context,
	account *Account,
	body []byte,
	model string,
	sessionKey string,
	onEvent func(json.RawMessage) error,
	reserved bool,
) (*ClaudeCodeForwardResult, error) {
	if !r.Enabled() {
		return nil, errors.New("claude code runtime is not enabled")
	}
	turn, err := BuildClaudeCodeTurn(body, model)
	if err != nil {
		return nil, newClaudeCodeRequestError(err)
	}
	if strings.TrimSpace(sessionKey) != "" {
		if sessionID, ok := r.sessions.Lookup(sessionKey, account.ID, time.Now()); ok {
			turn.ResumeSessionID = sessionID
			turn, err = latestClaudeCodeUserTurn(turn)
			if err != nil {
				return nil, newClaudeCodeRequestError(err)
			}
		}
	}
	result, runErr := r.pool.RunTurn(ctx, account, turn, onEvent, reserved)
	if runErr == nil && result != nil && result.Result != nil && strings.TrimSpace(sessionKey) != "" {
		r.sessions.Record(sessionKey, account.ID, result.Result.SessionID, time.Now())
	}
	// CC 可能自己刷新并轮换 refresh token；无论本次请求成功与否都要写回。
	r.persistRefreshedCredentials(ctx, account)
	return result, runErr
}

func (r *ClaudeCodeRuntime) ensureFresh(ctx context.Context, account *Account) (*Account, error) {
	if account == nil {
		return nil, errors.New("claude code runtime: nil account")
	}
	fresh := account

	// 复用现有 token provider：它自带账号级刷新锁、CAS 持久化与缓存。
	// CC 用 1 小时余量预刷新，避免进程在请求中途自己刷新并轮换 refresh token。
	if r.tokenProvider != nil {
		if refreshed, err := r.tokenProvider.EnsureFresh(ctx, account, time.Hour); err != nil {
			slog.Warn("claude_code: pre-refresh failed", "account_id", account.ID, "error", err)
		} else if refreshed != nil {
			fresh = refreshed
		}
		if r.accountRepo != nil {
			if loaded, err := r.accountRepo.GetByID(ctx, account.ID); err == nil && loaded != nil {
				fresh = loaded
			}
		}
	}

	if _, err := r.prepareHome(fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}

func (r *ClaudeCodeRuntime) newRunner(account *Account, onEvent func(json.RawMessage) error) (ClaudeCodeTurnRunner, error) {
	if account == nil {
		return nil, errors.New("claude code runtime: nil account")
	}
	if r.processPool == nil {
		return nil, errors.New("claude code runtime: process pool is not configured")
	}
	home, err := r.prepareHome(account)
	if err != nil {
		return nil, err
	}
	baseURL, err := r.processPool.EnsureLocalEndpoint(account.ID)
	if err != nil {
		return nil, err
	}

	timeout := defaultClaudeCodeRun
	if r.cfg != nil && r.cfg.ProcessPool.CCRunTimeoutSeconds > 0 {
		timeout = time.Duration(r.cfg.ProcessPool.CCRunTimeoutSeconds) * time.Second
	}
	return &ClaudeCodeRunner{
		BinaryPath: r.cfg.ProcessPool.ClaudeBinary,
		HomeDir:    home,
		BaseURL:    baseURL,
		Timeout:    timeout,
		OnEvent:    onEvent,
	}, nil
}

// prepareHome 生成/更新账号的独立 HOME：
//
//	<data_dir>/cc/acct-<id>/
//	  .claude.json             首次创建；之后保留（设备身份/会话状态不能每次变）
//	  .claude/.credentials.json 每次凭据变化时原子更新
func (r *ClaudeCodeRuntime) prepareHome(account *Account) (string, error) {
	if account == nil {
		return "", errors.New("claude code runtime: nil account")
	}
	home := r.homeDir(account)
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		return "", fmt.Errorf("claude code runtime: create home: %w", err)
	}

	credentials, err := buildClaudeCodeCredentials(account)
	if err != nil {
		return "", err
	}
	if err := writeFileIfChanged(filepath.Join(claudeDir, ".credentials.json"), credentials, 0o600); err != nil {
		return "", fmt.Errorf("claude code runtime: write credentials: %w", err)
	}

	configPath := filepath.Join(home, ".claude.json")
	if _, statErr := os.Stat(configPath); errors.Is(statErr, os.ErrNotExist) {
		payload, err := buildClaudeCodeConfig(account)
		if err != nil {
			return "", err
		}
		if err := writeFileIfChanged(configPath, payload, 0o600); err != nil {
			return "", fmt.Errorf("claude code runtime: write config: %w", err)
		}
	} else if statErr != nil {
		return "", fmt.Errorf("claude code runtime: stat config: %w", statErr)
	}
	return home, nil
}

func (r *ClaudeCodeRuntime) homeDir(account *Account) string {
	root := "/tmp/sub2api-process-pool"
	if r != nil && r.cfg != nil && strings.TrimSpace(r.cfg.ProcessPool.DataDir) != "" {
		root = r.cfg.ProcessPool.DataDir
	}
	return filepath.Join(root, "cc", fmt.Sprintf("acct-%d", account.ID))
}

// persistRefreshedCredentials 读取 CC 写完的 HOME 凭据，与 DB 比较后做 CAS 写回。
// 只写“比 DB 更新”的凭据；并发刷新由 repository 的 credentials 相等条件兜底。
func (r *ClaudeCodeRuntime) persistRefreshedCredentials(ctx context.Context, account *Account) {
	if r == nil || r.accountRepo == nil || account == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := os.ReadFile(filepath.Join(r.homeDir(account), ".claude", ".credentials.json"))
	if err != nil {
		return
	}
	var payload struct {
		ClaudeAIOAuth struct {
			AccessToken  string   `json:"accessToken"`
			RefreshToken string   `json:"refreshToken"`
			ExpiresAt    int64    `json:"expiresAt"`
			Scopes       []string `json:"scopes"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	oauth := payload.ClaudeAIOAuth
	homeAccess := strings.TrimSpace(oauth.AccessToken)
	homeRefresh := strings.TrimSpace(oauth.RefreshToken)
	if homeAccess == "" && homeRefresh == "" {
		return
	}

	db, err := r.accountRepo.GetByID(ctx, account.ID)
	if err != nil || db == nil {
		return
	}
	dbExpiry := int64(0)
	if t := db.GetCredentialAsTime("expires_at"); t != nil {
		dbExpiry = t.Unix()
	}
	homeExpiry := oauth.ExpiresAt / 1000
	if homeExpiry > 0 && homeExpiry < dbExpiry {
		return // DB 已有更新的 token
	}
	if homeExpiry == 0 && dbExpiry > 0 {
		return
	}
	if homeAccess == db.GetCredential("access_token") && homeRefresh == db.GetCredential("refresh_token") {
		return
	}

	credentials := make(map[string]any, len(db.Credentials)+3)
	for key, value := range db.Credentials {
		credentials[key] = value
	}
	if homeAccess != "" {
		credentials["access_token"] = homeAccess
	}
	if homeRefresh != "" {
		credentials["refresh_token"] = homeRefresh
	}
	if homeExpiry > 0 {
		credentials["expires_at"] = homeExpiry
	}
	if len(oauth.Scopes) > 0 {
		credentials["scopes"] = oauth.Scopes
	}

	repo, ok := r.accountRepo.(ClaudeOAuthRefreshSuccessRepository)
	if !ok {
		slog.Warn("claude_code: credential CAS repository unavailable", "account_id", db.ID)
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	applied, err := repo.UpdateClaudeOAuthCredentialsIfUnchanged(
		writeCtx, db.ID, db.Credentials, db.ProxyID, db.Type, credentials,
	)
	if err != nil {
		slog.Warn("claude_code: write back credentials failed", "account_id", db.ID, "error", err)
		return
	}
	if !applied {
		slog.Info("claude_code: credential write back skipped (concurrent change)", "account_id", db.ID)
		return
	}
	slog.Info("claude_code: credentials written back from CC home", "account_id", db.ID, "expires_at", homeExpiry)
}

func buildClaudeCodeCredentials(account *Account) ([]byte, error) {
	accessToken := strings.TrimSpace(account.GetCredential("access_token"))
	refreshToken := strings.TrimSpace(account.GetCredential("refresh_token"))
	if accessToken == "" {
		return nil, errors.New("claude code runtime: account has no access_token")
	}
	expiresAt := int64(0)
	if t := account.GetCredentialAsTime("expires_at"); t != nil {
		expiresAt = t.UnixMilli()
	}
	scopes := credentialStringSlice(account.Credentials["scopes"])
	if len(scopes) == 0 {
		scopes = []string{
			"user:file_upload", "user:inference", "user:mcp_servers",
			"user:profile", "user:sessions:claude_code",
		}
	}
	payload := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      accessToken,
			"refreshToken":     refreshToken,
			"expiresAt":        expiresAt,
			"scopes":           scopes,
			"subscriptionType": claudeCodeFirstNonEmpty(account.GetCredential("subscription_type"), claudeCodeExtraString(account, "subscription_type")),
			"accountUuid":      claudeCodeFirstNonEmpty(account.GetCredential("account_uuid"), claudeCodeExtraString(account, "account_uuid")),
			"email":            claudeCodeFirstNonEmpty(account.GetCredential("email_address"), claudeCodeExtraString(account, "email_address")),
		},
	}
	return json.MarshalIndent(payload, "", "  ")
}

func buildClaudeCodeConfig(account *Account) ([]byte, error) {
	sum := sha256.Sum256([]byte(fmt.Sprintf("sub2api-cc:%d", account.ID)))
	payload := map[string]any{
		"hasCompletedOnboarding": true,
		"numStartups":            1,
		"userID":                 hex.EncodeToString(sum[:]),
		"firstStartTime":         time.Now().UTC().Format(time.RFC3339),
		"oauthAccount": map[string]any{
			"accountUuid":      claudeCodeFirstNonEmpty(account.GetCredential("account_uuid"), claudeCodeExtraString(account, "account_uuid")),
			"emailAddress":     claudeCodeFirstNonEmpty(account.GetCredential("email_address"), claudeCodeExtraString(account, "email_address")),
			"organizationUuid": claudeCodeFirstNonEmpty(account.GetCredential("org_uuid"), claudeCodeExtraString(account, "org_uuid")),
		},
	}
	return json.MarshalIndent(payload, "", "  ")
}

func credentialStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		return strings.FieldsFunc(typed, func(r rune) bool { return r == ' ' || r == ',' })
	default:
		return nil
	}
}

func claudeCodeExtraString(account *Account, key string) string {
	if account == nil || account.Extra == nil {
		return ""
	}
	if value, ok := account.Extra[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func claudeCodeFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func writeFileIfChanged(path string, data []byte, perm os.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ClaudeCodeRuntimeStats 是号池运行时的只读汇总。
type ClaudeCodeRuntimeStats struct {
	Enabled        bool                    `json:"enabled"`
	OwnerID        string                  `json:"owner_id"`
	PersistentRuns bool                    `json:"persistent_runs"`
	Pool           ClaudeCodePoolSnapshot  `json:"pool"`
	RunCount       int                     `json:"run_count"`
	WaitingTools   int                     `json:"waiting_tools"`
	Sessions       int                     `json:"sessions"`
	Runs           []ClaudeCodeRunSnapshot `json:"runs"`
}

func (r *ClaudeCodeRuntime) Stats() ClaudeCodeRuntimeStats {
	if r == nil {
		return ClaudeCodeRuntimeStats{}
	}
	stats := ClaudeCodeRuntimeStats{
		Enabled:        r.Enabled(),
		OwnerID:        r.ownerID,
		PersistentRuns: r.persistentRunsEnabled(),
	}
	if r.pool != nil {
		stats.Pool = r.pool.Snapshot()
	}
	if r.runs != nil {
		stats.Runs = r.runs.Snapshot()
		stats.RunCount = len(stats.Runs)
		for _, run := range stats.Runs {
			if run.ToolPending > 0 {
				stats.WaitingTools++
			}
		}
	}
	if r.sessions != nil {
		r.sessions.mu.Lock()
		stats.Sessions = len(r.sessions.entries)
		r.sessions.mu.Unlock()
	}
	return stats
}

// ReleaseRun 主动结束指定 session 的本地 run；返回是否存在。
func (r *ClaudeCodeRuntime) ReleaseRun(sessionKey string) bool {
	if r == nil || r.runs == nil || sessionKey == "" {
		return false
	}
	if _, ok := r.runs.Lookup(sessionKey); !ok {
		return false
	}
	r.runs.Release(sessionKey)
	return true
}

// ClaudeCodeEvent 是号池运行事件（只读运维视图）。
type ClaudeCodeEvent struct {
	Time      time.Time `json:"time"`
	Type      string    `json:"type"`
	Session   string    `json:"session"`
	AccountID int64     `json:"account_id,omitempty"`
	Message   string    `json:"message,omitempty"`
}

type claudeCodeEventLog struct {
	mu    sync.Mutex
	max   int
	items []ClaudeCodeEvent
}

func newClaudeCodeEventLog(max int) *claudeCodeEventLog {
	return &claudeCodeEventLog{max: max}
}

func (l *claudeCodeEventLog) Add(event ClaudeCodeEvent) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = append(l.items, event)
	if len(l.items) > l.max {
		l.items = l.items[len(l.items)-l.max:]
	}
}

func (l *claudeCodeEventLog) Snapshot() []ClaudeCodeEvent {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]ClaudeCodeEvent, len(l.items))
	copy(out, l.items)
	return out
}

// RecordEvent 记录一条号池事件（供 handler/心跳/工具超时调用）。
func (r *ClaudeCodeRuntime) RecordEvent(eventType, session string, accountID int64, message string) {
	if r == nil || r.events == nil {
		return
	}
	if len(session) > 12 {
		session = session[:12]
	}
	r.events.Add(ClaudeCodeEvent{Time: time.Now(), Type: eventType, Session: session, AccountID: accountID, Message: message})
}

func (r *ClaudeCodeRuntime) Events() []ClaudeCodeEvent {
	if r == nil || r.events == nil {
		return nil
	}
	return r.events.Snapshot()
}

func (r *ClaudeCodeRuntime) recordRunStarted(sessionKey string, accountID int64) {
	r.RecordEvent("run_started", sessionKey, accountID, "")
}
