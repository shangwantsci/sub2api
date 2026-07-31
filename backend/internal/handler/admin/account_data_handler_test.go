package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type dataResponse struct {
	Code int         `json:"code"`
	Data dataPayload `json:"data"`
}

type dataPayload struct {
	Type           string        `json:"type"`
	Version        int           `json:"version"`
	Proxies        []dataProxy   `json:"proxies"`
	Accounts       []dataAccount `json:"accounts"`
	SkippedShadows int           `json:"skipped_shadows"`
}

type dataProxy struct {
	ProxyKey string `json:"proxy_key"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Status   string `json:"status"`
}

type dataAccount struct {
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Credentials map[string]any `json:"credentials"`
	Extra       map[string]any `json:"extra"`
	ProxyKey    *string        `json:"proxy_key"`
	Concurrency int            `json:"concurrency"`
	Priority    int            `json:"priority"`
}

func setupAccountDataRouter() (*gin.Engine, *stubAdminService) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()

	h := NewAccountHandler(
		adminSvc,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	router.GET("/api/v1/admin/accounts/data", h.ExportData)
	router.POST("/api/v1/admin/accounts/data", h.ImportData)
	return router, adminSvc
}

func TestExportDataIncludesSecrets(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	proxyID := int64(11)
	adminSvc.proxies = []service.Proxy{
		{
			ID:       proxyID,
			Name:     "proxy",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
		{
			ID:       12,
			Name:     "orphan",
			Protocol: "https",
			Host:     "10.0.0.1",
			Port:     443,
			Username: "o",
			Password: "p",
			Status:   service.StatusActive,
		},
	}
	adminSvc.accounts = []service.Account{
		{
			ID:          21,
			Name:        "account",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			Extra:       map[string]any{"note": "x"},
			ProxyID:     &proxyID,
			Concurrency: 3,
			Priority:    50,
			Status:      service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	// 导出必须带格式标识。dataType/dataVersion 常量与 validateDataHeader 的校验一直都在，
	// 只有导出侧漏了赋值，于是真实备份文件里根本没有这两个键，格式校验形同虚设。
	// 这两行原本断言的是那个漏洞本身。
	require.Equal(t, dataType, resp.Data.Type)
	require.Equal(t, dataVersion, resp.Data.Version)
	require.Len(t, resp.Data.Proxies, 1)
	require.Equal(t, "pass", resp.Data.Proxies[0].Password)
	require.Len(t, resp.Data.Accounts, 1)
	require.Equal(t, "secret", resp.Data.Accounts[0].Credentials["token"])
}

func TestExportDataWithoutProxies(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	proxyID := int64(11)
	adminSvc.proxies = []service.Proxy{
		{
			ID:       proxyID,
			Name:     "proxy",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
	}
	adminSvc.accounts = []service.Account{
		{
			ID:          21,
			Name:        "account",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			ProxyID:     &proxyID,
			Concurrency: 3,
			Priority:    50,
			Status:      service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data?include_proxies=false", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Proxies, 0)
	require.Len(t, resp.Data.Accounts, 1)
	require.Nil(t, resp.Data.Accounts[0].ProxyKey)
}

// TestExportDataExcludesSparkShadow 验证外审第5轮 P1/P2:导出时排除 spark 影子账号
// (影子无凭据、导入侧强制 credentials 非空,混入会产出无法还原的坏备份),并透出跳过计数。
func TestExportDataExcludesSparkShadow(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	parentID := int64(21)
	adminSvc.accounts = []service.Account{
		{
			ID:          parentID,
			Name:        "mother",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			Status:      service.StatusActive,
		},
		{
			ID:              22,
			Name:            "mother (Spark)",
			Platform:        service.PlatformOpenAI,
			Type:            service.AccountTypeOAuth,
			Credentials:     map[string]any{}, // 影子恒空凭据
			ParentAccountID: &parentID,        // 影子标记
			QuotaDimension:  service.QuotaDimensionSpark,
			Status:          service.StatusActive,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data?include_proxies=false", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Accounts, 1, "影子应被排除,仅导出母账号")
	require.Equal(t, "mother", resp.Data.Accounts[0].Name)
	require.Equal(t, 1, resp.Data.SkippedShadows, "跳过的影子数量应透出")
}

func TestExportDataPassesAccountFiltersAndSort(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.accounts = []service.Account{
		{ID: 1, Name: "acc-1", Status: service.StatusActive},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/accounts/data?platform=openai&type=oauth&status=active&group=12&privacy_mode=blocked&search=keyword&sort_by=priority&sort_order=desc",
		nil,
	)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, 1, adminSvc.lastListAccounts.calls)
	require.Equal(t, "openai", adminSvc.lastListAccounts.platform)
	require.Equal(t, "oauth", adminSvc.lastListAccounts.accountType)
	require.Equal(t, "active", adminSvc.lastListAccounts.status)
	require.Equal(t, int64(12), adminSvc.lastListAccounts.groupID)
	require.Equal(t, "blocked", adminSvc.lastListAccounts.privacyMode)
	require.Equal(t, "keyword", adminSvc.lastListAccounts.search)
	require.Equal(t, "priority", adminSvc.lastListAccounts.sortBy)
	require.Equal(t, "desc", adminSvc.lastListAccounts.sortOrder)
}

func TestExportDataSelectedIDsOverrideFilters(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/accounts/data?ids=1,2&platform=openai&search=keyword&sort_by=priority&sort_order=desc",
		nil,
	)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Accounts, 2)
	require.Equal(t, 0, adminSvc.lastListAccounts.calls)
}

func TestImportDataReusesProxyAndSkipsDefaultGroup(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	adminSvc.proxies = []service.Proxy{
		{
			ID:       1,
			Name:     "proxy",
			Protocol: "socks5",
			Host:     "1.2.3.4",
			Port:     1080,
			Username: "u",
			Password: "p",
			Status:   service.StatusActive,
		},
	}

	dataPayload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{
				{
					"proxy_key": "socks5|1.2.3.4|1080|u|p",
					"name":      "proxy",
					"protocol":  "socks5",
					"host":      "1.2.3.4",
					"port":      1080,
					"username":  "u",
					"password":  "p",
					"status":    "active",
				},
			},
			"accounts": []map[string]any{
				{
					"name":        "acc",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeOAuth,
					"credentials": map[string]any{"token": "x"},
					"proxy_key":   "socks5|1.2.3.4|1080|u|p",
					"concurrency": 3,
					"priority":    50,
				},
			},
		},
		"skip_default_group_bind": true,
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdProxies, 0)
	require.Len(t, adminSvc.createdAccounts, 1)
	require.True(t, adminSvc.createdAccounts[0].SkipDefaultGroupBind)
}

type fullDataResponse struct {
	Code int         `json:"code"`
	Data DataPayload `json:"data"`
}

func postAccountImport(t *testing.T, router *gin.Engine, accounts []map[string]any) DataImportResult {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"type":     dataType,
			"version":  dataVersion,
			"proxies":  []map[string]any{},
			"accounts": accounts,
		},
		"skip_default_group_bind": true,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Code int              `json:"code"`
		Data DataImportResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	return resp.Data
}

// 结算完全按 provider_user_id 聚合。这一列丢了，恢复出来的账号就不再计入任何
// 供号商——直接少付钱且全程无报错。分组同理：一个组都没绑的账号不参与调度，
// 用量恒为 0，归属恢复对了也白搭。
func TestExportDataCarriesProviderAndSchedulingFields(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	owner := int64(42)
	tier := "3"
	adminSvc.accounts = []service.Account{
		{
			ID:             21,
			Name:           "provider-account",
			Platform:       service.PlatformAnthropic,
			Type:           service.AccountTypeSetupToken,
			Credentials:    map[string]any{"token": "secret"},
			Concurrency:    3,
			Priority:       1,
			Status:         service.StatusDisabled,
			Schedulable:    false,
			ProviderUserID: &owner,
			ProviderTier:   &tier,
			GroupIDs:       []int64{2, 5},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp fullDataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data.Accounts, 1)

	acc := resp.Data.Accounts[0]
	require.NotNil(t, acc.ProviderUserID)
	require.Equal(t, int64(42), *acc.ProviderUserID)
	require.NotNil(t, acc.ProviderTier)
	require.Equal(t, "3", *acc.ProviderTier)
	require.Equal(t, service.StatusDisabled, acc.Status)
	require.NotNil(t, acc.Schedulable)
	require.False(t, *acc.Schedulable)
	require.Equal(t, []int64{2, 5}, acc.GroupIDs)
}

func TestImportDataRestoresProviderAndSchedulingFields(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.groups = []service.Group{{ID: 2}, {ID: 5}}
	adminSvc.users = []service.User{
		{ID: 42, Role: service.RoleUser, Status: service.StatusActive, IsProvider: true},
	}

	result := postAccountImport(t, router, []map[string]any{
		{
			"name":             "provider-account",
			"platform":         service.PlatformAnthropic,
			"type":             service.AccountTypeSetupToken,
			"credentials":      map[string]any{"token": "x"},
			"concurrency":      3,
			"priority":         1,
			"provider_user_id": 42,
			"provider_tier":    "3",
			"status":           service.StatusDisabled,
			"schedulable":      false,
			"group_ids":        []int64{2, 5},
		},
	})
	require.Empty(t, result.Errors)
	require.Equal(t, 1, result.AccountCreated)

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Len(t, adminSvc.createdAccounts, 1)
	created := adminSvc.createdAccounts[0]

	require.NotNil(t, created.ProviderUserID)
	require.Equal(t, int64(42), *created.ProviderUserID)
	require.NotNil(t, created.ProviderTier)
	require.Equal(t, "3", *created.ProviderTier)
	require.Equal(t, service.StatusDisabled, created.Status)
	require.NotNil(t, created.Schedulable)
	require.False(t, *created.Schedulable)
	require.Equal(t, []int64{2, 5}, created.GroupIDs)
}

// 备份不带这些字段时（旧格式）必须走安全默认，而不是报错或写坏数据。
func TestImportDataWithoutNewFieldsKeepsDefaults(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	result := postAccountImport(t, router, []map[string]any{
		{
			"name":        "legacy",
			"platform":    service.PlatformOpenAI,
			"type":        service.AccountTypeOAuth,
			"credentials": map[string]any{"token": "x"},
			"concurrency": 1,
			"priority":    50,
		},
	})
	require.Empty(t, result.Errors)
	require.Equal(t, 1, result.AccountCreated)

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	created := adminSvc.createdAccounts[0]
	require.Nil(t, created.ProviderUserID)
	require.Nil(t, created.ProviderTier)
	require.Empty(t, created.Status)
	require.Nil(t, created.Schedulable)
	require.Empty(t, created.GroupIDs)
}

// 归属指向的用户必须确实是本实例上的供号商。指向不存在的用户会让用量不计入任何人
// （少付），指向另一个真实供号商则是把钱付给错的人。
func TestImportDataDropsOwnershipWhenUserIsNotProvider(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.users = []service.User{
		{ID: 42, Role: service.RoleUser, Status: service.StatusActive, IsProvider: false},
	}

	result := postAccountImport(t, router, []map[string]any{
		{
			"name":             "acc",
			"platform":         service.PlatformOpenAI,
			"type":             service.AccountTypeOAuth,
			"credentials":      map[string]any{"token": "x"},
			"concurrency":      1,
			"priority":         50,
			"provider_user_id": 42,
			"provider_tier":    "3",
		},
	})
	require.Equal(t, 1, result.AccountCreated)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "not a provider")

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	created := adminSvc.createdAccounts[0]
	require.Nil(t, created.ProviderUserID)
	require.Nil(t, created.ProviderTier, "没有归属的档位没有意义，按档位回填只筛归属非空的账号")
}

// 停用的分组仍然是本实例上真实存在的分组，不能当成「不存在」把绑定丢掉。
func TestImportDataKeepsBindingToDisabledGroup(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.groups = []service.Group{
		{ID: 2, Status: service.StatusActive},
		{ID: 9, Status: service.StatusDisabled},
	}

	result := postAccountImport(t, router, []map[string]any{
		{
			"name":        "acc",
			"platform":    service.PlatformOpenAI,
			"type":        service.AccountTypeOAuth,
			"credentials": map[string]any{"token": "x"},
			"concurrency": 1,
			"priority":    50,
			"group_ids":   []int64{2, 9},
		},
	})
	require.Empty(t, result.Errors)

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Equal(t, []int64{2, 9}, adminSvc.createdAccounts[0].GroupIDs)
}

// 分组 id 只在同实例有意义。对不上的过滤掉但必须报出来——静默丢弃会得到一个
// 不绑任何组、因而永远没有用量、结算恒为 0 的账号。
func TestImportDataDropsUnknownGroupsWithWarning(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.groups = []service.Group{{ID: 2}}

	result := postAccountImport(t, router, []map[string]any{
		{
			"name":        "acc",
			"platform":    service.PlatformOpenAI,
			"type":        service.AccountTypeOAuth,
			"credentials": map[string]any{"token": "x"},
			"concurrency": 1,
			"priority":    50,
			"group_ids":   []int64{2, 999},
		},
	})
	require.Equal(t, 1, result.AccountCreated, "账号仍应建成功，只是少绑一个组")
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "999")

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Equal(t, []int64{2}, adminSvc.createdAccounts[0].GroupIDs)
}
