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

func TestTempUnschedCacheAnthropicOpaque429Backoff(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cache := NewTempUnschedCache(client)
	backoff, ok := cache.(service.Anthropic429BackoffCache)
	require.True(t, ok)

	ctx := context.Background()
	streak, err := backoff.NextAnthropicOpaque429Backoff(ctx, 42)
	require.NoError(t, err)
	require.Equal(t, 1, streak)

	// Concurrent/in-flight responses in the same short window must not inflate
	// the adaptive level.
	streak, err = backoff.NextAnthropicOpaque429Backoff(ctx, 42)
	require.NoError(t, err)
	require.Equal(t, 1, streak)

	key := anthropicOpaque429BackoffKeyPrefix + "42"
	require.NoError(t, client.HSet(
		ctx,
		key,
		"last_increment_at",
		time.Now().Add(-anthropicOpaque429DedupeWindow-time.Second).Unix(),
	).Err())
	streak, err = backoff.NextAnthropicOpaque429Backoff(ctx, 42)
	require.NoError(t, err)
	require.Equal(t, 2, streak)

	require.NoError(t, backoff.ResetAnthropicOpaque429Backoff(ctx, 42))
	streak, err = backoff.NextAnthropicOpaque429Backoff(ctx, 42)
	require.NoError(t, err)
	require.Equal(t, 1, streak)
}
