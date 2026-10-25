//go:build unit

package service

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type leaseHeartbeatCache struct {
	refreshOK atomic.Bool
	released  atomic.Int32
}

func (c *leaseHeartbeatCache) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, nil
}
func (c *leaseHeartbeatCache) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}
func (c *leaseHeartbeatCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}
func (c *leaseHeartbeatCache) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}
func (c *leaseHeartbeatCache) AcquireClaudeCodeRunLease(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (c *leaseHeartbeatCache) RefreshClaudeCodeRunLease(context.Context, string, string, time.Duration) (bool, error) {
	return c.refreshOK.Load(), nil
}
func (c *leaseHeartbeatCache) ReleaseClaudeCodeRunLease(context.Context, string, string) error {
	c.released.Add(1)
	return nil
}

func TestClaudeCodeRuntime_LeaseHeartbeatReleasesRun(t *testing.T) {
	cache := &leaseHeartbeatCache{}
	runtime := &ClaudeCodeRuntime{
		cache:          cache,
		runs:           NewClaudeCodeRunManager(),
		leaseHeartbeat: 10 * time.Millisecond,
		leaseFailures:  2,
	}
	runtime.runs.OnRelease = runtime.releaseRunLease
	proc := &fakeTurnProcess{}
	runtime.runs.NewProcess = func(context.Context, *Account, ClaudeCodeTurn, func(json.RawMessage) error) (ClaudeCodeTurnProcess, error) {
		return proc, nil
	}
	var released atomic.Int32
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	_, err := runtime.runs.RunTurn(context.Background(), "s1", account, func() { released.Add(1) }, ClaudeCodeTurn{Messages: []ClaudeCodeMessage{{Role: "user", Text: "hi"}}}, nil)
	require.NoError(t, err)

	runtime.startLeaseHeartbeat("s1")
	require.Eventually(t, func() bool {
		return released.Load() == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(1), cache.released.Load())
	proc.mu.Lock()
	assert.True(t, proc.closed)
	proc.mu.Unlock()
}

type leaseOwnershipCache struct {
	mu    sync.Mutex
	owner map[string]string
}

func (c *leaseOwnershipCache) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, nil
}
func (c *leaseOwnershipCache) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}
func (c *leaseOwnershipCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}
func (c *leaseOwnershipCache) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}
func (c *leaseOwnershipCache) AcquireClaudeCodeRunLease(_ context.Context, sessionKey, owner string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.owner == nil {
		c.owner = make(map[string]string)
	}
	if _, exists := c.owner[sessionKey]; exists {
		return false, nil
	}
	c.owner[sessionKey] = owner
	return true, nil
}
func (c *leaseOwnershipCache) RefreshClaudeCodeRunLease(_ context.Context, sessionKey, owner string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.owner[sessionKey] == owner, nil
}
func (c *leaseOwnershipCache) ReleaseClaudeCodeRunLease(_ context.Context, sessionKey, owner string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.owner[sessionKey] == owner {
		delete(c.owner, sessionKey)
	}
	return nil
}

func TestClaudeCodeRuntime_LeaseOwnership(t *testing.T) {
	cache := &leaseOwnershipCache{}
	a := &ClaudeCodeRuntime{cache: cache, ownerID: "owner-a", runs: NewClaudeCodeRunManager()}
	b := &ClaudeCodeRuntime{cache: cache, ownerID: "owner-b", runs: NewClaudeCodeRunManager()}
	ctx := context.Background()

	require.NoError(t, a.acquireRunLease(ctx, "s1"))
	require.ErrorIs(t, b.acquireRunLease(ctx, "s1"), ErrClaudeCodeSessionOwned)

	a.releaseRunLease("s1")
	require.NoError(t, b.acquireRunLease(ctx, "s1"))
}
