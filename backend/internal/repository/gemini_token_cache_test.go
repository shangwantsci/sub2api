//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGeminiTokenCache_DeleteAccessToken_RedisError(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	cache := NewGeminiTokenCache(rdb)
	err := cache.DeleteAccessToken(context.Background(), "broken")
	require.Error(t, err)
}

func TestGeminiTokenCache_OwnedRefreshLock(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	owned, ok := NewGeminiTokenCache(rdb).(service.OwnedOAuthRefreshLockCache)
	require.True(t, ok)
	ctx := context.Background()

	acquired, err := owned.AcquireOwnedRefreshLock(ctx, "account-1", "owner-1", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	renewed, err := owned.RenewOwnedRefreshLock(ctx, "account-1", "owner-2", time.Minute)
	require.NoError(t, err)
	require.False(t, renewed)
	renewed, err = owned.RenewOwnedRefreshLock(ctx, "account-1", "owner-1", time.Minute)
	require.NoError(t, err)
	require.True(t, renewed)

	require.NoError(t, owned.ReleaseOwnedRefreshLock(ctx, "account-1", "owner-2"))
	acquired, err = owned.AcquireOwnedRefreshLock(ctx, "account-1", "owner-2", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired)

	require.NoError(t, owned.ReleaseOwnedRefreshLock(ctx, "account-1", "owner-1"))
	acquired, err = owned.AcquireOwnedRefreshLock(ctx, "account-1", "owner-2", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
}
