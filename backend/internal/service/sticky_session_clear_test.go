package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type clearStickySessionCacheStub struct {
	GatewayCache

	deletedGroupID int64
	deletedSession string
	deleteCalls    int
}

func (c *clearStickySessionCacheStub) DeleteSessionAccountID(_ context.Context, groupID int64, sessionHash string) error {
	c.deletedGroupID = groupID
	c.deletedSession = sessionHash
	c.deleteCalls++
	return nil
}

func TestGatewayServiceClearStickySessionDeletesBinding(t *testing.T) {
	cache := &clearStickySessionCacheStub{}
	svc := &GatewayService{cache: cache}
	groupID := int64(7)

	err := svc.ClearStickySession(context.Background(), &groupID, "session-a")

	require.NoError(t, err)
	require.Equal(t, 1, cache.deleteCalls)
	require.Equal(t, int64(7), cache.deletedGroupID)
	require.Equal(t, "session-a", cache.deletedSession)
}

func TestGatewayServiceClearStickySessionNoopsWithoutSessionOrCache(t *testing.T) {
	cache := &clearStickySessionCacheStub{}
	svc := &GatewayService{cache: cache}

	require.NoError(t, svc.ClearStickySession(context.Background(), nil, ""))
	require.Zero(t, cache.deleteCalls)

	require.NoError(t, (&GatewayService{}).ClearStickySession(context.Background(), nil, "session-a"))
}
