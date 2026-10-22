package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// tierApplyRepo 是「应用到存量」预览/确认链路上用到的仓库替身。
// 其它 AccountRepository 方法一旦被调用就会因 nil 接口 panic，说明这条链路多做了不该做的事。
// handler 与真实的 adminServiceImpl 一起跑（service.NewAdminService），断言的是整条链路
// 到仓库为止的行为，尤其是「409 时没有任何 BulkUpdate」。
type tierApplyRepo struct {
	service.AccountRepository
	accounts  []service.Account
	listCalls int
	bulkCalls []tierApplyBulkCall
	// bulkRows 非 nil 时覆盖 BulkUpdate 报告的实际更新行数。
	bulkRows *int64
}

type tierApplyBulkCall struct {
	ids     []int64
	updates service.AccountBulkUpdate
}

func (r *tierApplyRepo) CreateWithAccountGroups(context.Context, *service.Account, []service.AccountGroup) error {
	return nil
}

func (r *tierApplyRepo) ListByProviderTier(_ context.Context, tier string) ([]service.Account, error) {
	r.listCalls++
	if tier != "3" {
		return nil, nil
	}
	return append([]service.Account(nil), r.accounts...), nil
}

func (r *tierApplyRepo) BulkUpdate(_ context.Context, ids []int64, updates service.AccountBulkUpdate) (int64, error) {
	r.bulkCalls = append(r.bulkCalls, tierApplyBulkCall{ids: append([]int64(nil), ids...), updates: updates})
	if r.bulkRows != nil {
		return *r.bulkRows, nil
	}
	return int64(len(ids)), nil
}

type tierApplyEnvelope struct {
	Code     int               `json:"code"`
	Message  string            `json:"message"`
	Reason   string            `json:"reason"`
	Metadata map[string]string `json:"metadata"`
	Data     json.RawMessage   `json:"data"`
}

func newTierApplyHandler(accounts ...service.Account) (*ProviderHandler, *tierApplyRepo, *inMemSettingRepo) {
	gin.SetMode(gin.TestMode)
	settingRepo := &inMemSettingRepo{data: map[string]string{}}
	settingService := service.NewSettingService(settingRepo, &config.Config{})
	repo := &tierApplyRepo{accounts: accounts}
	adminService := service.NewAdminService(
		nil, nil, repo, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	return NewProviderHandler(nil, settingService, adminService, repo), repo, settingRepo
}

func callTierApply(t *testing.T, handlerFn func(*gin.Context), method, tier, body string) (*httptest.ResponseRecorder, tierApplyEnvelope) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "tier", Value: tier}}
	c.Request = httptest.NewRequest(method, "/api/v1/admin/provider-settings/tiers/"+tier+"/apply", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	handlerFn(c)

	var envelope tierApplyEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope), "body=%s", rec.Body.String())
	return rec, envelope
}

// tier3Account 返回 5 项都与种子「3 档」（并发 3、会话 3、RPM 30、5h 额度 60、降速开）一致的账号。
func tier3Account(id int64) service.Account {
	return service.Account{
		ID: id, Name: fmt.Sprintf("acc-%d", id), Concurrency: 3,
		Extra: map[string]any{"max_sessions": 3, "base_rpm": 30, "window_cost_limit": 60.0, "adaptive_throttle": true},
	}
}

func tier3AccountWithConcurrency(id int64, concurrency int) service.Account {
	a := tier3Account(id)
	a.Concurrency = concurrency
	return a
}

// ---- GET apply-preview ----

// 响应形状是与前端的契约：data = {tier, matched, will_change, samples[{account_id, name, changes[{field, from, to}]}]}。
func TestPreviewTierApplication_ResponseShape(t *testing.T) {
	drifted := tier3AccountWithConcurrency(101, 10)
	drifted.Name = "alice@example.com"
	drifted.Extra = map[string]any{"max_sessions": 3, "base_rpm": 30, "window_cost_limit": 60.0} // 降速缺失 = false
	h, repo, _ := newTierApplyHandler(drifted, tier3Account(102), tier3Account(103))

	rec, envelope := callTierApply(t, h.PreviewTierApplication, http.MethodGet, "3", "")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)
	require.JSONEq(t, `{
		"tier": "3",
		"matched": 3,
		"will_change": 1,
		"samples": [{
			"account_id": 101,
			"name": "alice@example.com",
			"changes": [
				{"field": "concurrency", "from": 10, "to": 3},
				{"field": "adaptive_throttle", "from": false, "to": true},
				{"field": "load_factor", "from": 10, "to": 3}
			]
		}]
	}`, string(envelope.Data))
	require.Empty(t, repo.bulkCalls, "预览是只读的，不能写任何数据")
}

// changes[].field 的集合恰好是 6 个（并发、会话数、RPM、5h 额度、动态降速、负载因子），
// 且按这个固定顺序出现。负载因子比的是有效值（为空时取并发），手调值会被应用重置，所以要列出来。
func TestPreviewTierApplication_ChangeFieldsAreExactlyTheSixContractFields(t *testing.T) {
	handTuned := 7
	everything := tier3AccountWithConcurrency(1, 10)
	everything.LoadFactor = &handTuned
	everything.Extra = map[string]any{"max_sessions": 5, "base_rpm": 99, "window_cost_limit": 61.5, "adaptive_throttle": false}
	h, _, _ := newTierApplyHandler(everything)

	_, envelope := callTierApply(t, h.PreviewTierApplication, http.MethodGet, "3", "")

	var data struct {
		Samples []struct {
			Changes []struct {
				Field string `json:"field"`
			} `json:"changes"`
		} `json:"samples"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	require.Len(t, data.Samples, 1)
	var fields []string
	for _, change := range data.Samples[0].Changes {
		fields = append(fields, change.Field)
	}
	require.Equal(t, []string{
		"concurrency", "max_sessions", "base_rpm", "window_cost_limit", "adaptive_throttle", "load_factor",
	}, fields)
}

// 其余各项一致、只有负载因子被手调过的账号，同样计入 will_change。
func TestPreviewTierApplication_OnlyLoadFactorDifferingCountsTowardWillChange(t *testing.T) {
	handTuned := 99
	tuned := tier3Account(2)
	tuned.LoadFactor = &handTuned
	h, _, _ := newTierApplyHandler(tier3Account(1), tuned)

	_, envelope := callTierApply(t, h.PreviewTierApplication, http.MethodGet, "3", "")

	require.JSONEq(t, `{
		"tier": "3", "matched": 2, "will_change": 1,
		"samples": [{"account_id": 2, "name": "acc-2", "changes": [{"field": "load_factor", "from": 99, "to": 3}]}]
	}`, string(envelope.Data))
}

// samples 最多 20 条、只含将改动的账号、按账号 id 升序；will_change 仍是真实总数。
func TestPreviewTierApplication_SamplesCappedAtTwentyAndSortedAscending(t *testing.T) {
	accounts := make([]service.Account, 0, 30)
	for id := int64(30); id >= 1; id-- { // 倒序给出，验证服务端自己排
		if id%6 == 0 {
			accounts = append(accounts, tier3Account(id)) // 5 个一致的
		} else {
			accounts = append(accounts, tier3AccountWithConcurrency(id, 9)) // 25 个将改动
		}
	}
	h, _, _ := newTierApplyHandler(accounts...)

	_, envelope := callTierApply(t, h.PreviewTierApplication, http.MethodGet, "3", "")

	var data struct {
		Matched    int `json:"matched"`
		WillChange int `json:"will_change"`
		Samples    []struct {
			AccountID int64 `json:"account_id"`
		} `json:"samples"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	require.Equal(t, 30, data.Matched)
	require.Equal(t, 25, data.WillChange)
	require.Len(t, data.Samples, 20)

	var got, want []int64
	for _, sample := range data.Samples {
		got = append(got, sample.AccountID)
	}
	for id := int64(1); len(want) < 20; id++ {
		if id%6 != 0 {
			want = append(want, id)
		}
	}
	require.Equal(t, want, got)
}

func TestPreviewTierApplication_NothingToChangeReturnsEmptySamplesArray(t *testing.T) {
	h, _, _ := newTierApplyHandler(tier3Account(1))

	rec, _ := callTierApply(t, h.PreviewTierApplication, http.MethodGet, "3", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"will_change":0`)
	require.Contains(t, rec.Body.String(), `"samples":[]`, "samples 必须是 [] 而不是 null")
}

// :tier 的校验沿用 apply-existing 的规则与错误码：custom 不可批量回填；模板必须存在且对供货商开放。
func TestPreviewTierApplication_RejectsCustomUnknownAndNotOpenedTiers(t *testing.T) {
	h, repo, settingRepo := newTierApplyHandler(tier3Account(1))

	rec, envelope := callTierApply(t, h.PreviewTierApplication, http.MethodGet, "custom", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "CUSTOM_TIER_NOT_BACKFILLABLE", envelope.Reason)

	rec, envelope = callTierApply(t, h.PreviewTierApplication, http.MethodGet, "no-such-tier", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "INVALID_TIER", envelope.Reason)

	// 模板存在，但没有对供货商开放（enabled_tier_keys 不含它）。
	settingRepo.data[service.SettingKeyProviderEnabledTierKeys] = `["1","2"]`
	rec, envelope = callTierApply(t, h.PreviewTierApplication, http.MethodGet, "3", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "INVALID_TIER", envelope.Reason)

	require.Zero(t, repo.listCalls, "校验不过就不该去读账号")
}

// ---- POST apply-existing ----

func TestApplyTierToExisting_RequiresExpectedChanged(t *testing.T) {
	cases := map[string]string{
		"空请求体（旧版前端）": ``,
		"空对象":        `{}`,
		"null":       `{"expected_changed": null}`,
		"负数":         `{"expected_changed": -1}`,
		"字符串":        `{"expected_changed": "5"}`,
		"小数":         `{"expected_changed": 1.5}`,
		"不是 JSON":    `expected_changed=5`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			h, repo, _ := newTierApplyHandler(tier3AccountWithConcurrency(1, 9))

			rec, envelope := callTierApply(t, h.ApplyTierToExisting, http.MethodPost, "3", body)

			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Equal(t, "EXPECTED_CHANGED_REQUIRED", envelope.Reason)
			require.Empty(t, repo.bulkCalls, "没有确认数字就不能写任何数据")
		})
	}
}

// 确认的数字与服务端重算的不一致：409，message 带实际数量，且没有调用 BulkUpdate。
func TestApplyTierToExisting_MismatchedExpectedChangedIs409AndWritesNothing(t *testing.T) {
	h, repo, _ := newTierApplyHandler(
		tier3AccountWithConcurrency(1, 9), tier3AccountWithConcurrency(2, 9), tier3Account(3),
	) // 将改动 2 个

	for _, expected := range []int{0, 1, 3, 12} {
		rec, envelope := callTierApply(t, h.ApplyTierToExisting, http.MethodPost, "3",
			fmt.Sprintf(`{"expected_changed": %d}`, expected))

		require.Equal(t, http.StatusConflict, rec.Code, "expected=%d body=%s", expected, rec.Body.String())
		require.Equal(t, "PRESET_APPLY_CONFLICT", envelope.Reason)
		require.Contains(t, envelope.Message, "now 2", "message 里带上实际数量")
		require.Equal(t, "2", envelope.Metadata["will_change"])
		require.Empty(t, repo.bulkCalls, "409 时绝不能调用 BulkUpdate；expected=%d", expected)
	}
}

// 相等才执行，且只对将改动的账号 id 调 BulkUpdate；响应 {matched, updated}。
func TestApplyTierToExisting_UpdatesOnlyAccountsThatWouldChange(t *testing.T) {
	handTuned := 99
	onlyLoadFactor := tier3Account(6)
	onlyLoadFactor.LoadFactor = &handTuned // 其余各项一致，只有负载因子被手调：也在变更范围内
	h, repo, _ := newTierApplyHandler(
		tier3Account(1), tier3AccountWithConcurrency(2, 9), tier3Account(3), tier3AccountWithConcurrency(4, 1), tier3Account(5),
		onlyLoadFactor,
	)

	rec, envelope := callTierApply(t, h.ApplyTierToExisting, http.MethodPost, "3", `{"expected_changed": 3}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"matched": 6, "updated": 3}`, string(envelope.Data))

	require.Len(t, repo.bulkCalls, 1)
	call := repo.bulkCalls[0]
	require.Equal(t, []int64{2, 4, 6}, call.ids, "只写将改动的账号，不是该档的全部账号")
	require.Equal(t, 3, *call.updates.Concurrency)
	require.Equal(t, 3, *call.updates.LoadFactor, "load_factor 与并发保持一致")
	require.Equal(t, map[string]any{
		"max_sessions": 3, "base_rpm": 30, "window_cost_limit": 60.0, "adaptive_throttle": true,
	}, call.updates.Extra)
}

// updated 是仓库报告的实际更新行数。
func TestApplyTierToExisting_UpdatedReflectsRowsActuallyUpdated(t *testing.T) {
	h, repo, _ := newTierApplyHandler(tier3AccountWithConcurrency(1, 9), tier3AccountWithConcurrency(2, 9))
	rows := int64(1)
	repo.bulkRows = &rows

	_, envelope := callTierApply(t, h.ApplyTierToExisting, http.MethodPost, "3", `{"expected_changed": 2}`)

	require.JSONEq(t, `{"matched": 2, "updated": 1}`, string(envelope.Data))
}

// 没有任何账号需要改，且确认的也是 0：直接返回，不发 SQL。
func TestApplyTierToExisting_NothingToChangeSendsNoSQL(t *testing.T) {
	h, repo, _ := newTierApplyHandler(tier3Account(1), tier3Account(2))

	rec, envelope := callTierApply(t, h.ApplyTierToExisting, http.MethodPost, "3", `{"expected_changed": 0}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"matched": 2, "updated": 0}`, string(envelope.Data))
	require.Empty(t, repo.bulkCalls)
}

// tier 校验沿用既有规则：custom 与不存在 / 未开放的模板在读账号之前就被拒绝。
func TestApplyTierToExisting_KeepsTierValidation(t *testing.T) {
	h, repo, _ := newTierApplyHandler(tier3AccountWithConcurrency(1, 9))

	rec, envelope := callTierApply(t, h.ApplyTierToExisting, http.MethodPost, "custom", `{"expected_changed": 0}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "CUSTOM_TIER_NOT_BACKFILLABLE", envelope.Reason)

	rec, envelope = callTierApply(t, h.ApplyTierToExisting, http.MethodPost, "no-such-tier", `{"expected_changed": 0}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "INVALID_TIER", envelope.Reason)

	require.Zero(t, repo.listCalls)
	require.Empty(t, repo.bulkCalls)
}
