//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// 站点关闭时供号商不能登录。
//
// 供号商的业务接口在关站后由 ProviderOnly 返回 404，但登录复用普通用户的
// /auth/login。不拦的话供号商仍能拿到 JWT 进入面板，只是每个接口都 404，
// 对外表现成「系统坏了」而不是「站点已关闭」。
func TestAssertProviderPortalOpenFor(t *testing.T) {
	provider := &User{ID: 1, Role: RoleUser, IsProvider: true}
	normal := &User{ID: 2, Role: RoleUser}
	admin := &User{ID: 3, Role: RoleAdmin, IsProvider: true}

	t.Run("站点关闭时拒绝供号商", func(t *testing.T) {
		svc := &AuthService{settingService: newPortalSettingService(t, false)}
		require.ErrorIs(t, svc.assertProviderPortalOpenFor(context.Background(), provider),
			ErrProviderPortalDisabled)
	})

	t.Run("站点开启时放行供号商", func(t *testing.T) {
		svc := &AuthService{settingService: newPortalSettingService(t, true)}
		require.NoError(t, svc.assertProviderPortalOpenFor(context.Background(), provider))
	})

	t.Run("普通用户不受影响", func(t *testing.T) {
		svc := &AuthService{settingService: newPortalSettingService(t, false)}
		require.NoError(t, svc.assertProviderPortalOpenFor(context.Background(), normal))
	})

	t.Run("管理员即便带 is_provider 也不受影响", func(t *testing.T) {
		// 关站时把管理员一起挡在门外会导致没人能进去重新打开站点。
		svc := &AuthService{settingService: newPortalSettingService(t, false)}
		require.NoError(t, svc.assertProviderPortalOpenFor(context.Background(), admin))
	})

	t.Run("设置服务缺失时对供号商 fail-closed", func(t *testing.T) {
		svc := &AuthService{}
		require.ErrorIs(t, svc.assertProviderPortalOpenFor(context.Background(), provider),
			ErrProviderPortalDisabled)
		// 但不能顺带把普通用户挡住。
		require.NoError(t, svc.assertProviderPortalOpenFor(context.Background(), normal))
	})
}

func newPortalSettingService(t *testing.T, enabled bool) *SettingService {
	t.Helper()
	value := "false"
	if enabled {
		value = "true"
	}
	return NewSettingService(
		&portalGateSettingRepo{values: map[string]string{
			SettingKeyProviderPortalEnabled: value,
		}},
		nil,
	)
}

type portalGateSettingRepo struct {
	SettingRepository
	values map[string]string
}

func (r *portalGateSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}

func (r *portalGateSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := r.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}
