//go:build integration

package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type GeminiTokenCacheSuite struct {
	IntegrationRedisSuite
	cache service.GeminiTokenCache
}

func (s *GeminiTokenCacheSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.cache = NewGeminiTokenCache(s.rdb)
}

func (s *GeminiTokenCacheSuite) TestDeleteAccessToken() {
	cacheKey := "project-123"
	token := "token-value"
	require.NoError(s.T(), s.cache.SetAccessToken(s.ctx, cacheKey, token, time.Minute))

	got, err := s.cache.GetAccessToken(s.ctx, cacheKey)
	require.NoError(s.T(), err)
	require.Equal(s.T(), token, got)

	require.NoError(s.T(), s.cache.DeleteAccessToken(s.ctx, cacheKey))

	_, err = s.cache.GetAccessToken(s.ctx, cacheKey)
	require.True(s.T(), errors.Is(err, redis.Nil), "expected redis.Nil after delete")
}

func (s *GeminiTokenCacheSuite) TestDeleteAccessToken_MissingKey() {
	require.NoError(s.T(), s.cache.DeleteAccessToken(s.ctx, "missing-key"))
}

func (s *GeminiTokenCacheSuite) TestOwnedRefreshLockOnlyReleasesMatchingOwner() {
	owned, ok := s.cache.(service.OwnedOAuthRefreshLockCache)
	require.True(s.T(), ok)

	acquired, err := owned.AcquireOwnedRefreshLock(s.ctx, "account-1", "owner-1", time.Minute)
	require.NoError(s.T(), err)
	require.True(s.T(), acquired)
	renewed, err := owned.RenewOwnedRefreshLock(s.ctx, "account-1", "owner-2", time.Minute)
	require.NoError(s.T(), err)
	require.False(s.T(), renewed)
	renewed, err = owned.RenewOwnedRefreshLock(s.ctx, "account-1", "owner-1", time.Minute)
	require.NoError(s.T(), err)
	require.True(s.T(), renewed)

	require.NoError(s.T(), owned.ReleaseOwnedRefreshLock(s.ctx, "account-1", "owner-2"))
	acquired, err = owned.AcquireOwnedRefreshLock(s.ctx, "account-1", "owner-2", time.Minute)
	require.NoError(s.T(), err)
	require.False(s.T(), acquired)

	require.NoError(s.T(), owned.ReleaseOwnedRefreshLock(s.ctx, "account-1", "owner-1"))
	acquired, err = owned.AcquireOwnedRefreshLock(s.ctx, "account-1", "owner-2", time.Minute)
	require.NoError(s.T(), err)
	require.True(s.T(), acquired)
}

func TestGeminiTokenCacheSuite(t *testing.T) {
	suite.Run(t, new(GeminiTokenCacheSuite))
}
