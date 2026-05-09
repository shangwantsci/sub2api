//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaudePoolHandlerGetStatus_PublicEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewClaudePoolHandler(service.NewClaudePoolStatusService(nil))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/claude-pool/status", nil)

	h.GetStatus(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, service.ClaudePoolStatusUnavailable, resp.Data.Status)
}
