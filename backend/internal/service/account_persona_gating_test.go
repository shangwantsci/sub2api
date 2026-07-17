package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// fakePersonaRPMCache 是 RPMCache 的最小假实现，仅用于人格日上限测试。
type fakePersonaRPMCache struct {
	daily   map[string]int
	getErr  error
	incrErr error
}

func (f *fakePersonaRPMCache) IncrementRPM(context.Context, int64) (int, error) { return 0, nil }
func (f *fakePersonaRPMCache) GetRPM(context.Context, int64) (int, error)       { return 0, nil }
func (f *fakePersonaRPMCache) GetRPMBatch(context.Context, []int64) (map[int64]int, error) {
	return map[int64]int{}, nil
}
func (f *fakePersonaRPMCache) IncrementAccountDaily(_ context.Context, id int64, dayKey string) (int, error) {
	if f.incrErr != nil {
		return 0, f.incrErr
	}
	if f.daily == nil {
		f.daily = map[string]int{}
	}
	f.daily[fmt.Sprintf("%d:%s", id, dayKey)]++
	return f.daily[fmt.Sprintf("%d:%s", id, dayKey)], nil
}
func (f *fakePersonaRPMCache) GetAccountDaily(_ context.Context, id int64, dayKey string) (int, error) {
	if f.getErr != nil {
		return 0, f.getErr
	}
	if f.daily == nil {
		return 0, nil
	}
	return f.daily[fmt.Sprintf("%d:%s", id, dayKey)], nil
}

func personaDailyAccount(enabled bool, cap int) *Account {
	extra := map[string]any{extraPersonaEnabled: enabled, extraPersonaTimezone: "UTC"}
	if cap > 0 {
		extra[extraPersonaDailyCap] = cap
	}
	return &Account{ID: 42, Concurrency: 5, Extra: extra}
}

func TestGatewayService_IsAccountWithinPersonaDailyCap(t *testing.T) {
	ctx := context.Background()

	// 开关 OFF：即使超限也放行。
	svcOff, _ := newPersonaGatingTestService(false)
	svcOff.rpmCache = &fakePersonaRPMCache{}
	require.True(t, svcOff.isAccountWithinPersonaDailyCap(ctx, personaDailyAccount(true, 1)))

	svc, _ := newPersonaGatingTestService(true)
	fake := &fakePersonaRPMCache{}
	svc.rpmCache = fake

	// cap=0（默认不限）→ 放行。
	require.True(t, svc.isAccountWithinPersonaDailyCap(ctx, personaDailyAccount(true, 0)))

	// cap=2：count 0/1 放行，达到 2 拦截。
	acc := personaDailyAccount(true, 2)
	dayKey := acc.GetPersonaEnvelope().DayKey(time.Now())
	require.True(t, svc.isAccountWithinPersonaDailyCap(ctx, acc)) // 0 < 2
	fake.daily = map[string]int{fmt.Sprintf("%d:%s", acc.ID, dayKey): 1}
	require.True(t, svc.isAccountWithinPersonaDailyCap(ctx, acc)) // 1 < 2
	fake.daily[fmt.Sprintf("%d:%s", acc.ID, dayKey)] = 2
	require.False(t, svc.isAccountWithinPersonaDailyCap(ctx, acc)) // 2 >= 2

	// 读取出错 → fail-open 放行。
	svc.rpmCache = &fakePersonaRPMCache{getErr: fmt.Errorf("redis down")}
	require.True(t, svc.isAccountWithinPersonaDailyCap(ctx, personaDailyAccount(true, 1)))

	// rpmCache 缺失 → 放行。
	svc.rpmCache = nil
	require.True(t, svc.isAccountWithinPersonaDailyCap(ctx, personaDailyAccount(true, 1)))

	// persona 未启用 → 放行。
	svc.rpmCache = &fakePersonaRPMCache{daily: map[string]int{}}
	require.True(t, svc.isAccountWithinPersonaDailyCap(ctx, personaDailyAccount(false, 1)))
}

func TestGatewayService_IncrementAccountPersonaDailyRequest(t *testing.T) {
	ctx := context.Background()

	// 开关 ON + persona 启用 + cap>0 → 计数递增。
	svc, _ := newPersonaGatingTestService(true)
	fake := &fakePersonaRPMCache{}
	svc.rpmCache = fake
	acc := personaDailyAccount(true, 5)
	svc.IncrementAccountPersonaDailyRequest(ctx, acc)
	svc.IncrementAccountPersonaDailyRequest(ctx, acc)
	dayKey := acc.GetPersonaEnvelope().DayKey(time.Now())
	require.Equal(t, 2, fake.daily[fmt.Sprintf("%d:%s", acc.ID, dayKey)])

	// cap=0 → 不写入（避免无谓 Redis 写）。
	fake2 := &fakePersonaRPMCache{}
	svc.rpmCache = fake2
	svc.IncrementAccountPersonaDailyRequest(ctx, personaDailyAccount(true, 0))
	require.Empty(t, fake2.daily)

	// 开关 OFF → 不写入。
	svcOff, _ := newPersonaGatingTestService(false)
	fake3 := &fakePersonaRPMCache{}
	svcOff.rpmCache = fake3
	svcOff.IncrementAccountPersonaDailyRequest(ctx, personaDailyAccount(true, 5))
	require.Empty(t, fake3.daily)
}

func TestValidatePersonaFieldValues_RejectsZeroLengthWindow(t *testing.T) {
	// start==end（非零）：歧义，拒绝。
	require.Error(t, ValidatePersonaExtraPatch(map[string]any{
		extraPersonaActiveStart: 9, extraPersonaActiveEnd: 9,
	}))
	// 全天 0..24：合法。
	require.NoError(t, ValidatePersonaExtraPatch(map[string]any{
		extraPersonaActiveStart: 0, extraPersonaActiveEnd: 24,
	}))
	// 正常窗口：合法。
	require.NoError(t, ValidatePersonaExtraPatch(map[string]any{
		extraPersonaActiveStart: 9, extraPersonaActiveEnd: 18,
	}))
	// 仅给 start（无 end）：不做跨字段校验，合法。
	require.NoError(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaActiveStart: 9}))
}

// fakeProxyLatencyCache 是 ProxyLatencyCache 的最小假实现，用于地理 prefill/一致性测试。
type fakeProxyLatencyCache struct {
	infos map[int64]*ProxyLatencyInfo
}

func (f *fakeProxyLatencyCache) GetProxyLatencies(_ context.Context, ids []int64) (map[int64]*ProxyLatencyInfo, error) {
	out := map[int64]*ProxyLatencyInfo{}
	for _, id := range ids {
		if v, ok := f.infos[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}
func (f *fakeProxyLatencyCache) SetProxyLatency(context.Context, int64, *ProxyLatencyInfo) error {
	return nil
}

func TestApplyPersonaGeoDefaults(t *testing.T) {
	ctx := context.Background()
	pid := int64(7)

	// persona 启用 + 时区空 + 代理出口 JP → prefill Asia/Tokyo。
	svc := &adminServiceImpl{proxyLatencyCache: &fakeProxyLatencyCache{infos: map[int64]*ProxyLatencyInfo{7: {CountryCode: "JP"}}}}
	extra := map[string]any{extraPersonaEnabled: true}
	svc.applyPersonaGeoDefaults(ctx, extra, &pid)
	require.Equal(t, "Asia/Tokyo", extra[extraPersonaTimezone])
	require.Equal(t, "ja-JP", extra[extraPersonaLocale])

	// 时区已填但与代理国家不一致 → 不改写（只告警），保持原值。
	extraMismatch := map[string]any{extraPersonaEnabled: true, extraPersonaTimezone: "Asia/Shanghai"}
	svcUS := &adminServiceImpl{proxyLatencyCache: &fakeProxyLatencyCache{infos: map[int64]*ProxyLatencyInfo{7: {CountryCode: "US"}}}}
	svcUS.applyPersonaGeoDefaults(ctx, extraMismatch, &pid)
	require.Equal(t, "Asia/Shanghai", extraMismatch[extraPersonaTimezone])
	require.Equal(t, "en-US", extraMismatch[extraPersonaLocale])

	// 未配置 persona → 完全不动。
	noPersona := map[string]any{}
	svc.applyPersonaGeoDefaults(ctx, noPersona, &pid)
	require.NotContains(t, noPersona, extraPersonaTimezone)
	require.NotContains(t, noPersona, extraPersonaLocale)

	// 代理国家未知 → 不 prefill。
	svcUnknown := &adminServiceImpl{proxyLatencyCache: &fakeProxyLatencyCache{infos: map[int64]*ProxyLatencyInfo{}}}
	extraUnknown := map[string]any{extraPersonaEnabled: true}
	svcUnknown.applyPersonaGeoDefaults(ctx, extraUnknown, &pid)
	require.NotContains(t, extraUnknown, extraPersonaTimezone)
	require.NotContains(t, extraUnknown, extraPersonaLocale)
}

// utcAt 构造指定 UTC 小时的固定时刻（日期任意取非 DST 敏感值）。
func utcAt(hour int) time.Time {
	return time.Date(2026, 1, 15, hour, 30, 0, 0, time.UTC)
}

// TestPersonaEnvelope_IsWithinActiveHours 覆盖作息窗口判定的四类形态：
// 当日窗口、跨夜窗口、未配置窗口（fail-open）、非 UTC 时区换算。
func TestPersonaEnvelope_IsWithinActiveHours(t *testing.T) {
	// 当日窗口 [9, 18)
	sameDay := PersonaEnvelope{Enabled: true, Timezone: "UTC", ActiveStart: 9, ActiveEnd: 18}
	require.True(t, sameDay.IsWithinActiveHours(utcAt(9)))
	require.True(t, sameDay.IsWithinActiveHours(utcAt(12)))
	require.True(t, sameDay.IsWithinActiveHours(utcAt(17)))
	require.False(t, sameDay.IsWithinActiveHours(utcAt(8)))
	require.False(t, sameDay.IsWithinActiveHours(utcAt(18)))
	require.False(t, sameDay.IsWithinActiveHours(utcAt(23)))

	// 跨夜窗口 [22, 24) ∪ [0, 6)
	overnight := PersonaEnvelope{Enabled: true, Timezone: "UTC", ActiveStart: 22, ActiveEnd: 6}
	require.True(t, overnight.IsWithinActiveHours(utcAt(22)))
	require.True(t, overnight.IsWithinActiveHours(utcAt(23)))
	require.True(t, overnight.IsWithinActiveHours(utcAt(0)))
	require.True(t, overnight.IsWithinActiveHours(utcAt(5)))
	require.False(t, overnight.IsWithinActiveHours(utcAt(6)))
	require.False(t, overnight.IsWithinActiveHours(utcAt(12)))
	require.False(t, overnight.IsWithinActiveHours(utcAt(21)))

	// 未配置窗口（start=end=0）：即使启用 persona 也恒放行
	unset := PersonaEnvelope{Enabled: true, Timezone: "UTC"}
	require.True(t, unset.IsWithinActiveHours(utcAt(3)))
	require.True(t, unset.IsWithinActiveHours(utcAt(15)))

	// persona 未启用：恒放行
	disabled := PersonaEnvelope{Enabled: false, ActiveStart: 9, ActiveEnd: 18}
	require.False(t, disabled.Enabled)
	require.True(t, disabled.IsWithinActiveHours(utcAt(3)))

	// 非 UTC 时区（Asia/Shanghai = UTC+8，无 DST）：窗口 [9, 18) 按当地小时判定
	shanghai := PersonaEnvelope{Enabled: true, Timezone: "Asia/Shanghai", ActiveStart: 9, ActiveEnd: 18}
	require.True(t, shanghai.IsWithinActiveHours(utcAt(2)))   // 当地 10:30
	require.False(t, shanghai.IsWithinActiveHours(utcAt(12))) // 当地 20:30
	require.False(t, shanghai.IsWithinActiveHours(utcAt(0)))  // 当地 08:30

	// 无效时区回落 UTC（不 panic，按 UTC 小时判定）
	badTZ := PersonaEnvelope{Enabled: true, Timezone: "Not/AZone", ActiveStart: 9, ActiveEnd: 18}
	require.True(t, badTZ.IsWithinActiveHours(utcAt(12)))
	require.False(t, badTZ.IsWithinActiveHours(utcAt(20)))
}

// newPersonaGatingTestService 构造带假 setting 仓储的 GatewayService，并重置全局转发设置缓存。
func newPersonaGatingTestService(flagOn bool) (*GatewayService, *gatewayTTLSettingRepo) {
	repo := &gatewayTTLSettingRepo{data: map[string]string{}}
	if flagOn {
		repo.data[SettingKeyEnablePersonaGating] = "true"
	}
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
	svc := &GatewayService{
		settingService: NewSettingService(repo, &config.Config{}),
	}
	return svc, repo
}

func personaAccount(enabled bool, maxConcurrency int) *Account {
	extra := map[string]any{
		extraPersonaEnabled: enabled,
	}
	if maxConcurrency > 0 {
		extra[extraPersonaMaxConcurrency] = maxConcurrency
	}
	return &Account{Concurrency: 5, Extra: extra}
}

// TestGatewayService_EffectiveAccountConcurrency_PersonaGating 覆盖 {开关 on/off} × {persona 启用/未启用}。
func TestGatewayService_EffectiveAccountConcurrency_PersonaGating(t *testing.T) {
	ctx := context.Background()

	// 开关 OFF：无论 persona 是否启用，一律返回 account.Concurrency
	svc, _ := newPersonaGatingTestService(false)
	require.Equal(t, 5, svc.effectiveAccountConcurrency(ctx, personaAccount(true, 2)))
	require.Equal(t, 5, svc.effectiveAccountConcurrency(ctx, personaAccount(false, 2)))

	// 开关 ON + persona 启用：使用人格并发；未配置人格并发时回落 account.Concurrency
	svc, _ = newPersonaGatingTestService(true)
	require.Equal(t, 2, svc.effectiveAccountConcurrency(ctx, personaAccount(true, 2)))
	require.Equal(t, 5, svc.effectiveAccountConcurrency(ctx, personaAccount(true, 0)))

	// 开关 ON + persona 未启用：不受影响
	require.Equal(t, 5, svc.effectiveAccountConcurrency(ctx, personaAccount(false, 2)))

	// settingService 缺失：恒回落 account.Concurrency
	bare := &GatewayService{}
	require.Equal(t, 5, bare.effectiveAccountConcurrency(ctx, personaAccount(true, 2)))

	// nil account 防御
	require.Equal(t, 0, svc.effectiveAccountConcurrency(ctx, nil))
}

// TestGatewayService_IsAccountWithinPersonaActiveHours 覆盖 {开关 on/off} × {persona 启用/未启用}
// 的短路语义；真实时钟仅用于「窗口排除当前小时」的负例（带跨小时边界重试防抖）。
func TestGatewayService_IsAccountWithinPersonaActiveHours(t *testing.T) {
	ctx := context.Background()

	inWindowAll := &Account{Concurrency: 5, Extra: map[string]any{
		extraPersonaEnabled:     true,
		extraPersonaTimezone:    "UTC",
		extraPersonaActiveStart: 0,
		extraPersonaActiveEnd:   24,
	}}

	// 开关 OFF：即使配置了 persona 也恒放行（零行为变化）
	svc, _ := newPersonaGatingTestService(false)
	require.True(t, svc.isAccountWithinPersonaActiveHours(ctx, inWindowAll))
	require.True(t, svc.isAccountWithinPersonaActiveHours(ctx, personaAccount(false, 0)))

	// 开关 ON + persona 未启用：放行
	svc, _ = newPersonaGatingTestService(true)
	require.True(t, svc.isAccountWithinPersonaActiveHours(ctx, personaAccount(false, 0)))

	// 开关 ON + persona 启用 + 未配置窗口：fail-open 放行
	require.True(t, svc.isAccountWithinPersonaActiveHours(ctx, personaAccount(true, 0)))

	// 开关 ON + persona 启用 + 全天窗口 [0,24)：任何时刻都在窗口内
	require.True(t, svc.isAccountWithinPersonaActiveHours(ctx, inWindowAll))

	// 开关 ON + persona 启用 + 窗口排除当前小时：应被拦截。
	// 用真实时钟构造 [h+1, h+2) 窗口；若断言期间跨越小时边界则重试。
	for attempt := 0; attempt < 3; attempt++ {
		h := time.Now().UTC().Hour()
		start := (h + 1) % 24
		outOfWindow := &Account{Concurrency: 5, Extra: map[string]any{
			extraPersonaEnabled:     true,
			extraPersonaTimezone:    "UTC",
			extraPersonaActiveStart: start,
			extraPersonaActiveEnd:   start + 1,
		}}
		got := svc.isAccountWithinPersonaActiveHours(ctx, outOfWindow)
		if time.Now().UTC().Hour() != h {
			continue // 恰好跨小时边界，重试
		}
		require.False(t, got)
		break
	}

	// nil account / settingService 缺失：放行
	require.True(t, svc.isAccountWithinPersonaActiveHours(ctx, nil))
	bare := &GatewayService{}
	require.True(t, bare.isAccountWithinPersonaActiveHours(ctx, inWindowAll))
}

// TestSettingService_IsPersonaGatingEnabled_DefaultOff 验证键缺失/非 true 时默认关闭。
func TestSettingService_IsPersonaGatingEnabled_DefaultOff(t *testing.T) {
	ctx := context.Background()

	repo := &gatewayTTLSettingRepo{data: map[string]string{}}
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
	settingSvc := NewSettingService(repo, &config.Config{})
	require.False(t, settingSvc.IsPersonaGatingEnabled(ctx))

	repo.data[SettingKeyEnablePersonaGating] = "false"
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
	require.False(t, settingSvc.IsPersonaGatingEnabled(ctx))

	repo.data[SettingKeyEnablePersonaGating] = "true"
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
	require.True(t, settingSvc.IsPersonaGatingEnabled(ctx))
}
