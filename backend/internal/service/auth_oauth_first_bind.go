package service

import (
	"context"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"

	entsql "entgo.io/ent/dialect/sql"
)

// ApplyProviderDefaultSettingsOnFirstBind applies provider-specific bootstrap
// settings the first time a user binds a third-party identity. The grant is
// idempotent per user/provider pair.
func (s *AuthService) ApplyProviderDefaultSettingsOnFirstBind(
	ctx context.Context,
	userID int64,
	providerType string,
) error {
	if s == nil || s.entClient == nil || s.settingService == nil || userID <= 0 {
		return nil
	}

	if dbent.TxFromContext(ctx) != nil {
		return s.applyProviderDefaultSettingsOnFirstBind(ctx, userID, providerType)
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin first bind defaults transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := s.applyProviderDefaultSettingsOnFirstBind(txCtx, userID, providerType); err != nil {
		return err
	}
	return tx.Commit()
}

// granteeIsProviderUser 报告受赠人是否为供号商。
//
// 必须用传入的 client 查，而不是 UserRepository —— 本函数运行在首绑事务内部，
// 而 userRepository.GetByID 走的是仓储自己的非事务 client。用它会在事务打开期间
// 从事务外再读一次同一张表：SQLite 下直接卡在驱动互斥锁上（首绑整条链路死锁，
// 测试表现为永久挂起），PostgreSQL 下虽不死锁但读的是事务外快照，语义也是错的。
//
// 「查不到」与「查失败」区别对待：查询出错时返回 error 让整个首绑事务回滚，
// 而不是当成「是供号商」静默跳过——那样一次数据库抖动就会让普通用户永久拿不到
// 注册赠送。回滚后赠送标记不落库，下次登录会自然重试。
func granteeIsProviderUser(ctx context.Context, client *dbent.Client, userID int64) (bool, error) {
	if client == nil || userID <= 0 {
		// 没有可用 client 时保持保守：不发放。这是配置缺失而非瞬时故障，重试也无用。
		return true, nil
	}
	u, err := client.User.Query().
		Where(dbuser.IDEQ(userID)).
		Select(dbuser.FieldRole, dbuser.FieldIsProvider).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return false, fmt.Errorf("grantee %d not found", userID)
		}
		return false, fmt.Errorf("look up grantee %d: %w", userID, err)
	}
	// 与 User.IsProviderUser() 保持同一判定：管理员即便带 is_provider 也不算供号商。
	return u.IsProvider && u.Role != RoleAdmin, nil
}

func (s *AuthService) applyProviderDefaultSettingsOnFirstBind(
	ctx context.Context,
	userID int64,
	providerType string,
) error {
	providerDefaults, enabled, err := s.settingService.ResolveAuthSourceGrantSettings(ctx, providerType, true)
	if err != nil {
		return fmt.Errorf("load auth source defaults: %w", err)
	}
	if !enabled {
		return nil
	}

	client := s.entClient
	if tx := dbent.TxFromContext(ctx); tx != nil {
		client = tx.Client()
	}

	// 供号商是纯供货方，不该拥有余额、并发或订阅。这里在发放点本身拦一道，
	// 而不是只靠路由层的 ProviderDenyConsumerRoutes：绑定第三方身份的入口不止一处，
	// 逐个路由堵容易漏，堵在唯一的发放点更可靠。
	//
	// 注意此处 provider 一词有两义：函数名里的 provider 指 OAuth 身份源
	// （linuxdo/github 等），而 IsProviderUser 指供号商。
	isProvider, err := granteeIsProviderUser(ctx, client, userID)
	if err != nil {
		return fmt.Errorf("resolve grantee kind: %w", err)
	}
	if isProvider {
		return nil
	}

	var result entsql.Result
	if err := client.Driver().Exec(
		ctx,
		`INSERT INTO user_provider_default_grants (user_id, provider_type, grant_reason)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, provider_type, grant_reason) DO NOTHING`,
		[]any{userID, strings.TrimSpace(providerType), "first_bind"},
		&result,
	); err != nil {
		return fmt.Errorf("record first bind provider grant: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read first bind provider grant result: %w", err)
	}
	if affected == 0 {
		return nil
	}

	if providerDefaults.Balance != 0 {
		if err := client.User.UpdateOneID(userID).AddBalance(providerDefaults.Balance).Exec(ctx); err != nil {
			return fmt.Errorf("apply first bind balance default: %w", err)
		}
	}
	if providerDefaults.Concurrency != 0 {
		if err := client.User.UpdateOneID(userID).AddConcurrency(providerDefaults.Concurrency).Exec(ctx); err != nil {
			return fmt.Errorf("apply first bind concurrency default: %w", err)
		}
	}
	if s.defaultSubAssigner != nil {
		for _, item := range providerDefaults.Subscriptions {
			if _, _, err := s.defaultSubAssigner.AssignOrExtendSubscription(ctx, &AssignSubscriptionInput{
				UserID:       userID,
				GroupID:      item.GroupID,
				ValidityDays: item.ValidityDays,
				Notes:        "auto assigned by first bind defaults",
			}); err != nil {
				return fmt.Errorf("apply first bind subscription default: %w", err)
			}
		}
	}

	return nil
}
