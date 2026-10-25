//go:build unit

package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestPoolHandler_EmptyRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewPoolHandler(nil, nil, nil)
	r := gin.New()
	r.GET("/pool/overview", h.Overview)
	r.GET("/pool/runs", h.Runs)
	r.GET("/pool/health", h.Health)
	r.DELETE("/pool/runs/:session", h.ReleaseRun)
	r.GET("/pool/events", h.Events)
	r.GET("/pool/accounts", h.Accounts)
	r.POST("/pool/usage", h.Usage)

	for _, path := range []string{"/pool/overview", "/pool/runs"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, rec.Code, path)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pool/health", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/pool/runs/s1", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pool/events", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pool/accounts?page=1&page_size=20&search=max", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pool/usage", strings.NewReader(`{"account_ids":[1,2]}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
}
