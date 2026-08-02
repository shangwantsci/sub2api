//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// MinPriorityByGroup 必须只统计**当前可调度**的账号。
//
// 供号商上号时按它对齐 priority，而调度里的 filterByMinPriority 是从已经过状态
// 过滤的可调度候选里取最小值。两者口径一旦不一致，后果是单向且完全静默的：
//
//	分组里有个 priority=1 的号被暂停了，在跑的号全是 priority=5。
//	若这里把暂停的号也算进来，新号会对齐到 1 —— 于是新号成为分组内唯一
//	priority=1 的可调度账号，filterByMinPriority 只保留它，
//	其余 priority=5 的号一个请求都拿不到，账号状态显示正常、用量恒为 0。
//
// 而这是可以被主动构造的：先 pause 一个号，再上新号。
func TestMinPriorityByGroupCountsOnlySchedulableAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newAccountRepositoryWithSQL(client, tx, nil)

	group := mustCreateGroup(t, client, &service.Group{Name: "g-min-priority"})

	// 在跑的账号：priority 5。
	running := mustCreateAccount(t, client, &service.Account{Name: "min-priority-running", Schedulable: true})
	_, err := client.Account.UpdateOneID(running.ID).SetPriority(5).Save(ctx)
	require.NoError(t, err)

	// 被暂停的账号：priority 1。数值更小，但它拿不到任何请求，
	// 不该成为其它账号对齐的门槛。
	paused := mustCreateAccount(t, client, &service.Account{Name: "min-priority-paused", Schedulable: false})
	_, err = client.Account.UpdateOneID(paused.ID).SetPriority(1).Save(ctx)
	require.NoError(t, err)

	// status=error 的账号：priority 2。同样不可调度。
	errored := mustCreateAccount(t, client, &service.Account{Name: "min-priority-error", Schedulable: true})
	_, err = client.Account.UpdateOneID(errored.ID).SetPriority(2).SetStatus(service.StatusError).Save(ctx)
	require.NoError(t, err)

	for _, id := range []int64{running.ID, paused.ID, errored.ID} {
		require.NoError(t, repo.BindGroups(ctx, id, []int64{group.ID}))
	}

	byGroup, err := repo.MinPriorityByGroup(ctx)
	require.NoError(t, err)
	require.Equal(t, 5, byGroup[group.ID],
		"必须取可调度账号的最小 priority(5)，而不是被暂停/报错账号的 1 或 2")
}

// 分组里一个可调度账号都没有时，该分组不应出现在结果里。
//
// 调用方（resolveGroupMinPriority）据此判定 ok=false 并回落设置里的默认优先级，
// 这是正确的：没有在跑的账号就没有需要对齐的门槛。
func TestMinPriorityByGroupOmitsGroupsWithNoSchedulableAccount(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newAccountRepositoryWithSQL(client, tx, nil)

	group := mustCreateGroup(t, client, &service.Group{Name: "g-all-paused"})

	paused := mustCreateAccount(t, client, &service.Account{Name: "all-paused-1", Schedulable: false})
	_, err := client.Account.UpdateOneID(paused.ID).SetPriority(1).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.BindGroups(ctx, paused.ID, []int64{group.ID}))

	byGroup, err := repo.MinPriorityByGroup(ctx)
	require.NoError(t, err)
	_, found := byGroup[group.ID]
	require.False(t, found, "全员不可调度的分组不该出现在结果里")
}
