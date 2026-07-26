package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

var (
	// ErrProviderPortalDisabled 供号商站点未开启。
	ErrProviderPortalDisabled = infraerrors.Forbidden("PROVIDER_PORTAL_DISABLED", "provider portal is not available")
	// ErrProviderInviteRequired 供号商注册必须提供邀请码。
	ErrProviderInviteRequired = infraerrors.BadRequest("PROVIDER_INVITE_REQUIRED", "an invite code is required to register as a provider")
	// ErrProviderInviteInvalid 邀请码不存在、类型不符或已被使用。
	ErrProviderInviteInvalid = infraerrors.BadRequest("PROVIDER_INVITE_INVALID", "invite code is invalid, expired, or already used")
)

// RegisterProvider 以供号商身份注册。
//
// 与普通注册的差别：
//   - 邀请码强制必填，且类型必须是 provider_invite（普通 invitation 码不接受）
//   - 建号时 is_provider=true、balance=0、不走签到赠送与订阅分配（纯供货方不消费）
//   - 受 provider_portal_enabled 而非 registration_enabled 控制
//
// 邮箱验证码、保留邮箱、后缀白名单等策略与普通注册保持一致。
func (s *AuthService) RegisterProvider(ctx context.Context, email, password, verifyCode, inviteCode string) (string, *User, error) {
	if s.settingService == nil || !s.settingService.IsProviderPortalEnabled(ctx) {
		return "", nil, ErrProviderPortalDisabled
	}

	if isReservedEmail(email) {
		return "", nil, ErrEmailReserved
	}
	if err := s.validateRegistrationEmailPolicy(ctx, email); err != nil {
		return "", nil, err
	}

	// 邀请码是供号商注册的唯一准入手段，任何情况下都不能跳过。
	inviteCode = strings.TrimSpace(inviteCode)
	if inviteCode == "" {
		return "", nil, ErrProviderInviteRequired
	}
	redeemCode, err := s.redeemRepo.GetByCode(ctx, inviteCode)
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Provider] invite lookup failed: %v", err)
		return "", nil, ErrProviderInviteInvalid
	}
	if redeemCode.Type != RedeemTypeProviderInvite || !redeemCode.CanUse() {
		logger.LegacyPrintf("service.auth", "[Provider] invite rejected: type=%s status=%s", redeemCode.Type, redeemCode.Status)
		return "", nil, ErrProviderInviteInvalid
	}

	if s.settingService.IsEmailVerifyEnabled(ctx) {
		if s.emailService == nil {
			logger.LegacyPrintf("service.auth", "%s", "[Provider] email verification enabled but email service not configured")
			return "", nil, ErrServiceUnavailable
		}
		if verifyCode == "" {
			return "", nil, ErrEmailVerifyRequired
		}
		if err := s.emailService.VerifyCode(ctx, email, verifyCode); err != nil {
			return "", nil, fmt.Errorf("verify code: %w", err)
		}
	}

	existsEmail, err := s.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Provider] database error checking email: %v", err)
		return "", nil, ErrServiceUnavailable
	}
	if existsEmail {
		return "", nil, ErrEmailExists
	}

	hashedPassword, err := s.HashPassword(password)
	if err != nil {
		return "", nil, fmt.Errorf("hash password: %w", err)
	}

	// 纯供货方：无余额、无并发额度、无订阅。这些字段留默认值，
	// 且刻意不调用 resolveSignupGrantPlan / assignSubscriptions。
	user := &User{
		Email:        email,
		PasswordHash: hashedPassword,
		Role:         RoleUser,
		IsProvider:   true,
		Balance:      0,
		Status:       StatusActive,
	}

	// 建用户与消费邀请码必须同事务：分开做的话，两个并发请求会各自通过 CanUse
	// 校验并各自建号，只有一个 Use 成功，另一个失败却仍拿到了账号和 token——
	// 一枚邀请码就放进了多个供号商。
	//
	// 这里与普通注册的「标记失败只记日志」刻意不同：普通邀请码只影响赠送，
	// 供号商邀请码是准入凭证，绝不能出现"码没消费掉但人已经进来了"。
	if err := s.runInTx(ctx, func(txCtx context.Context) error {
		if createErr := s.userRepo.Create(txCtx, user); createErr != nil {
			return createErr
		}
		return s.redeemRepo.Use(txCtx, redeemCode.ID, user.ID)
	}); err != nil {
		switch {
		case errors.Is(err, ErrEmailExists):
			return "", nil, ErrEmailExists
		case errors.Is(err, ErrRedeemCodeUsed):
			// 并发抢同一枚邀请码，输的一方走到这里；用户创建已随事务回滚。
			return "", nil, ErrProviderInviteInvalid
		}
		logger.LegacyPrintf("service.auth", "[Provider] register transaction failed: %v", err)
		return "", nil, ErrServiceUnavailable
	}

	// bootstrap 是事务外的 best-effort 副作用（写 signup_source、touch 登录时间），
	// 失败不该回滚已经成立的注册。
	s.postAuthUserBootstrap(ctx, user, "email", true)

	token, err := s.GenerateToken(user)
	if err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	return token, user, nil
}

// runInTx 在一个 Ent 事务内执行 fn。已处于外层事务时直接复用，由外层负责提交。
func (s *AuthService) runInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.entClient == nil {
		return fn(ctx)
	}
	if dbent.TxFromContext(ctx) != nil {
		return fn(ctx)
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		if errors.Is(err, dbent.ErrTxStarted) {
			return fn(ctx)
		}
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(dbent.NewTxContext(ctx, tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
