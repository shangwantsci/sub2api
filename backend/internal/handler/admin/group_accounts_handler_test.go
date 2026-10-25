//go:build unit

package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupHandler_AccountsRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewGroupHandler(&stubAdminService{}, nil, nil)
	r := gin.New()
	r.GET("/groups/:id/accounts", h.ListAccounts)
	r.PUT("/groups/:id/accounts", h.SetAccounts)

	t.Run("list", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/groups/7/accounts", nil)
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"code":0`)
	})

	t.Run("set", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/groups/7/accounts", strings.NewReader(`{"account_ids":[1,2]}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"account_count":2`)
	})

	t.Run("invalid id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/groups/0/accounts", nil)
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	})
}
