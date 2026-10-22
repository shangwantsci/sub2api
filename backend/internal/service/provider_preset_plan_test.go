//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// tier3 是种子里的「3 档」：并发 3、会话 3、RPM 30、5h 额度 60、动态降速开。
func tier3() ResolvedProviderTier {
	return ResolvedProviderTier{
		Tier: "3", Concurrency: 3, MaxSessions: 3, BaseRPM: 30, WindowCostLimit: 60,
		AdaptiveThrottle: boolPtr(true),
	}
}

// tier3Account 返回各项都与 tier3 一致的账号。load_factor 为空：有效负载因子取并发，同样是 3。
func tier3Account(id int64) Account {
	return Account{
		ID: id, Name: fmt.Sprintf("acc-%d", id), Concurrency: 3,
		Extra: map[string]any{
			"max_sessions": 3, "base_rpm": 30, "window_cost_limit": 60.0, "adaptive_throttle": true,
		},
	}
}

// withExtra 在 tier3Account 的基础上覆盖（或用 nil 删除）extra 里的键。
func withExtra(a Account, patch map[string]any) Account {
	extra := make(map[string]any, len(a.Extra))
	for k, v := range a.Extra {
		extra[k] = v
	}
	for k, v := range patch {
		if v == nil {
			delete(extra, k)
		} else {
			extra[k] = v
		}
	}
	a.Extra = extra
	return a
}

func TestPlanPresetApplication_AllInSyncYieldsEmptyPlan(t *testing.T) {
	plan := PlanPresetApplication(tier3(), []Account{tier3Account(1), tier3Account(2)})
	require.Empty(t, plan)
	require.NotNil(t, plan, "空结果也要是 [] 而不是 null，预览响应直接序列化它")

	require.Empty(t, PlanPresetApplication(tier3(), nil))
}

// 6 项里每一项单独不同，都恰好只产生那一项的变更；Field 取值是与前端的契约，写字面量。
func TestPlanPresetApplication_EachFieldAloneIsDetected(t *testing.T) {
	// 账号显式记着 load_factor=3（上号与「应用到存量」都是这样写的）：这样只改并发时，
	// 有效负载因子不会跟着变，「并发」这一项才是真正单独不同的。load_factor 为空时的联动单独测。
	explicitLoadFactor, handTunedLoadFactor := 3, 99
	base := tier3Account(7)
	base.LoadFactor = &explicitLoadFactor
	cases := []struct {
		name    string
		account Account
		want    FieldChange
	}{
		{
			"并发",
			func() Account { a := base; a.Concurrency = 10; return a }(),
			FieldChange{Field: "concurrency", From: 10, To: 3},
		},
		{
			"负载因子",
			func() Account { a := base; a.LoadFactor = &handTunedLoadFactor; return a }(),
			FieldChange{Field: "load_factor", From: 99, To: 3},
		},
		{
			"会话数",
			withExtra(base, map[string]any{"max_sessions": 5}),
			FieldChange{Field: "max_sessions", From: 5, To: 3},
		},
		{
			"RPM",
			withExtra(base, map[string]any{"base_rpm": 99}),
			FieldChange{Field: "base_rpm", From: 99, To: 30},
		},
		{
			"5h 额度",
			withExtra(base, map[string]any{"window_cost_limit": 61.5}),
			FieldChange{Field: "window_cost_limit", From: 61.5, To: 60.0},
		},
		{
			"动态降速",
			withExtra(base, map[string]any{"adaptive_throttle": false}),
			FieldChange{Field: "adaptive_throttle", From: false, To: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := PlanPresetApplication(tier3(), []Account{tc.account})
			require.Equal(t, []AccountChange{{AccountID: 7, Name: "acc-7", Changes: []FieldChange{tc.want}}}, plan)
		})
	}
}

// 一个账号多项不同：只列出确实不同的项，顺序固定。
// load_factor 为空时有效负载因子取并发：并发变了，负载因子也跟着列出（from 是旧并发）。
func TestPlanPresetApplication_ListsOnlyDifferingFieldsInFixedOrder(t *testing.T) {
	a := withExtra(tier3Account(5), map[string]any{"adaptive_throttle": false, "base_rpm": 10})
	a.Concurrency = 1

	plan := PlanPresetApplication(tier3(), []Account{a})
	require.Len(t, plan, 1)
	require.Equal(t, []FieldChange{
		{Field: "concurrency", From: 1, To: 3},
		{Field: "base_rpm", From: 10, To: 30},
		{Field: "adaptive_throttle", From: false, To: true},
		{Field: "load_factor", From: 1, To: 3},
	}, plan[0].Changes)
}

// 6 项全不同：changes 按固定顺序列出（并发 / 会话数 / RPM / 5h 额度 / 动态降速 / 负载因子）。
// 顺序与字段名都是与前端的契约，写字面量，增删或调换都会让这里变红。
func TestPlanPresetApplication_AllSixFieldsAreListedInFixedOrder(t *testing.T) {
	handTunedLoadFactor := 7
	a := withExtra(tier3Account(5), map[string]any{
		"max_sessions": 5, "base_rpm": 99, "window_cost_limit": 61.5, "adaptive_throttle": false,
	})
	a.Concurrency = 10
	a.LoadFactor = &handTunedLoadFactor

	plan := PlanPresetApplication(tier3(), []Account{a})
	require.Len(t, plan, 1)
	require.Equal(t, []FieldChange{
		{Field: "concurrency", From: 10, To: 3},
		{Field: "max_sessions", From: 5, To: 3},
		{Field: "base_rpm", From: 99, To: 30},
		{Field: "window_cost_limit", From: 61.5, To: 60.0},
		{Field: "adaptive_throttle", From: false, To: true},
		{Field: "load_factor", From: 7, To: 3},
	}, plan[0].Changes)
}

// 动态降速是严格 bool、缺失按 false：与模板的真值表。
func TestPlanPresetApplication_AdaptiveThrottleIsStrictBoolMissingMeansFalse(t *testing.T) {
	cases := []struct {
		name      string
		tierValue *bool
		extra     any // nil = 键缺失
		wantFrom  any // nil = 不应产生变更
	}{
		{"模板关 / 账号缺失 = 一致", boolPtr(false), nil, nil},
		{"模板关 / 账号 false = 一致", boolPtr(false), false, nil},
		{"模板关 / 账号 true = 变更", boolPtr(false), true, true},
		{"模板开 / 账号缺失 = 变更（缺失按 false）", boolPtr(true), nil, false},
		{"模板开 / 账号 false = 变更", boolPtr(true), false, false},
		{"模板开 / 账号 true = 一致", boolPtr(true), true, nil},
		{"模板开 / 账号是字符串 true = 变更（严格 bool）", boolPtr(true), "true", false},
		{"模板开 / 账号是数字 1 = 变更（严格 bool）", boolPtr(true), 1, false},
		{"模板关 / 账号是字符串 true = 一致（不被识别，按 false）", boolPtr(false), "true", nil},
		{"模板没有定义降速（nil）/ 账号缺失 = 不比较", nil, nil, nil},
		{"模板没有定义降速（nil）/ 账号 true = 不比较", nil, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier := tier3()
			tier.AdaptiveThrottle = tc.tierValue
			a := withExtra(tier3Account(1), map[string]any{"adaptive_throttle": tc.extra})

			plan := PlanPresetApplication(tier, []Account{a})
			if tc.wantFrom == nil {
				require.Empty(t, plan)
				return
			}
			require.Len(t, plan, 1)
			require.Equal(t, []FieldChange{
				{Field: "adaptive_throttle", From: tc.wantFrom, To: *tc.tierValue},
			}, plan[0].Changes)
		})
	}
}

// extra 里数字的写法不统一（JSON 解出来是 float64，手改库可能是字符串）：
// 一律按网关实际读取的口径（账号上现有的 getter）比较，写法不同但数值相同不算变更。
func TestPlanPresetApplication_ToleratesNumericRepresentationsInExtra(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
	}{
		{"float64", map[string]any{"max_sessions": float64(3), "base_rpm": float64(30), "window_cost_limit": float64(60)}},
		{"int64", map[string]any{"max_sessions": int64(3), "base_rpm": int64(30), "window_cost_limit": int64(60)}},
		{"字符串", map[string]any{"max_sessions": "3", "base_rpm": " 30 ", "window_cost_limit": "60"}},
		{"json.Number", map[string]any{"max_sessions": json.Number("3"), "base_rpm": json.Number("30"), "window_cost_limit": json.Number("60.0")}},
		{"额度在容差内", map[string]any{"window_cost_limit": 60.0000000001}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := withExtra(tier3Account(1), tc.extra)
			require.Empty(t, PlanPresetApplication(tier3(), []Account{a}))
		})
	}

	// 写法不同且数值也不同：变更里的 from 是解析后的数值，不是原始写法。
	a := withExtra(tier3Account(2), map[string]any{"max_sessions": "5", "base_rpm": float64(45), "window_cost_limit": "75.5"})
	plan := PlanPresetApplication(tier3(), []Account{a})
	require.Len(t, plan, 1)
	require.Equal(t, []FieldChange{
		{Field: "max_sessions", From: 5, To: 3},
		{Field: "base_rpm", From: 45, To: 30},
		{Field: "window_cost_limit", From: 75.5, To: 60.0},
	}, plan[0].Changes)

	// 解析不出数字的脏值读成 0（与网关一致），和模板的非 0 值比就是变更。
	garbled := withExtra(tier3Account(3), map[string]any{"base_rpm": "abc", "window_cost_limit": "NaN"})
	plan = PlanPresetApplication(tier3(), []Account{garbled})
	require.Len(t, plan, 1)
	require.Equal(t, "base_rpm", plan[0].Changes[0].Field)
	require.Equal(t, 0, plan[0].Changes[0].From)
	require.Equal(t, "window_cost_limit", plan[0].Changes[1].Field, "NaN 不能被当成与模板一致")
}

// 5h 额度 0 表示「不限」：与「键缺失」等价，二者都和模板的 0 一致。
func TestPlanPresetApplication_WindowCostLimitZeroVersusMissing(t *testing.T) {
	zeroTier := tier3()
	zeroTier.WindowCostLimit = 0

	missing := withExtra(tier3Account(1), map[string]any{"window_cost_limit": nil})
	explicitZero := withExtra(tier3Account(2), map[string]any{"window_cost_limit": 0.0})
	limited := tier3Account(3) // 60

	require.Empty(t, PlanPresetApplication(zeroTier, []Account{missing, explicitZero}), "模板 0 对 键缺失 / 显式 0 都一致")
	plan := PlanPresetApplication(zeroTier, []Account{limited})
	require.Equal(t, []FieldChange{{Field: "window_cost_limit", From: 60.0, To: 0.0}}, plan[0].Changes, "模板关掉额度要把账号上旧的限制清掉")

	// 反过来：模板 60，账号缺失 = 当前不限，变更为 60。
	plan = PlanPresetApplication(tier3(), []Account{missing})
	require.Equal(t, []FieldChange{{Field: "window_cost_limit", From: 0.0, To: 60.0}}, plan[0].Changes)
}

// 账号没有 extra（nil map）时，所有 extra 项都按 0 / false 读。
func TestPlanPresetApplication_AccountWithoutExtra(t *testing.T) {
	plan := PlanPresetApplication(tier3(), []Account{{ID: 1, Name: "bare", Concurrency: 3}})
	require.Len(t, plan, 1)
	require.Equal(t, []FieldChange{
		{Field: "max_sessions", From: 0, To: 3},
		{Field: "base_rpm", From: 0, To: 30},
		{Field: "window_cost_limit", From: 0.0, To: 60.0},
		{Field: "adaptive_throttle", From: false, To: true},
	}, plan[0].Changes)
}

// 多个账号只返回有差异的，且按账号 id 升序，不依赖传入顺序。
func TestPlanPresetApplication_ReturnsOnlyDifferingAccountsSortedByID(t *testing.T) {
	diffA := tier3Account(30)
	diffA.Concurrency = 9
	diffB := withExtra(tier3Account(10), map[string]any{"base_rpm": 5})
	diffC := withExtra(tier3Account(20), map[string]any{"adaptive_throttle": false})

	plan := PlanPresetApplication(tier3(), []Account{diffA, tier3Account(15), diffB, tier3Account(25), diffC})

	ids := make([]int64, 0, len(plan))
	for _, change := range plan {
		ids = append(ids, change.AccountID)
	}
	require.Equal(t, []int64{10, 20, 30}, ids)
	require.Equal(t, "acc-10", plan[0].Name)
}

// 应用时会把将改动账号的 load_factor 写成档位并发，所以负载因子要进计划：比的是「有效负载因子」
// （load_factor 为空或非正时取并发，并发也不是正数时取 1）与档位并发，手调过的负载因子会被重置，
// 这次覆盖必须在预览里看得见。
func TestPlanPresetApplication_LoadFactorComparesTheEffectiveValue(t *testing.T) {
	ptr := func(v int) *int { return &v }
	cases := []struct {
		name        string
		concurrency int
		loadFactor  *int
		want        []FieldChange // nil = 该账号不应出现在计划里
	}{
		{"load_factor 为空且并发已等于档位并发：有效值相等，不算变化", 3, nil, nil},
		{"load_factor 显式等于档位并发：不算变化", 3, ptr(3), nil},
		{"load_factor 为 0 视同为空（有效值取并发）：不算变化", 3, ptr(0), nil},
		{"load_factor 为负视同为空（有效值取并发）：不算变化", 3, ptr(-1), nil},
		{
			"load_factor 手调成别的值：算变化，from 是手调值",
			3, ptr(99),
			[]FieldChange{{Field: "load_factor", From: 99, To: 3}},
		},
		{
			"load_factor 为空但并发不同：有效值是旧并发，concurrency 与 load_factor 一起变",
			10, nil,
			[]FieldChange{{Field: "concurrency", From: 10, To: 3}, {Field: "load_factor", From: 10, To: 3}},
		},
		{
			"并发不同但 load_factor 恰好已等于档位并发：只有并发变",
			10, ptr(3),
			[]FieldChange{{Field: "concurrency", From: 10, To: 3}},
		},
		{
			"并发为 0（不限）且 load_factor 为空：有效值取 1，与档位并发不同",
			0, nil,
			[]FieldChange{{Field: "concurrency", From: 0, To: 3}, {Field: "load_factor", From: 1, To: 3}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := tier3Account(1)
			a.Concurrency = tc.concurrency
			a.LoadFactor = tc.loadFactor

			plan := PlanPresetApplication(tier3(), []Account{a})
			if tc.want == nil {
				require.Empty(t, plan)
				return
			}
			require.Len(t, plan, 1)
			require.Equal(t, tc.want, plan[0].Changes)
		})
	}
}

func TestPlanPresetApplication_DoesNotMutateInput(t *testing.T) {
	accounts := []Account{
		withExtra(tier3Account(2), map[string]any{"base_rpm": 5}),
		withExtra(tier3Account(1), map[string]any{"base_rpm": 7}),
	}

	plan := PlanPresetApplication(tier3(), accounts)

	require.Equal(t, int64(1), plan[0].AccountID, "结果按 id 升序")
	require.Equal(t, int64(2), accounts[0].ID, "不能原地排序入参")
	require.Equal(t, int64(1), accounts[1].ID)
	require.Equal(t, 5, accounts[0].Extra["base_rpm"])
	require.Equal(t, 7, accounts[1].Extra["base_rpm"])
}

// ---- PreviewTierApplication ----

func previewSvc(accounts ...Account) *adminServiceImpl {
	return &adminServiceImpl{accountRepo: &applyTierRepo{byTier: map[string][]Account{"3": accounts}}}
}

func TestPreviewTierApplication_CountsMatchedAndWillChange(t *testing.T) {
	changed := tier3Account(2)
	changed.Concurrency = 10
	svc := previewSvc(tier3Account(1), changed, tier3Account(3))

	preview, err := svc.PreviewTierApplication(context.Background(), tier3())
	require.NoError(t, err)

	require.Equal(t, "3", preview.Tier)
	require.Equal(t, 3, preview.Matched)
	require.Equal(t, 1, preview.WillChange)
	require.Equal(t, []AccountChange{{
		AccountID: 2, Name: "acc-2",
		// load_factor 为空时有效负载因子取并发，并发变了负载因子也跟着列出。
		Changes: []FieldChange{
			{Field: "concurrency", From: 10, To: 3},
			{Field: "load_factor", From: 10, To: 3},
		},
	}}, preview.Samples)
}

// 其余各项都与模板一致、只有负载因子被手调过的账号，同样计入 will_change：
// 它会被应用重置，管理员必须在确认前看到。load_factor 为空或已等于并发的账号不计。
func TestPreviewTierApplication_AccountDifferingOnlyInLoadFactorCountsTowardWillChange(t *testing.T) {
	handTuned, explicit := 99, 3
	tuned := tier3Account(2)
	tuned.LoadFactor = &handTuned
	aligned := tier3Account(3)
	aligned.LoadFactor = &explicit
	svc := previewSvc(tier3Account(1), tuned, aligned)

	preview, err := svc.PreviewTierApplication(context.Background(), tier3())
	require.NoError(t, err)

	require.Equal(t, 3, preview.Matched)
	require.Equal(t, 1, preview.WillChange)
	require.Equal(t, []AccountChange{{
		AccountID: 2, Name: "acc-2",
		Changes: []FieldChange{{Field: "load_factor", From: 99, To: 3}},
	}}, preview.Samples)
}

// 响应形状是与前端的契约。
func TestPreviewTierApplication_JSONShape(t *testing.T) {
	a := tier3Account(101)
	a.Name = "alice@example.com"
	a.Concurrency = 10
	a = withExtra(a, map[string]any{"adaptive_throttle": nil}) // 缺失，按 false
	svc := previewSvc(a, tier3Account(102))

	preview, err := svc.PreviewTierApplication(context.Background(), tier3())
	require.NoError(t, err)

	raw, err := json.Marshal(preview)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"tier": "3", "matched": 2, "will_change": 1,
		"samples": [{"account_id": 101, "name": "alice@example.com", "changes": [
			{"field": "concurrency", "from": 10, "to": 3},
			{"field": "adaptive_throttle", "from": false, "to": true},
			{"field": "load_factor", "from": 10, "to": 3}
		]}]
	}`, string(raw))
}

// 预览响应里 changes[].field 的集合恰好是这 6 个，且按固定顺序出现。
func TestPreviewTierApplication_ChangeFieldsAreExactlyTheSixContractFields(t *testing.T) {
	handTuned := 7
	a := withExtra(tier3Account(1), map[string]any{
		"max_sessions": 5, "base_rpm": 99, "window_cost_limit": 61.5, "adaptive_throttle": false,
	})
	a.Concurrency = 10
	a.LoadFactor = &handTuned
	svc := previewSvc(a)

	preview, err := svc.PreviewTierApplication(context.Background(), tier3())
	require.NoError(t, err)
	raw, err := json.Marshal(preview)
	require.NoError(t, err)

	var decoded struct {
		Samples []struct {
			Changes []struct {
				Field string `json:"field"`
			} `json:"changes"`
		} `json:"samples"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Len(t, decoded.Samples, 1)
	fields := make([]string, 0, 6)
	for _, change := range decoded.Samples[0].Changes {
		fields = append(fields, change.Field)
	}
	require.Equal(t, []string{
		"concurrency", "max_sessions", "base_rpm", "window_cost_limit", "adaptive_throttle", "load_factor",
	}, fields)
}

// 样例最多 20 个、只含将改动的账号、按 id 升序；will_change 仍是真实总数。
func TestPreviewTierApplication_SamplesCappedAtTwentyAndSortedByID(t *testing.T) {
	accounts := make([]Account, 0, 31)
	// 倒序传入：25 个将改动 + 6 个一致，id 穿插。
	for id := int64(31); id >= 1; id-- {
		a := tier3Account(id)
		if id%5 != 0 { // 1..31 里 25 个不是 5 的倍数
			a.Concurrency = 8
		}
		accounts = append(accounts, a)
	}
	svc := previewSvc(accounts...)

	preview, err := svc.PreviewTierApplication(context.Background(), tier3())
	require.NoError(t, err)

	require.Equal(t, 31, preview.Matched)
	require.Equal(t, 25, preview.WillChange)
	require.Len(t, preview.Samples, 20)
	want := make([]int64, 0, 20)
	for id := int64(1); len(want) < 20; id++ {
		if id%5 != 0 {
			want = append(want, id)
		}
	}
	got := make([]int64, 0, 20)
	for _, sample := range preview.Samples {
		got = append(got, sample.AccountID)
	}
	require.Equal(t, want, got)
}

func TestPreviewTierApplication_NothingToChangeHasEmptySamplesNotNull(t *testing.T) {
	for name, svc := range map[string]*adminServiceImpl{
		"有账号但都一致": previewSvc(tier3Account(1)),
		"没有账号":    previewSvc(),
	} {
		t.Run(name, func(t *testing.T) {
			preview, err := svc.PreviewTierApplication(context.Background(), tier3())
			require.NoError(t, err)
			require.Equal(t, 0, preview.WillChange)
			require.NotNil(t, preview.Samples)

			raw, err := json.Marshal(preview)
			require.NoError(t, err)
			require.Contains(t, string(raw), `"samples":[]`)
		})
	}
}

func TestPreviewTierApplication_IsReadOnlyAndPropagatesRepositoryErrors(t *testing.T) {
	changed := tier3Account(1)
	changed.Concurrency = 10
	repo := &applyTierRepo{byTier: map[string][]Account{"3": {changed}}}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.PreviewTierApplication(context.Background(), tier3())
	require.NoError(t, err)
	require.Empty(t, repo.bulkCalls, "预览绝不能写数据")

	boom := errors.New("db down")
	failing := &adminServiceImpl{accountRepo: &applyTierRepo{listErr: boom}}
	_, err = failing.PreviewTierApplication(context.Background(), tier3())
	require.ErrorIs(t, err, boom)
}
