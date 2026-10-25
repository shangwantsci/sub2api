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

func TestGatewayCache_ClaudeCodeStickyFirst(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := NewGatewayCache(rdb).(*gatewayCache)
	ctx := context.Background()

	first, err := cache.GetClaudeCodeStickyFirst(ctx, 7, "session-1")
	require.NoError(t, err)
	require.True(t, first.IsZero())

	require.NoError(t, cache.SetClaudeCodeStickyFirst(ctx, 7, "session-1"))
	first, err = cache.GetClaudeCodeStickyFirst(ctx, 7, "session-1")
	require.NoError(t, err)
	require.False(t, first.IsZero())
	require.WithinDuration(t, time.Now(), first, 2*time.Second)

	// SetNX 不覆盖首次绑定时间。
	server.FastForward(10 * time.Minute)
	require.NoError(t, cache.SetClaudeCodeStickyFirst(ctx, 7, "session-1"))
	again, err := cache.GetClaudeCodeStickyFirst(ctx, 7, "session-1")
	require.NoError(t, err)
	require.Equal(t, first.Unix(), again.Unix())

	server.FastForward(2 * time.Hour)
	expired, err := cache.GetClaudeCodeStickyFirst(ctx, 7, "session-1")
	require.NoError(t, err)
	require.True(t, expired.IsZero())
}
