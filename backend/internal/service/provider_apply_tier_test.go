//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type applyTierRepo struct {
	accountRepoStub
	byTier    map[string][]Account
	bulkCalls []capacityBulkCall
	bulkErr   error
	listErr   error
}

func (r *applyTierRepo) ListByProviderTier(_ context.Context, tier string) ([]Account, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.byTier[tier], nil
}

func (r *applyTierRepo) BulkUpdate(_ context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	r.bulkCalls = append(r.bulkCalls, capacityBulkCall{ids: append([]int64(nil), ids...), updates: updates})
	if r.bulkErr != nil {
		return 0, r.bulkErr
	}
	return int64(len(ids)), nil
}

func resolvedSeedTier(t *testing.T, key string) ResolvedProviderTier {
	t.Helper()
	settings := defaultProviderSettings()
	resolved, err := ResolveProviderTier(settings, key, nil)
	require.NoError(t, err)
	return resolved
}

// 「应用到存量」改走 BulkUpdate 的增量写：一条 SQL 覆盖全部命中账号，
// 并发列与 load_factor 一起写成档位并发，extra 只合并 4 个键，其余持久设置原样保留。
func TestApplyTierToExistingProviderAccounts_UsesBulkUpdateWithFourExtraKeys(t *testing.T) {
	repo := &applyTierRepo{byTier: map[string][]Account{"3": {{ID: 11}, {ID: 12}}}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"))
	require.NoError(t, err)
	require.Equal(t, ProviderTierApplyResult{Matched: 2, Updated: 2}, result)

	require.Len(t, repo.bulkCalls, 1, "一条 SQL，没有逐账号的读改写")
	call := repo.bulkCalls[0]
	require.Equal(t, []int64{11, 12}, call.ids)
	require.NotNil(t, call.updates.Concurrency)
	require.Equal(t, 3, *call.updates.Concurrency)
	require.NotNil(t, call.updates.LoadFactor)
	require.Equal(t, 3, *call.updates.LoadFactor, "load_factor 与并发保持一致（与上号时的赋值相同）")
	require.Equal(t, map[string]any{
		"max_sessions":      3,
		"base_rpm":          30,
		"window_cost_limit": 60.0,
		"adaptive_throttle": true,
	}, call.updates.Extra, "只合并档位相关的 4 个键，绝不整体覆盖 extra")

	// 不碰其它列。
	require.Nil(t, call.updates.Name)
	require.Nil(t, call.updates.Priority)
	require.Nil(t, call.updates.Status)
	require.Nil(t, call.updates.Schedulable)
	require.Empty(t, call.updates.Credentials)
}

// 动态降速写显式布尔：JSONB 合并删不了键，模板关掉降速时必须写 false，
// 否则存量账号上旧的 true 会一直留着。
func TestApplyTierToExistingProviderAccounts_WritesExplicitFalseForNonAdaptiveTier(t *testing.T) {
	repo := &applyTierRepo{byTier: map[string][]Account{"7": {{ID: 1}}}}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), ResolvedProviderTier{
		Tier: "7", Concurrency: 2, MaxSessions: 0, BaseRPM: 0, WindowCostLimit: 0, AdaptiveThrottle: boolPtr(false),
	})
	require.NoError(t, err)

	require.Len(t, repo.bulkCalls, 1)
	extra := repo.bulkCalls[0].updates.Extra
	require.Contains(t, extra, "adaptive_throttle")
	require.Equal(t, false, extra["adaptive_throttle"])
	// 0 = 该限制关闭，同样显式写 0，把存量账号上旧的限制清掉。
	require.Equal(t, 0, extra["max_sessions"])
	require.Equal(t, 0, extra["base_rpm"])
	require.Equal(t, 0.0, extra["window_cost_limit"])
}

// 不定义动态降速的档位（nil）不写这个键，存量账号上已有的值原样保留。
func TestApplyTierToExistingProviderAccounts_TierWithoutAdaptiveDefinitionLeavesFlagAlone(t *testing.T) {
	repo := &applyTierRepo{byTier: map[string][]Account{"8": {{ID: 1}}}}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), ResolvedProviderTier{
		Tier: "8", Concurrency: 2, MaxSessions: 2, BaseRPM: 20, WindowCostLimit: 40,
	})
	require.NoError(t, err)

	require.Len(t, repo.bulkCalls, 1)
	require.NotContains(t, repo.bulkCalls[0].updates.Extra, "adaptive_throttle")
	require.Equal(t, 2, repo.bulkCalls[0].updates.Extra["max_sessions"])
}

func TestApplyTierToExistingProviderAccounts_NoMatchedAccountsDoesNothing(t *testing.T) {
	repo := &applyTierRepo{byTier: map[string][]Account{}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"))
	require.NoError(t, err)
	require.Equal(t, ProviderTierApplyResult{}, result)
	require.Empty(t, repo.bulkCalls)
}

func TestApplyTierToExistingProviderAccounts_PropagatesRepositoryErrors(t *testing.T) {
	boom := errors.New("db down")

	listFail := &adminServiceImpl{accountRepo: &applyTierRepo{listErr: boom}}
	_, err := listFail.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"))
	require.ErrorIs(t, err, boom)

	bulkFail := &adminServiceImpl{accountRepo: &applyTierRepo{
		byTier:  map[string][]Account{"3": {{ID: 1}}},
		bulkErr: boom,
	}}
	result, err := bulkFail.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"))
	require.ErrorIs(t, err, boom)
	require.Equal(t, 1, result.Matched)
	require.Equal(t, 0, result.Updated)
}
