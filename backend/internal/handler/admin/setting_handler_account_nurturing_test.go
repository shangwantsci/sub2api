package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func putAdminSettings(t *testing.T, handler *SettingHandler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.UpdateSettings(c)
	return rec
}

func newAccountNurturingSettingHandler(initial map[string]string) (*SettingHandler, *settingHandlerRepoStub) {
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: initial}
	svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	return NewSettingHandler(svc, nil, nil, nil, nil, nil, nil), repo
}

// 账号养号全局开关：PUT /admin/settings 带 account_nurturing_enabled 才改，不带保持原值；
// 响应里回显新值。键名是与前端的契约。
func TestSettingHandler_UpdateSettings_AccountNurturingEnabled(t *testing.T) {
	handler, repo := newAccountNurturingSettingHandler(map[string]string{service.SettingKeyPromoCodeEnabled: "true"})

	rec := putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "account_nurturing_enabled": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyAccountNurturingEnabled])

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, true, envelope.Data["account_nurturing_enabled"])

	// 请求里不带这个字段：保持原值（true），不被悄悄关掉。
	rec = putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyAccountNurturingEnabled])

	// 显式 false 才关闭。
	rec = putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "account_nurturing_enabled": false})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "false", repo.values[service.SettingKeyAccountNurturingEnabled])
}

// 严格 bool：字符串 "true" 不会被悄悄当成 true（也不会被当成 false），请求直接 400，设置不落库。
func TestSettingHandler_UpdateSettings_AccountNurturingEnabledMustBeBoolean(t *testing.T) {
	handler, repo := newAccountNurturingSettingHandler(map[string]string{service.SettingKeyPromoCodeEnabled: "true"})

	rec := putAdminSettings(t, handler, map[string]any{"promo_code_enabled": true, "account_nurturing_enabled": "true"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.NotContains(t, repo.values, service.SettingKeyAccountNurturingEnabled)
}

func TestDiffSettings_DetectsAccountNurturingChange(t *testing.T) {
	before := &service.SystemSettings{AccountNurturingEnabled: false}
	after := &service.SystemSettings{AccountNurturingEnabled: true}

	require.Contains(t, diffSettings(before, after, nil, nil, UpdateSettingsRequest{}), "account_nurturing_enabled")
	require.NotContains(t, diffSettings(before, before, nil, nil, UpdateSettingsRequest{}), "account_nurturing_enabled")
}
