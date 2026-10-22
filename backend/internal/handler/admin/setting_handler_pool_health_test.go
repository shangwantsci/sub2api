package admin

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func decodeSettingsEnvelope(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope), string(body))
	return envelope.Data
}

// 公开 pool-health 开关：缺省开启；PUT 带 pool_health_enabled 才改，不带保持原值；响应里回显新值。
// 键名是与前端的契约。
func TestSettingHandler_UpdateSettings_PoolHealthEnabled(t *testing.T) {
	handler, repo := newAccountNurturingSettingHandler(map[string]string{service.SettingKeyPromoCodeEnabled: "true"})

	// 库里没有这个键、请求也不带：按缺省开启，响应回显 true。
	rec := putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyPoolHealthEnabled])
	require.Equal(t, true, decodeSettingsEnvelope(t, rec.Body.Bytes())["pool_health_enabled"])

	// 显式 false 才关闭。
	rec = putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "pool_health_enabled": false})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "false", repo.values[service.SettingKeyPoolHealthEnabled])
	require.Equal(t, false, decodeSettingsEnvelope(t, rec.Body.Bytes())["pool_health_enabled"])

	// 请求里不带这个字段：保持原值（false），不被悄悄改回缺省的开启。
	rec = putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "false", repo.values[service.SettingKeyPoolHealthEnabled])
	require.Equal(t, false, decodeSettingsEnvelope(t, rec.Body.Bytes())["pool_health_enabled"])

	rec = putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "pool_health_enabled": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyPoolHealthEnabled])
}

// 分组名：缺省 ccmax；读写时 trim；空串/纯空白按缺省处理；不带保持原值。
func TestSettingHandler_UpdateSettings_PoolHealthGroupName(t *testing.T) {
	handler, repo := newAccountNurturingSettingHandler(map[string]string{service.SettingKeyPromoCodeEnabled: "true"})

	rec := putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "ccmax", repo.values[service.SettingKeyPoolHealthGroupName])
	require.Equal(t, "ccmax", decodeSettingsEnvelope(t, rec.Body.Bytes())["pool_health_group_name"])

	rec = putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "pool_health_group_name": "  team \t"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "team", repo.values[service.SettingKeyPoolHealthGroupName], "落库的是 trim 后的值")
	require.Equal(t, "team", decodeSettingsEnvelope(t, rec.Body.Bytes())["pool_health_group_name"])

	// 请求里不带这个字段：保持原值。
	rec = putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "team", repo.values[service.SettingKeyPoolHealthGroupName])
	require.Equal(t, "team", decodeSettingsEnvelope(t, rec.Body.Bytes())["pool_health_group_name"])

	for _, blank := range []string{"", "   ", "\t\n"} {
		rec = putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "pool_health_group_name": "team"})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		rec = putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "pool_health_group_name": blank})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, "ccmax", repo.values[service.SettingKeyPoolHealthGroupName], "%q 应回落到缺省分组", blank)
		require.Equal(t, "ccmax", decodeSettingsEnvelope(t, rec.Body.Bytes())["pool_health_group_name"])
	}
}

// 严格类型：字符串 "true" / 数字分组名都不会被悄悄转换，请求直接 400，设置不落库。
func TestSettingHandler_UpdateSettings_PoolHealthFieldsMustHaveRightTypes(t *testing.T) {
	handler, repo := newAccountNurturingSettingHandler(map[string]string{service.SettingKeyPromoCodeEnabled: "true"})

	rec := putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "pool_health_enabled": "true"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "pool_health_group_name": 123})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	require.NotContains(t, repo.values, service.SettingKeyPoolHealthEnabled)
	require.NotContains(t, repo.values, service.SettingKeyPoolHealthGroupName)
}

func TestDiffSettings_DetectsPoolHealthChanges(t *testing.T) {
	before := &service.SystemSettings{PoolHealthEnabled: true, PoolHealthGroupName: "ccmax"}

	switchOnly := &service.SystemSettings{PoolHealthEnabled: false, PoolHealthGroupName: "ccmax"}
	changed := diffSettings(before, switchOnly, nil, nil, UpdateSettingsRequest{})
	require.Contains(t, changed, "pool_health_enabled")
	require.NotContains(t, changed, "pool_health_group_name")

	nameOnly := &service.SystemSettings{PoolHealthEnabled: true, PoolHealthGroupName: "team"}
	changed = diffSettings(before, nameOnly, nil, nil, UpdateSettingsRequest{})
	require.Contains(t, changed, "pool_health_group_name")
	require.NotContains(t, changed, "pool_health_enabled")

	changed = diffSettings(before, before, nil, nil, UpdateSettingsRequest{})
	require.NotContains(t, changed, "pool_health_enabled")
	require.NotContains(t, changed, "pool_health_group_name")
}

// 端到端审计：PUT 里把分组名写成空串（等价于缺省 ccmax）不能被记成「分组名变更」，
// 真正改了才记；开关同理。审计 diff 比较的是归一化后的值。
func TestSettingHandler_UpdateSettings_PoolHealthAuditUsesNormalizedValues(t *testing.T) {
	handler, _ := newAccountNurturingSettingHandler(map[string]string{service.SettingKeyPromoCodeEnabled: "true"})

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previousLogger)

	// 初始状态：分组名缺省（ccmax）、开关缺省（开）。空串 / 纯空白都等价于没改。
	rec := putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "pool_health_group_name": "  "})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, logs.String(), "pool_health_group_name", "空白分组名等于缺省，不是变更")
	require.NotContains(t, logs.String(), "pool_health_enabled")

	rec = putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "pool_health_group_name": " team ", "pool_health_enabled": false})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, logs.String(), "settings updated")
	require.Contains(t, logs.String(), "pool_health_group_name")
	require.Contains(t, logs.String(), "pool_health_enabled")
}
