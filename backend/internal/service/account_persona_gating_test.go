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

func presetAccount(preset string) *Account {
	return &Account{ID: 42, Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset: preset,
	}}
}

func TestGatewayService_IsAccountWithinPersonaDailyCap(t *testing.T) {
	ctx := context.Background()

	// 不再依赖全局开关——直接基于账号配置生效
	svc, _ := newPersonaGatingTestService(false)
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

	// preset 账号默认不限日请求（DailyCap=0，由动态并发调节），恒放行
	presetAcc := &Account{ID: 99, Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset:   "balanced",
		extraPersonaTimezone: "UTC",
	}}
	env := presetAcc.GetPersonaEnvelope()
	require.Equal(t, 0, env.DailyCap) // balanced 默认不限
	svc.rpmCache = &fakePersonaRPMCache{}
	require.True(t, svc.isAccountWithinPersonaDailyCap(ctx, presetAcc))

	// 手动设了 DailyCap 的 preset 账号，日上限生效
	presetWithCap := &Account{ID: 100, Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset:   "balanced",
		extraPersonaTimezone: "UTC",
		extraPersonaDailyCap: 300,
	}}
	envCap := presetWithCap.GetPersonaEnvelope()
	require.Equal(t, 300, envCap.DailyCap)
	svc.rpmCache = &fakePersonaRPMCache{daily: map[string]int{
		fmt.Sprintf("%d:%s", presetWithCap.ID, envCap.DayKey(time.Now())): 300,
	}}
	require.False(t, svc.isAccountWithinPersonaDailyCap(ctx, presetWithCap))
}

func TestGatewayService_IncrementAccountPersonaDailyRequest(t *testing.T) {
	ctx := context.Background()

	// persona 启用 + cap>0 → 计数递增（不再需要全局开关）。
	svc, _ := newPersonaGatingTestService(false)
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

	// persona 未启用 → 不写入。
	fake3 := &fakePersonaRPMCache{}
	svc.rpmCache = fake3
	svc.IncrementAccountPersonaDailyRequest(ctx, personaDailyAccount(false, 5))
	require.Empty(t, fake3.daily)

	// preset 账号默认 DailyCap=0 → 不写入（由动态并发调节，无需日计数）
	fake4 := &fakePersonaRPMCache{}
	svc.rpmCache = fake4
	svc.IncrementAccountPersonaDailyRequest(ctx, presetAccount("balanced"))
	require.Empty(t, fake4.daily)

	// preset 账号手动设了 DailyCap → 递增
	fake5 := &fakePersonaRPMCache{}
	svc.rpmCache = fake5
	accWithCap := &Account{ID: 42, Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset:   "balanced",
		extraPersonaDailyCap: 500,
	}}
	svc.IncrementAccountPersonaDailyRequest(ctx, accWithCap)
	require.NotEmpty(t, fake5.daily)
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

func TestValidatePersonaFieldValues_PresetValidation(t *testing.T) {
	// 合法预设名。
	require.NoError(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaPreset: "balanced"}))
	require.NoError(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaPreset: "aggressive"}))
	require.NoError(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaPreset: "conservative"}))
	require.NoError(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaPreset: "off"}))
	// 非法预设名。
	require.Error(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaPreset: "turbo"}))
	require.Error(t, ValidatePersonaExtraPatch(map[string]any{extraPersonaPreset: "max"}))
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

// TestGatewayService_EffectiveAccountConcurrency_PersonaGating 覆盖 persona 启用/未启用 的并发计算。
// 不再依赖全局开关——配了 persona 就生效。
func TestGatewayService_EffectiveAccountConcurrency_PersonaGating(t *testing.T) {
	ctx := context.Background()

	svc, _ := newPersonaGatingTestService(false)

	// persona 启用：使用人格并发（动态调节，无利用率数据时 = base）
	require.Equal(t, 2, svc.effectiveAccountConcurrency(ctx, personaAccount(true, 2)))
	// persona 启用但未配置人格并发：回落 account.Concurrency
	require.Equal(t, 5, svc.effectiveAccountConcurrency(ctx, personaAccount(true, 0)))

	// persona 未启用：不受影响
	require.Equal(t, 5, svc.effectiveAccountConcurrency(ctx, personaAccount(false, 2)))

	// nil account 防御
	require.Equal(t, 0, svc.effectiveAccountConcurrency(ctx, nil))

	// preset 账号自动生效
	require.Equal(t, 3, svc.effectiveAccountConcurrency(ctx, presetAccount("balanced")))
	require.Equal(t, 5, svc.effectiveAccountConcurrency(ctx, presetAccount("aggressive")))
	require.Equal(t, 2, svc.effectiveAccountConcurrency(ctx, presetAccount("conservative")))
	// preset=off → 使用 account.Concurrency
	require.Equal(t, 5, svc.effectiveAccountConcurrency(ctx, presetAccount("off")))
}

// TestGatewayService_IsAccountWithinPersonaActiveHours 覆盖 persona 启用/未启用
// 的短路语义；真实时钟仅用于「窗口排除当前小时」的负例（带跨小时边界重试防抖）。
func TestGatewayService_IsAccountWithinPersonaActiveHours(t *testing.T) {
	ctx := context.Background()

	inWindowAll := &Account{Concurrency: 5, Extra: map[string]any{
		extraPersonaEnabled:     true,
		extraPersonaTimezone:    "UTC",
		extraPersonaActiveStart: 0,
		extraPersonaActiveEnd:   24,
	}}

	svc, _ := newPersonaGatingTestService(false)

	// persona 启用 + 全天窗口：放行
	require.True(t, svc.isAccountWithinPersonaActiveHours(ctx, inWindowAll))

	// persona 未启用：放行
	require.True(t, svc.isAccountWithinPersonaActiveHours(ctx, personaAccount(false, 0)))

	// persona 启用 + 未配置窗口：fail-open 放行
	require.True(t, svc.isAccountWithinPersonaActiveHours(ctx, personaAccount(true, 0)))

	// persona 启用 + 窗口排除当前小时：应被拦截。
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

	// nil account：放行
	require.True(t, svc.isAccountWithinPersonaActiveHours(ctx, nil))

	// preset 账号（无作息窗口）：恒放行
	require.True(t, svc.isAccountWithinPersonaActiveHours(ctx, presetAccount("balanced")))
}

// TestSettingService_IsPersonaGatingEnabled_DefaultOff 验证键缺失/非 true 时默认关闭。
// 注：全局开关保留用于向后兼容，但新设计下不再是必需的。
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

// TestEffectivePersonaMaxConcurrency_DynamicByTime 验证活跃/非活跃时段动态并发。
// 旧模式（手动配置）保持活跃/非活跃逻辑 + 动态调节。
func TestEffectivePersonaMaxConcurrency_DynamicByTime(t *testing.T) {
	// 1. 未启用 persona → 返回 account.Concurrency
	disabled := &Account{Concurrency: 8, Extra: map[string]any{
		extraPersonaEnabled:        false,
		extraPersonaMaxConcurrency: 4,
	}}
	require.Equal(t, 8, disabled.EffectivePersonaMaxConcurrency())

	// 未配置 Extra → 回落 Concurrency
	bare := &Account{Concurrency: 3}
	require.Equal(t, 3, bare.EffectivePersonaMaxConcurrency())

	for attempt := 0; attempt < 3; attempt++ {
		h := time.Now().UTC().Hour()

		// 2. 启用 persona + 全天窗口 → 返回 MaxConcurrency（无利用率数据时 = base）
		inWindow := &Account{Concurrency: 8, Extra: map[string]any{
			extraPersonaEnabled:        true,
			extraPersonaTimezone:       "UTC",
			extraPersonaActiveStart:    0,
			extraPersonaActiveEnd:      24,
			extraPersonaMaxConcurrency: 6,
		}}
		require.Equal(t, 6, inWindow.EffectivePersonaMaxConcurrency())

		// 窗口精确覆盖当前小时
		start := h
		end := h + 1
		if end > 24 {
			end = 24
		}
		inExact := &Account{Concurrency: 8, Extra: map[string]any{
			extraPersonaEnabled:        true,
			extraPersonaTimezone:       "UTC",
			extraPersonaActiveStart:    start,
			extraPersonaActiveEnd:      end,
			extraPersonaMaxConcurrency: 6,
		}}
		gotIn := inExact.EffectivePersonaMaxConcurrency()
		if time.Now().UTC().Hour() != h {
			continue
		}
		require.Equal(t, 6, gotIn)

		// 3. 非活跃时段 + 显式 OffPeakConcurrency
		offStart := (h + 1) % 24
		offEnd := offStart + 1
		if offEnd > 24 {
			offEnd = 24
		}
		outExplicit := &Account{Concurrency: 8, Extra: map[string]any{
			extraPersonaEnabled:            true,
			extraPersonaTimezone:           "UTC",
			extraPersonaActiveStart:        offStart,
			extraPersonaActiveEnd:          offEnd,
			extraPersonaMaxConcurrency:     6,
			extraPersonaOffPeakConcurrency: 2,
		}}
		gotOut := outExplicit.EffectivePersonaMaxConcurrency()
		if time.Now().UTC().Hour() != h {
			continue
		}
		require.Equal(t, 2, gotOut)

		// 4. 非活跃时段 + OffPeakConcurrency=0 → max(MaxConcurrency/2, 1)
		outAuto := &Account{Concurrency: 8, Extra: map[string]any{
			extraPersonaEnabled:        true,
			extraPersonaTimezone:       "UTC",
			extraPersonaActiveStart:    offStart,
			extraPersonaActiveEnd:      offEnd,
			extraPersonaMaxConcurrency: 6,
		}}
		gotAuto := outAuto.EffectivePersonaMaxConcurrency()
		if time.Now().UTC().Hour() != h {
			continue
		}
		require.Equal(t, 3, gotAuto) // 6/2 = 3

		// 5. MaxConcurrency=1 + 非活跃 → 返回 1（不会返回 0）
		outMin := &Account{Concurrency: 8, Extra: map[string]any{
			extraPersonaEnabled:        true,
			extraPersonaTimezone:       "UTC",
			extraPersonaActiveStart:    offStart,
			extraPersonaActiveEnd:      offEnd,
			extraPersonaMaxConcurrency: 1,
		}}
		gotMin := outMin.EffectivePersonaMaxConcurrency()
		if time.Now().UTC().Hour() != h {
			continue
		}
		require.Equal(t, 1, gotMin)
		return
	}
	t.Fatal("could not complete DynamicByTime assertions without crossing hour boundary")
}

// ========== 新增：预设档位 + 动态并发测试 ==========

func TestPersonaPreset_GetPersonaEnvelope(t *testing.T) {
	// balanced preset 自动填充默认值
	acc := &Account{Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset: "balanced",
	}}
	env := acc.GetPersonaEnvelope()
	require.True(t, env.Enabled)
	require.Equal(t, PersonaPresetBalanced, env.Preset)
	require.Equal(t, 3, env.MaxConcurrency)
	require.Equal(t, 0, env.DailyCap) // 不限，由动态并发调节
	require.Equal(t, "America/New_York", env.Timezone)
	require.Equal(t, "en-US", env.Locale)

	// aggressive preset
	accAgg := &Account{Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset: "aggressive",
	}}
	envAgg := accAgg.GetPersonaEnvelope()
	require.True(t, envAgg.Enabled)
	require.Equal(t, 5, envAgg.MaxConcurrency)
	require.Equal(t, 0, envAgg.DailyCap)

	// conservative preset
	accCon := &Account{Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset: "conservative",
	}}
	envCon := accCon.GetPersonaEnvelope()
	require.True(t, envCon.Enabled)
	require.Equal(t, 2, envCon.MaxConcurrency)
	require.Equal(t, 0, envCon.DailyCap)

	// off preset → 未启用
	accOff := &Account{Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset: "off",
	}}
	envOff := accOff.GetPersonaEnvelope()
	require.False(t, envOff.Enabled)

	// 手动字段覆盖预设默认值
	accOverride := &Account{Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset:         "balanced",
		extraPersonaMaxConcurrency: 4,
		extraPersonaDailyCap:       300,
		extraPersonaTimezone:       "America/Los_Angeles",
	}}
	envOverride := accOverride.GetPersonaEnvelope()
	require.True(t, envOverride.Enabled)
	require.Equal(t, 4, envOverride.MaxConcurrency) // 手动覆盖
	require.Equal(t, 300, envOverride.DailyCap)     // 手动覆盖
	require.Equal(t, "America/Los_Angeles", envOverride.Timezone) // 手动覆盖
	require.Equal(t, "en-US", envOverride.Locale)                  // 预设默认值

	// 无预设无 persona → 全部为零值
	accNone := &Account{Concurrency: 5, Extra: map[string]any{}}
	envNone := accNone.GetPersonaEnvelope()
	require.False(t, envNone.Enabled)
	require.Equal(t, 0, envNone.MaxConcurrency)
}

func TestThrottleFactor(t *testing.T) {
	// 无数据 → 全速
	require.InDelta(t, 1.0, throttleFactor(0), 0.01)
	require.InDelta(t, 1.0, throttleFactor(-1), 0.01)

	// 低利用率 → 全速
	require.InDelta(t, 1.0, throttleFactor(0.20), 0.01)
	require.InDelta(t, 1.0, throttleFactor(0.49), 0.01)

	// 50% 边界
	require.InDelta(t, 1.0, throttleFactor(0.50), 0.01)

	// 中等利用率 → 线性下降
	require.InDelta(t, 0.75, throttleFactor(0.65), 0.01) // 50-80 区间中点

	// 80% 边界
	require.InDelta(t, 0.50, throttleFactor(0.80), 0.01)

	// 高利用率 → 继续下降
	require.InDelta(t, 0.30, throttleFactor(0.90), 0.05) // 大约在中间

	// 95% 边界（>= 0.95 进入最低档 0.1）
	require.InDelta(t, 0.10, throttleFactor(0.95), 0.01)

	// 超高 → 最低
	require.InDelta(t, 0.10, throttleFactor(0.99), 0.01)
	require.InDelta(t, 0.10, throttleFactor(1.00), 0.01)
}

func TestDynamicConcurrency(t *testing.T) {
	// 无利用率 → 返回 base
	env := PersonaEnvelope{Enabled: true, MaxConcurrency: 5}
	require.Equal(t, 5, env.DynamicConcurrency(5))

	// 低利用率 → 保持 base
	envLow := PersonaEnvelope{Enabled: true, Utilization5h: 0.3, Utilization7d: 0.2}
	require.Equal(t, 5, envLow.DynamicConcurrency(5))

	// 5h 高利用率 → 减速
	envHigh5h := PersonaEnvelope{Enabled: true, Utilization5h: 0.85}
	result := envHigh5h.DynamicConcurrency(5)
	require.True(t, result < 5, "should reduce from base 5, got %d", result)
	require.True(t, result >= 1, "should be at least 1, got %d", result)

	// 7d 高利用率 → 减速
	envHigh7d := PersonaEnvelope{Enabled: true, Utilization7d: 0.90}
	result7d := envHigh7d.DynamicConcurrency(5)
	require.True(t, result7d < 5, "should reduce from base 5, got %d", result7d)

	// 两个都高 → 取更低的因子
	envBothHigh := PersonaEnvelope{Enabled: true, Utilization5h: 0.60, Utilization7d: 0.90}
	resultBoth := envBothHigh.DynamicConcurrency(5)
	require.True(t, resultBoth <= result7d, "should not exceed 7d-only result")

	// 极高利用率 → 最低并发
	envExtreme := PersonaEnvelope{Enabled: true, Utilization5h: 0.99, Utilization7d: 0.99}
	require.Equal(t, 1, envExtreme.DynamicConcurrency(5))

	// base 为 0 或负数 → 返回 1
	require.Equal(t, 1, env.DynamicConcurrency(0))
	require.Equal(t, 1, env.DynamicConcurrency(-1))
}

func TestPresetAccount_DynamicConcurrency_WithUtilization(t *testing.T) {
	// balanced preset + 低利用率 → base=3
	accLow := &Account{Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset:            "balanced",
		extraSessionWindowUtilization: 0.2,
	}}
	require.Equal(t, 3, accLow.EffectivePersonaConcurrency())

	// balanced preset + 高利用率 → 降速
	accHigh := &Account{Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset:            "balanced",
		extraSessionWindowUtilization: 0.85,
	}}
	result := accHigh.EffectivePersonaConcurrency()
	require.True(t, result < 3 && result >= 1,
		"balanced base=3 at 85%% utilization should reduce, got %d", result)

	// aggressive preset + 极高利用率 → 最低
	accExtreme := &Account{Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset:            "aggressive",
		extraSessionWindowUtilization: 0.99,
		extraPassiveUsage7d:           0.98,
	}}
	require.Equal(t, 1, accExtreme.EffectivePersonaConcurrency())
}

func TestDynamicRPM(t *testing.T) {
	// 无利用率 → 返回 base
	env := PersonaEnvelope{Enabled: true}
	require.Equal(t, 10, env.DynamicRPM(10))

	// 低利用率 → 保持 base
	envLow := PersonaEnvelope{Enabled: true, Utilization5h: 0.3}
	require.Equal(t, 10, envLow.DynamicRPM(10))

	// 高利用率 → 减速
	envHigh := PersonaEnvelope{Enabled: true, Utilization5h: 0.85}
	result := envHigh.DynamicRPM(10)
	require.True(t, result < 10 && result >= 1,
		"RPM should reduce at 85%% utilization, got %d", result)

	// 极高利用率 → 最低
	envExtreme := PersonaEnvelope{Enabled: true, Utilization5h: 0.99, Utilization7d: 0.99}
	require.Equal(t, 1, envExtreme.DynamicRPM(10))

	// base=0 → 保持 0（未配置）
	require.Equal(t, 0, env.DynamicRPM(0))
}

func TestEffectivePersonaRPM(t *testing.T) {
	// 未启用 persona → 返回原始 baseRPM
	acc := &Account{Concurrency: 5, Extra: map[string]any{
		"base_rpm": 20,
	}}
	require.Equal(t, 20, acc.EffectivePersonaRPM())

	// 启用 preset + 低利用率 → 保持 baseRPM
	accPreset := &Account{Concurrency: 5, Extra: map[string]any{
		"base_rpm":                    20,
		extraPersonaPreset:            "balanced",
		extraSessionWindowUtilization: 0.2,
	}}
	require.Equal(t, 20, accPreset.EffectivePersonaRPM())

	// 启用 preset + 高利用率 → RPM 降低
	accHigh := &Account{Concurrency: 5, Extra: map[string]any{
		"base_rpm":                    20,
		extraPersonaPreset:            "balanced",
		extraSessionWindowUtilization: 0.85,
	}}
	result := accHigh.EffectivePersonaRPM()
	require.True(t, result < 20 && result >= 1,
		"RPM should reduce at high utilization, got %d", result)

	// 无 base_rpm + balanced → 使用档位推荐值 30
	accNoRPM := &Account{Concurrency: 5, Extra: map[string]any{
		extraPersonaPreset: "balanced",
	}}
	require.Equal(t, 30, accNoRPM.EffectivePersonaRPM())

	// 显式 base_rpm 覆盖推荐值
	accOverride := &Account{Concurrency: 5, Extra: map[string]any{
		"base_rpm":         12,
		extraPersonaPreset: "aggressive",
	}}
	require.Equal(t, 12, accOverride.EffectivePersonaRPM())

	// 未启用 persona 且无 base_rpm → 0
	require.Equal(t, 0, (&Account{Concurrency: 5}).EffectivePersonaRPM())
}

func TestIsPersonaEnabled_PresetVsManual(t *testing.T) {
	// preset 生效
	require.True(t, presetAccount("balanced").IsPersonaEnabled())
	require.True(t, presetAccount("aggressive").IsPersonaEnabled())
	require.True(t, presetAccount("conservative").IsPersonaEnabled())
	// preset=off → 未启用
	require.False(t, presetAccount("off").IsPersonaEnabled())
	// 无预设 + 手动启用
	require.True(t, personaAccount(true, 0).IsPersonaEnabled())
	// 无预设 + 手动未启用
	require.False(t, personaAccount(false, 0).IsPersonaEnabled())
	// 空 extra
	require.False(t, (&Account{Concurrency: 5}).IsPersonaEnabled())
}
