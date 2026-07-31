package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type proxyDataResponse struct {
	Code int         `json:"code"`
	Data DataPayload `json:"data"`
}

type proxyImportResponse struct {
	Code int              `json:"code"`
	Data DataImportResult `json:"data"`
}

func setupProxyDataRouter() (*gin.Engine, *stubAdminService) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()

	h := NewProxyHandler(adminSvc)
	router.GET("/api/v1/admin/proxies/data", h.ExportData)
	router.POST("/api/v1/admin/proxies/data", h.ImportData)

	return router, adminSvc
}

func TestProxyExportDataRespectsFilters(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	adminSvc.proxies = []service.Proxy{
		{
			ID:       1,
			Name:     "proxy-a",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
		{
			ID:       2,
			Name:     "proxy-b",
			Protocol: "https",
			Host:     "10.0.0.2",
			Port:     443,
			Username: "u",
			Password: "p",
			Status:   service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/data?protocol=https", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp proxyDataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	// 导出必须带格式标识，原来这两行断言的是「导出侧漏了赋值」这个漏洞本身。
	require.Equal(t, dataType, resp.Data.Type)
	require.Equal(t, dataVersion, resp.Data.Version)
	require.Len(t, resp.Data.Proxies, 1)
	require.Len(t, resp.Data.Accounts, 0)
	require.Equal(t, "https", resp.Data.Proxies[0].Protocol)
	require.Equal(t, 1, adminSvc.lastListProxies.calls)
	require.Equal(t, "https", adminSvc.lastListProxies.protocol)
	require.Equal(t, "id", adminSvc.lastListProxies.sortBy)
	require.Equal(t, "desc", adminSvc.lastListProxies.sortOrder)
}

func TestProxyExportDataWithSelectedIDs(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	adminSvc.proxies = []service.Proxy{
		{
			ID:       1,
			Name:     "proxy-a",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
		{
			ID:       2,
			Name:     "proxy-b",
			Protocol: "https",
			Host:     "10.0.0.2",
			Port:     443,
			Username: "u",
			Password: "p",
			Status:   service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/data?ids=2", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp proxyDataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Proxies, 1)
	require.Equal(t, "https", resp.Data.Proxies[0].Protocol)
	require.Equal(t, "10.0.0.2", resp.Data.Proxies[0].Host)
	require.Equal(t, 0, adminSvc.lastListProxies.calls)
}

func TestProxyExportDataPassesSortParams(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	adminSvc.proxies = []service.Proxy{
		{
			ID:       1,
			Name:     "proxy-a",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/data?protocol=http&status=active&search=proxy&sort_by=name&sort_order=asc", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, 1, adminSvc.lastListProxies.calls)
	require.Equal(t, "http", adminSvc.lastListProxies.protocol)
	require.Equal(t, "active", adminSvc.lastListProxies.status)
	require.Equal(t, "proxy", adminSvc.lastListProxies.search)
	require.Equal(t, "name", adminSvc.lastListProxies.sortBy)
	require.Equal(t, "asc", adminSvc.lastListProxies.sortOrder)
}

func TestProxyExportDataSortByAccountCountUsesAccountCountListing(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	adminSvc.proxies = []service.Proxy{
		{
			ID:       1,
			Name:     "proxy-id-1",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Status:   service.StatusActive,
		},
		{
			ID:       2,
			Name:     "proxy-id-2",
			Protocol: "http",
			Host:     "127.0.0.2",
			Port:     8081,
			Status:   service.StatusActive,
		},
	}
	adminSvc.proxyCounts = []service.ProxyWithAccountCount{
		{
			Proxy: service.Proxy{
				ID:       2,
				Name:     "proxy-count-high",
				Protocol: "http",
				Host:     "127.0.0.2",
				Port:     8081,
				Status:   service.StatusActive,
			},
			AccountCount: 9,
		},
		{
			Proxy: service.Proxy{
				ID:       1,
				Name:     "proxy-count-low",
				Protocol: "http",
				Host:     "127.0.0.1",
				Port:     8080,
				Status:   service.StatusActive,
			},
			AccountCount: 1,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/data?sort_by=account_count&sort_order=desc", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp proxyDataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Proxies, 2)
	require.Equal(t, "proxy-count-high", resp.Data.Proxies[0].Name)
	require.Equal(t, "proxy-count-low", resp.Data.Proxies[1].Name)
	require.Equal(t, 0, adminSvc.lastListProxies.calls)
}

func TestProxyImportDataReusesAndTriggersLatencyProbe(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	adminSvc.proxies = []service.Proxy{
		{
			ID:       1,
			Name:     "proxy-a",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
	}

	payload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{
				{
					"proxy_key": "http|127.0.0.1|8080|user|pass",
					"name":      "proxy-a",
					"protocol":  "http",
					"host":      "127.0.0.1",
					"port":      8080,
					"username":  "user",
					"password":  "pass",
					"status":    "inactive",
				},
				{
					"proxy_key": "https|10.0.0.2|443|u|p",
					"name":      "proxy-b",
					"protocol":  "https",
					"host":      "10.0.0.2",
					"port":      443,
					"username":  "u",
					"password":  "p",
					"status":    "active",
				},
			},
			"accounts": []map[string]any{},
		},
	}

	body, _ := json.Marshal(payload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp proxyImportResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 1, resp.Data.ProxyCreated)
	require.Equal(t, 1, resp.Data.ProxyReused)
	require.Equal(t, 0, resp.Data.ProxyFailed)

	adminSvc.mu.Lock()
	updatedIDs := append([]int64(nil), adminSvc.updatedProxyIDs...)
	adminSvc.mu.Unlock()
	require.Contains(t, updatedIDs, int64(1))

	require.Eventually(t, func() bool {
		adminSvc.mu.Lock()
		defer adminSvc.mu.Unlock()
		return len(adminSvc.testedProxyIDs) == 1
	}, time.Second, 10*time.Millisecond)
}

func postProxyImport(t *testing.T, router *gin.Engine, proxies []map[string]any) DataImportResult {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"type":     dataType,
			"version":  dataVersion,
			"proxies":  proxies,
			"accounts": []map[string]any{},
		},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp proxyImportResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	return resp.Data
}

// 归属与共享开关必须随备份走。丢了 provider_user_id，供号商自带的代理恢复后会被
// 当成平台自有，管理员一旦把它勾进自动分配池，就会把这家自费的出口分给另一家供号商。
func TestProxyExportDataCarriesOwnership(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	owner := int64(42)
	adminSvc.proxies = []service.Proxy{
		{
			ID: 1, Name: "platform-shared", Protocol: "http", Host: "127.0.0.1", Port: 8080,
			Status: service.StatusActive, AutoAssignable: true,
		},
		{
			ID: 2, Name: "provider-42-10.0.0.2:1080", Protocol: "socks5h", Host: "10.0.0.2", Port: 1080,
			Status: service.StatusActive, ProviderUserID: &owner,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/data", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp proxyDataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data.Proxies, 2)

	shared := resp.Data.Proxies[0]
	require.True(t, shared.AutoAssignable)
	require.Nil(t, shared.ProviderUserID)

	owned := resp.Data.Proxies[1]
	require.False(t, owned.AutoAssignable)
	require.NotNil(t, owned.ProviderUserID)
	require.Equal(t, int64(42), *owned.ProviderUserID)
}

func TestProxyImportDataRestoresOwnership(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()
	// stub 默认带一条 http/127.0.0.1/8080 的代理，会把下面第一条撞成 reuse。
	adminSvc.proxies = nil
	adminSvc.users = []service.User{
		{ID: 42, Role: service.RoleUser, Status: service.StatusActive, IsProvider: true},
	}

	result := postProxyImport(t, router, []map[string]any{
		{
			"name": "platform-shared", "protocol": "http", "host": "127.0.0.1", "port": 8080,
			"status": "active", "auto_assignable": true,
		},
		{
			"name": "provider-owned", "protocol": "socks5h", "host": "10.0.0.2", "port": 1080,
			"status": "active", "provider_user_id": 42,
		},
	})
	require.Empty(t, result.Errors)
	require.Equal(t, 2, result.ProxyCreated)
	require.Equal(t, 0, result.ProxyFailed)

	adminSvc.mu.Lock()
	created := append([]*service.CreateProxyInput(nil), adminSvc.createdProxies...)
	adminSvc.mu.Unlock()
	require.Len(t, created, 2)

	require.True(t, created[0].AutoAssignable)
	require.Nil(t, created[0].ProviderUserID)

	require.False(t, created[1].AutoAssignable)
	require.NotNil(t, created[1].ProviderUserID)
	require.Equal(t, int64(42), *created[1].ProviderUserID)
}

// 供号商自带的代理不能同时是共享的。service.CreateProxy 也会拒绝这个组合，
// 但导入侧要提前拦，让错误带上 proxy_key —— 否则管理员只收到一条没有上下文的报错，
// 不知道是备份里的哪一条有问题。
func TestProxyImportDataRejectsSharedProviderOwnedProxy(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	result := postProxyImport(t, router, []map[string]any{
		{
			"name": "bad", "protocol": "socks5h", "host": "10.0.0.2", "port": 1080,
			"status": "active", "provider_user_id": 42, "auto_assignable": true,
		},
	})
	require.Equal(t, 0, result.ProxyCreated)
	require.Equal(t, 1, result.ProxyFailed)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "auto-assignable")
	require.NotEmpty(t, result.Errors[0].ProxyKey)

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Empty(t, adminSvc.createdProxies, "非法条目不得落库")
}

func TestProxyImportDataRejectsInvalidProviderUserID(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	result := postProxyImport(t, router, []map[string]any{
		{
			"name": "bad", "protocol": "socks5h", "host": "10.0.0.2", "port": 1080,
			"status": "active", "provider_user_id": 0,
		},
	})
	require.Equal(t, 0, result.ProxyCreated)
	require.Equal(t, 1, result.ProxyFailed)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "provider_user_id")

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Empty(t, adminSvc.createdProxies)
}

// 复用已有代理时刻意不改归属：归属只在创建时确定，而共享开关是安全敏感的，
// 一份旧备份不该悄悄把管理员后来关掉的开关重新打开。但差异必须显式报出来，
// 否则恢复方会以为归属跟着回来了。
func TestProxyImportDataWarnsOnOwnershipMismatchWhenReusing(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	// 现状：平台自有、未开放共享。
	adminSvc.proxies = []service.Proxy{
		{
			ID: 1, Name: "existing", Protocol: "http", Host: "127.0.0.1", Port: 8080,
			Username: "user", Password: "pass", Status: service.StatusActive,
		},
	}

	result := postProxyImport(t, router, []map[string]any{
		{
			"proxy_key": "http|127.0.0.1|8080|user|pass",
			"name":      "existing", "protocol": "http", "host": "127.0.0.1", "port": 8080,
			"username": "user", "password": "pass", "status": "active",
			"provider_user_id": 42,
		},
	})
	require.Equal(t, 1, result.ProxyReused)
	require.Equal(t, 0, result.ProxyCreated)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "provider_user_id=42")
	require.Contains(t, result.Errors[0].Message, "existing none")

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Empty(t, adminSvc.createdProxies)
	// status 一致，不该有任何写操作把归属改掉。
	require.Empty(t, adminSvc.updatedProxyIDs)
}

// 归属是个裸 id。跨实例恢复时它可能指向不存在的用户、或另一个真实的普通用户 ——
// 前者让用量不计入任何供号商（少付），后者把钱付给了错的人。
// 校验不过就丢掉归属，落成「管理员自有」还能人工纠正。
func TestProxyImportDataDropsOwnershipWhenUserIsNotProvider(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()
	adminSvc.proxies = nil
	// 42 号存在，但不是供号商。
	adminSvc.users = []service.User{
		{ID: 42, Role: service.RoleUser, Status: service.StatusActive, IsProvider: false},
	}

	result := postProxyImport(t, router, []map[string]any{
		{
			"name": "owned", "protocol": "socks5h", "host": "10.0.0.2", "port": 1080,
			"status": "active", "provider_user_id": 42,
		},
	})
	require.Equal(t, 1, result.ProxyCreated)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "not a provider")

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Len(t, adminSvc.createdProxies, 1)
	require.Nil(t, adminSvc.createdProxies[0].ProviderUserID, "归属必须被丢弃")
	require.False(t, adminSvc.createdProxies[0].AutoAssignable,
		"归属没落上就绝不能顺带把它变成可共享的平台代理")
}

// 两个供号商完全可能从同一家买到同一个 host:port:user:pass。
// proxy_key 不带归属的话，后一条会命中前一条被「复用」掉，
// B 的账号就走上了 A 自费的出口 —— 两家账号共用同一个出口 IP。
func TestProxyImportDataKeepsSameConnectionStringApart(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()
	adminSvc.proxies = nil
	adminSvc.users = []service.User{
		{ID: 42, Role: service.RoleUser, Status: service.StatusActive, IsProvider: true},
		{ID: 43, Role: service.RoleUser, Status: service.StatusActive, IsProvider: true},
	}

	shared := map[string]any{
		"protocol": "socks5h", "host": "10.0.0.2", "port": 1080,
		"username": "u", "password": "p", "status": "active",
	}
	first := map[string]any{"name": "a", "provider_user_id": 42}
	second := map[string]any{"name": "b", "provider_user_id": 43}
	for k, v := range shared {
		first[k] = v
		second[k] = v
	}

	result := postProxyImport(t, router, []map[string]any{first, second})
	require.Equal(t, 2, result.ProxyCreated, "不同归属的同参数代理必须各建一条")
	require.Equal(t, 0, result.ProxyReused)

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Len(t, adminSvc.createdProxies, 2)
	require.Equal(t, int64(42), *adminSvc.createdProxies[0].ProviderUserID)
	require.Equal(t, int64(43), *adminSvc.createdProxies[1].ProviderUserID)
}

func TestProxyImportDataSilentWhenOwnershipMatches(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	adminSvc.proxies = []service.Proxy{
		{
			ID: 1, Name: "existing", Protocol: "http", Host: "127.0.0.1", Port: 8080,
			Username: "user", Password: "pass", Status: service.StatusActive,
			AutoAssignable: true,
		},
	}

	result := postProxyImport(t, router, []map[string]any{
		{
			"proxy_key": "http|127.0.0.1|8080|user|pass",
			"name":      "existing", "protocol": "http", "host": "127.0.0.1", "port": 8080,
			"username": "user", "password": "pass", "status": "active",
			"auto_assignable": true,
		},
	})
	require.Equal(t, 1, result.ProxyReused)
	require.Empty(t, result.Errors)
}
