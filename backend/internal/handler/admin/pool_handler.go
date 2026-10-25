package admin

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// PoolHandler 提供 Claude 订阅号池的只读运行时视图。
type PoolHandler struct {
	accountService *service.AccountService
	runtime        *service.ClaudeCodeRuntime
	usageService   *service.AccountUsageService
}

func NewPoolHandler(accountService *service.AccountService, runtime *service.ClaudeCodeRuntime, usageService *service.AccountUsageService) *PoolHandler {
	return &PoolHandler{accountService: accountService, runtime: runtime, usageService: usageService}
}

type poolAccountCounts struct {
	Total       int `json:"total"`
	Active      int `json:"active"`
	Schedulable int `json:"schedulable"`
	RateLimited int `json:"rate_limited"`
	Error       int `json:"error"`
}

// Overview GET /admin/pool/overview
func (h *PoolHandler) Overview(c *gin.Context) {
	counts := poolAccountCounts{}
	if h.accountService != nil {
		accounts, err := h.accountService.ListByPlatform(c.Request.Context(), service.PlatformAnthropic)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		for i := range accounts {
			account := &accounts[i]
			if account.Transport() != service.TransportClaudeCode {
				continue
			}
			counts.Total++
			if account.Status == service.StatusActive {
				counts.Active++
			}
			if account.IsSchedulable() {
				counts.Schedulable++
			}
			if account.IsRateLimited() {
				counts.RateLimited++
			}
			if strings.TrimSpace(account.ErrorMessage) != "" {
				counts.Error++
			}
		}
	}

	stats := service.ClaudeCodeRuntimeStats{}
	if h.runtime != nil {
		stats = h.runtime.Stats()
	}
	response.Success(c, gin.H{"accounts": counts, "runtime": stats})
}

// Runs GET /admin/pool/runs
func (h *PoolHandler) Runs(c *gin.Context) {
	if h.runtime == nil {
		response.Success(c, gin.H{"items": []any{}})
		return
	}
	stats := h.runtime.Stats()
	nameByID := map[int64]string{}
	if h.accountService != nil {
		if accounts, err := h.accountService.ListByPlatform(c.Request.Context(), service.PlatformAnthropic); err == nil {
			for i := range accounts {
				nameByID[accounts[i].ID] = accounts[i].Name
			}
		}
	}
	items := make([]gin.H, 0, len(stats.Runs))
	for _, run := range stats.Runs {
		session := run.SessionKey
		if len(session) > 12 {
			session = session[:12]
		}
		items = append(items, gin.H{
			"session":      session,
			"account_id":   run.AccountID,
			"account_name": nameByID[run.AccountID],
			"state":        run.State,
			"started_at":   run.StartedAt,
			"last_used_at": run.LastUsedAt,
			"tool_pending": run.ToolPending,
		})
	}
	response.Success(c, gin.H{"items": items})
}

// Health GET /admin/pool/health
func (h *PoolHandler) Health(c *gin.Context) {
	enabled := h.runtime != nil && h.runtime.Enabled()
	status := http.StatusOK
	if !enabled {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"enabled": enabled, "owner_id": func() string {
		if h.runtime == nil {
			return ""
		}
		return h.runtime.Stats().OwnerID
	}()})
}

// ReleaseRun DELETE /admin/pool/runs/:session
func (h *PoolHandler) ReleaseRun(c *gin.Context) {
	sessionKey := c.Param("session")
	if sessionKey == "" || h.runtime == nil {
		response.BadRequest(c, "Invalid session")
		return
	}
	if !h.runtime.ReleaseRun(sessionKey) {
		response.NotFound(c, "Run not found")
		return
	}
	h.runtime.RecordEvent("manual_release", sessionKey, 0, "")
	response.Success(c, gin.H{"released": true})
}

// Events GET /admin/pool/events
func (h *PoolHandler) Events(c *gin.Context) {
	if h.runtime == nil {
		response.Success(c, gin.H{"items": []any{}})
		return
	}
	events := h.runtime.Events()
	items := make([]gin.H, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		items = append(items, gin.H{
			"time": e.Time, "type": e.Type, "session": e.Session,
			"account_id": e.AccountID, "message": e.Message,
		})
	}
	response.Success(c, gin.H{"items": items})
}

// Accounts GET /admin/pool/accounts
// 只返回 Claude 订阅账号，服务端分页/搜索。
func (h *PoolHandler) Accounts(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	status := strings.TrimSpace(c.Query("status"))
	search := strings.TrimSpace(c.Query("search"))
	sortBy := c.DefaultQuery("sort_by", "name")
	sortOrder := c.DefaultQuery("sort_order", "asc")
	if h.accountService == nil {
		response.Success(c, gin.H{"items": []any{}, "total": 0, "page": page, "page_size": pageSize})
		return
	}
	accounts, total, err := h.accountService.ListClaudeSubscriptionAccounts(
		c.Request.Context(),
		pagination.PaginationParams{Page: page, PageSize: pageSize, SortBy: sortBy, SortOrder: sortOrder},
		status, search, sortBy, sortOrder,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]*dto.Account, 0, len(accounts))
	for i := range accounts {
		items = append(items, dto.AccountFromService(&accounts[i]))
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// Usage POST /admin/pool/usage
// 批量返回 Claude 订阅账号的被动 5h/7d 窗口，最多 50 个。
func (h *PoolHandler) Usage(c *gin.Context) {
	var req struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	if len(req.AccountIDs) > 50 {
		req.AccountIDs = req.AccountIDs[:50]
	}
	out := make(map[string]any, len(req.AccountIDs))
	if h.usageService == nil {
		response.Success(c, gin.H{"usage": out})
		return
	}
	seen := make(map[int64]struct{}, len(req.AccountIDs))
	ids := make([]int64, 0, len(req.AccountIDs))
	for _, id := range req.AccountIDs {
		if id > 0 {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	type usageResult struct {
		id    int64
		usage *service.UsageInfo
	}
	ch := make(chan usageResult, len(ids))
	sem := make(chan struct{}, 8)
	for _, id := range ids {
		id := id
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			usage, err := h.usageService.GetPassiveUsage(c.Request.Context(), id)
			if err == nil {
				ch <- usageResult{id: id, usage: usage}
			} else {
				ch <- usageResult{id: id}
			}
		}()
	}
	for range ids {
		res := <-ch
		if res.usage == nil {
			continue
		}
		out[fmt.Sprintf("%d", res.id)] = gin.H{
			"five_hour": res.usage.FiveHour,
			"seven_day": res.usage.SevenDay,
			"source":    res.usage.Source,
		}
	}
	response.Success(c, gin.H{"usage": out})
}
