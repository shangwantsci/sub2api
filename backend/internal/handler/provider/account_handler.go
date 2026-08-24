package provider

import (
	"context"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
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
	h.attachRuntimeOccupancy(ctx, accounts, views)

	response.Paginated(c, views, result.Total, page, pageSize)
}

// GetAccountUsage 返回账号在 Anthropic 侧的额度用量。
// GET /api/v1/provider/accounts/:id/usage?source=passive|active
//
// 两种数据来源的代价差很多，默认走 passive：
//   - passive 只读账号 extra 里的被动采样值（网关每次请求顺带采回来的响应头），
//     零外部调用，列表页逐行拉也不会打爆什么；
//   - active 会真的去问一次 Anthropic，供号商手动点刷新时才用。
//
// 响应走 AccountUsageView 白名单收敛，不透传 service.UsageInfo —— 后者挂着各平台
// 的一堆字段和三种口径的金额。
func (h *Handler) GetAccountUsage(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	account, ok := h.ownedAccount(ctx, c, providerID)
	if !ok {
		return
	}
	if h.accountUsageService == nil {
		response.ErrorFrom(c, infraerrors.NotFound("USAGE_UNAVAILABLE", "usage data is unavailable"))
		return
	}

	// 非 Anthropic OAuth/Setup Token 的账号没有这套窗口数据。供号商账号本来就强制
	// PlatformAnthropic，这里只是防御性返回空，而不是把 service 层的报错抛给他看。
	if !account.IsAnthropicOAuthOrSetupToken() {
		response.Success(c, AccountUsageView{})
		return
	}

	var (
		info *service.UsageInfo
		err  error
	)
	if c.DefaultQuery("source", "passive") == "active" {
		info, err = h.accountUsageService.GetUsage(ctx, account.ID)
	} else {
		info, err = h.accountUsageService.GetPassiveUsage(ctx, account.ID)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, AccountUsageViewFromService(info))
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

// UpdateAccountRequest 是账号编辑请求。
//
// 三项都用指针，nil 表示「这次不动这一项」。
// 特别注意 Notes：传空字符串是**清空备注**，与 nil 不是一回事，前端不要图省事恒传空串。
type UpdateAccountRequest struct {
	Name  *string `json:"name"`
	Notes *string `json:"notes"`
	// Tier 换速率档位。自定义档时 CustomTier 必填，仍受 provider_custom_tier_caps 护栏约束。
	Tier       *string            `json:"tier"`
	CustomTier *CustomTierPayload `json:"custom_tier"`
}

// UpdateAccount 编辑已上号的账号：名称、备注、速率档位。
// PATCH /api/v1/provider/accounts/:id
//
// 刻意不开放两项，它们各自需要单独设计而不该搭在这个接口上：
//   - 托管类型：改分组必须同步重算 priority。priority 在调度里是硬门槛而非权重
//     （filterByMinPriority 只保留分组内最小的那批），取值不对会让账号被静默饿死，
//     或者反过来独占整个分组把自有账号挤出去，两种后果都没有任何报错。
//   - 出口代理：换代理要处理旧私有代理的回收与同参数去重，删错了会波及别人的账号。
func (h *Handler) UpdateAccount(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	account, ok := h.ownedAccount(ctx, c, providerID)
	if !ok {
		return
	}

	var req UpdateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	settings, err := h.loadSettings(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if req.Name != nil || req.Notes != nil {
		account, err = h.updateAccountProfile(ctx, account, req)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	if req.Tier != nil {
		account, err = h.updateAccountTier(ctx, account, settings, req)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	response.Success(c, AccountViewFromService(account, settings, nil))
}

// updateAccountProfile 只改名称与备注。
func (h *Handler) updateAccountProfile(
	ctx context.Context,
	account *service.Account,
	req UpdateAccountRequest,
) (*service.Account, error) {
	input, err := buildProfileUpdateInput(req)
	if err != nil {
		return nil, err
	}
	return h.adminService.UpdateAccount(ctx, account.ID, input)
}

// buildProfileUpdateInput 构造只改名称与备注的最小更新。
//
// **除 Name 与 Notes 外的字段必须全部留零值/nil，这是唯一安全的调用形态**，
// 别为了「顺手把当前值也带上」而回填其它字段：
//   - Extra 只要非 nil 就是整体覆盖，会把 enable_tls_fingerprint、
//     session_id_masking_enabled 与 account_uuid 一起清掉。伪装失效是完全静默的：
//     账号照常跑、没有任何报错，只是伪装不再生效。
//   - Credentials 的非敏感键同样由入参说了算，回填不全就会丢键。
//   - Priority 传 0 会真的写 0，而 0 是最高优先级，该账号会独占整个分组。
//
// 拆成纯函数是为了让上面这条约束能被测试钉住，见 account_update_test.go。
func buildProfileUpdateInput(req UpdateAccountRequest) (*service.UpdateAccountInput, error) {
	// Name 的空串在 UpdateAccountInput 里表示「不改」，所以显式传了空名字必须拦下来，
	// 否则供号商会以为改成功了，实际名字纹丝未动。
	name := ""
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, infraerrors.BadRequest("NAME_REQUIRED", "account name is required")
		}
	}
	return &service.UpdateAccountInput{
		Name:  name,
		Notes: req.Notes,
	}, nil
}

// updateAccountTier 换速率档位。
//
// extra 必须基于账号**现有** extra 增量合并（ApplyProviderTierToExtra），
// 绝不能构造一个只含档位键的 map —— 落库是整列覆盖，那样会清掉 persona_*、
// window_cost_sticky_reserve、身份字段等所有另行配置的持久设置。
func (h *Handler) updateAccountTier(
	ctx context.Context,
	account *service.Account,
	settings service.ProviderSettings,
	req UpdateAccountRequest,
) (*service.Account, error) {
	tier, err := service.ResolveProviderTier(settings, *req.Tier, toServiceCustomTier(req.CustomTier))
	if err != nil {
		return nil, err
	}
	// 并发与负载因子都取档位并发，与上号时的赋值保持一致（见 BuildProviderAccountInput）。
	if err := h.accountRepo.UpdateProviderAccountTier(
		ctx, account.ID, tier.Tier, tier.Concurrency, tier.Concurrency,
		service.ApplyProviderTierToExtra(account.Extra, tier),
	); err != nil {
		return nil, err
	}
	// 重新读回：档位走的是定向更新，内存里的 account 还是旧值，
	// 直接拿它构造响应会让界面显示成「没改成功」。
	refreshed, err := h.adminService.GetAccount(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if refreshed == nil {
		return nil, infraerrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")
	}
	return refreshed, nil
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
