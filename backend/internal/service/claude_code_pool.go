package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// AccountTransport 是账号的运输方式，由账号类型唯一决定：
//   - Anthropic 的 OAuth / Setup Token 账号走 Claude Code 进程池（cc）；
//   - 其余账号（API Key / Bedrock / 其它平台）走原来的 Go 网关（api）。
//
// 它不按请求切换，也不允许互相 fallback：否则旧的 Go 伪装栈又会变成
// “只在一部分请求上活着”的半死功能。
type AccountTransport string

const (
	TransportAPI        AccountTransport = "api"
	TransportClaudeCode AccountTransport = "cc"
)

// Transport 返回账号的运输方式。
func (a *Account) Transport() AccountTransport {
	if a == nil {
		return TransportAPI
	}
	if a.Platform == PlatformAnthropic && (a.Type == AccountTypeOAuth || a.Type == AccountTypeSetupToken) {
		return TransportClaudeCode
	}
	return TransportAPI
}

// ClaudeCodeRequestShape 已由 BuildClaudeCodeTurn 直接检查，这里只保留错误码。
const (
	CodeClaudeCodeToolsUnsupported       = "CLAUDE_CODE_TOOLS_UNSUPPORTED"
	CodeClaudeCodeTemperatureUnsupported = "CLAUDE_CODE_TEMPERATURE_UNSUPPORTED"
)

// ClaudeCodeModelAlias 把客户端模型名映射成 CC 接受的别名。
// 返回 ok=false 表示不认识这个模型，调用方应拒绝而不是静默落到默认模型。
func ClaudeCodeModelAlias(model string) (alias string, ok bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case m == "":
		return "", true // 交给 CC 的默认模型
	case strings.Contains(m, "haiku"):
		return "haiku", true
	case strings.Contains(m, "sonnet"):
		return "sonnet", true
	case strings.Contains(m, "opus"):
		return "opus", true
	default:
		return "", false
	}
}

// ClaudeCodeTurnRunner 是进程池对单次 CC 调用的抽象，便于单测注入假 runner。
type ClaudeCodeTurnRunner interface {
	Run(context.Context, ClaudeCodeTurn) (*ClaudeCodeResult, error)
}

// ClaudeCodeRunnerFactory 按账号创建 runner（不同账号有不同 HOME / BASE_URL）。
// onEvent 非空时用于流式透传；factory 应把它设置到 runner 上。
type ClaudeCodeRunnerFactory func(account *Account, onEvent func(json.RawMessage) error) (ClaudeCodeTurnRunner, error)

// ClaudeCodePool 负责账号级与全局并发限制，以及 spawn 前的凭据预刷新。
// 它不关心 HTTP/SSE 翻译，那是网关层的事。
type ClaudeCodePool struct {
	NewRunner   ClaudeCodeRunnerFactory
	EnsureFresh func(context.Context, *Account) (*Account, error)

	perAccountLimit int
	globalLimit     int
	queueLimit      int
	queueWait       time.Duration

	global chan struct{}

	mu            sync.Mutex
	accounts      map[int64]chan struct{}
	waiting       map[int64]int
	globalWaiting int
}

const defaultClaudeCodePerAccountLimit = 4

// NewClaudeCodePool 创建进程池。perAccountLimit <= 0 时取默认 4；globalLimit <= 0 表示不限。
func NewClaudeCodePool(globalLimit, perAccountLimit int) *ClaudeCodePool {
	if perAccountLimit <= 0 {
		perAccountLimit = defaultClaudeCodePerAccountLimit
	}
	pool := &ClaudeCodePool{
		perAccountLimit: perAccountLimit,
		globalLimit:     globalLimit,
		accounts:        make(map[int64]chan struct{}),
		waiting:         make(map[int64]int),
	}
	if globalLimit > 0 {
		pool.global = make(chan struct{}, globalLimit)
	}
	return pool
}

// ErrClaudeCodePoolBusy 表示账号或全局等待队列已满。
var ErrClaudeCodePoolBusy = errors.New("claude code pool: queue is full")

// SetQueueLimits 设置账号/全局等待队列上限与等待超时。0 表示不限制/一直等到 ctx 取消。
func (p *ClaudeCodePool) SetQueueLimits(limit int, wait time.Duration) {
	if p == nil {
		return
	}
	p.queueLimit = limit
	p.queueWait = wait
}

// Run 执行一次 CC 调用。顺序：账号信号量 → 全局信号量 → 预刷新 → 创建 runner → 执行。
// 预刷新放在拿到账号信号量之后，避免同一账号的并发请求同时触发刷新。
func (p *ClaudeCodePool) Run(ctx context.Context, account *Account, turn ClaudeCodeTurn) (*ClaudeCodeResult, error) {
	return p.RunWithEvents(ctx, account, turn, nil)
}

// RunWithEvents 与 Run 相同，但把每个 stream_event 同步交给 onEvent（用于 SSE 透传）。
func (p *ClaudeCodePool) RunWithEvents(ctx context.Context, account *Account, turn ClaudeCodeTurn, onEvent func(json.RawMessage) error) (*ClaudeCodeResult, error) {
	return p.run(ctx, account, turn, onEvent, false)
}

// RunWithEventsReserved 执行一个已由 TryAcquire 预占槽位的 CC 调用。
// 调用方负责释放；这里不再 acquire，避免双重占用。
func (p *ClaudeCodePool) RunWithEventsReserved(ctx context.Context, account *Account, turn ClaudeCodeTurn, onEvent func(json.RawMessage) error) (*ClaudeCodeResult, error) {
	return p.run(ctx, account, turn, onEvent, true)
}

func (p *ClaudeCodePool) run(ctx context.Context, account *Account, turn ClaudeCodeTurn, onEvent func(json.RawMessage) error, reserved bool) (*ClaudeCodeResult, error) {
	if p == nil {
		return nil, errors.New("claude code pool: nil pool")
	}
	if account == nil {
		return nil, errors.New("claude code pool: nil account")
	}
	if account.Transport() != TransportClaudeCode {
		return nil, fmt.Errorf("claude code pool: account %d is not a Claude Code transport account", account.ID)
	}
	if p.NewRunner == nil {
		return nil, errors.New("claude code pool: runner factory is not configured")
	}

	if !reserved {
		releaseAccount, err := p.acquireAccount(ctx, account.ID)
		if err != nil {
			return nil, err
		}
		defer releaseAccount()

		releaseGlobal, err := p.acquireGlobal(ctx)
		if err != nil {
			return nil, err
		}
		defer releaseGlobal()
	}

	freshAccount := account
	if p.EnsureFresh != nil {
		refreshed, err := p.EnsureFresh(ctx, account)
		if err != nil {
			return nil, fmt.Errorf("claude code pool: ensure credentials: %w", err)
		}
		if refreshed != nil {
			freshAccount = refreshed
		}
	}

	runner, err := p.NewRunner(freshAccount, onEvent)
	if err != nil {
		return nil, fmt.Errorf("claude code pool: create runner: %w", err)
	}
	if runner == nil {
		return nil, errors.New("claude code pool: runner factory returned nil")
	}
	return runner.Run(ctx, turn)
}

// TryAcquire 非阻塞地预占账号槽 + 全局槽，返回释放函数。失败不改变状态。
// scheduler 用它在选号阶段原子预留，避免 HasCapacity 检查与真正 Run 之间的竞态。
func (p *ClaudeCodePool) TryAcquire(accountID int64) (func(), bool) {
	if p == nil {
		return nil, false
	}
	p.mu.Lock()
	sem := p.accounts[accountID]
	if sem == nil {
		sem = make(chan struct{}, p.perAccountLimit)
		p.accounts[accountID] = sem
	}
	p.mu.Unlock()

	select {
	case sem <- struct{}{}:
	default:
		return nil, false
	}
	releaseAccount := func() { <-sem }
	if p.global != nil {
		select {
		case p.global <- struct{}{}:
		default:
			releaseAccount()
			return nil, false
		}
	}
	return func() {
		if p.global != nil {
			<-p.global
		}
		releaseAccount()
	}, true
}

// HasCapacity 报告账号与全局是否还有空槽。它只是调度阶段的快速判断，
// 真正占用仍由 RunWithEvents 的信号量完成；存在极短竞态，但能让 scheduler
// 在大仓位满时先尝试其它账号，而不是选中后阻塞。
func (p *ClaudeCodePool) HasCapacity(accountID int64) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	sem := p.accounts[accountID]
	p.mu.Unlock()
	if sem != nil && len(sem) >= cap(sem) {
		return false
	}
	if p.global != nil && len(p.global) >= cap(p.global) {
		return false
	}
	return true
}

func (p *ClaudeCodePool) acquireAccount(ctx context.Context, accountID int64) (func(), error) {
	p.mu.Lock()
	sem := p.accounts[accountID]
	if sem == nil {
		sem = make(chan struct{}, p.perAccountLimit)
		p.accounts[accountID] = sem
	}
	if p.queueLimit > 0 && p.waiting[accountID] >= p.queueLimit {
		p.mu.Unlock()
		return nil, ErrClaudeCodePoolBusy
	}
	p.waiting[accountID]++
	p.mu.Unlock()
	defer p.releaseWaiting(accountID)

	return acquireSemaphoreWithWait(ctx, sem, p.queueWait)
}

func (p *ClaudeCodePool) acquireGlobal(ctx context.Context) (func(), error) {
	if p.global == nil {
		return func() {}, nil
	}
	p.mu.Lock()
	if p.queueLimit > 0 && p.globalWaiting >= p.queueLimit {
		p.mu.Unlock()
		return nil, ErrClaudeCodePoolBusy
	}
	p.globalWaiting++
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.globalWaiting--
		p.mu.Unlock()
	}()

	return acquireSemaphoreWithWait(ctx, p.global, p.queueWait)
}

func (p *ClaudeCodePool) releaseWaiting(accountID int64) {
	p.mu.Lock()
	if p.waiting[accountID] <= 1 {
		delete(p.waiting, accountID)
	} else {
		p.waiting[accountID]--
	}
	p.mu.Unlock()
}

func acquireSemaphoreWithWait(ctx context.Context, sem chan struct{}, wait time.Duration) (func(), error) {
	if sem == nil {
		return func() {}, nil
	}
	if wait > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, wait)
		defer cancel()
	}
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ClaudeCodePoolSnapshot 是号池运行时的只读快照。
type ClaudeCodePoolSnapshot struct {
	GlobalLimit     int `json:"global_limit"`
	PerAccountLimit int `json:"per_account_limit"`
	QueueLimit      int `json:"queue_limit"`
	ActiveGlobal    int `json:"active_global"`
	ActiveAccounts  int `json:"active_accounts"`
	WaitingAccounts int `json:"waiting_accounts"`
	WaitingGlobal   int `json:"waiting_global"`
}

// Snapshot 返回池容量与当前占用；只读，不做阻塞。
func (p *ClaudeCodePool) Snapshot() ClaudeCodePoolSnapshot {
	if p == nil {
		return ClaudeCodePoolSnapshot{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	snap := ClaudeCodePoolSnapshot{
		GlobalLimit:     p.globalLimit,
		PerAccountLimit: p.perAccountLimit,
		QueueLimit:      p.queueLimit,
		WaitingGlobal:   p.globalWaiting,
	}
	if p.global != nil {
		snap.ActiveGlobal = len(p.global)
	}
	for id, sem := range p.accounts {
		if len(sem) > 0 {
			snap.ActiveAccounts++
		}
		if p.waiting[id] > 0 {
			snap.WaitingAccounts++
		}
	}
	return snap
}
