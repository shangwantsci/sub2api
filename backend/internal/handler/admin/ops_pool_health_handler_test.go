package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// poolHealthSettingRepo 在 testSettingRepo 之上加了「读取出错」的注入点。
type poolHealthSettingRepo struct {
	*testSettingRepo
	getMultipleErr error
}

func (r *poolHealthSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if r.getMultipleErr != nil {
		return nil, r.getMultipleErr
	}
	return r.testSettingRepo.GetMultiple(ctx, keys)
}

// poolHealthAccountRepo 只实现 OpsService 取数用到的 ListOpsAccountsForStats；其余方法来自内嵌的
// nil 接口，被意外调用会直接 panic。calls 记录取数次数，用来断言「关闭时根本没取数」。
type poolHealthAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
	calls    int
	platform string
}

func (r *poolHealthAccountRepo) ListOpsAccountsForStats(_ context.Context, platformFilter string, _ *int64) ([]service.Account, error) {
	r.calls++
	r.platform = platformFilter
	return r.accounts, nil
}

type poolHealthFixture struct {
	settings *poolHealthSettingRepo
	accounts *poolHealthAccountRepo
	router   *gin.Engine
}

// newPoolHealthFixture 搭一个只挂公开 pool-health 路由的 router。stored 是 settings 表里已有的键。
//
// 账号：分组 ccmax 里有 1 个 setup-token 账号（并发 2）和 1 个 oauth 账号（并发 7，不应被统计），
// 分组 team 里有 1 个 setup-token 账号（并发 5），另有一个 openai 账号在 ccmax 里（不应被统计）。
// 因此：ccmax -> total=1/total_slots=2，team -> total=1/total_slots=5。
func newPoolHealthFixture(t *testing.T, stored map[string]string) *poolHealthFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, logger.Init(logger.InitOptions{
		Level:       "info",
		Format:      "json",
		ServiceName: "sub2api",
		Environment: "test",
		Output:      logger.OutputOptions{ToStdout: false, ToFile: false},
	}))

	values := make(map[string]string, len(stored))
	for k, v := range stored {
		values[k] = v
	}
	settings := &poolHealthSettingRepo{testSettingRepo: &testSettingRepo{values: values}}

	ccmax := &service.Group{ID: 7, Name: "ccmax", Platform: service.PlatformAnthropic}
	team := &service.Group{ID: 8, Name: "team", Platform: service.PlatformAnthropic}
	newAccount := func(id int64, platform, accountType string, concurrency int, group *service.Group) service.Account {
		return service.Account{
			ID:          id,
			Platform:    platform,
			Type:        accountType,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: concurrency,
			Groups:      []*service.Group{group},
		}
	}
	accounts := &poolHealthAccountRepo{accounts: []service.Account{
		newAccount(1, service.PlatformAnthropic, service.AccountTypeSetupToken, 2, ccmax),
		newAccount(2, service.PlatformAnthropic, service.AccountTypeOAuth, 7, ccmax),
		newAccount(3, service.PlatformAnthropic, service.AccountTypeSetupToken, 5, team),
		newAccount(4, service.PlatformOpenAI, service.AccountTypeSetupToken, 9, ccmax),
	}}

	cfg := &config.Config{
		Ops: config.OpsConfig{Enabled: true},
		Log: config.LogConfig{Level: "info", Caller: true, StacktraceLevel: "error"},
	}
	opsService := service.NewOpsService(nil, settings, cfg, accounts, nil, nil, nil, nil, nil, nil, nil)

	router := gin.New()
	router.GET("/api/v1/public/pool-health", NewOpsHandler(opsService).GetPublicPoolHealth)
	return &poolHealthFixture{settings: settings, accounts: accounts, router: router}
}

func (f *poolHealthFixture) get(query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/public/pool-health"+query, nil))
	return rec
}

type poolHealthResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
	Data    struct {
		Platform       string `json:"platform"`
		GroupName      string `json:"group_name"`
		AccountType    string `json:"account_type"`
		HorizonSeconds int64  `json:"horizon_seconds"`
		Accounts       struct {
			Total int `json:"total"`
		} `json:"accounts"`
		Capacity struct {
			TotalSlots int `json:"total_slots"`
		} `json:"capacity"`
	} `json:"data"`
}

func decodePoolHealthResponse(t *testing.T, rec *httptest.ResponseRecorder) poolHealthResponse {
	t.Helper()
	var resp poolHealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), rec.Body.String())
	return resp
}

// 404 的文案必须是通用的 "Not found"：不透露「功能被关闭」，也没有 reason，和一个不存在的入口无从区分。
func requirePoolHealthNotFound(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	resp := decodePoolHealthResponse(t, rec)
	require.Equal(t, http.StatusNotFound, resp.Code)
	require.Equal(t, "Not found", resp.Message)
	require.Empty(t, resp.Reason)
	require.Empty(t, resp.Data.GroupName, "404 响应里不能带任何池子数据")
}

// 开启 + 设置里没有分组名：统计 ccmax 分组，响应里 group_name 就是实际使用的分组名。
func TestGetPublicPoolHealth_EnabledByDefaultUsesCcmaxGroup(t *testing.T) {
	f := newPoolHealthFixture(t, nil)

	rec := f.get("")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp := decodePoolHealthResponse(t, rec)
	require.Equal(t, "ccmax", resp.Data.GroupName)
	require.Equal(t, 1, resp.Data.Accounts.Total)
	require.Equal(t, 2, resp.Data.Capacity.TotalSlots)

	// 平台与账号类型仍写死：只取 anthropic 的 setup-token 账号（oauth 账号与 openai 账号不计入）。
	require.Equal(t, 1, f.accounts.calls)
	require.Equal(t, service.PlatformAnthropic, f.accounts.platform)
	require.Equal(t, service.PlatformAnthropic, resp.Data.Platform)
	require.Equal(t, service.AccountTypeSetupToken, resp.Data.AccountType)
	require.Equal(t, int64(4*60*60), resp.Data.HorizonSeconds, "horizon 缺省 4 小时")
}

func TestGetPublicPoolHealth_GroupNameComesFromSettings(t *testing.T) {
	cases := []struct {
		name      string
		stored    map[string]string
		wantGroup string
		wantTotal int
		wantSlots int
	}{
		{
			name:      "显式开启 + 缺省分组名",
			stored:    map[string]string{service.SettingKeyPoolHealthEnabled: "true"},
			wantGroup: "ccmax", wantTotal: 1, wantSlots: 2,
		},
		{
			name:      "自定义分组名",
			stored:    map[string]string{service.SettingKeyPoolHealthGroupName: "team"},
			wantGroup: "team", wantTotal: 1, wantSlots: 5,
		},
		{
			name:      "分组名首尾空白被 trim 后使用",
			stored:    map[string]string{service.SettingKeyPoolHealthGroupName: " \t team  \n"},
			wantGroup: "team", wantTotal: 1, wantSlots: 5,
		},
		{
			name:      "空串按缺省处理",
			stored:    map[string]string{service.SettingKeyPoolHealthGroupName: ""},
			wantGroup: "ccmax", wantTotal: 1, wantSlots: 2,
		},
		{
			name:      "纯空白按缺省处理",
			stored:    map[string]string{service.SettingKeyPoolHealthGroupName: "   "},
			wantGroup: "ccmax", wantTotal: 1, wantSlots: 2,
		},
		{
			name:      "分组不存在：仍是 200，只是没有账号",
			stored:    map[string]string{service.SettingKeyPoolHealthGroupName: "no-such-group"},
			wantGroup: "no-such-group", wantTotal: 0, wantSlots: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPoolHealthFixture(t, tc.stored)

			rec := f.get("")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			resp := decodePoolHealthResponse(t, rec)
			require.Equal(t, tc.wantGroup, resp.Data.GroupName)
			require.Equal(t, tc.wantTotal, resp.Data.Accounts.Total)
			require.Equal(t, tc.wantSlots, resp.Data.Capacity.TotalSlots)
		})
	}
}

// 关闭：404，且根本没有去取账号数据；开关判断在参数校验之前，所以带非法 horizon 也只是 404。
func TestGetPublicPoolHealth_DisabledReturns404WithoutFetching(t *testing.T) {
	f := newPoolHealthFixture(t, map[string]string{service.SettingKeyPoolHealthEnabled: "false"})

	for _, query := range []string{"", "?horizon=1h", "?horizon=not-a-duration", "?horizon=30s"} {
		requirePoolHealthNotFound(t, f.get(query))
	}
	require.Zero(t, f.accounts.calls, "关闭时不能调用 opsService 的取数")
}

// 读设置出错：按关闭处理（fail-closed），404，同样不取数。
func TestGetPublicPoolHealth_SettingReadErrorFailsClosed(t *testing.T) {
	f := newPoolHealthFixture(t, nil)
	f.settings.getMultipleErr = errors.New("db down")

	requirePoolHealthNotFound(t, f.get(""))
	requirePoolHealthNotFound(t, f.get("?horizon=not-a-duration"))
	require.Zero(t, f.accounts.calls, "读设置失败时不能调用 opsService 的取数")
}

// 键存在但不是严格的 "true"：一律按关闭，不会因为手工写库的脏值把免登录端点放开。
func TestGetPublicPoolHealth_PresentNonTrueValueIsClosed(t *testing.T) {
	for _, raw := range []string{"false", "FALSE", "0", "1", "yes", "on", " true", "True"} {
		f := newPoolHealthFixture(t, map[string]string{service.SettingKeyPoolHealthEnabled: raw})

		requirePoolHealthNotFound(t, f.get(""))
		require.Zero(t, f.accounts.calls, "pool_health_enabled=%q", raw)
	}
}

// 开启时 horizon 校验保持原样：非法值 400 且不取数，合法值按所给时长统计。
func TestGetPublicPoolHealth_EnabledStillValidatesHorizon(t *testing.T) {
	f := newPoolHealthFixture(t, nil)

	for _, query := range []string{"?horizon=not-a-duration", "?horizon=30s", "?horizon=25h"} {
		rec := f.get(query)
		require.Equal(t, http.StatusBadRequest, rec.Code, query)
	}
	require.Zero(t, f.accounts.calls)

	rec := f.get("?horizon=1h")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, int64(60*60), decodePoolHealthResponse(t, rec).Data.HorizonSeconds)
}
