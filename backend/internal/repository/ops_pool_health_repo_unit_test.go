package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

// 公开 pool-health 统计的取数必须在「真实 repository + 真实库」上跑一遍。
//
// 历史教训：运维面板的窄查询 ListOpsAccountsForStats 只选了计数用的几列，pool-health 复用它之后
// type / extra / 会话窗口 / 过期字段全是零值，真实库里 accounts.total 恒为 0；而 handler 测试用的
// stub 直接塞带 Type 的 Account，永远抓不到。所以这里不用 stub，数据落在 SQLite 内存库里，
// 经 repository 的真实 SELECT 再交给 OpsService。
//
// 六个账号各自独占一列，任何一列从 SELECT 里被拿掉，至少有一条断言变红。
func TestOpsPoolHealth_RealRepositoryFeedsEveryColumnTheCalculatorReads(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, logger.Init(logger.InitOptions{
		Level:       "info",
		Format:      "json",
		ServiceName: "sub2api",
		Environment: "test",
		Output:      logger.OutputOptions{ToStdout: false, ToFile: false},
	}))

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })

	ccmax, err := client.Group.Create().SetName("ccmax").SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)
	other, err := client.Group.Create().SetName("other").SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)

	now := time.Now()
	newAccount := func(name, platform, accountType string, concurrency int, group *dbent.Group, customize func(*dbent.AccountCreate)) {
		t.Helper()
		create := client.Account.Create().
			SetName(name).
			SetPlatform(platform).
			SetType(accountType).
			SetStatus(service.StatusActive).
			SetSchedulable(true).
			SetConcurrency(concurrency).
			SetCredentials(map[string]any{"access_token": "placeholder"})
		if customize != nil {
			customize(create)
		}
		account, err := create.Save(ctx)
		require.NoError(t, err)
		_, err = client.AccountGroup.Create().SetAccountID(account.ID).SetGroupID(group.ID).SetPriority(1).Save(ctx)
		require.NoError(t, err)
	}

	// 统计范围内的 4 个账号（anthropic / setup-token / 分组 ccmax），各自覆盖不同的列：
	// healthy：读 extra 里的用量，可用；
	newAccount("healthy", service.PlatformAnthropic, service.AccountTypeSetupToken, 3, ccmax, func(c *dbent.AccountCreate) {
		c.SetExtra(map[string]any{"session_window_utilization": 0.30})
	})
	// exhausted：额度耗尽靠 session_window_status = rejected + 用量 100% 判定，恢复时间取 session_window_end；
	newAccount("exhausted", service.PlatformAnthropic, service.AccountTypeSetupToken, 2, ccmax, func(c *dbent.AccountCreate) {
		c.SetExtra(map[string]any{"session_window_utilization": 1.0}).
			SetSessionWindowStatus("rejected").
			SetSessionWindowEnd(now.Add(30 * time.Minute))
	})
	// expired：过期靠 auto_pause_on_expired + expires_at 判定；
	newAccount("expired", service.PlatformAnthropic, service.AccountTypeSetupToken, 1, ccmax, func(c *dbent.AccountCreate) {
		c.SetAutoPauseOnExpired(true).SetExpiresAt(now.Add(-time.Hour))
	})
	// paused：手工下线。
	newAccount("paused", service.PlatformAnthropic, service.AccountTypeSetupToken, 4, ccmax, func(c *dbent.AccountCreate) {
		c.SetSchedulable(false)
	})

	// 统计范围外：类型不对、分组不对、平台不对，都不该被算进来。
	newAccount("oauth-not-counted", service.PlatformAnthropic, service.AccountTypeOAuth, 9, ccmax, nil)
	newAccount("other-group", service.PlatformAnthropic, service.AccountTypeSetupToken, 9, other, nil)
	newAccount("openai-in-ccmax", service.PlatformOpenAI, service.AccountTypeSetupToken, 9, ccmax, nil)

	cfg := &config.Config{
		Ops: config.OpsConfig{Enabled: true},
		Log: config.LogConfig{Level: "info", Caller: true, StacktraceLevel: "error"},
	}
	repo := newAccountRepositoryWithSQL(client, db, nil)
	ops := service.NewOpsService(nil, nil, cfg, repo, nil, nil, nil, nil, nil, nil, nil)

	snapshot, err := ops.GetPublicPoolHealth(ctx, service.PublicPoolHealthRequest{GroupName: "ccmax"})
	require.NoError(t, err)

	// type + 分组 + 平台：4 个账号在范围内。
	require.Equal(t, 4, snapshot.Accounts.Total, "type 列没读到时，setup-token 筛选会把所有账号筛掉")
	// extra：healthy 与 exhausted 两个账号有用量数据。
	require.Equal(t, 2, snapshot.Accounts.Measured, "extra 列没读到时，用量百分比读不出来")
	// session_window_status：exhausted 才会被判为额度耗尽。
	require.Equal(t, 1, snapshot.Accounts.Available, "只有 healthy 可用；exhausted / expired / paused 都不可用")
	require.Equal(t, 3, snapshot.Accounts.Unavailable)
	require.Equal(t, 1, snapshot.Accounts.Blocked, "额度耗尽算运行时受阻")
	require.Equal(t, 1, snapshot.Accounts.Exhausted)
	// expires_at + auto_pause_on_expired：expired 账号不再算「有效」。
	require.Equal(t, 2, snapshot.Accounts.Effective, "healthy 与 exhausted 有效；expired 因过期、paused 因手工下线而无效")

	reasons := map[string]int{}
	for _, item := range snapshot.UnavailableBreakdown {
		reasons[item.Reason] = item.Count
	}
	require.Equal(t, map[string]int{"quota_exhausted": 1, "expired": 1, "manual_disabled": 1}, reasons)

	// 并发槽位：4 个账号 3+2+1+4，可调度的只有 healthy 的 3。
	require.Equal(t, 10, snapshot.Capacity.TotalSlots)
	require.Equal(t, 3, snapshot.Capacity.SchedulableSlots)

	// session_window_end：exhausted 账号约 30 分钟后恢复，落在 4 小时的展望期里。
	require.Len(t, snapshot.RecoveryBuckets, 1, "session_window_end 列没读到时，不会有恢复时间桶")
	require.Equal(t, 1, snapshot.RecoveryBuckets[0].Count)
	require.InDelta(t, 1800, snapshot.RecoveryBuckets[0].AfterSeconds, 120)
}
