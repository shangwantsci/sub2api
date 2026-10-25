//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type poolTestRunner struct {
	fn func(context.Context, ClaudeCodeTurn) (*ClaudeCodeResult, error)
}

func (r poolTestRunner) Run(ctx context.Context, turn ClaudeCodeTurn) (*ClaudeCodeResult, error) {
	if r.fn != nil {
		return r.fn(ctx, turn)
	}
	return &ClaudeCodeResult{Text: "ok"}, nil
}

func TestAccountTransport(t *testing.T) {
	cases := []struct {
		name    string
		account *Account
		want    AccountTransport
	}{
		{"nil", nil, TransportAPI},
		{"anthropic oauth", &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}, TransportClaudeCode},
		{"anthropic setup token", &Account{Platform: PlatformAnthropic, Type: AccountTypeSetupToken}, TransportClaudeCode},
		{"anthropic api key", &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}, TransportAPI},
		{"openai oauth", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, TransportAPI},
		{"gemini", &Account{Platform: PlatformGemini, Type: AccountTypeOAuth}, TransportAPI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.account.Transport())
		})
	}
}

func TestClaudeCodeModelAlias(t *testing.T) {
	cases := []struct {
		model string
		want  string
		ok    bool
	}{
		{"", "", true},
		{"claude-opus-5-5", "opus", true},
		{"claude-3-5-sonnet-20241022", "sonnet", true},
		{"claude-haiku-4-5", "haiku", true},
		{"HAIKU", "haiku", true},
		{"gpt-4", "", false},
		{"unknown-model", "", false},
	}
	for _, tc := range cases {
		alias, ok := ClaudeCodeModelAlias(tc.model)
		assert.Equal(t, tc.ok, ok, tc.model)
		assert.Equal(t, tc.want, alias, tc.model)
	}
}

func TestClaudeCodePool_PerAccountConcurrencyLimit(t *testing.T) {
	pool := NewClaudeCodePool(0, 2)
	var current, peak int32
	pool.NewRunner = func(*Account, func(json.RawMessage) error) (ClaudeCodeTurnRunner, error) {
		return poolTestRunner{fn: func(context.Context, ClaudeCodeTurn) (*ClaudeCodeResult, error) {
			n := atomic.AddInt32(&current, 1)
			for {
				old := atomic.LoadInt32(&peak)
				if n <= old || atomic.CompareAndSwapInt32(&peak, old, n) {
					break
				}
			}
			time.Sleep(40 * time.Millisecond)
			atomic.AddInt32(&current, -1)
			return &ClaudeCodeResult{Text: "ok"}, nil
		}}, nil
	}

	account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = pool.Run(context.Background(), account, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}})
		}()
	}
	close(start)
	wg.Wait()

	assert.LessOrEqual(t, atomic.LoadInt32(&peak), int32(2), "per-account concurrency must be capped")
	assert.Greater(t, atomic.LoadInt32(&peak), int32(0))
}

func TestClaudeCodePool_GlobalConcurrencyLimit(t *testing.T) {
	pool := NewClaudeCodePool(3, 10)
	var current, peak int32
	pool.NewRunner = func(*Account, func(json.RawMessage) error) (ClaudeCodeTurnRunner, error) {
		return poolTestRunner{fn: func(context.Context, ClaudeCodeTurn) (*ClaudeCodeResult, error) {
			n := atomic.AddInt32(&current, 1)
			for {
				old := atomic.LoadInt32(&peak)
				if n <= old || atomic.CompareAndSwapInt32(&peak, old, n) {
					break
				}
			}
			time.Sleep(40 * time.Millisecond)
			atomic.AddInt32(&current, -1)
			return &ClaudeCodeResult{Text: "ok"}, nil
		}}, nil
	}

	accounts := []*Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeSetupToken},
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		account := accounts[i%len(accounts)]
		go func() {
			defer wg.Done()
			<-start
			_, _ = pool.Run(context.Background(), account, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}})
		}()
	}
	close(start)
	wg.Wait()

	assert.LessOrEqual(t, atomic.LoadInt32(&peak), int32(3), "global concurrency must be capped")
}

func TestClaudeCodePool_EnsureFreshRunsBeforeRunner(t *testing.T) {
	pool := NewClaudeCodePool(0, 2)
	var fresh, ranAfterFresh atomic.Bool
	refreshedAccount := &Account{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	pool.EnsureFresh = func(_ context.Context, _ *Account) (*Account, error) {
		fresh.Store(true)
		return refreshedAccount, nil
	}
	var runnerAccount *Account
	pool.NewRunner = func(account *Account, _ func(json.RawMessage) error) (ClaudeCodeTurnRunner, error) {
		runnerAccount = account
		return poolTestRunner{fn: func(context.Context, ClaudeCodeTurn) (*ClaudeCodeResult, error) {
			ranAfterFresh.Store(fresh.Load())
			return &ClaudeCodeResult{Text: "ok"}, nil
		}}, nil
	}

	_, err := pool.Run(context.Background(), &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}, ClaudeCodeTurn{
		Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}},
	})
	require.NoError(t, err)
	assert.True(t, ranAfterFresh.Load(), "credentials must be refreshed before spawning CC")
	assert.Same(t, refreshedAccount, runnerAccount, "runner must receive the refreshed account, not the stale snapshot")
}

func TestClaudeCodePool_EnsureFreshErrorSkipsRunner(t *testing.T) {
	pool := NewClaudeCodePool(0, 2)
	pool.EnsureFresh = func(context.Context, *Account) (*Account, error) { return nil, errors.New("refresh failed") }
	var called atomic.Bool
	pool.NewRunner = func(*Account, func(json.RawMessage) error) (ClaudeCodeTurnRunner, error) {
		called.Store(true)
		return poolTestRunner{}, nil
	}

	_, err := pool.Run(context.Background(), &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}, ClaudeCodeTurn{
		Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refresh failed")
	assert.False(t, called.Load())
}

func TestClaudeCodePool_RejectsNonCCTAccount(t *testing.T) {
	pool := NewClaudeCodePool(0, 2)
	pool.NewRunner = func(*Account, func(json.RawMessage) error) (ClaudeCodeTurnRunner, error) {
		return poolTestRunner{}, nil
	}

	_, err := pool.Run(context.Background(), &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}, ClaudeCodeTurn{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a Claude Code transport account")
}

func TestClaudeCodePool_PropagatesRunnerError(t *testing.T) {
	pool := NewClaudeCodePool(0, 2)
	pool.NewRunner = func(*Account, func(json.RawMessage) error) (ClaudeCodeTurnRunner, error) {
		return poolTestRunner{fn: func(context.Context, ClaudeCodeTurn) (*ClaudeCodeResult, error) {
			return nil, errors.New("cc failed")
		}}, nil
	}

	_, err := pool.Run(context.Background(), &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}, ClaudeCodeTurn{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cc failed")
}

func TestClaudeCodePool_HasCapacity(t *testing.T) {
	pool := NewClaudeCodePool(2, 1)
	accountID := int64(7)
	require.True(t, pool.HasCapacity(accountID))

	pool.mu.Lock()
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	pool.accounts[accountID] = sem
	pool.mu.Unlock()

	assert.False(t, pool.HasCapacity(accountID), "account slot full")

	other := int64(8)
	pool.mu.Lock()
	pool.accounts[other] = make(chan struct{}, 1)
	pool.mu.Unlock()
	assert.True(t, pool.HasCapacity(other))
}

func TestStickyTTLForAccount(t *testing.T) {
	svc := &GatewayService{}
	assert.Equal(t, claudeCodeStickySessionTTL, svc.stickyTTLForAccount(&Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}))
	assert.Equal(t, stickySessionTTL, svc.stickyTTLForAccount(&Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}))
}

func TestClaudeCodePool_QueueFull(t *testing.T) {
	pool := NewClaudeCodePool(0, 1)
	pool.SetQueueLimits(1, 2*time.Second)
	account := &Account{ID: 99, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	pool.NewRunner = func(*Account, func(json.RawMessage) error) (ClaudeCodeTurnRunner, error) {
		return poolTestRunner{fn: func(context.Context, ClaudeCodeTurn) (*ClaudeCodeResult, error) {
			once.Do(func() { close(started) })
			<-release
			return &ClaudeCodeResult{Text: "ok"}, nil
		}}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_, _ = pool.Run(ctx, account, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "first"}}})
	}()
	<-started

	secondDone := make(chan error, 1)
	go func() {
		_, err := pool.Run(ctx, account, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "second"}}})
		secondDone <- err
	}()

	require.Eventually(t, func() bool {
		pool.mu.Lock()
		defer pool.mu.Unlock()
		return pool.waiting[account.ID] == 1
	}, time.Second, 10*time.Millisecond)

	_, err := pool.Run(ctx, account, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "third"}}})
	require.ErrorIs(t, err, ErrClaudeCodePoolBusy)

	close(release)
	require.NoError(t, <-secondDone)
}
