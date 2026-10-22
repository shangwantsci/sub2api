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

// providerCascadeRepo 是带状态的账号仓库替身：BulkUpdate 的语义与真实仓库一致
// （schedulable 直接覆盖，extra 是 JSONB 顶层合并），这样「停用 → 启用」这类多步流程
// 才能用最终账号状态来断言，而不是只看调用记录。
type providerCascadeRepo struct {
	accountRepoStub
	accounts  []Account
	listCalls []int64
	bulkCalls []capacityBulkCall
	listErr   error
	bulkErr   error
}

func (r *providerCascadeRepo) ListByProvider(_ context.Context, providerUserID int64) ([]Account, error) {
	r.listCalls = append(r.listCalls, providerUserID)
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]Account, len(r.accounts))
	copy(out, r.accounts)
	return out, nil
}

func (r *providerCascadeRepo) BulkUpdate(_ context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	r.bulkCalls = append(r.bulkCalls, capacityBulkCall{ids: append([]int64(nil), ids...), updates: updates})
	if r.bulkErr != nil {
		return 0, r.bulkErr
	}
	var rows int64
	for i := range r.accounts {
		hit := false
		for _, id := range ids {
			hit = hit || id == r.accounts[i].ID
		}
		if !hit {
			continue
		}
		rows++
		if updates.Schedulable != nil {
			r.accounts[i].Schedulable = *updates.Schedulable
		}
		if len(updates.Extra) > 0 {
			merged := make(map[string]any, len(r.accounts[i].Extra)+len(updates.Extra))
			for k, v := range r.accounts[i].Extra {
				merged[k] = v
			}
			for k, v := range updates.Extra {
				merged[k] = v
			}
			r.accounts[i].Extra = merged
		}
	}
	return rows, nil
}

func (r *providerCascadeRepo) account(id int64) Account {
	for _, a := range r.accounts {
		if a.ID == id {
			return a
		}
	}
	panic("unknown account in test")
}

const cascadeProviderID = int64(42)

// providerAccountsFixture 是一个供号商名下四种状态的账号：
//
//	1 正常在跑            2 管理员事先手动暂停的   3 status=error（本来就不参与调度）   4 正常在跑（带别的 extra）
func providerAccountsFixture() []Account {
	owner := cascadeProviderID
	return []Account{
		{ID: 1, Name: "live-1", Status: StatusActive, Schedulable: true, ProviderUserID: &owner},
		{ID: 2, Name: "manually-paused", Status: StatusActive, Schedulable: false, ProviderUserID: &owner},
		{ID: 3, Name: "errored", Status: StatusError, Schedulable: true, ProviderUserID: &owner},
		{ID: 4, Name: "live-2", Status: StatusActive, Schedulable: true, ProviderUserID: &owner,
			Extra: map[string]any{"base_rpm": 30}},
	}
}

type providerCascadeFixture struct {
	svc         *adminServiceImpl
	users       *rpmUserRepoStub
	accounts    *providerCascadeRepo
	invalidator *authCacheInvalidatorStub
}

func newProviderCascadeFixture(user *User) *providerCascadeFixture {
	users := &rpmUserRepoStub{userRepoStub: &userRepoStub{user: user}}
	accounts := &providerCascadeRepo{accounts: providerAccountsFixture()}
	invalidator := &authCacheInvalidatorStub{}
	return &providerCascadeFixture{
		svc: &adminServiceImpl{
			userRepo:             users,
			accountRepo:          accounts,
			redeemCodeRepo:       &redeemRepoStub{},
			authCacheInvalidator: invalidator,
		},
		users:       users,
		accounts:    accounts,
		invalidator: invalidator,
	}
}

func newActiveProviderFixture() *providerCascadeFixture {
	return newProviderCascadeFixture(&User{
		ID: cascadeProviderID, Email: "provider@example.com", Role: RoleUser, IsProvider: true, Status: StatusActive,
	})
}

func (f *providerCascadeFixture) setStatus(status string) error {
	_, err := f.svc.UpdateUser(context.Background(), cascadeProviderID, &UpdateUserInput{Status: status})
	return err
}

func requireSuspended(t *testing.T, a Account) {
	t.Helper()
	require.False(t, a.Schedulable, "account %d 应被暂停", a.ID)
	require.Equal(t, true, a.Extra["provider_suspended"], "account %d 应带暂停标记", a.ID)
}

// 停用供号商：只暂停 active 且可调度的账号并打标记；手动暂停的、error 的原样不动。
func TestUpdateUser_DisablingProviderSuspendsOnlyActiveSchedulableAccounts(t *testing.T) {
	f := newActiveProviderFixture()

	require.NoError(t, f.setStatus(StatusDisabled))

	require.NotNil(t, f.users.lastUpdated)
	require.Equal(t, StatusDisabled, f.users.lastUpdated.Status, "用户状态照常落库")

	require.Equal(t, []int64{cascadeProviderID}, f.accounts.listCalls)
	require.Len(t, f.accounts.bulkCalls, 1, "一条 SQL")
	call := f.accounts.bulkCalls[0]
	require.Equal(t, []int64{1, 4}, call.ids)
	require.NotNil(t, call.updates.Schedulable)
	require.False(t, *call.updates.Schedulable)
	require.Equal(t, map[string]any{"provider_suspended": true}, call.updates.Extra)
	require.Nil(t, call.updates.Status, "只暂停调度，不改账号 status")
	require.Nil(t, call.updates.Concurrency)
	require.Nil(t, call.updates.Priority)
	require.Empty(t, call.updates.Credentials)

	requireSuspended(t, f.accounts.account(1))
	requireSuspended(t, f.accounts.account(4))
	require.Equal(t, 30, f.accounts.account(4).Extra["base_rpm"], "标记是 extra 的顶层合并，不覆盖原有键")

	manual := f.accounts.account(2)
	require.False(t, manual.Schedulable)
	require.NotContains(t, manual.Extra, "provider_suspended", "手动暂停的账号不打标记")
	errored := f.accounts.account(3)
	require.True(t, errored.Schedulable, "error 账号本来就不参与调度，不碰")
	require.NotContains(t, errored.Extra, "provider_suspended")
}

// 再启用：只恢复带标记的账号并清除标记。
func TestUpdateUser_EnablingProviderResumesOnlyMarkedAccounts(t *testing.T) {
	f := newActiveProviderFixture()
	require.NoError(t, f.setStatus(StatusDisabled))
	f.accounts.bulkCalls = nil

	require.NoError(t, f.setStatus(StatusActive))

	require.Equal(t, StatusActive, f.users.lastUpdated.Status)
	require.Len(t, f.accounts.bulkCalls, 1)
	call := f.accounts.bulkCalls[0]
	require.Equal(t, []int64{1, 4}, call.ids, "只有当初被停用级联暂停的账号")
	require.NotNil(t, call.updates.Schedulable)
	require.True(t, *call.updates.Schedulable)
	require.Equal(t, map[string]any{"provider_suspended": false}, call.updates.Extra, "JSONB 合并删不了键，写显式 false")

	for _, id := range []int64{1, 4} {
		a := f.accounts.account(id)
		require.True(t, a.Schedulable)
		require.Equal(t, false, a.Extra["provider_suspended"])
	}
	require.Equal(t, 30, f.accounts.account(4).Extra["base_rpm"])
	// error 账号从头到尾没被碰过。
	require.True(t, f.accounts.account(3).Schedulable)
	require.NotContains(t, f.accounts.account(3).Extra, "provider_suspended")
}

// 管理员在停用之前就手动暂停的账号，启用后仍然保持暂停。
func TestUpdateUser_ManuallyPausedAccountStaysPausedAfterReEnable(t *testing.T) {
	f := newActiveProviderFixture()
	require.NoError(t, f.setStatus(StatusDisabled))
	require.NoError(t, f.setStatus(StatusActive))

	manual := f.accounts.account(2)
	require.False(t, manual.Schedulable, "手动暂停的账号不能被「启用供号商」顺手恢复")
	require.NotContains(t, manual.Extra, "provider_suspended")
	for _, call := range f.accounts.bulkCalls {
		require.NotContains(t, call.ids, int64(2), "任何一次批量更新都不应包含这个账号")
	}
}

// 非供号商用户：无论怎么改状态都不碰账号表。
func TestUpdateUser_NonProviderUserLeavesAccountsAlone(t *testing.T) {
	f := newProviderCascadeFixture(&User{ID: cascadeProviderID, Email: "u@example.com", Role: RoleUser, Status: StatusActive})

	require.NoError(t, f.setStatus(StatusDisabled))
	require.NoError(t, f.setStatus(StatusActive))

	require.Empty(t, f.accounts.listCalls)
	require.Empty(t, f.accounts.bulkCalls)
	require.Equal(t, StatusActive, f.users.lastUpdated.Status, "用户状态本身的修改照常生效")
}

// 请求没带 status（只改别的字段）：不动账号，即便用户当前正处于 disabled。
func TestUpdateUser_RequestWithoutStatusLeavesAccountsAlone(t *testing.T) {
	for _, current := range []string{StatusActive, StatusDisabled} {
		f := newProviderCascadeFixture(&User{
			ID: cascadeProviderID, Email: "provider@example.com", Role: RoleUser, IsProvider: true, Status: current,
		})

		newName := "renamed"
		_, err := f.svc.UpdateUser(context.Background(), cascadeProviderID, &UpdateUserInput{Username: &newName})
		require.NoError(t, err)

		require.Empty(t, f.accounts.listCalls, "current=%s", current)
		require.Empty(t, f.accounts.bulkCalls, "current=%s", current)
	}
}

// active → active 不是状态变更，不触发恢复。
func TestUpdateUser_ProviderStaysActiveLeavesAccountsAlone(t *testing.T) {
	f := newActiveProviderFixture()

	require.NoError(t, f.setStatus(StatusActive))

	require.Empty(t, f.accounts.listCalls)
	require.Empty(t, f.accounts.bulkCalls)
}

// 管理员账号：原有「不能停用管理员」保护不变，且 is_provider 误置在管理员身上也不触发级联。
func TestUpdateUser_AdminIsNeverCascaded(t *testing.T) {
	f := newProviderCascadeFixture(&User{
		ID: cascadeProviderID, Email: "admin@example.com", Role: RoleAdmin, IsProvider: true, Status: StatusActive,
	})

	err := f.setStatus(StatusDisabled)
	require.EqualError(t, err, "cannot disable admin user")
	require.Nil(t, f.users.lastUpdated)
	require.Empty(t, f.accounts.listCalls)

	require.NoError(t, f.setStatus(StatusActive))
	require.Empty(t, f.accounts.listCalls, "管理员不算供号商")
	require.Empty(t, f.accounts.bulkCalls)
}

// 用户表写入失败时不应级联：级联只发生在 userRepo.Update 成功之后。
func TestUpdateUser_NoCascadeWhenUserUpdateFails(t *testing.T) {
	boom := errors.New("db down")
	accounts := &providerCascadeRepo{accounts: providerAccountsFixture()}
	svc := &adminServiceImpl{
		userRepo: &balanceUserRepoStub{
			userRepoStub: &userRepoStub{user: &User{
				ID: cascadeProviderID, Role: RoleUser, IsProvider: true, Status: StatusActive,
			}},
			updateErr: boom,
		},
		accountRepo:    accounts,
		redeemCodeRepo: &redeemRepoStub{},
	}

	_, err := svc.UpdateUser(context.Background(), cascadeProviderID, &UpdateUserInput{Status: StatusDisabled})

	require.ErrorIs(t, err, boom)
	require.Empty(t, accounts.listCalls)
	require.Empty(t, accounts.bulkCalls)
}

// 重复提交 disabled 是幂等的：无状态、可重试，已经暂停的账号不会再次被写。
func TestUpdateUser_ResendingDisabledIsIdempotent(t *testing.T) {
	f := newActiveProviderFixture()
	require.NoError(t, f.setStatus(StatusDisabled))
	require.Len(t, f.accounts.bulkCalls, 1)
	before := append([]Account(nil), f.accounts.accounts...)

	require.NoError(t, f.setStatus(StatusDisabled))

	require.Len(t, f.accounts.listCalls, 2, "旧状态已是 disabled 也要重新检查，这是「可重试」的前提")
	require.Len(t, f.accounts.bulkCalls, 1, "没有新的可暂停账号，不再发 SQL")
	require.Equal(t, before, f.accounts.accounts)
}

// 级联失败：返回错误（用户状态已落库、错误信息说明这一点），修好之后再提交一次 disabled 就能补上。
func TestUpdateUser_SuspendFailureIsReportedAndRetryCompletesIt(t *testing.T) {
	boom := errors.New("bulk update failed")
	f := newActiveProviderFixture()
	f.accounts.bulkErr = boom

	user, err := f.svc.UpdateUser(context.Background(), cascadeProviderID, &UpdateUserInput{Status: StatusDisabled})

	require.Error(t, err)
	require.ErrorIs(t, err, boom, "底层错误挂在 cause 上，便于日志排查")
	appErr := infraerrors.FromError(err)
	require.Equal(t, int32(http.StatusInternalServerError), appErr.Code)
	require.Equal(t, "PROVIDER_ACCOUNTS_SUSPEND_FAILED", appErr.Reason)
	require.Contains(t, appErr.Message, "状态已更新")
	require.Contains(t, appErr.Message, "重试")
	require.NotNil(t, user, "用户本身的修改已生效，user 随错误一并返回")
	require.Equal(t, StatusDisabled, f.users.lastUpdated.Status, "用户状态此时已落库")
	require.True(t, f.accounts.account(1).Schedulable, "账号没被暂停")

	// 管理员修好之后再发一次 disabled。
	f.accounts.bulkErr = nil
	require.NoError(t, f.setStatus(StatusDisabled))
	requireSuspended(t, f.accounts.account(1))
	requireSuspended(t, f.accounts.account(4))
}

func TestUpdateUser_ListFailureIsAlsoReported(t *testing.T) {
	f := newActiveProviderFixture()
	f.accounts.listErr = errors.New("db down")

	_, err := f.svc.UpdateUser(context.Background(), cascadeProviderID, &UpdateUserInput{Status: StatusDisabled})

	require.Error(t, err)
	require.Equal(t, "PROVIDER_ACCOUNTS_SUSPEND_FAILED", infraerrors.Reason(err))
	require.Equal(t, StatusDisabled, f.users.lastUpdated.Status)
	require.Empty(t, f.accounts.bulkCalls)
}

// 恢复失败同样返回错误；按错误信息里的办法（先再停用、再启用）能补上。
func TestUpdateUser_ResumeFailureIsReportedAndDisableThenEnableRecovers(t *testing.T) {
	boom := errors.New("bulk update failed")
	f := newActiveProviderFixture()
	require.NoError(t, f.setStatus(StatusDisabled))

	f.accounts.bulkErr = boom
	_, err := f.svc.UpdateUser(context.Background(), cascadeProviderID, &UpdateUserInput{Status: StatusActive})

	require.ErrorIs(t, err, boom)
	require.Equal(t, "PROVIDER_ACCOUNTS_RESUME_FAILED", infraerrors.Reason(err))
	require.Equal(t, int32(http.StatusInternalServerError), infraerrors.FromError(err).Code)
	require.Contains(t, infraerrors.Message(err), "状态已更新")
	require.Equal(t, StatusActive, f.users.lastUpdated.Status)
	requireSuspended(t, f.accounts.account(1))

	f.accounts.bulkErr = nil
	require.NoError(t, f.setStatus(StatusDisabled)) // 账号都已暂停，这一步没有可写的
	require.NoError(t, f.setStatus(StatusActive))

	for _, id := range []int64{1, 4} {
		require.True(t, f.accounts.account(id).Schedulable, "account %d", id)
		require.Equal(t, false, f.accounts.account(id).Extra["provider_suspended"])
	}
	require.False(t, f.accounts.account(2).Schedulable, "手动暂停的账号仍保持暂停")
}

// 级联失败不能让既有的善后动作被跳过：认证缓存照常失效、并发调整记录照常生成，错误留到最后返回。
func TestUpdateUser_CascadeFailureDoesNotSkipExistingSideEffects(t *testing.T) {
	f := newProviderCascadeFixture(&User{
		ID: cascadeProviderID, Email: "provider@example.com", Role: RoleUser, IsProvider: true,
		Status: StatusActive, Concurrency: 1,
	})
	redeems := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
	f.svc.redeemCodeRepo = redeems
	f.accounts.bulkErr = errors.New("bulk update failed")

	newConcurrency := 5
	_, err := f.svc.UpdateUser(context.Background(), cascadeProviderID, &UpdateUserInput{
		Status: StatusDisabled, Concurrency: &newConcurrency,
	})

	require.Equal(t, "PROVIDER_ACCOUNTS_SUSPEND_FAILED", infraerrors.Reason(err))
	require.Equal(t, []int64{cascadeProviderID}, f.invalidator.userIDs, "认证缓存仍要失效")
	require.Len(t, redeems.created, 1, "并发调整记录仍要生成")
	require.Equal(t, float64(4), redeems.created[0].Value)
}

// 暂停标记是纯内部状态：供号商不能提交（SanitizeProviderExtra 会丢弃它），
// 也不是容量键（进了 capacityExtraKeys，批量更新会把它从补丁里剔除）。
func TestProviderSuspendedMarkerStaysOutOfSubmittableAndCapacityKeyLists(t *testing.T) {
	require.NotContains(t, providerSubmittableExtraKeys, "provider_suspended")
	require.NotContains(t, capacityExtraKeys, "provider_suspended")

	patch := map[string]any{"provider_suspended": true}
	stripped, removed := StripCapacityExtra(patch)
	require.False(t, removed)
	require.Equal(t, patch, stripped)

	kept, dropped := SanitizeProviderExtra(patch)
	require.Empty(t, kept)
	require.Equal(t, []string{"provider_suspended"}, dropped)
}
