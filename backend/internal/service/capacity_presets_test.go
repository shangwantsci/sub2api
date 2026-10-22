//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func newPresetSettingService(data map[string]string) (*SettingService, *gatewayTTLSettingRepo) {
	repo := &gatewayTTLSettingRepo{data: data}
	return &SettingService{settingRepo: repo}, repo
}

// tierUsageStub 是 ProviderTierUsageCounter 的假实现：按 key 返回仍在使用它的供货商账号数。
type tierUsageStub struct {
	counts map[string]int
	err    error
	asked  []string
}

func (s *tierUsageStub) CountByProviderTier(_ context.Context, tier string) (int, error) {
	s.asked = append(s.asked, tier)
	if s.err != nil {
		return 0, s.err
	}
	return s.counts[tier], nil
}

func requireBadRequest(t *testing.T, err error, reason string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err), "err=%v", err)
	require.Equal(t, reason, infraerrors.Reason(err), "err=%v", err)
}

// 种子值是对外契约（前端的默认值与测试都依赖它），逐项钉死。
func TestDefaultCapacityPresets_Seed(t *testing.T) {
	presets := DefaultCapacityPresets()

	require.Equal(t, "3", presets.DefaultKey)
	require.Equal(t, []string{"1", "2", "3", "4", "5"}, presets.Keys())
	require.Equal(t, []CapacityPreset{
		{Key: "1", Label: "入门", Concurrency: 1, MaxSessions: 1, BaseRPM: 10, WindowCostLimit: 20, AdaptiveThrottle: true},
		{Key: "2", Label: "保守", Concurrency: 2, MaxSessions: 2, BaseRPM: 20, WindowCostLimit: 40, AdaptiveThrottle: true},
		{Key: "3", Label: "均衡", Concurrency: 3, MaxSessions: 3, BaseRPM: 30, WindowCostLimit: 60, AdaptiveThrottle: true},
		{Key: "4", Label: "激进", Concurrency: 5, MaxSessions: 5, BaseRPM: 50, WindowCostLimit: 100, AdaptiveThrottle: true},
		{Key: "5", Label: "旗舰", Concurrency: 8, MaxSessions: 8, BaseRPM: 80, WindowCostLimit: 0, AdaptiveThrottle: true},
	}, presets.Presets)

	// 种子必须能通过严格校验，否则首次部署一保存设置就失败。
	validated, err := ValidateCapacityPresets(presets)
	require.NoError(t, err)
	require.Equal(t, presets, validated)

	// JSON 形状是与前端的契约。
	raw, err := json.Marshal(presets)
	require.NoError(t, err)
	var shape map[string]any
	require.NoError(t, json.Unmarshal(raw, &shape))
	require.Equal(t, "3", shape["default_key"])
	first := shape["presets"].([]any)[0].(map[string]any)
	require.ElementsMatch(t,
		[]string{"key", "label", "concurrency", "max_sessions", "base_rpm", "window_cost_limit", "adaptive_throttle"},
		keysOf(first))
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestValidateCapacityPresets_RejectsInvalidTables(t *testing.T) {
	valid := func() CapacityPresets {
		return CapacityPresets{
			Presets: []CapacityPreset{
				{Key: "a", Label: "甲", Concurrency: 2, MaxSessions: 2, BaseRPM: 20, WindowCostLimit: 40},
				{Key: "b", Label: "乙", Concurrency: 3, MaxSessions: 3, BaseRPM: 30, WindowCostLimit: 60},
			},
			DefaultKey: "a",
		}
	}

	cases := []struct {
		name   string
		mutate func(*CapacityPresets)
		reason string
	}{
		{"没有任何模板", func(p *CapacityPresets) { p.Presets = nil }, "NO_CAPACITY_PRESET"},
		{"key 为空", func(p *CapacityPresets) { p.Presets[0].Key = "  " }, "INVALID_PRESET_KEY"},
		{"key 是保留字 custom", func(p *CapacityPresets) { p.Presets[0].Key = "custom" }, "INVALID_PRESET_KEY"},
		{"key 超过 20 个字符（provider_tier 列上限）", func(p *CapacityPresets) { p.Presets[0].Key = strings.Repeat("k", 21) }, "INVALID_PRESET_KEY"},
		{"key 含斜杠（会进 URL）", func(p *CapacityPresets) { p.Presets[0].Key = "a/b" }, "INVALID_PRESET_KEY"},
		{"key 重复", func(p *CapacityPresets) { p.Presets[1].Key = "a" }, "DUPLICATE_PRESET_KEY"},
		{"key 去空白后重复", func(p *CapacityPresets) { p.Presets[1].Key = " a " }, "DUPLICATE_PRESET_KEY"},
		{"并发为 0（0 是不限，不是最小）", func(p *CapacityPresets) { p.Presets[0].Concurrency = 0 }, "INVALID_PRESET_CONCURRENCY"},
		{"并发为负", func(p *CapacityPresets) { p.Presets[0].Concurrency = -1 }, "INVALID_PRESET_CONCURRENCY"},
		{"并发超上限", func(p *CapacityPresets) { p.Presets[0].Concurrency = capacityPresetMaxConcurrency + 1 }, "INVALID_PRESET_CONCURRENCY"},
		{"会话数为负", func(p *CapacityPresets) { p.Presets[0].MaxSessions = -1 }, "INVALID_PRESET_SESSIONS"},
		{"会话数超上限", func(p *CapacityPresets) { p.Presets[0].MaxSessions = capacityPresetMaxSessions + 1 }, "INVALID_PRESET_SESSIONS"},
		{"RPM 为负", func(p *CapacityPresets) { p.Presets[0].BaseRPM = -1 }, "INVALID_PRESET_RPM"},
		{"RPM 超上限", func(p *CapacityPresets) { p.Presets[0].BaseRPM = capacityPresetMaxBaseRPM + 1 }, "INVALID_PRESET_RPM"},
		{"5h 额度为负", func(p *CapacityPresets) { p.Presets[0].WindowCostLimit = -0.01 }, "INVALID_PRESET_WINDOW_COST"},
		{"5h 额度超上限", func(p *CapacityPresets) { p.Presets[0].WindowCostLimit = capacityPresetMaxWindowCostLimit + 1 }, "INVALID_PRESET_WINDOW_COST"},
		{"5h 额度是 NaN", func(p *CapacityPresets) { p.Presets[0].WindowCostLimit = math.NaN() }, "INVALID_PRESET_WINDOW_COST"},
		{"4 元组重复", func(p *CapacityPresets) {
			p.Presets[1].Concurrency, p.Presets[1].MaxSessions = 2, 2
			p.Presets[1].BaseRPM, p.Presets[1].WindowCostLimit = 20, 40
		}, "DUPLICATE_PRESET_VALUES"},
		{"默认 key 为空", func(p *CapacityPresets) { p.DefaultKey = "" }, "MISSING_DEFAULT_PRESET"},
		{"默认 key 不存在", func(p *CapacityPresets) { p.DefaultKey = "zzz" }, "INVALID_DEFAULT_PRESET"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			presets := valid()
			tc.mutate(&presets)
			_, err := ValidateCapacityPresets(presets)
			requireBadRequest(t, err, tc.reason)
		})
	}

	// 合法表通过——确认上面拒的是具体问题，而不是把整条路径堵死。
	_, err := ValidateCapacityPresets(valid())
	require.NoError(t, err)
}

// 并发之外的三项 0 表示「该限制关闭（不限）」，是既有约定，必须放行；
// 4 元组只要有一项不同就算不同（不限的 0 与具体数值可以共存）。
func TestValidateCapacityPresets_ZeroMeansUnlimitedExceptConcurrency(t *testing.T) {
	out, err := ValidateCapacityPresets(CapacityPresets{
		Presets: []CapacityPreset{
			{Key: "unlimited", Concurrency: 1, MaxSessions: 0, BaseRPM: 0, WindowCostLimit: 0},
			{Key: "limited", Concurrency: 1, MaxSessions: 1, BaseRPM: 10, WindowCostLimit: 20},
		},
		DefaultKey: "limited",
	})
	require.NoError(t, err)
	require.Len(t, out.Presets, 2)
}

// 边界值本身合法（上限含）。
func TestValidateCapacityPresets_AcceptsGuardRailBoundaries(t *testing.T) {
	_, err := ValidateCapacityPresets(CapacityPresets{
		Presets: []CapacityPreset{{
			Key: strings.Repeat("k", capacityPresetMaxKeyLength), Label: "max",
			Concurrency: capacityPresetMaxConcurrency, MaxSessions: capacityPresetMaxSessions,
			BaseRPM: capacityPresetMaxBaseRPM, WindowCostLimit: capacityPresetMaxWindowCostLimit,
		}},
		DefaultKey: strings.Repeat("k", capacityPresetMaxKeyLength),
	})
	require.NoError(t, err)
}

func TestValidateCapacityPresets_NormalizesWhitespaceAndLabel(t *testing.T) {
	out, err := ValidateCapacityPresets(CapacityPresets{
		Presets: []CapacityPreset{
			{Key: " x ", Label: "  ", Concurrency: 1},
			{Key: "y", Label: " 乙 ", Concurrency: 2},
		},
		DefaultKey: " x ",
	})
	require.NoError(t, err)
	require.Equal(t, "x", out.Presets[0].Key)
	require.Equal(t, "x", out.Presets[0].Label, "空标签回落 key")
	require.Equal(t, "乙", out.Presets[1].Label)
	require.Equal(t, "x", out.DefaultKey)
}

// 读路径必须宽松：迁移 227 从旧供货商档位表原样搬来的数据可能不满足新的写入规则。
func TestParseCapacityPresets_IsLenientForMigratedData(t *testing.T) {
	// 两个模板 4 元组相同（旧表从未要求唯一）、有行缺标签、默认键指向不存在的 key。
	raw := `{"presets":[
		{"key":"1","label":"","concurrency":2,"max_sessions":2,"base_rpm":20,"window_cost_limit":40,"adaptive_throttle":false},
		{"key":"2","label":"保守","concurrency":2,"max_sessions":2,"base_rpm":20,"window_cost_limit":40,"adaptive_throttle":true}
	],"default_key":"nope"}`

	got, ok := parseCapacityPresets(raw)
	require.True(t, ok)
	require.Len(t, got.Presets, 2, "重复 4 元组不能让读路径拒绝整张表")
	require.Equal(t, "1", got.Presets[0].Label, "缺标签回落 key")
	require.Equal(t, "1", got.DefaultKey, "默认键悬空时落到第一行")

	// 同一张表走严格校验会被拒——这正是读写两条路径规则不同的原因。
	_, err := ValidateCapacityPresets(got)
	requireBadRequest(t, err, "DUPLICATE_PRESET_VALUES")
}

func TestParseCapacityPresets_DropsUnusableRowsAndRejectsGarbage(t *testing.T) {
	got, ok := parseCapacityPresets(`{"presets":[
		{"key":"","concurrency":1},
		{"key":"custom","concurrency":1},
		{"key":"a","concurrency":1},
		{"key":" a ","concurrency":9},
		{"key":"b","concurrency":2}
	],"default_key":"b"}`)
	require.True(t, ok)
	require.Equal(t, []string{"a", "b"}, got.Keys(), "缺 key / custom / 重复 key 的行被丢弃，重复取第一行")
	require.Equal(t, 1, got.Presets[0].Concurrency)
	require.Equal(t, "b", got.DefaultKey)

	for _, raw := range []string{"", "   ", "not json", `[]`, `{}`, `{"presets":[]}`, `{"presets":[{"key":"custom"}]}`, `{"presets":"x"}`} {
		_, ok := parseCapacityPresets(raw)
		require.False(t, ok, "raw=%q", raw)
	}
}

// 存的表有内容却不可用（字段类型不对会让整张表解析失败）时，读路径回落种子——
// 必须不报错、不 panic；保存时按种子表比对「仍在使用的 key」，所以也不会误放行删除。
func TestGetCapacityPresets_UnusableStoredTableFallsBackToSeed(t *testing.T) {
	for name, raw := range map[string]string{
		"数值字段写成了字符串": `{"presets":[{"key":"a","concurrency":"3"}],"default_key":"a"}`,
		"整张表是数组":     `[{"key":"a","concurrency":1}]`,
		"只有被丢弃的行":    `{"presets":[{"key":"custom","concurrency":1}],"default_key":"custom"}`,
	} {
		t.Run(name, func(t *testing.T) {
			svc, _ := newPresetSettingService(map[string]string{SettingKeyCapacityPresets: raw})
			got, err := svc.GetCapacityPresets(context.Background())
			require.NoError(t, err)
			require.Equal(t, DefaultCapacityPresets(), got)
		})
	}
}

func TestGetCapacityPresets_FallsBackToSeedAndReadsStoredTable(t *testing.T) {
	ctx := context.Background()

	// 设置缺失 → 种子。
	svc, _ := newPresetSettingService(map[string]string{})
	got, err := svc.GetCapacityPresets(ctx)
	require.NoError(t, err)
	require.Equal(t, DefaultCapacityPresets(), got)

	// 存的是垃圾 → 种子（不报错）。
	svc, _ = newPresetSettingService(map[string]string{SettingKeyCapacityPresets: "not json"})
	got, err = svc.GetCapacityPresets(ctx)
	require.NoError(t, err)
	require.Equal(t, DefaultCapacityPresets(), got)

	// 存了合法表 → 读到存的表。
	svc, _ = newPresetSettingService(map[string]string{
		SettingKeyCapacityPresets: `{"presets":[{"key":"solo","label":"独档","concurrency":4,"max_sessions":4,"base_rpm":40,"window_cost_limit":80,"adaptive_throttle":true}],"default_key":"solo"}`,
	})
	got, err = svc.GetCapacityPresets(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"solo"}, got.Keys())
	require.Equal(t, "solo", got.DefaultKey)
	require.True(t, got.Presets[0].AdaptiveThrottle)

	// 服务不可用 → 种子 + 错误。
	var nilSvc *SettingService
	got, err = nilSvc.GetCapacityPresets(ctx)
	require.Error(t, err)
	require.Equal(t, DefaultCapacityPresets(), got)
}

func TestSaveCapacityPresets_PersistsNormalizedTable(t *testing.T) {
	ctx := context.Background()
	svc, repo := newPresetSettingService(map[string]string{})

	saved, err := svc.SaveCapacityPresets(ctx, CapacityPresets{
		Presets: []CapacityPreset{
			{Key: " 1 ", Label: "", Concurrency: 1, MaxSessions: 1, BaseRPM: 10, WindowCostLimit: 20, AdaptiveThrottle: true},
			{Key: "2", Label: "二档", Concurrency: 2, MaxSessions: 2, BaseRPM: 20, WindowCostLimit: 40},
		},
		DefaultKey: "1",
	}, &tierUsageStub{})
	require.NoError(t, err)
	require.Equal(t, "1", saved.Presets[0].Key)
	require.Equal(t, "1", saved.Presets[0].Label)

	stored, ok := repo.data[SettingKeyCapacityPresets]
	require.True(t, ok)
	var roundTrip CapacityPresets
	require.NoError(t, json.Unmarshal([]byte(stored), &roundTrip))
	require.Equal(t, saved, roundTrip)

	got, err := svc.GetCapacityPresets(ctx)
	require.NoError(t, err)
	require.Equal(t, saved, got)
}

// 无效的表不能落库，已有的表原样保留。
func TestSaveCapacityPresets_InvalidTableIsNotPersisted(t *testing.T) {
	svc, repo := newPresetSettingService(map[string]string{SettingKeyCapacityPresets: `{"presets":[{"key":"keep","concurrency":1}],"default_key":"keep"}`})
	before := repo.data[SettingKeyCapacityPresets]

	_, err := svc.SaveCapacityPresets(context.Background(), CapacityPresets{
		Presets:    []CapacityPreset{{Key: "bad", Concurrency: 0}},
		DefaultKey: "bad",
	}, &tierUsageStub{})
	requireBadRequest(t, err, "INVALID_PRESET_CONCURRENCY")
	require.Equal(t, before, repo.data[SettingKeyCapacityPresets])
}

// 已存在的 key 若仍被供货商账号的 provider_tier 引用，不能被删除或改名（改名 = 删旧 key + 加新 key）；
// 没被引用的 key 可以删除，新增 key 永远可以。
func TestSaveCapacityPresets_ProtectsKeysStillUsedByProviderAccounts(t *testing.T) {
	ctx := context.Background()
	seed := DefaultCapacityPresets()
	storedSeed, err := json.Marshal(seed)
	require.NoError(t, err)

	table := func(keys ...string) CapacityPresets {
		out := CapacityPresets{DefaultKey: keys[0]}
		for i, key := range keys {
			out.Presets = append(out.Presets, CapacityPreset{
				Key: key, Label: key, Concurrency: i + 1, MaxSessions: i + 1, BaseRPM: (i + 1) * 10,
			})
		}
		return out
	}

	t.Run("删除仍在使用的 key 被拒绝", func(t *testing.T) {
		svc, repo := newPresetSettingService(map[string]string{SettingKeyCapacityPresets: string(storedSeed)})
		usage := &tierUsageStub{counts: map[string]int{"3": 4}}

		_, err := svc.SaveCapacityPresets(ctx, table("1", "2", "4", "5"), usage) // 去掉了 3
		requireBadRequest(t, err, "CAPACITY_PRESET_IN_USE")
		require.Contains(t, infraerrors.Message(err), `"3"`)
		require.Equal(t, string(storedSeed), repo.data[SettingKeyCapacityPresets], "被拒绝时不能落库")
	})

	t.Run("改名等价于删旧 key，同样被拒绝", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{SettingKeyCapacityPresets: string(storedSeed)})
		usage := &tierUsageStub{counts: map[string]int{"3": 1}}

		_, err := svc.SaveCapacityPresets(ctx, table("1", "2", "3-renamed", "4", "5"), usage)
		requireBadRequest(t, err, "CAPACITY_PRESET_IN_USE")
	})

	t.Run("没被引用的 key 可以删除，新增 key 可以", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{SettingKeyCapacityPresets: string(storedSeed)})
		usage := &tierUsageStub{counts: map[string]int{"3": 4}}

		saved, err := svc.SaveCapacityPresets(ctx, table("1", "3", "9"), usage) // 删 2/4/5，增 9
		require.NoError(t, err)
		require.Equal(t, []string{"1", "3", "9"}, saved.Keys())
		// 只会为「被移除的旧 key」查询使用情况。
		require.ElementsMatch(t, []string{"2", "4", "5"}, usage.asked)
	})

	t.Run("查询使用情况出错时整体失败，不落库", func(t *testing.T) {
		svc, repo := newPresetSettingService(map[string]string{SettingKeyCapacityPresets: string(storedSeed)})
		boom := errors.New("db down")

		_, err := svc.SaveCapacityPresets(ctx, table("1"), &tierUsageStub{err: boom})
		require.ErrorIs(t, err, boom)
		require.Equal(t, string(storedSeed), repo.data[SettingKeyCapacityPresets])
	})

	t.Run("首次保存（还没有存过表）按种子表比对", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{})
		usage := &tierUsageStub{counts: map[string]int{"5": 2}}

		_, err := svc.SaveCapacityPresets(ctx, table("1", "2", "3", "4"), usage) // 去掉种子里的 5
		requireBadRequest(t, err, "CAPACITY_PRESET_IN_USE")
	})
}

// ===== 供货商设置与模板表的关系 =====

func TestGetProviderSettings_TierDefaultsAndReferences(t *testing.T) {
	ctx := context.Background()

	t.Run("什么都没存：种子模板、全部开放、默认 3", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{})
		got, err := svc.GetProviderSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, DefaultCapacityPresets().Presets, got.Presets)
		require.Equal(t, []string{"1", "2", "3", "4", "5"}, got.EnabledTierKeys)
		require.Equal(t, "3", got.DefaultTier)
		require.Len(t, got.EnabledTiers(), 5)
	})

	t.Run("开放列表缺省 = 模板表里的全部 key（含自定义模板）", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{
			SettingKeyCapacityPresets: `{"presets":[{"key":"a","concurrency":1},{"key":"b","concurrency":2}],"default_key":"b"}`,
		})
		got, err := svc.GetProviderSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"a", "b"}, got.EnabledTierKeys)
		require.Equal(t, "b", got.DefaultTier, "没存默认档位时取模板表的默认 key")
	})

	t.Run("存了开放列表：只开放列出的、且存在的 key", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{
			SettingKeyProviderEnabledTierKeys: `["2","4","ghost","2"]`,
			SettingKeyProviderDefaultTier:     "4",
		})
		got, err := svc.GetProviderSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"2", "4"}, got.EnabledTierKeys, "悬空 key 与重复项被忽略")
		require.Equal(t, "4", got.DefaultTier)
		keys := make([]string, 0)
		for _, preset := range got.EnabledTiers() {
			keys = append(keys, preset.Key)
		}
		require.Equal(t, []string{"2", "4"}, keys)
	})

	t.Run("存的默认档位不在开放列表里：落到第一个开放档位", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{
			SettingKeyProviderEnabledTierKeys: `["2","4"]`,
			SettingKeyProviderDefaultTier:     "3",
		})
		got, err := svc.GetProviderSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, "2", got.DefaultTier)
	})

	t.Run("开放列表不是合法 JSON：按全部开放处理", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{SettingKeyProviderEnabledTierKeys: "not json"})
		got, err := svc.GetProviderSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"1", "2", "3", "4", "5"}, got.EnabledTierKeys)
	})

	// JSON null 与「设置缺失」同义（= 全部开放）；json.Unmarshal 对 null 不报错且得到 nil 切片，
	// 必须和显式存下的 [] 区分开，否则一个 null 就会让供号商上号页没有任何固定档。
	t.Run("开放列表是 JSON null：按设置缺失处理，全部开放", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{SettingKeyProviderEnabledTierKeys: "null"})
		got, err := svc.GetProviderSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"1", "2", "3", "4", "5"}, got.EnabledTierKeys)
		require.Equal(t, "3", got.DefaultTier)
	})

	t.Run("显式存下的空数组就是没有任何档位对供号商开放", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{SettingKeyProviderEnabledTierKeys: "[]"})
		got, err := svc.GetProviderSettings(ctx)
		require.NoError(t, err)
		require.NotNil(t, got.EnabledTierKeys)
		require.Empty(t, got.EnabledTierKeys)
		require.Empty(t, got.EnabledTiers())

		raw, err := json.Marshal(got)
		require.NoError(t, err)
		require.Contains(t, string(raw), `"enabled_tier_keys":[]`, "空列表必须序列化成 [] 而不是 null")
	})

	// 管理员把对供号商开放的那几个模板都删了（它们没被账号引用所以允许删）：
	// 过滤后一个不剩。结果是「没开放任何固定档」，而不是静默把新建的模板全部暴露给供号商。
	t.Run("开放的 key 全部悬空：不开放任何档位（失败即关闭），新模板不会被悄悄暴露", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{
			SettingKeyCapacityPresets:         `{"presets":[{"key":"a","concurrency":1},{"key":"b","concurrency":2}],"default_key":"a"}`,
			SettingKeyProviderEnabledTierKeys: `["1","2","3"]`,
			SettingKeyProviderDefaultTier:     "3",
		})
		got, err := svc.GetProviderSettings(ctx)
		require.NoError(t, err)
		require.Empty(t, got.EnabledTierKeys)
		require.Empty(t, got.EnabledTiers())
		_, ok := got.FindTier("a")
		require.False(t, ok, "管理员没对 a 开放，它不能被供号商选到")
	})

	t.Run("旧的 provider_capacity_tiers 行不再被读取", func(t *testing.T) {
		svc, _ := newPresetSettingService(map[string]string{
			"provider_capacity_tiers": `[{"tier":"legacy","label":"旧","concurrency":9,"enabled":true}]`,
		})
		got, err := svc.GetProviderSettings(ctx)
		require.NoError(t, err)
		require.NotContains(t, got.EnabledTierKeys, "legacy")
	})
}

func TestProviderSettingsJSONShape(t *testing.T) {
	settings := defaultProviderSettings()
	raw, err := json.Marshal(settings)
	require.NoError(t, err)

	var shape map[string]any
	require.NoError(t, json.Unmarshal(raw, &shape))
	require.Contains(t, shape, "enabled_tier_keys")
	require.Contains(t, shape, "default_tier")
	require.NotContains(t, shape, "capacity_tiers", "capacity_tiers 已并入容量档位模板")
	require.NotContains(t, shape, "presets", "模板表不随供货商设置序列化")
	require.Equal(t, []any{"1", "2", "3", "4", "5"}, shape["enabled_tier_keys"])
}

func TestSaveProviderSettings_WritesReferencesOnly(t *testing.T) {
	ctx := context.Background()
	svc, repo := newPresetSettingService(map[string]string{})

	in := defaultProviderSettings()
	in.EnabledTierKeys = []string{"3", "1"}
	in.DefaultTier = "3"
	// 调用方带来的 Presets 不被信任：保存时以库里的模板为准。
	in.Presets = []CapacityPreset{{Key: "forged", Concurrency: 1}}
	require.NoError(t, svc.SaveProviderSettings(ctx, in))

	require.JSONEq(t, `["3","1"]`, repo.data[SettingKeyProviderEnabledTierKeys])
	require.Equal(t, "3", repo.data[SettingKeyProviderDefaultTier])
	require.NotContains(t, repo.data, SettingKeyCapacityPresets, "保存供货商设置绝不能写模板表")
	require.NotContains(t, repo.data, "provider_capacity_tiers")

	got, err := svc.GetProviderSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"3", "1"}, got.EnabledTierKeys)
	require.Equal(t, DefaultCapacityPresets().Presets, got.Presets)
}

func TestSaveProviderSettings_ValidatesAgainstLatestPresets(t *testing.T) {
	ctx := context.Background()
	svc, repo := newPresetSettingService(map[string]string{
		SettingKeyCapacityPresets: `{"presets":[{"key":"a","concurrency":1},{"key":"b","concurrency":2}],"default_key":"a"}`,
	})

	// 默认种子里的 "3" 在当前模板表里不存在。
	in := defaultProviderSettings()
	err := svc.SaveProviderSettings(ctx, in)
	requireBadRequest(t, err, "UNKNOWN_TIER")
	require.NotContains(t, repo.data, SettingKeyProviderEnabledTierKeys)

	in.EnabledTierKeys = []string{"a"}
	in.DefaultTier = "b" // 存在但没开放
	err = svc.SaveProviderSettings(ctx, in)
	requireBadRequest(t, err, "INVALID_DEFAULT_TIER")

	in.DefaultTier = "a"
	require.NoError(t, svc.SaveProviderSettings(ctx, in))
	require.JSONEq(t, `["a"]`, repo.data[SettingKeyProviderEnabledTierKeys])
}
