//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// capacityCaptureCache 在 mockConcurrencyCache 之上记录选号阶段交给并发服务的两个数：
// 拿槽位时的硬上限（AcquireAccountSlot 的 maxConcurrency）与负载排序用的分母
// （GetAccountsLoadBatch 里每个账号的 MaxConcurrency）。
type capacityCaptureCache struct {
	mockConcurrencyCache
	acquireMax map[int64]int
	loadMax    map[int64]int
}

func newCapacityCaptureCache() *capacityCaptureCache {
	return &capacityCaptureCache{acquireMax: map[int64]int{}, loadMax: map[int64]int{}}
}

func (c *capacityCaptureCache) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	c.acquireMax[accountID] = maxConcurrency
	return c.mockConcurrencyCache.AcquireAccountSlot(ctx, accountID, maxConcurrency, requestID)
}

func (c *capacityCaptureCache) GetAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	for _, acc := range accounts {
		c.loadMax[acc.ID] = acc.MaxConcurrency
	}
	return c.mockConcurrencyCache.GetAccountsLoadBatch(ctx, accounts)
}

// selectOneWithCapacity 让负载感知选号选出唯一的候选账号，并返回并发服务看到的两个数。
// slotAsked 为 false 表示并发服务没有去缓存里拿槽位：并发 <= 0（不限）时它直接放行，不占槽位。
func selectOneWithCapacity(t *testing.T, account Account) (acquireMax int, slotAsked bool, loadMax int) {
	t.Helper()
	return selectOneWithCapacityVia(t, account, false)
}

// selectOneWithCapacityVia 的 routed=true 时走「分组模型路由」那条负载感知分支（另一处负载分母），
// 否则走默认的候选集分支。两处必须用同样的分母与硬上限。
func selectOneWithCapacityVia(t *testing.T, account Account, routed bool) (acquireMax int, slotAsked bool, loadMax int) {
	t.Helper()
	const groupID = int64(31)
	account.Platform = PlatformAnthropic
	account.Type = AccountTypeOAuth
	account.Status = StatusActive
	account.Schedulable = true
	account.Priority = 1

	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{},
	}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}

	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true

	cache := newCapacityCaptureCache()
	svc := &GatewayService{
		accountRepo:        repo,
		cache:              &mockGatewayCacheForPlatform{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(cache),
	}

	var groupArg *int64
	model := "claude-3-5-sonnet-20241022"
	if routed {
		repo.accounts[0].AccountGroups = []AccountGroup{{GroupID: groupID}}
		repo.accountsByID[account.ID] = &repo.accounts[0]
		svc.groupRepo = &mockGroupRepoForGateway{groups: map[int64]*Group{
			groupID: {
				ID: groupID, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true,
				ModelRoutingEnabled: true,
				ModelRouting:        map[string][]int64{"claude-routed": {account.ID}},
			},
		}}
		groupArg = &[]int64{groupID}[0]
		model = "claude-routed"
	}

	result, err := svc.SelectAccountWithLoadAwareness(context.Background(), groupArg, "", model, nil, "", int64(0))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Account)
	require.Equal(t, account.ID, result.Account.ID)
	require.True(t, result.Acquired, "mock 缓存总是放行（并发不限时直接放行），应当拿到账号")

	acquired, slotAsked := cache.acquireMax[account.ID]
	load, ok := cache.loadMax[account.ID]
	require.True(t, ok, "负载感知选号必须查询负载")
	return acquired, slotAsked, load
}

// 选号阶段两个并发数的分工（官方语义恢复）：
//   - 拿槽位的硬上限 = EffectiveConcurrency()：并发列 × 降速系数；
//   - 负载排序的分母 = LoadBalanceCapacity()：EffectiveLoadFactor() × 降速系数。
//
// 5h 0.85 → 系数 0.4：并发 5→2，load_factor 20→8。
func TestSelectAccountWithLoadAwareness_UsesThrottledConcurrencyAndLoadDenominator(t *testing.T) {
	loadFactor := 20
	acquired, asked, load := selectOneWithCapacity(t, Account{
		ID: 1, Concurrency: 5, LoadFactor: &loadFactor,
		Extra: map[string]any{"adaptive_throttle": true, "session_window_utilization": 0.85},
	})
	require.True(t, asked)
	require.Equal(t, 2, acquired, "硬并发上限按并发列缩放")
	require.Equal(t, 8, load, "负载分母按 load_factor 缩放")
}

// 分组模型路由分支里另有一处负载分母，必须与默认分支保持一致。
func TestSelectAccountWithLoadAwareness_ModelRoutingBranchUsesSameCapacityNumbers(t *testing.T) {
	loadFactor := 20
	acquired, asked, load := selectOneWithCapacityVia(t, Account{
		ID: 6, Concurrency: 5, LoadFactor: &loadFactor,
		Extra: map[string]any{"adaptive_throttle": true, "session_window_utilization": 0.85},
	}, true)
	require.True(t, asked)
	require.Equal(t, 2, acquired)
	require.Equal(t, 8, load)

	acquired, asked, load = selectOneWithCapacityVia(t, Account{ID: 7, Concurrency: 5, LoadFactor: &loadFactor}, true)
	require.True(t, asked)
	require.Equal(t, 5, acquired)
	require.Equal(t, 20, load)
}

// 开关关闭：利用率再高也不缩放，两个数分别是并发列与 load_factor 的原值。
func TestSelectAccountWithLoadAwareness_NoThrottleWhenSwitchOff(t *testing.T) {
	loadFactor := 20
	acquired, asked, load := selectOneWithCapacity(t, Account{
		ID: 2, Concurrency: 5, LoadFactor: &loadFactor,
		Extra: map[string]any{"adaptive_throttle": false, "session_window_utilization": 0.99},
	})
	require.True(t, asked)
	require.Equal(t, 5, acquired)
	require.Equal(t, 20, load)
}

// 没有 load_factor 时分母回落到并发列——官方语义。HEAD 上这条路径无视 load_factor，
// 是改动前后最容易悄悄变掉的地方，所以钉住。
func TestSelectAccountWithLoadAwareness_LoadDenominatorFollowsLoadFactorOverConcurrency(t *testing.T) {
	acquired, asked, load := selectOneWithCapacity(t, Account{ID: 3, Concurrency: 5})
	require.True(t, asked)
	require.Equal(t, 5, acquired)
	require.Equal(t, 5, load, "没有 load_factor：分母 = 并发列")

	loadFactor := 12
	acquired, asked, load = selectOneWithCapacity(t, Account{ID: 4, Concurrency: 5, LoadFactor: &loadFactor})
	require.True(t, asked)
	require.Equal(t, 5, acquired, "load_factor 不影响硬并发上限")
	require.Equal(t, 12, load, "load_factor 优先于并发列作负载分母")
}

// 并发=0 表示不限：不被降速成 1（否则「不限」的账号会被卡成串行），因此并发服务直接放行、
// 不去占槽位（槽位逻辑里 maxConcurrency <= 0 = 不限）；
// 没有 load_factor 时官方分母是 1（不是 0），降速系数也不会把它变没。
func TestSelectAccountWithLoadAwareness_UnlimitedConcurrencyStaysUnlimited(t *testing.T) {
	_, asked, load := selectOneWithCapacity(t, Account{
		ID: 5, Concurrency: 0,
		Extra: map[string]any{"adaptive_throttle": true, "session_window_utilization": 0.99},
	})
	require.False(t, asked, "并发 0（不限）原样不动：不占槽位，没有被降成「限 1」")
	require.Equal(t, 1, load, "concurrency=0 且无 load_factor：官方分母为 1")
}

// ===== 其余调用点：槽位上限与等待计划上限都必须是降速后的值 =====

// capacitySite 描述要走到的选号分支。账号固定为：并发 5、开启动态降速、5h 利用率 0.85，
// 因此降速后的硬并发上限是 2（未降速是 5），一眼就能看出有没有被换回原值。
type capacitySite struct {
	routed       bool  // 分组模型路由命中该账号
	mixed        bool  // 分组启用 Anthropic 混合类型加权（只有 setup-token 池有权重）
	legacy       bool  // LoadBatchEnabled=false，走不查负载的传统选择
	sticky       bool  // 会话已绑定到该账号
	slotFull     bool  // 槽位已满（AcquireAccountSlot 返回 false），逼出等待计划
	loadFull     bool  // 负载率 100，候选在负载感知阶段被排除，落到兜底排队
	loadBatchErr error // 负载批量查询失败，落到旧顺序选择
}

type capacityOutcome struct {
	result     *AccountSelectionResult
	acquireMax int
	slotAsked  bool
}

func selectWithCapacitySite(t *testing.T, site capacitySite) capacityOutcome {
	t.Helper()
	const (
		groupID   = int64(41)
		accountID = int64(21)
		stickyKey = "sticky-session"
		routedMod = "claude-routed"
		plainMod  = "claude-3-5-sonnet-20241022"
	)
	account := Account{
		ID: accountID, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive,
		Schedulable: true, Priority: 1, Concurrency: 5,
		Extra: map[string]any{"adaptive_throttle": true, "session_window_utilization": 0.85},
	}
	grouped := site.routed || site.mixed
	if grouped {
		account.AccountGroups = []AccountGroup{{GroupID: groupID}}
	}
	repo := &mockAccountRepoForPlatform{accounts: []Account{account}, accountsByID: map[int64]*Account{}}
	repo.accountsByID[accountID] = &repo.accounts[0]

	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = !site.legacy
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 45 * time.Second
	cfg.Gateway.Scheduling.FallbackWaitTimeout = 30 * time.Second
	cfg.Gateway.Scheduling.FallbackMaxWaiting = 100

	cache := newCapacityCaptureCache()
	if site.slotFull {
		cache.acquireResults = map[int64]bool{accountID: false}
	}
	if site.loadFull {
		cache.loadMap = map[int64]*AccountLoadInfo{accountID: {AccountID: accountID, LoadRate: 100}}
	}
	cache.loadBatchErr = site.loadBatchErr

	gatewayCache := &mockGatewayCacheForPlatform{}
	sessionHash := ""
	if site.sticky {
		sessionHash = stickyKey
		gatewayCache.sessionBindings = map[string]int64{stickyKey: accountID}
	}

	svc := &GatewayService{
		accountRepo:        repo,
		cache:              gatewayCache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(cache),
	}

	var groupArg *int64
	model := plainMod
	if grouped {
		group := &Group{ID: groupID, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true}
		if site.routed {
			group.ModelRoutingEnabled = true
			group.ModelRouting = map[string][]int64{routedMod: {accountID}}
			model = routedMod
		}
		if site.mixed {
			group.AnthropicMixedTypeWeightEnabled = true
			group.AnthropicSetupTokenPoolWeight = 1
		}
		svc.groupRepo = &mockGroupRepoForGateway{groups: map[int64]*Group{groupID: group}}
		groupArg = &[]int64{groupID}[0]
	}

	result, err := svc.SelectAccountWithLoadAwareness(context.Background(), groupArg, sessionHash, model, nil, "", int64(0))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Account)
	require.Equal(t, accountID, result.Account.ID)

	acquired, asked := cache.acquireMax[accountID]
	return capacityOutcome{result: result, acquireMax: acquired, slotAsked: asked}
}

// 拿到槽位的所有分支：交给并发服务的硬上限都是降速后的 2，而不是并发列的 5。
func TestSelection_EveryAcquirePathUsesThrottledConcurrency(t *testing.T) {
	cases := []struct {
		name string
		site capacitySite
	}{
		{"传统选择（不查负载）", capacitySite{legacy: true}},
		{"模型路由 + 粘性命中", capacitySite{routed: true, sticky: true}},
		{"模型路由候选", capacitySite{routed: true}},
		{"粘性命中（无模型路由）", capacitySite{sticky: true}},
		{"负载感知默认分支", capacitySite{}},
		{"混合类型加权池", capacitySite{mixed: true}},
		{"负载批量查询失败 → 旧顺序", capacitySite{loadBatchErr: errors.New("redis down")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := selectWithCapacitySite(t, tc.site)
			require.True(t, out.result.Acquired, "槽位没满，应当拿到账号")
			require.Nil(t, out.result.WaitPlan)
			require.True(t, out.slotAsked)
			require.Equal(t, 2, out.acquireMax, "降速后的并发上限，不是并发列的 5")
		})
	}
}

// 槽位满了转成等待计划的所有分支：等待计划里的 MaxConcurrency 同样是降速后的 2。
// 等待者拿到槽位时用的就是这个值，与上面「拿槽位」的口径必须一致。
func TestSelection_EveryWaitPlanPathUsesThrottledConcurrency(t *testing.T) {
	cases := []struct {
		name        string
		site        capacitySite
		wantWaiting int // 期望的等待队列上限：粘性路径用粘性配置，其余用兜底/粘性配置
	}{
		{"传统选择：兜底等待计划", capacitySite{legacy: true, slotFull: true}, 100},
		{"传统选择：粘性等待计划", capacitySite{legacy: true, sticky: true, slotFull: true}, 3},
		{"模型路由：粘性等待计划", capacitySite{routed: true, sticky: true, slotFull: true}, 3},
		{"模型路由：路由账号全满的等待计划", capacitySite{routed: true, slotFull: true}, 3},
		{"粘性命中（无模型路由）：等待计划", capacitySite{sticky: true, slotFull: true}, 3},
		{"负载率 100 → 兜底排队的等待计划", capacitySite{loadFull: true}, 100},
		{"槽位全满 → 兜底排队的等待计划", capacitySite{slotFull: true}, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := selectWithCapacitySite(t, tc.site)
			require.False(t, out.result.Acquired)
			require.NotNil(t, out.result.WaitPlan, "槽位拿不到时应返回等待计划")
			require.Equal(t, 2, out.result.WaitPlan.MaxConcurrency, "等待计划里的并发上限必须是降速后的值")
			require.Equal(t, tc.wantWaiting, out.result.WaitPlan.MaxWaiting)
		})
	}
}

// ===== RPM 门 =====

type fixedRPMCache struct{ count int }

func (c fixedRPMCache) IncrementRPM(context.Context, int64) (int, error) { return c.count + 1, nil }
func (c fixedRPMCache) GetRPM(context.Context, int64) (int, error)       { return c.count, nil }
func (c fixedRPMCache) GetRPMBatch(_ context.Context, ids []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(ids))
	for _, id := range ids {
		out[id] = c.count
	}
	return out, nil
}

func rpmGate(account *Account, currentRPM int, sticky bool) bool {
	svc := &GatewayService{rpmCache: fixedRPMCache{count: currentRPM}}
	return svc.isAccountSchedulableForRPM(context.Background(), account, sticky)
}

// 动态降速作用于 RPM 门本身：5h 0.85 → 系数 0.4 → base_rpm 50 降成 20。
// 三区（绿/黄/红）对降速后的上限生效（旧的人格档位账号曾经完全绕过三区策略），粘性缓冲 10 不随降速缩放。
func TestIsAccountSchedulableForRPM_ThrottledLimitAppliesToThreeZones(t *testing.T) {
	throttled := &Account{
		ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 3,
		Extra: map[string]any{
			"base_rpm": 50, "max_sessions": 3,
			"adaptive_throttle": true, "session_window_utilization": 0.85,
		},
	}
	require.True(t, rpmGate(throttled, 19, false), "未到降速后的 20：绿区")
	require.False(t, rpmGate(throttled, 20, false), "到顶：新会话不再调度")
	require.True(t, rpmGate(throttled, 20, true), "黄区：粘性会话仍可调度")
	require.True(t, rpmGate(throttled, 29, true))
	require.False(t, rpmGate(throttled, 30, true), "红区（20+缓冲 10）：粘性也不行")

	// 未开启降速：同样的账号上限仍是 50。
	plain := &Account{
		ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 3,
		Extra: map[string]any{"base_rpm": 50, "max_sessions": 3, "session_window_utilization": 0.85},
	}
	require.True(t, rpmGate(plain, 49, false))
	require.False(t, rpmGate(plain, 50, false))
	require.True(t, rpmGate(plain, 59, true))
	require.False(t, rpmGate(plain, 60, true))
}

// 粘性豁免策略对降速后的 RPM 同样生效：到顶只放行粘性会话，永远没有红区。
func TestIsAccountSchedulableForRPM_StickyExemptUsesThrottledLimit(t *testing.T) {
	account := &Account{
		ID: 3, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 3,
		Extra: map[string]any{
			"base_rpm": 50, "rpm_strategy": "sticky_exempt",
			"adaptive_throttle": true, "session_window_utilization": 0.85,
		},
	}
	require.True(t, rpmGate(account, 19, false))
	require.False(t, rpmGate(account, 20, false))
	require.True(t, rpmGate(account, 20, true))
	require.True(t, rpmGate(account, 9999, true))
}

// base_rpm 未配置（不限）、以及非 Claude OAuth/SetupToken 账号，不受 RPM 门约束。
func TestIsAccountSchedulableForRPM_UnlimitedAndNonClaudeAccountsPass(t *testing.T) {
	noLimit := &Account{
		ID: 4, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Extra: map[string]any{"adaptive_throttle": true, "session_window_utilization": 0.99},
	}
	require.True(t, rpmGate(noLimit, 9999, false))

	apiKey := &Account{ID: 5, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Extra: map[string]any{"base_rpm": 1}}
	require.True(t, rpmGate(apiKey, 9999, false))
}

// 预取 RPM 计数时「是否计 RPM」按 base_rpm > 0 判断（降速后的有效值最低为 1，不会改变这个结论）。
func TestWithRPMPrefetch_OnlyCountsClaudeOAuthAccountsWithBaseRPM(t *testing.T) {
	svc := &GatewayService{rpmCache: fixedRPMCache{count: 7}}
	accounts := []Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"base_rpm": 30}},
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"adaptive_throttle": true}},
		{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Extra: map[string]any{"base_rpm": 30}},
	}

	ctx := svc.withRPMPrefetch(context.Background(), accounts)

	count, ok := rpmFromPrefetchContext(ctx, 1)
	require.True(t, ok)
	require.Equal(t, 7, count)
	_, ok = rpmFromPrefetchContext(ctx, 2)
	require.False(t, ok, "没配 base_rpm 的账号不需要预取")
	_, ok = rpmFromPrefetchContext(ctx, 3)
	require.False(t, ok, "非 OAuth/SetupToken 账号不需要预取")
}
