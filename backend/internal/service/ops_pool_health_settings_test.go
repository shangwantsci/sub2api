//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// poolHealthSettingsRepoStub 只实现 GetMultiple（可注入错误并记录请求的键）；
// 其余方法来自内嵌的 nil 接口，被意外调用会直接 panic。
type poolHealthSettingsRepoStub struct {
	SettingRepository
	values map[string]string
	err    error
	keys   []string
}

func (r *poolHealthSettingsRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.keys = keys
	if r.err != nil {
		return nil, r.err
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if v, ok := r.values[key]; ok {
			out[key] = v
		}
	}
	return out, nil
}

func TestOpsService_GetPublicPoolHealthSettings(t *testing.T) {
	cases := []struct {
		name        string
		values      map[string]string
		wantEnabled bool
		wantGroup   string
	}{
		{"键缺失取缺省：开启 + ccmax", nil, true, "ccmax"},
		{"显式开启", map[string]string{SettingKeyPoolHealthEnabled: "true"}, true, "ccmax"},
		{"显式关闭", map[string]string{SettingKeyPoolHealthEnabled: "false"}, false, "ccmax"},
		{"空串视同未设置", map[string]string{SettingKeyPoolHealthEnabled: ""}, true, "ccmax"},
		{"存在但不是严格 true 的脏值按关闭", map[string]string{SettingKeyPoolHealthEnabled: "1"}, false, "ccmax"},
		{"大小写不同的 TRUE 按关闭", map[string]string{SettingKeyPoolHealthEnabled: "TRUE"}, false, "ccmax"},
		{"自定义分组名", map[string]string{SettingKeyPoolHealthGroupName: "team"}, true, "team"},
		{"分组名 trim", map[string]string{SettingKeyPoolHealthGroupName: "  team \t"}, true, "team"},
		{"空白分组名取缺省", map[string]string{SettingKeyPoolHealthGroupName: "   "}, true, "ccmax"},
		{"关闭时仍返回分组名", map[string]string{SettingKeyPoolHealthEnabled: "false", SettingKeyPoolHealthGroupName: "team"}, false, "team"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &poolHealthSettingsRepoStub{values: tc.values}
			svc := &OpsService{settingRepo: repo}

			got, err := svc.GetPublicPoolHealthSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, PublicPoolHealthSettings{Enabled: tc.wantEnabled, GroupName: tc.wantGroup}, got)
			require.ElementsMatch(t, []string{SettingKeyPoolHealthEnabled, SettingKeyPoolHealthGroupName}, repo.keys,
				"只读这两个键，一次取完")
		})
	}
}

// 读取出错 fail-closed：返回零值（Enabled=false）和错误，调用方即使忽略 err 也不会放行。
func TestOpsService_GetPublicPoolHealthSettings_ReadErrorFailsClosed(t *testing.T) {
	dbDown := errors.New("db down")
	svc := &OpsService{settingRepo: &poolHealthSettingsRepoStub{err: dbDown}}

	got, err := svc.GetPublicPoolHealthSettings(context.Background())
	require.ErrorIs(t, err, dbDown)
	require.Equal(t, PublicPoolHealthSettings{}, got)
	require.False(t, got.Enabled)
}

func TestOpsService_GetPublicPoolHealthSettings_NoRepoFailsClosed(t *testing.T) {
	for _, svc := range []*OpsService{nil, {}} {
		got, err := svc.GetPublicPoolHealthSettings(context.Background())
		require.Error(t, err)
		require.Equal(t, PublicPoolHealthSettings{}, got)
	}
}

// 管理端设置视图（parseSettings）与公开端点闸门（GetPublicPoolHealthSettings）必须对同一份存储值得出
// 同样的结论，否则会出现「设置页显示开着、端点却 404」（或反过来）。
func TestPoolHealthSettings_AdminViewAndEndpointGateAgree(t *testing.T) {
	settingSvc := NewSettingService(&settingUpdateRepoStub{}, &config.Config{})

	enabledValues := []*string{nil}
	for _, v := range []string{"true", "false", "", "1", "TRUE", " true", "yes"} {
		v := v
		enabledValues = append(enabledValues, &v)
	}
	groupValues := []*string{nil}
	for _, v := range []string{"", "   ", "team", "  team  "} {
		v := v
		groupValues = append(groupValues, &v)
	}

	for _, enabled := range enabledValues {
		for _, group := range groupValues {
			stored := map[string]string{}
			if enabled != nil {
				stored[SettingKeyPoolHealthEnabled] = *enabled
			}
			if group != nil {
				stored[SettingKeyPoolHealthGroupName] = *group
			}

			view := settingSvc.parseSettings(stored)
			gate, err := (&OpsService{settingRepo: &poolHealthSettingsRepoStub{values: stored}}).GetPublicPoolHealthSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, view.PoolHealthEnabled, gate.Enabled, "stored=%v", stored)
			require.Equal(t, view.PoolHealthGroupName, gate.GroupName, "stored=%v", stored)
		}
	}
}

func TestNormalizePoolHealthGroupName(t *testing.T) {
	require.Equal(t, "ccmax", defaultPublicPoolHealthGroupName, "缺省分组名是 ccmax")
	for in, want := range map[string]string{
		"":          "ccmax",
		"   ":       "ccmax",
		"\t\n":      "ccmax",
		"ccmax":     "ccmax",
		"team":      "team",
		"  team \t": "team",
		"Team A":    "Team A",
	} {
		require.Equal(t, want, NormalizePoolHealthGroupName(in), "输入 %q", in)
	}
}

// 统计入口对分组名的归一化与设置读写走同一个函数：缺省值只有一份。
func TestNormalizePublicPoolHealthOptions_GroupNameUsesSharedDefault(t *testing.T) {
	require.Equal(t, defaultPublicPoolHealthGroupName, normalizePublicPoolHealthOptions(publicPoolHealthOptions{}).GroupName)
	require.Equal(t, defaultPublicPoolHealthGroupName, normalizePublicPoolHealthOptions(publicPoolHealthOptions{GroupName: "  "}).GroupName)
	require.Equal(t, "team", normalizePublicPoolHealthOptions(publicPoolHealthOptions{GroupName: " team "}).GroupName)
}

// 响应里的 group_name 是实际使用的分组名（前端据此显示，不再硬编码），并且按这个分组过滤账号。
func TestBuildPublicPoolHealthSnapshot_ReportsConfiguredGroupName(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	ccmax := &Group{ID: 7, Name: "ccmax", Platform: PlatformAnthropic}
	team := &Group{ID: 8, Name: "team", Platform: PlatformAnthropic}
	newAccount := func(id int64, concurrency int, group *Group) Account {
		return Account{
			ID: id, Platform: PlatformAnthropic, Type: AccountTypeSetupToken,
			Status: StatusActive, Schedulable: true, Concurrency: concurrency, Groups: []*Group{group},
		}
	}
	accounts := []Account{newAccount(1, 2, ccmax), newAccount(2, 5, team), newAccount(3, 5, team)}

	snapshot := buildPublicPoolHealthSnapshot(accounts, nil, now, publicPoolHealthOptions{
		Platform:    PlatformAnthropic,
		GroupName:   NormalizePoolHealthGroupName("  team "),
		AccountType: AccountTypeSetupToken,
		Horizon:     4 * time.Hour,
	})
	require.Equal(t, "team", snapshot.GroupName)
	require.Equal(t, 2, snapshot.Accounts.Total)
	require.Equal(t, 10, snapshot.Capacity.TotalSlots)

	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, "team", decoded["group_name"], "JSON 键名 group_name 是与前端的契约")
}
