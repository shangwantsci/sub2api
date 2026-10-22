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

// capacityBulkRepo 记录每一次 BulkUpdate 的目标 id 与补丁，用来断言「容量键只作用于
// Claude OAuth / SetupToken 账号」时到底拆成了几条 SQL、各自带了什么。
type capacityBulkRepo struct {
	accountRepoStub
	loaded []*Account
	calls  []capacityBulkCall
	// failOnCall > 0 时，第 N 次 BulkUpdate 返回错误（调用记录里仍包含这一次）。
	failOnCall int

	extraUpdates []map[string]any
}

type capacityBulkCall struct {
	ids     []int64
	updates AccountBulkUpdate
}

func (r *capacityBulkRepo) GetByIDs(_ context.Context, _ []int64) ([]*Account, error) {
	return r.loaded, nil
}

func (r *capacityBulkRepo) BulkUpdate(_ context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	r.calls = append(r.calls, capacityBulkCall{ids: append([]int64(nil), ids...), updates: updates})
	if r.failOnCall > 0 && len(r.calls) == r.failOnCall {
		return 0, errors.New("bulk update failed")
	}
	return int64(len(ids)), nil
}

func (r *capacityBulkRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.extraUpdates = append(r.extraUpdates, updates)
	return nil
}

func claudeOAuthAccount(id int64) *Account {
	return &Account{ID: id, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
}

func capacityPatch() map[string]any {
	return map[string]any{
		"max_sessions":      3,
		"base_rpm":          30,
		"window_cost_limit": 60,
		"adaptive_throttle": true,
		"privacy_mode":      "on", // 非容量键：所有账号都要写
	}
}

// 目标全是 Claude OAuth / SetupToken：仍是一条 SQL 的原子更新，补丁原样下发，没有任何跳过。
func TestBulkUpdateAccounts_CapacityKeysAllApplicable(t *testing.T) {
	repo := &capacityBulkRepo{loaded: []*Account{
		claudeOAuthAccount(1),
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeSetupToken},
	}}
	svc := &adminServiceImpl{accountRepo: repo}
	concurrency := 5

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:  []int64{1, 2},
		Concurrency: &concurrency,
		Extra:       capacityPatch(),
	})
	require.NoError(t, err)

	require.Len(t, repo.calls, 1, "全部符合资格时必须保持单条 SQL 的原子更新")
	require.Equal(t, []int64{1, 2}, repo.calls[0].ids)
	require.Equal(t, capacityPatch(), repo.calls[0].updates.Extra)
	require.Equal(t, 5, *repo.calls[0].updates.Concurrency)
	require.Equal(t, 2, result.Success)
	require.Equal(t, 0, result.Skipped)
}

// 混选了其它平台的账号：容量键只写给 Claude OAuth / SetupToken；其它账号剔除容量键、
// 其余字段照常更新（并发是账号表的列，对所有平台有效，不在跳过范围内），并计入 Skipped。
func TestBulkUpdateAccounts_CapacityKeysSkipNonClaudeOAuthAccounts(t *testing.T) {
	repo := &capacityBulkRepo{loaded: []*Account{
		claudeOAuthAccount(1),
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}, // Claude 的 API Key 账号也不适用
		{ID: 4, Platform: PlatformGemini, Type: AccountTypeAPIKey},
	}}
	svc := &adminServiceImpl{accountRepo: repo}
	concurrency := 5

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:  []int64{1, 2, 3, 4},
		Concurrency: &concurrency,
		Extra:       capacityPatch(),
	})
	require.NoError(t, err)

	require.Len(t, repo.calls, 2)
	applicable, skipped := repo.calls[0], repo.calls[1]

	require.Equal(t, []int64{1}, applicable.ids)
	require.Equal(t, capacityPatch(), applicable.updates.Extra)
	require.Equal(t, 5, *applicable.updates.Concurrency)

	require.Equal(t, []int64{2, 3, 4}, skipped.ids)
	require.Equal(t, map[string]any{"privacy_mode": "on"}, skipped.updates.Extra,
		"被跳过的账号只剔除容量键，其余 extra 照写")
	require.NotNil(t, skipped.updates.Concurrency, "并发列对所有平台都有意义，不被跳过")
	require.Equal(t, 5, *skipped.updates.Concurrency)

	require.Equal(t, 4, result.Success, "被跳过容量键的账号其它字段照常更新，仍算成功")
	require.Equal(t, 0, result.Failed)
	require.Equal(t, 3, result.Skipped)
	require.ElementsMatch(t, []int64{1, 2, 3, 4}, result.SuccessIDs)
}

// 请求里重复的 id 只算一个账号：Skipped 统计的是账号数，不是 id 个数。
func TestBulkUpdateAccounts_DuplicateIDsAreCountedOnceInSkipped(t *testing.T) {
	repo := &capacityBulkRepo{loaded: []*Account{
		claudeOAuthAccount(1),
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{2, 2, 1, 1, 2},
		Extra:      capacityPatch(),
	})
	require.NoError(t, err)

	require.Equal(t, 1, result.Skipped)
	require.Len(t, repo.calls, 2)
	require.Equal(t, []int64{1}, repo.calls[0].ids)
	require.Equal(t, []int64{2}, repo.calls[1].ids)
}

// 预取没返回的 id（例如已被删除）无从判断资格，归入符合资格的一组：
// 它们在 SQL 里本来就匹配不到行（deleted_at IS NULL），放哪组都无害，也不会被误计入 Skipped。
func TestBulkUpdateAccounts_UnknownIDsAreNotSkipped(t *testing.T) {
	repo := &capacityBulkRepo{loaded: []*Account{
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1, 2, 3},
		Extra:      capacityPatch(),
	})
	require.NoError(t, err)

	require.Len(t, repo.calls, 2)
	require.Equal(t, []int64{1, 3}, repo.calls[0].ids)
	require.Equal(t, []int64{2}, repo.calls[1].ids)
	require.Equal(t, 1, result.Skipped)
}

// 混选时是两条 SQL（不在同一个事务里）：第二条失败时接口返回错误，第一条已经生效。
// 两条都是幂等的 JSONB 合并，原样重试同一个请求即可恢复——这里钉住这个已知行为。
func TestBulkUpdateAccounts_SecondStatementFailureSurfacesTheError(t *testing.T) {
	repo := &capacityBulkRepo{
		loaded: []*Account{
			claudeOAuthAccount(1),
			{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		},
		failOnCall: 2,
	}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1, 2},
		Extra:      capacityPatch(),
	})
	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, repo.calls, 2, "第一条（符合资格的账号）已经发出")
	require.Equal(t, []int64{1}, repo.calls[0].ids)
}

// 补丁里只有容量键、又没有别的字段时，被跳过的账号没有任何东西要写：不发第二条 SQL。
func TestBulkUpdateAccounts_CapacityOnlyPatchSendsNothingToSkippedAccounts(t *testing.T) {
	repo := &capacityBulkRepo{loaded: []*Account{
		claudeOAuthAccount(1),
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1, 2},
		Extra:      map[string]any{"base_rpm": 30, "adaptive_throttle": true},
	})
	require.NoError(t, err)

	require.Len(t, repo.calls, 1)
	require.Equal(t, []int64{1}, repo.calls[0].ids)
	require.Equal(t, 1, result.Skipped)
	require.Equal(t, 2, result.Success)
}

// 目标里一个符合资格的都没有：只发一条给被跳过的账号（不带容量键）。
func TestBulkUpdateAccounts_CapacityKeysAllSkipped(t *testing.T) {
	repo := &capacityBulkRepo{loaded: []*Account{
		{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{7},
		Extra:      capacityPatch(),
	})
	require.NoError(t, err)

	require.Len(t, repo.calls, 1)
	require.Equal(t, []int64{7}, repo.calls[0].ids)
	require.Equal(t, map[string]any{"privacy_mode": "on"}, repo.calls[0].updates.Extra)
	require.Equal(t, 1, result.Skipped)
}

// 补丁里没有任何容量键：不预取账号、不拆分，行为与改动前完全一致。
func TestBulkUpdateAccounts_NoCapacityKeysKeepsSingleUpdateAndNoSkips(t *testing.T) {
	repo := &capacityBulkRepo{loaded: []*Account{{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}}
	svc := &adminServiceImpl{accountRepo: repo}
	concurrency := 9

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:  []int64{1, 2},
		Concurrency: &concurrency,
		Extra:       map[string]any{"privacy_mode": "on"},
	})
	require.NoError(t, err)

	require.Len(t, repo.calls, 1)
	require.Equal(t, []int64{1, 2}, repo.calls[0].ids)
	require.Equal(t, 0, result.Skipped)
}

// 仅批量改并发（没有任何容量 extra 键）对所有平台生效，不被「容量键只作用于 Claude」误伤。
func TestBulkUpdateAccounts_ConcurrencyAloneAppliesToEveryPlatform(t *testing.T) {
	repo := &capacityBulkRepo{loaded: []*Account{{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}}
	svc := &adminServiceImpl{accountRepo: repo}
	concurrency := 12

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:  []int64{2},
		Concurrency: &concurrency,
	})
	require.NoError(t, err)

	require.Len(t, repo.calls, 1)
	require.Equal(t, 12, *repo.calls[0].updates.Concurrency)
	require.Equal(t, 0, result.Skipped)
}

// 容量开关必须是严格 bool：字符串 "true" 会被 getExtraBool 静默当成 false，所以在入口直接拒绝（400），
// 而且在任何写操作之前拒绝。
func TestBulkUpdateAccounts_RejectsNonBooleanCapacitySwitches(t *testing.T) {
	for _, key := range []string{"adaptive_throttle", "nurturing_enabled"} {
		t.Run(key, func(t *testing.T) {
			repo := &capacityBulkRepo{loaded: []*Account{claudeOAuthAccount(1)}}
			svc := &adminServiceImpl{accountRepo: repo}

			_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
				AccountIDs: []int64{1},
				Extra:      map[string]any{key: "true"},
			})
			require.Error(t, err)
			require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
			require.Empty(t, repo.calls, "校验失败必须发生在任何写入之前")
		})
	}
}

func TestUpdateAccountExtra_RejectsNonBooleanCapacitySwitches(t *testing.T) {
	repo := &capacityBulkRepo{}
	svc := &adminServiceImpl{accountRepo: repo}

	err := svc.UpdateAccountExtra(context.Background(), 1, map[string]any{"adaptive_throttle": "true"})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Empty(t, repo.extraUpdates)

	// 严格 bool（含显式 false，这是「关闭」的唯一写法）放行。
	require.NoError(t, svc.UpdateAccountExtra(context.Background(), 1, map[string]any{
		"adaptive_throttle": false, "nurturing_enabled": true, "base_rpm": 30,
	}))
	require.Len(t, repo.extraUpdates, 1)
}

func TestBuildAccountForCreate_RejectsNonBooleanCapacitySwitches(t *testing.T) {
	input := &CreateAccountInput{Name: "n", Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	_, err := buildAccountForCreate(input, map[string]any{"nurturing_enabled": "true"})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))

	account, err := buildAccountForCreate(input, map[string]any{"adaptive_throttle": true, "nurturing_enabled": false})
	require.NoError(t, err)
	require.True(t, account.IsAdaptiveThrottleEnabled())
	require.False(t, account.IsNurturingEnabled())
}

// 整体写入的 UpdateAccount 路径同样校验：它是前端编辑弹窗保存时走的路径。
func TestUpdateAccount_RejectsNonBooleanCapacitySwitches(t *testing.T) {
	repo := &updateAccountCredsRepoStub{account: &Account{
		ID: 11, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive,
		Extra: map[string]any{"base_rpm": 30},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.UpdateAccount(context.Background(), 11, &UpdateAccountInput{
		Extra: map[string]any{"adaptive_throttle": "true"},
	})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Zero(t, repo.updateCalls, "校验失败时不能落库")
}

func TestUpdateAccount_PersistsStrictCapacitySwitches(t *testing.T) {
	repo := &updateAccountCredsRepoStub{account: &Account{
		ID: 12, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive,
		Extra: map[string]any{"base_rpm": 30},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.UpdateAccount(context.Background(), 12, &UpdateAccountInput{
		Extra: map[string]any{"base_rpm": 30, "adaptive_throttle": true, "nurturing_enabled": false},
	})
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.True(t, repo.account.IsAdaptiveThrottleEnabled())
	require.Equal(t, false, repo.account.Extra["nurturing_enabled"], "关闭写显式 false，而不是删键")
}
