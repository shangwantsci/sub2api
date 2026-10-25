package service

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ProcessPool 管理每个账号的 CC 本地 HTTPS 出口。
//
// Go 网关不再经过 Bun Forwarder：API Key/Bedrock 直接走 Go 的 HTTP 客户端，
// OAuth/SetupToken 走 Claude Code 进程，CC 通过 127.0.0.1:<port> 的 passthrough
// forwarder 出站。旧 Unix socket + token 注入路径已删除。
type ProcessPool struct {
	mu           sync.RWMutex
	ccForwarders map[int64]*forwarderProcess

	bunPath      string        // path to bun binary
	forwarderJS  string        // path to forwarder.js
	upstream     string        // upstream API URL
	startTimeout time.Duration // 等待 HTTPS 端口就绪的最长时间
}

const defaultForwarderStartTimeout = 5 * time.Second

func (p *ProcessPool) forwarderStartTimeout() time.Duration {
	if p.startTimeout > 0 {
		return p.startTimeout
	}
	return defaultForwarderStartTimeout
}

func NewProcessPool(cfg *config.Config) *ProcessPool {
	pool := &ProcessPool{
		ccForwarders: make(map[int64]*forwarderProcess),
		bunPath:      cfg.ProcessPool.BunPath,
		forwarderJS:  cfg.ProcessPool.ForwarderJS,
		upstream:     cfg.ProcessPool.Upstream,
		startTimeout: defaultForwarderStartTimeout,
	}
	if pool.bunPath == "" {
		pool.bunPath = "bun"
	}
	if pool.upstream == "" {
		pool.upstream = "https://api.anthropic.com"
	}
	return pool
}

type forwarderProcess struct {
	accountID int64
	httpsPort int
	cmd       *exec.Cmd
	startedAt time.Time

	// done 由唯一的 superviseForwarder goroutine 在 cmd.Wait() 返回后关闭，
	// 作为「进程已退出且已被回收」的唯一信号。不能用 Signal(0) 判断存活：
	// 子进程退出但尚未 Wait 时它仍是僵尸进程，Signal(0) 会返回 nil。
	done chan struct{}
}

func (f *forwarderProcess) alive() bool {
	if f == nil || f.done == nil {
		return false
	}
	select {
	case <-f.done:
		return false
	default:
		return true
	}
}

// stop 发送中断信号，最多等 timeout；仍未退出就强杀。进程回收由 superviseForwarder 负责。
func (f *forwarderProcess) stop(timeout time.Duration) {
	if f == nil || f.cmd == nil || f.cmd.Process == nil {
		return
	}
	_ = f.cmd.Process.Signal(os.Interrupt)
	select {
	case <-f.done:
		return
	case <-time.After(timeout):
	}
	_ = f.cmd.Process.Kill()
	select {
	case <-f.done:
	case <-time.After(time.Second):
		slog.Warn("process_pool: forwarder did not exit after kill", "account_id", f.accountID)
	}
}

// superviseForwarder 是每个子进程唯一的 Wait 调用点：回收进程并关闭 done。
func superviseForwarder(fwd *forwarderProcess) {
	err := fwd.cmd.Wait()
	close(fwd.done)
	if err != nil {
		slog.Warn("process_pool: forwarder exited", "account_id", fwd.accountID, "error", err)
		return
	}
	slog.Info("process_pool: forwarder exited", "account_id", fwd.accountID)
}

// ForwarderAvailable 报告 forwarder 脚本是否可用。
func (p *ProcessPool) ForwarderAvailable() bool {
	return p != nil && p.forwarderJS != ""
}

// EnsureLocalEndpoint 为账号启动（或复用）一个 passthrough HTTPS forwarder，
// 返回 Claude Code 可以使用的 ANTHROPIC_BASE_URL（https://127.0.0.1:<port>）。
// CC 自带 OAuth 凭据，所以这里不注入 token。
func (p *ProcessPool) EnsureLocalEndpoint(accountID int64) (string, error) {
	if !p.ForwarderAvailable() {
		return "", errors.New("process pool: forwarder_js is not configured")
	}
	p.mu.RLock()
	fwd := p.ccForwarders[accountID]
	p.mu.RUnlock()
	if fwd.alive() && fwd.httpsPort > 0 {
		return ccLocalBaseURL(fwd.httpsPort), nil
	}
	return p.startCCForwarder(accountID)
}

func (p *ProcessPool) startCCForwarder(accountID int64) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if fwd := p.ccForwarders[accountID]; fwd.alive() && fwd.httpsPort > 0 {
		return ccLocalBaseURL(fwd.httpsPort), nil
	}
	delete(p.ccForwarders, accountID)

	port, err := reserveLocalPort()
	if err != nil {
		return "", fmt.Errorf("process pool: reserve local port: %w", err)
	}
	cmd := exec.Command(p.bunPath, "run", p.forwarderJS,
		"--port", strconv.Itoa(port),
		"--upstream", p.upstream,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("process pool: start cc forwarder: %w", err)
	}

	fwd := &forwarderProcess{
		accountID: accountID,
		httpsPort: port,
		cmd:       cmd,
		startedAt: time.Now(),
		done:      make(chan struct{}),
	}
	go superviseForwarder(fwd)

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(p.forwarderStartTimeout())
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			p.ccForwarders[accountID] = fwd
			slog.Info("process_pool: cc forwarder started", "account_id", accountID, "port", port, "pid", cmd.Process.Pid)
			return ccLocalBaseURL(port), nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	fwd.stop(2 * time.Second)
	return "", fmt.Errorf("process pool: cc forwarder port %d not ready after %s", port, p.forwarderStartTimeout())
}

func ccLocalBaseURL(port int) string {
	return "https://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

func reserveLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = listener.Close() }()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("unexpected listener address type")
	}
	return addr.Port, nil
}

// StopAccount stops the CC forwarder for a specific account.
func (p *ProcessPool) StopAccount(accountID int64) {
	p.mu.Lock()
	fwd := p.ccForwarders[accountID]
	delete(p.ccForwarders, accountID)
	p.mu.Unlock()

	if fwd == nil {
		return
	}
	fwd.stop(5 * time.Second)
	slog.Info("process_pool: forwarder stopped", "account_id", accountID)
}

// Shutdown stops all CC forwarders.
func (p *ProcessPool) Shutdown() {
	p.mu.Lock()
	ids := make([]int64, 0, len(p.ccForwarders))
	for id := range p.ccForwarders {
		ids = append(ids, id)
	}
	p.mu.Unlock()

	for _, id := range ids {
		p.StopAccount(id)
	}
	slog.Info("process_pool: all forwarders stopped", "count", len(ids))
}

// Stats returns running CC forwarder count.
func (p *ProcessPool) Stats() map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]any{
		"cc_forwarders": len(p.ccForwarders),
		"bun_path":      p.bunPath,
		"forwarder_js":  p.forwarderJS,
	}
}
