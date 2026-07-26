package provider

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ListAccounts 分页返回当前供号商名下的账号（脱敏 + 本期用量）。
// GET /api/v1/provider/accounts?page=1&page_size=20
func (h *Handler) ListAccounts(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	settings, err := h.loadSettings(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	page, pageSize := response.ParsePagination(c)
	accounts, result, err := h.accountRepo.ListByProviderPaged(
		ctx, providerID,
		pagination.PaginationParams{Page: page, PageSize: pageSize},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	window, err := h.settlementService.CurrentWindow(ctx, providerID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// 一次查完本期所有账号的用量再按当页取用，避免逐账号 N+1。
	// 聚合本身按供号商整体做，与分页无关。
	usageByAccount, err := h.usageReader.GetProviderAccountTotalsBatch(ctx, providerID, window)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	views := make([]AccountView, 0, len(accounts))
	for i := range accounts {
		acc := accounts[i]
		var usage *service.ProviderPeriodTotals
		if v, found := usageByAccount[acc.ID]; found {
			usage = &v
		}
		views = append(views, AccountViewFromService(&acc, settings, usage))
	}

	response.Paginated(c, views, result.Total, page, pageSize)
}

// GetAccountStats 返回单个账号的本期 1 倍率用量。
// GET /api/v1/provider/accounts/:id/stats
func (h *Handler) GetAccountStats(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	account, ok := h.ownedAccount(ctx, c, providerID)
	if !ok {
		return
	}

	window, err := h.settlementService.CurrentWindow(ctx, providerID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	totals, err := h.usageReader.GetProviderSingleAccountTotals(ctx, account.ID, window)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	settings, err := h.loadSettings(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"account":      AccountViewFromService(account, settings, totals),
		"period_start": window.Start,
		"totals":       totals,
	})
}

// PauseAccount 暂停调度。
// POST /api/v1/provider/accounts/:id/pause
func (h *Handler) PauseAccount(c *gin.Context) {
	h.setSchedulable(c, false)
}

// ResumeAccount 恢复调度。
// POST /api/v1/provider/accounts/:id/resume
func (h *Handler) ResumeAccount(c *gin.Context) {
	h.setSchedulable(c, true)
}

func (h *Handler) setSchedulable(c *gin.Context, schedulable bool) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	account, ok := h.ownedAccount(ctx, c, providerID)
	if !ok {
		return
	}
	updated, err := h.adminService.SetAccountSchedulable(ctx, account.ID, schedulable)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	settings, err := h.loadSettings(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, AccountViewFromService(updated, settings, nil))
}

// OfflineAccount 下线账号（软删除）。
// DELETE /api/v1/provider/accounts/:id
//
// 已产生的金额不受影响：对账查询按 provider_user_id 联表且不过滤 deleted_at，
// 该账号本期用量会继续出现在明细里并标注「已下线」。
func (h *Handler) OfflineAccount(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	account, ok := h.ownedAccount(ctx, c, providerID)
	if !ok {
		return
	}
	if err := h.adminService.DeleteAccount(ctx, account.ID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": account.ID, "offline": true})
}
