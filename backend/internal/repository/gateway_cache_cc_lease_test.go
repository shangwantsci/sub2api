//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCache_ClaudeCodeRunLease(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := NewGatewayCache(rdb).(*gatewayCache)
	ctx := context.Background()

	acquired, err := cache.AcquireClaudeCodeRunLease(ctx, "s1", "owner-a", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = cache.AcquireClaudeCodeRunLease(ctx, "s1", "owner-b", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired)

	refreshed, err := cache.RefreshClaudeCodeRunLease(ctx, "s1", "owner-b", time.Minute)
	require.NoError(t, err)
	require.False(t, refreshed)
	refreshed, err = cache.RefreshClaudeCodeRunLease(ctx, "s1", "owner-a", time.Minute)
	require.NoError(t, err)
	require.True(t, refreshed)

	require.NoError(t, cache.ReleaseClaudeCodeRunLease(ctx, "s1", "owner-b"))
	acquired, err = cache.AcquireClaudeCodeRunLease(ctx, "s1", "owner-b", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired, "non-owner release must not delete")

	require.NoError(t, cache.ReleaseClaudeCodeRunLease(ctx, "s1", "owner-a"))
	acquired, err = cache.AcquireClaudeCodeRunLease(ctx, "s1", "owner-b", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
}

func TestGatewayCache_ClaudeCodeRunLeaseExpiryTakeover(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := NewGatewayCache(rdb).(*gatewayCache)
	ctx := context.Background()

	acquired, err := cache.AcquireClaudeCodeRunLease(ctx, "s1", "owner-a", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	server.FastForward(time.Minute + time.Second)

	acquired, err = cache.AcquireClaudeCodeRunLease(ctx, "s1", "owner-b", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired, "expired owner lease can be taken over")

	refreshed, err := cache.RefreshClaudeCodeRunLease(ctx, "s1", "owner-a", time.Minute)
	require.NoError(t, err)
	require.False(t, refreshed, "old owner must not refresh new lease")

	require.NoError(t, cache.ReleaseClaudeCodeRunLease(ctx, "s1", "owner-a"))
	acquired, err = cache.AcquireClaudeCodeRunLease(ctx, "s1", "owner-c", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired, "old owner release must not delete new lease")
}

func TestGatewayCache_ClaudeCodeRunLeaseSharedRedisTwoInstances(t *testing.T) {
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = clientA.Close(); _ = clientB.Close() })
	a := NewGatewayCache(clientA).(*gatewayCache)
	b := NewGatewayCache(clientB).(*gatewayCache)
	ctx := context.Background()

	acquired, err := a.AcquireClaudeCodeRunLease(ctx, "shared-session", "instance-a", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = b.AcquireClaudeCodeRunLease(ctx, "shared-session", "instance-b", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired)

	require.NoError(t, a.ReleaseClaudeCodeRunLease(ctx, "shared-session", "instance-a"))
	acquired, err = b.AcquireClaudeCodeRunLease(ctx, "shared-session", "instance-b", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
}
