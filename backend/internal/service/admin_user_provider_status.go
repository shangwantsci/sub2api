package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// 供号商被停用 / 启用时，其名下账号要跟着走。
//
// users.status 只管登录：停用只是封了供号商的登录，他名下的账号仍在号池里被调度，
// 继续产生用量与待结算金额。所以停用时要把这些账号暂停，启用时再恢复。
//
// 恢复必须区分「因停用被暂停」与「停用之前就被管理员手动暂停」：后者在启用后仍应保持暂停。
// 做法是暂停时在 extra 里打一个标记 provider_suspended=true，恢复时只动带标记的账号。
//
// 这个标记是纯内部状态：
//   - 不进供号商前台的任何响应（handler/provider/view.go 的字段是白名单）；
//   - 不进 providerSubmittableExtraKeys（供号商不能提交）；
//   - 不进 capacityExtraKeys（它不是容量键，进去会让批量更新把它剔除）；
//   - 选号阶段不读它（暂停靠 schedulable 列），因此不需要进调度快照白名单。
const extraKeyProviderSuspended = "provider_suspended"

// syncProviderAccountsWithUserStatus 在供号商的用户状态被改动之后，让其名下账号跟着暂停或恢复。
// 必须在 userRepo.Update 成功之后调用。
//
//   - 请求带 status=disabled 的供号商：暂停名下 active 且可调度的账号并打标记。
//     无状态、可重试：不要求旧状态不是 disabled，这样级联失败后管理员再提交一次 disabled 就能补上，
//     已经暂停的账号会被跳过，重复提交是幂等的。
//   - 请求带 status=active 且旧状态为 disabled：只恢复带标记的账号并清除标记。
//   - 其余情况（非供号商、请求没带 status、管理员账号、active 到 active）一律不动账号。
//
// 级联失败返回 500，此时用户状态已经落库，错误信息里说明这一点，方便管理员重试。
func (s *adminServiceImpl) syncProviderAccountsWithUserStatus(
	ctx context.Context,
	user *User,
	oldStatus string,
	input *UpdateUserInput,
) error {
	if input.Status == "" || !user.IsProviderUser() {
		return nil
	}

	switch {
	case input.Status == StatusDisabled:
		if err := s.switchProviderAccounts(ctx, user.ID, input.ActorAdminID, true); err != nil {
			return infraerrors.InternalServer("PROVIDER_ACCOUNTS_SUSPEND_FAILED",
				"状态已更新，但暂停该供号商名下的账号失败，请重试（再次提交「停用」即可补上）").WithCause(err)
		}
	case input.Status == StatusActive && oldStatus == StatusDisabled:
		if err := s.switchProviderAccounts(ctx, user.ID, input.ActorAdminID, false); err != nil {
			return infraerrors.InternalServer("PROVIDER_ACCOUNTS_RESUME_FAILED",
				"状态已更新，但恢复该供号商名下的账号失败，请先再次停用、再启用该供号商以重试").WithCause(err)
		}
	}
	return nil
}

// switchProviderAccounts 暂停（suspend=true）或恢复（suspend=false）某供号商名下的账号。
//
// 暂停只挑 status=active 且 schedulable=true 的账号：已经暂停的（包括管理员手动暂停的）不打标记，
// 所以它们在恢复时也不会被动；状态为 error 等本来就不参与调度的账号同样不碰。
// 恢复只挑带 provider_suspended=true 的账号。
//
// 一条 BulkUpdate 完成：schedulable 与标记同一条 SQL 写入，不会出现暂停了却没打标记。
// 标记是 JSONB 顶层合并，删不了键，恢复时写显式 false。
func (s *adminServiceImpl) switchProviderAccounts(ctx context.Context, providerUserID, actorAdminID int64, suspend bool) error {
	accounts, err := s.accountRepo.ListByProvider(ctx, providerUserID)
	if err != nil {
		return err
	}

	ids := make([]int64, 0, len(accounts))
	for i := range accounts {
		a := &accounts[i]
		if suspend {
			if a.Status == StatusActive && a.Schedulable {
				ids = append(ids, a.ID)
			}
			continue
		}
		if a.getExtraBool(extraKeyProviderSuspended) {
			ids = append(ids, a.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	schedulable := !suspend
	if _, err := s.accountRepo.BulkUpdate(ctx, ids, AccountBulkUpdate{
		Schedulable: &schedulable,
		Extra:       map[string]any{extraKeyProviderSuspended: suspend},
	}); err != nil {
		return err
	}

	action := "resumed"
	if suspend {
		action = "suspended"
	}
	logger.LegacyPrintf("service.admin", "audit: provider accounts %s actor_admin_id=%d provider_user_id=%d accounts=%d",
		action, actorAdminID, providerUserID, len(ids))
	return nil
}
