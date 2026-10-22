//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type applyTierRepo struct {
	accountRepoStub
	byTier    map[string][]Account
	bulkCalls []capacityBulkCall
	bulkErr   error
	listErr   error
	// bulkRows 非 nil 时覆盖 BulkUpdate 返回的「实际更新行数」。
	bulkRows *int64
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
	if r.bulkRows != nil {
		return *r.bulkRows, nil
	}
	return int64(len(ids)), nil
}

// statefulTierRepo 让 BulkUpdate 真的改写内存里的账号（并发列、load_factor 列、extra 顶层合并），
// 用来验证「应用之后再算一次计划必须为空」。
type statefulTierRepo struct {
	accountRepoStub
	accounts []Account
}

func (r *statefulTierRepo) ListByProviderTier(context.Context, string) ([]Account, error) {
	return append([]Account(nil), r.accounts...), nil
}

func (r *statefulTierRepo) BulkUpdate(_ context.Context, ids []int64, u AccountBulkUpdate) (int64, error) {
	var rows int64
	for i := range r.accounts {
		a := &r.accounts[i]
		hit := false
		for _, id := range ids {
			hit = hit || id == a.ID
		}
		if !hit {
			continue
		}
		rows++
		if u.Concurrency != nil {
			a.Concurrency = *u.Concurrency
		}
		if u.LoadFactor != nil {
			lf := *u.LoadFactor
			a.LoadFactor = &lf
		}
		merged := make(map[string]any, len(a.Extra)+len(u.Extra))
		for k, v := range a.Extra {
			merged[k] = v
		}
		for k, v := range u.Extra {
			merged[k] = v
		}
		a.Extra = merged
	}
	return rows, nil
}

func resolvedSeedTier(t *testing.T, key string) ResolvedProviderTier {
	t.Helper()
	settings := defaultProviderSettings()
	resolved, err := ResolveProviderTier(settings, key, nil)
	require.NoError(t, err)
	return resolved
}

// 「应用到存量」改走 BulkUpdate 的增量写：一条 SQL 覆盖全部将改动的账号，
// 并发列与 load_factor 一起写成档位并发，extra 只合并 4 个键，其余持久设置原样保留。
func TestApplyTierToExistingProviderAccounts_UsesBulkUpdateWithFourExtraKeys(t *testing.T) {
	repo := &applyTierRepo{byTier: map[string][]Account{"3": {{ID: 11}, {ID: 12}}}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"), 2)
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

// 只写「确实会变」的账号：各项（含有效负载因子）已经与模板一致的账号一行 SQL 都不碰。
// 其余各项一致、只有负载因子被手调过的账号也在变更范围内（应用会把它重置成档位并发）。
func TestApplyTierToExistingProviderAccounts_OnlyWritesAccountsThatWouldChange(t *testing.T) {
	aligned := 3
	inSyncExplicit := tier3Account(1)
	inSyncExplicit.LoadFactor = &aligned // 显式写成并发（上号与应用都是这样写的），仍然一致

	diffConcurrency := tier3Account(2)
	diffConcurrency.Concurrency = 10
	diffAdaptive := withExtra(tier3Account(3), map[string]any{"adaptive_throttle": false})
	handTuned := 99
	onlyLoadFactor := tier3Account(5)
	onlyLoadFactor.LoadFactor = &handTuned

	repo := &applyTierRepo{byTier: map[string][]Account{
		"3": {inSyncExplicit, diffConcurrency, diffAdaptive, tier3Account(4), onlyLoadFactor},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"), 3)
	require.NoError(t, err)
	require.Equal(t, ProviderTierApplyResult{Matched: 5, Updated: 3}, result)

	require.Len(t, repo.bulkCalls, 1)
	require.Equal(t, []int64{2, 3, 5}, repo.bulkCalls[0].ids, "只对将改动的账号 id 写入，不是全部命中的账号")
	require.Equal(t, 3, *repo.bulkCalls[0].updates.LoadFactor)
}

// 预览与应用共用同一份计划：预览说会改哪几个账号，应用就恰好写这几个，
// 数量也与预览的 will_change 一致（包括「只有负载因子被手调」的账号）。
func TestPreviewAndApplyAgreeOnWhichAccountsChange(t *testing.T) {
	handTuned, aligned := 99, 3
	onlyLoadFactor := tier3Account(40)
	onlyLoadFactor.LoadFactor = &handTuned
	inSyncExplicit := tier3Account(10)
	inSyncExplicit.LoadFactor = &aligned
	diffConcurrency := tier3Account(30)
	diffConcurrency.Concurrency = 7
	accounts := []Account{
		onlyLoadFactor,
		tier3Account(20),
		diffConcurrency,
		inSyncExplicit,
		withExtra(tier3Account(50), map[string]any{"window_cost_limit": "75"}),
		{ID: 60, Name: "bare"},
	}
	repo := &applyTierRepo{byTier: map[string][]Account{"3": accounts}}
	svc := &adminServiceImpl{accountRepo: repo}
	tier := resolvedSeedTier(t, "3")

	preview, err := svc.PreviewTierApplication(context.Background(), tier)
	require.NoError(t, err)
	previewIDs := make([]int64, 0, len(preview.Samples))
	for _, sample := range preview.Samples {
		previewIDs = append(previewIDs, sample.AccountID)
	}
	require.Equal(t, []int64{30, 40, 50, 60}, previewIDs)

	result, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), tier, preview.WillChange)
	require.NoError(t, err)

	require.Len(t, repo.bulkCalls, 1)
	require.Equal(t, previewIDs, repo.bulkCalls[0].ids)
	require.Equal(t, preview.WillChange, result.Updated)
	require.Equal(t, preview.Matched, result.Matched)
}

// 确认的数字与服务端重算的不一致（预览之后账号被改过，或模板又变了）：409 且什么都不写。
func TestApplyTierToExistingProviderAccounts_RejectsMismatchedExpectedCount(t *testing.T) {
	changed1 := tier3Account(1)
	changed1.Concurrency = 10
	changed2 := tier3Account(2)
	changed2.Concurrency = 10
	accounts := []Account{changed1, changed2, tier3Account(3)} // 将改动 2 个

	for _, expected := range []int{0, 1, 3, 4, 100} {
		repo := &applyTierRepo{byTier: map[string][]Account{"3": accounts}}
		svc := &adminServiceImpl{accountRepo: repo}

		result, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"), expected)

		require.Error(t, err, "expected=%d", expected)
		require.True(t, infraerrors.IsConflict(err), "expected=%d err=%v", expected, err)
		appErr := infraerrors.FromError(err)
		require.Equal(t, int32(http.StatusConflict), appErr.Code)
		require.Equal(t, "PRESET_APPLY_CONFLICT", appErr.Reason)
		require.Contains(t, appErr.Message, "now 2", "message 里带上实际数量")
		require.Equal(t, map[string]string{"will_change": "2"}, appErr.Metadata)
		require.Empty(t, repo.bulkCalls, "不一致时绝不能写任何数据；expected=%d", expected)
		require.Equal(t, 3, result.Matched)
		require.Equal(t, 0, result.Updated)
	}
}

// 没有任何账号需要改：确认 0 直接返回，不发 SQL；确认了非 0 则是冲突。
func TestApplyTierToExistingProviderAccounts_NothingToChangeWritesNothing(t *testing.T) {
	repo := &applyTierRepo{byTier: map[string][]Account{"3": {tier3Account(1), tier3Account(2)}}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"), 0)
	require.NoError(t, err)
	require.Equal(t, ProviderTierApplyResult{Matched: 2, Updated: 0}, result)
	require.Empty(t, repo.bulkCalls)

	_, err = svc.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"), 1)
	require.True(t, infraerrors.IsConflict(err))
	require.Empty(t, repo.bulkCalls)
}

// updated 是仓库报告的实际更新行数（计划里的账号可能在两次读写之间被删掉），不是计划条数。
func TestApplyTierToExistingProviderAccounts_UpdatedIsTheRowCountFromTheRepository(t *testing.T) {
	rows := int64(1)
	repo := &applyTierRepo{
		byTier:   map[string][]Account{"3": {{ID: 1}, {ID: 2}}},
		bulkRows: &rows,
	}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"), 2)
	require.NoError(t, err)
	require.Equal(t, ProviderTierApplyResult{Matched: 2, Updated: 1}, result)
}

// 应用之后再算一次计划必须为空：计划里比较的每一项，应用都真的写了。
// 否则预览会永远列出一批「怎么应用都改不好」的账号。五个种子档位都过一遍（含 5h 额度为 0 的旗舰档）。
func TestApplyTierToExistingProviderAccounts_ConvergesToAnEmptyPlan(t *testing.T) {
	for _, key := range []string{"1", "2", "3", "4", "5"} {
		t.Run(key, func(t *testing.T) {
			handTunedLoadFactor := 99
			repo := &statefulTierRepo{accounts: []Account{
				{ID: 1, Concurrency: 9, Extra: map[string]any{
					"max_sessions": "7", "base_rpm": 99.0, "window_cost_limit": "12.5", "adaptive_throttle": "true",
				}},
				{ID: 2},
				{
					ID: 3, Concurrency: 1, LoadFactor: &handTunedLoadFactor,
					Extra: map[string]any{"adaptive_throttle": true, "nurturing_enabled": true},
				},
			}}
			svc := &adminServiceImpl{accountRepo: repo}
			tier := resolvedSeedTier(t, key)
			require.Len(t, PlanPresetApplication(tier, repo.accounts), 3, "三个账号起初都与模板不一致")

			result, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), tier, 3)
			require.NoError(t, err)
			require.Equal(t, ProviderTierApplyResult{Matched: 3, Updated: 3}, result)

			require.Empty(t, PlanPresetApplication(tier, repo.accounts), "应用之后账号与模板完全一致")
			for _, a := range repo.accounts {
				require.NotNil(t, a.LoadFactor)
				require.Equal(t, tier.Concurrency, *a.LoadFactor, "account %d：load_factor 与并发对齐", a.ID)
			}
			require.Equal(t, true, repo.accounts[2].Extra["nurturing_enabled"], "只合并 4 个键，其它持久设置原样保留")

			// 再应用一次是空操作：确认 0 即可，不发 SQL 也不冲突。
			again, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), tier, 0)
			require.NoError(t, err)
			require.Equal(t, ProviderTierApplyResult{Matched: 3, Updated: 0}, again)
		})
	}
}

// 动态降速写显式布尔：JSONB 合并删不了键，模板关掉降速时必须写 false，
// 否则存量账号上旧的 true 会一直留着。
func TestApplyTierToExistingProviderAccounts_WritesExplicitFalseForNonAdaptiveTier(t *testing.T) {
	repo := &applyTierRepo{byTier: map[string][]Account{"7": {{ID: 1}}}}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), ResolvedProviderTier{
		Tier: "7", Concurrency: 2, MaxSessions: 0, BaseRPM: 0, WindowCostLimit: 0, AdaptiveThrottle: boolPtr(false),
	}, 1)
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
	}, 1)
	require.NoError(t, err)

	require.Len(t, repo.bulkCalls, 1)
	require.NotContains(t, repo.bulkCalls[0].updates.Extra, "adaptive_throttle")
	require.Equal(t, 2, repo.bulkCalls[0].updates.Extra["max_sessions"])
}

func TestApplyTierToExistingProviderAccounts_NoMatchedAccountsDoesNothing(t *testing.T) {
	repo := &applyTierRepo{byTier: map[string][]Account{}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"), 0)
	require.NoError(t, err)
	require.Equal(t, ProviderTierApplyResult{}, result)
	require.Empty(t, repo.bulkCalls)
}

func TestApplyTierToExistingProviderAccounts_PropagatesRepositoryErrors(t *testing.T) {
	boom := errors.New("db down")

	listFail := &adminServiceImpl{accountRepo: &applyTierRepo{listErr: boom}}
	_, err := listFail.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"), 0)
	require.ErrorIs(t, err, boom)

	bulkFail := &adminServiceImpl{accountRepo: &applyTierRepo{
		byTier:  map[string][]Account{"3": {{ID: 1}}},
		bulkErr: boom,
	}}
	result, err := bulkFail.ApplyTierToExistingProviderAccounts(context.Background(), resolvedSeedTier(t, "3"), 1)
	require.ErrorIs(t, err, boom)
	require.Equal(t, 1, result.Matched)
	require.Equal(t, 0, result.Updated)
}
