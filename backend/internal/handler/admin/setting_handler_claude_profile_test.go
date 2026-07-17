package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// inMemSettingRepo is a minimal read/write SettingRepository for handler tests
// (settingHandlerRepoStub panics on Set/Delete, which the publish path needs).
type inMemSettingRepo struct{ data map[string]string }

func (r *inMemSettingRepo) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}
func (r *inMemSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if v, ok := r.data[key]; ok {
		return v, nil
	}
	return "", service.ErrSettingNotFound
}
func (r *inMemSettingRepo) Set(_ context.Context, key, value string) error {
	r.data[key] = value
	return nil
}
func (r *inMemSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := r.data[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}
func (r *inMemSettingRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	for k, v := range settings {
		r.data[k] = v
	}
	return nil
}
func (r *inMemSettingRepo) GetAll(context.Context) (map[string]string, error) { return r.data, nil }
func (r *inMemSettingRepo) Delete(_ context.Context, key string) error {
	delete(r.data, key)
	return nil
}

func newClaudeProfileTestHandler() (*SettingHandler, *inMemSettingRepo) {
	gin.SetMode(gin.TestMode)
	repo := &inMemSettingRepo{data: map[string]string{}}
	svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	return NewSettingHandler(svc, nil, nil, nil, nil, nil, nil), repo
}

const validProfileJSON = `{
  "schema_version": 1,
  "cli_version": "2.1.212",
  "captured_at": "2026-07-16T00:00:00Z",
  "source": "cc-calibrate",
  "headers": {"template": {"User-Agent": "claude-cli/2.1.212 (external, sdk-cli)"}, "absent": ["x-client-request-id"]},
  "beta_rules": {"messages|sonnet|": ["claude-code-20250219"]},
  "guard": {"salt_verified": true, "checked": 2, "ok": 2}
}`

func doJSON(h func(*gin.Context), method, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, "/api/v1/admin/settings/claude-calibrated-profile", bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	h(c)
	return rec
}

func TestPublishClaudeCalibratedProfile_ValidAndClear(t *testing.T) {
	handler, repo := newClaudeProfileTestHandler()

	rec := doJSON(handler.PublishClaudeCalibratedProfile, http.MethodPost, validProfileJSON)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "2.1.212")
	require.NotEmpty(t, repo.data[service.SettingKeyClaudeCodeCalibratedProfile])

	// GET status reflects the published profile.
	getRec := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(getRec)
	gc.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/claude-calibrated-profile", nil)
	handler.GetClaudeCalibratedProfile(gc)
	require.Equal(t, http.StatusOK, getRec.Code)
	require.Contains(t, getRec.Body.String(), `"published":true`)
	require.Contains(t, getRec.Body.String(), `"valid":true`)

	// DELETE clears it.
	delRec := httptest.NewRecorder()
	dc, _ := gin.CreateTestContext(delRec)
	dc.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/settings/claude-calibrated-profile", nil)
	handler.ClearClaudeCalibratedProfile(dc)
	require.Equal(t, http.StatusOK, delRec.Code)
	require.Empty(t, strings.TrimSpace(repo.data[service.SettingKeyClaudeCodeCalibratedProfile]))
}

func TestPublishClaudeCalibratedProfile_RejectsGuardFailure(t *testing.T) {
	handler, repo := newClaudeProfileTestHandler()
	guardFail := strings.Replace(validProfileJSON, `"salt_verified": true`, `"salt_verified": false`, 1)

	rec := doJSON(handler.PublishClaudeCalibratedProfile, http.MethodPost, guardFail)
	require.Equal(t, http.StatusBadRequest, rec.Code, "guard-failed profile must be rejected with 400")
	require.Empty(t, repo.data[service.SettingKeyClaudeCodeCalibratedProfile], "rejected profile must not be persisted")
}

func TestPublishClaudeCalibratedProfile_RejectsVersionUAMismatch(t *testing.T) {
	handler, _ := newClaudeProfileTestHandler()
	mismatch := strings.Replace(validProfileJSON, "claude-cli/2.1.212", "claude-cli/2.1.999", 1)

	rec := doJSON(handler.PublishClaudeCalibratedProfile, http.MethodPost, mismatch)
	require.Equal(t, http.StatusBadRequest, rec.Code, "UA/cc_version mismatch must be rejected")
}
