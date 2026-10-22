//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type nurturingRepoStub struct {
	accountRepoStub
	accounts  []Account
	listCalls int
}

func (r *nurturingRepoStub) ListSchedulableByPlatform(_ context.Context, platform string) ([]Account, error) {
	r.listCalls++
	if platform != PlatformAnthropic {
		return nil, errors.New("nurturing must only list anthropic accounts")
	}
	return r.accounts, nil
}

// nurturingFailingSettingRepo 的 GetValue 永远出错，模拟数据库读失败。
type nurturingFailingSettingRepo struct{ gatewayTTLSettingRepo }

func (r *nurturingFailingSettingRepo) GetValue(context.Context, string) (string, error) {
	return "", errors.New("db down")
}

func newNurturingService(repo *nurturingRepoStub, settings SettingRepository) *AccountNurturingService {
	svc := NewAccountNurturingService(repo, nil, time.Hour)
	svc.SetSettingRepository(settings)
	return svc
}

// 全局开关默认关闭，且严格 "true" 才开：缺失、"false"、"TRUE"、"1"、空白都不开，
// 读取出错也按关闭处理——养号是主动 opt-in 的能力，宁可少做不能误做。
func TestAccountNurturingGlobalSwitch_StrictTrueOnly(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		repo SettingRepository
		want bool
	}{
		{"未注入设置仓储", nil, false},
		{"设置不存在", &gatewayTTLSettingRepo{data: map[string]string{}}, false},
		{"true", &gatewayTTLSettingRepo{data: map[string]string{SettingKeyAccountNurturingEnabled: "true"}}, true},
		{"false", &gatewayTTLSettingRepo{data: map[string]string{SettingKeyAccountNurturingEnabled: "false"}}, false},
		{"TRUE（大小写不同）", &gatewayTTLSettingRepo{data: map[string]string{SettingKeyAccountNurturingEnabled: "TRUE"}}, false},
		{"1", &gatewayTTLSettingRepo{data: map[string]string{SettingKeyAccountNurturingEnabled: "1"}}, false},
		{"空串", &gatewayTTLSettingRepo{data: map[string]string{SettingKeyAccountNurturingEnabled: ""}}, false},
		{"读取出错", &nurturingFailingSettingRepo{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewAccountNurturingService(&nurturingRepoStub{}, nil, time.Hour)
			if tc.repo != nil {
				svc.SetSettingRepository(tc.repo)
			}
			require.Equal(t, tc.want, svc.globalSwitchEnabled(ctx))
		})
	}
}

// 全局开关关闭时整轮空转：连账号都不列出来。
func TestAccountNurturingRunOnce_GlobalSwitchOffDoesNothing(t *testing.T) {
	repo := &nurturingRepoStub{accounts: []Account{{ID: 1}}}
	svc := newNurturingService(repo, &gatewayTTLSettingRepo{data: map[string]string{}})

	svc.runOnce()
	require.Zero(t, repo.listCalls, "全局开关关闭时不应该查询账号")

	// 未注入设置仓储（例如测试或装配遗漏）同样视为关闭。
	noRepo := &nurturingRepoStub{}
	NewAccountNurturingService(noRepo, nil, time.Hour).runOnce()
	require.Zero(t, noRepo.listCalls)
}

// 全局开关开启后才会扫描账号（这里账号列表为空，避免真的去养号）。
func TestAccountNurturingRunOnce_GlobalSwitchOnScansAccounts(t *testing.T) {
	repo := &nurturingRepoStub{}
	svc := newNurturingService(repo, &gatewayTTLSettingRepo{data: map[string]string{SettingKeyAccountNurturingEnabled: "true"}})

	svc.runOnce()
	require.Equal(t, 1, repo.listCalls)
}

func TestAccountNurturing_IsNurtureCandidate(t *testing.T) {
	svc := NewAccountNurturingService(&nurturingRepoStub{}, nil, time.Hour)
	recent := time.Now().Add(-30 * time.Minute)
	stale := time.Now().Add(-3 * time.Hour)

	account := func(accountType string, extra map[string]any, lastUsed *time.Time) *Account {
		return &Account{ID: 1, Platform: PlatformAnthropic, Type: accountType, Extra: extra, LastUsedAt: lastUsed}
	}
	optIn := map[string]any{"nurturing_enabled": true}

	cases := []struct {
		name    string
		account *Account
		want    bool
	}{
		{"opt-in OAuth 账号且近期有使用", account(AccountTypeOAuth, optIn, &recent), true},
		{"opt-in setup-token 账号且近期有使用", account(AccountTypeSetupToken, optIn, &recent), true},
		{"没开 nurturing_enabled（缺省）", account(AccountTypeOAuth, map[string]any{}, &recent), false},
		{"nurturing_enabled=false", account(AccountTypeOAuth, map[string]any{"nurturing_enabled": false}, &recent), false},
		{"nurturing_enabled 是字符串 true（非严格 bool）", account(AccountTypeOAuth, map[string]any{"nurturing_enabled": "true"}, &recent), false},
		{"API Key 账号不养", account(AccountTypeAPIKey, optIn, &recent), false},
		{"超过 2 小时没使用", account(AccountTypeOAuth, optIn, &stale), false},
		{"从未使用过", account(AccountTypeOAuth, optIn, nil), false},
		{"nil 账号", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, svc.isNurtureCandidate(tc.account))
		})
	}
}

func TestAccountNurturing_HasRecentActivityBoundary(t *testing.T) {
	svc := NewAccountNurturingService(&nurturingRepoStub{}, nil, time.Hour)

	justInside := time.Now().Add(-119 * time.Minute)
	justOutside := time.Now().Add(-121 * time.Minute)
	require.True(t, svc.hasRecentActivity(&Account{LastUsedAt: &justInside}))
	require.False(t, svc.hasRecentActivity(&Account{LastUsedAt: &justOutside}))
	require.False(t, svc.hasRecentActivity(&Account{}))
	require.False(t, svc.hasRecentActivity(nil))
}
