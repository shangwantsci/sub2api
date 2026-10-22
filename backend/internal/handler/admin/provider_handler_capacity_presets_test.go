package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// presetUsageRepo 只实现 CapacityPresets 保存时用到的 CountByProviderTier；
// 其它 AccountRepository 方法一旦被调用就会因 nil 接口 panic，说明保存路径多做了不该做的事。
type presetUsageRepo struct {
	service.AccountRepository
	counts map[string]int
}

func (r *presetUsageRepo) CountByProviderTier(_ context.Context, tier string) (int, error) {
	return r.counts[tier], nil
}

type capacityPresetsEnvelope struct {
	Code    int                     `json:"code"`
	Message string                  `json:"message"`
	Reason  string                  `json:"reason"`
	Data    service.CapacityPresets `json:"data"`
}

func newCapacityPresetsTestHandler(counts map[string]int) (*ProviderHandler, *inMemSettingRepo) {
	gin.SetMode(gin.TestMode)
	repo := &inMemSettingRepo{data: map[string]string{}}
	settingService := service.NewSettingService(repo, &config.Config{})
	return NewProviderHandler(nil, settingService, nil, &presetUsageRepo{counts: counts}), repo
}

func callCapacityPresets(t *testing.T, handlerFn func(*gin.Context), method, body string) (*httptest.ResponseRecorder, capacityPresetsEnvelope) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, "/api/v1/admin/settings/capacity-presets", bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	handlerFn(c)

	var envelope capacityPresetsEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope), "body=%s", rec.Body.String())
	return rec, envelope
}

// 设置里还没有模板表时，GET 返回种子：key "1".."5"，默认 "3"。
func TestGetCapacityPresets_ReturnsSeedWhenNothingStored(t *testing.T) {
	h, repo := newCapacityPresetsTestHandler(nil)

	rec, envelope := callCapacityPresets(t, h.GetCapacityPresets, http.MethodGet, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 0, envelope.Code)
	require.Equal(t, service.DefaultCapacityPresets(), envelope.Data)
	require.Empty(t, repo.data, "读取不能写库")

	// 响应形状是与前端的契约：{"presets":[{key,label,concurrency,max_sessions,base_rpm,window_cost_limit,adaptive_throttle}],"default_key"}
	var raw struct {
		Data struct {
			Presets    []map[string]any `json:"presets"`
			DefaultKey string           `json:"default_key"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	require.Equal(t, "3", raw.Data.DefaultKey)
	require.Len(t, raw.Data.Presets, 5)
	require.Equal(t, map[string]any{
		"key": "3", "label": "均衡", "concurrency": float64(3), "max_sessions": float64(3),
		"base_rpm": float64(30), "window_cost_limit": float64(60), "adaptive_throttle": true,
	}, raw.Data.Presets[2])
}

func TestUpdateCapacityPresets_SavesAndGetReadsItBack(t *testing.T) {
	h, repo := newCapacityPresetsTestHandler(nil)

	body := `{"presets":[
		{"key":"1","label":"入门","concurrency":1,"max_sessions":1,"base_rpm":10,"window_cost_limit":20,"adaptive_throttle":true},
		{"key":"3","label":"自定义均衡","concurrency":4,"max_sessions":4,"base_rpm":35,"window_cost_limit":70.5,"adaptive_throttle":false}
	],"default_key":"3"}`
	rec, envelope := callCapacityPresets(t, h.UpdateCapacityPresets, http.MethodPut, body)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "3", envelope.Data.DefaultKey)
	require.Equal(t, []string{"1", "3"}, envelope.Data.Keys())
	require.Equal(t, 70.5, envelope.Data.Presets[1].WindowCostLimit)
	require.False(t, envelope.Data.Presets[1].AdaptiveThrottle)
	require.NotEmpty(t, repo.data[service.SettingKeyCapacityPresets], "必须落在设置键 capacity_presets 下")

	_, readBack := callCapacityPresets(t, h.GetCapacityPresets, http.MethodGet, "")
	require.Equal(t, envelope.Data, readBack.Data)
}

func TestUpdateCapacityPresets_RejectsInvalidTablesWith400(t *testing.T) {
	cases := map[string]struct {
		body   string
		reason string
	}{
		"并发为 0": {
			`{"presets":[{"key":"a","concurrency":0}],"default_key":"a"}`, "INVALID_PRESET_CONCURRENCY",
		},
		"key 是 custom": {
			`{"presets":[{"key":"custom","concurrency":1}],"default_key":"custom"}`, "INVALID_PRESET_KEY",
		},
		"key 重复": {
			`{"presets":[{"key":"a","concurrency":1},{"key":"a","concurrency":2}],"default_key":"a"}`, "DUPLICATE_PRESET_KEY",
		},
		"4 元组重复": {
			`{"presets":[{"key":"a","concurrency":2,"max_sessions":2,"base_rpm":20,"window_cost_limit":40},` +
				`{"key":"b","concurrency":2,"max_sessions":2,"base_rpm":20,"window_cost_limit":40}],"default_key":"a"}`,
			"DUPLICATE_PRESET_VALUES",
		},
		"默认 key 不存在": {
			`{"presets":[{"key":"a","concurrency":1}],"default_key":"zzz"}`, "INVALID_DEFAULT_PRESET",
		},
		"并发超上限": {
			`{"presets":[{"key":"a","concurrency":51}],"default_key":"a"}`, "INVALID_PRESET_CONCURRENCY",
		},
		"空表": {
			`{"presets":[],"default_key":""}`, "NO_CAPACITY_PRESET",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h, repo := newCapacityPresetsTestHandler(nil)

			rec, envelope := callCapacityPresets(t, h.UpdateCapacityPresets, http.MethodPut, tc.body)

			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Equal(t, tc.reason, envelope.Reason)
			require.Empty(t, repo.data, "被拒绝的表绝不能落库")
		})
	}
}

func TestUpdateCapacityPresets_RejectsMalformedJSON(t *testing.T) {
	h, repo := newCapacityPresetsTestHandler(nil)

	rec, _ := callCapacityPresets(t, h.UpdateCapacityPresets, http.MethodPut, `{"presets": [`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, repo.data)
}

// 仍有供货商账号的 provider_tier 记着的 key，不能被删除或改名；没人用的可以删。
func TestUpdateCapacityPresets_ProtectsKeysStillUsedByProviderAccounts(t *testing.T) {
	h, repo := newCapacityPresetsTestHandler(map[string]int{"3": 2})

	// 先存下种子（首次保存按种子表比对，且本次不删任何 key）。
	seed, err := json.Marshal(service.DefaultCapacityPresets())
	require.NoError(t, err)
	rec, _ := callCapacityPresets(t, h.UpdateCapacityPresets, http.MethodPut, string(seed))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	stored := repo.data[service.SettingKeyCapacityPresets]

	// 去掉仍被 2 个账号使用的 "3"：400。
	withoutThree := `{"presets":[
		{"key":"1","concurrency":1,"max_sessions":1,"base_rpm":10,"window_cost_limit":20},
		{"key":"2","concurrency":2,"max_sessions":2,"base_rpm":20,"window_cost_limit":40}
	],"default_key":"1"}`
	rec, envelope := callCapacityPresets(t, h.UpdateCapacityPresets, http.MethodPut, withoutThree)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Equal(t, "CAPACITY_PRESET_IN_USE", envelope.Reason)
	require.Equal(t, stored, repo.data[service.SettingKeyCapacityPresets], "被拒绝时库里的表不变")

	// 保留 "3"、删掉没人用的 "4" 与 "5"：允许。
	keepThree := `{"presets":[
		{"key":"1","concurrency":1,"max_sessions":1,"base_rpm":10,"window_cost_limit":20},
		{"key":"2","concurrency":2,"max_sessions":2,"base_rpm":20,"window_cost_limit":40},
		{"key":"3","concurrency":3,"max_sessions":3,"base_rpm":30,"window_cost_limit":60}
	],"default_key":"3"}`
	rec, envelope = callCapacityPresets(t, h.UpdateCapacityPresets, http.MethodPut, keepThree)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []string{"1", "2", "3"}, envelope.Data.Keys())
}
