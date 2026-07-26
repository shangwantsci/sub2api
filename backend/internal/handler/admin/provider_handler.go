package admin

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/handler/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// ProviderHandler 是管理端供号商对账面板的接口。
type ProviderHandler struct {
	settlementService *service.ProviderSettlementService
	settingService    *service.SettingService
	adminService      service.AdminService
	accountRepo       service.AccountRepository
}

func NewProviderHandler(
	settlementService *service.ProviderSettlementService,
	settingService *service.SettingService,
	adminService service.AdminService,
	accountRepo service.AccountRepository,
) *ProviderHandler {
	return &ProviderHandler{
		settlementService: settlementService,
		settingService:    settingService,
		adminService:      adminService,
		accountRepo:       accountRepo,
	}
}

func actorUserID(c *gin.Context) int64 {
	if subject, ok := middleware.GetAuthSubjectFromContext(c); ok {
		return subject.UserID
	}
	return 0
}

func parsePathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid id")
		return 0, false
	}
	return id, true
}

// List 返回供号商列表与待结算汇总。
// GET /api/v1/admin/providers
func (h *ProviderHandler) List(c *gin.Context) {
	summaries, err := h.settlementService.ListProviderSummaries(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, summaries)
}

// GetCurrentPeriod 返回某供号商本期分账号明细，供结算前核对。
// GET /api/v1/admin/providers/:id/current-period
func (h *ProviderHandler) GetCurrentPeriod(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	period, err := h.settlementService.GetCurrentPeriod(c.Request.Context(), id, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, period)
}

// SettleRequest 是单个结算请求。
type SettleRequest struct {
	Notes string `json:"notes"`
}

// Settle 对一个供号商封账。
// POST /api/v1/admin/providers/:id/settle
func (h *ProviderHandler) Settle(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	var req SettleRequest
	_ = c.ShouldBindJSON(&req)

	record, err := h.settlementService.Settle(
		c.Request.Context(), id, actorUserID(c), req.Notes, time.Time{},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

// SettleBatchRequest 是批量结算请求。
type SettleBatchRequest struct {
	ProviderUserIDs []int64 `json:"provider_user_ids" binding:"required,min=1"`
	Notes           string  `json:"notes"`
}

// SettleBatch 批量封账，所有结算单共用同一个周期终点。
// POST /api/v1/admin/providers/settle-batch
func (h *ProviderHandler) SettleBatch(c *gin.Context) {
	var req SettleBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.settlementService.SettleBatch(
		c.Request.Context(), req.ProviderUserIDs, actorUserID(c), req.Notes,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// VoidRequest 是作废请求。
type VoidRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// Void 作废最近一期结算单，金额回到待结算。
// POST /api/v1/admin/providers/settlements/:id/void
func (h *ProviderHandler) Void(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	var req VoidRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.settlementService.Void(c.Request.Context(), id, actorUserID(c), req.Reason); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": id, "voided": true})
}

// ListSettlements 返回某供号商的结算历史。
// GET /api/v1/admin/providers/:id/settlements
func (h *ProviderHandler) ListSettlements(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	records, result, err := h.settlementService.ListSettlements(
		c.Request.Context(), id,
		pagination.PaginationParams{Page: page, PageSize: pageSize},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, records, result.Total, page, pageSize)
}

// ExportSettlement 导出某期结算单的分账号明细 CSV。
// GET /api/v1/admin/providers/settlements/:id/export
//
// CSV 输出完整精度金额，作为对账凭证。
func (h *ProviderHandler) ExportSettlement(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	record, err := h.settlementService.GetSettlement(ctx, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	accounts, err := h.settlementService.GetSettlementBreakdown(ctx, record)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	filename := fmt.Sprintf("settlement-%d-provider-%d.csv", record.ID, record.ProviderUserID)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString("\xEF\xBB\xBF")

	w := csv.NewWriter(c.Writer)
	defer w.Flush()

	_ = w.Write([]string{"settlement_id", strconv.FormatInt(record.ID, 10)})
	_ = w.Write([]string{"provider_user_id", strconv.FormatInt(record.ProviderUserID, 10)})
	_ = w.Write([]string{"period_start", record.PeriodStart.Format(time.RFC3339)})
	_ = w.Write([]string{"period_end", record.PeriodEnd.Format(time.RFC3339)})
	_ = w.Write([]string{"status", record.Status})
	_ = w.Write([]string{"snapshot_standard_cost_usd", provider.CSVAmount(record.StandardCost)})
	_ = w.Write([]string{})
	_ = w.Write([]string{"account_id", "account_name", "offline", "requests", "tokens", "standard_cost_usd"})

	total := decimal.Zero
	for _, a := range accounts {
		offline := "false"
		if a.Offline {
			offline = "true"
		}
		_ = w.Write([]string{
			strconv.FormatInt(a.AccountID, 10),
			// 账号名来自供号商，可能以 = + - @ 开头触发表格公式，必须转义。
			provider.CSVCell(a.AccountName),
			offline,
			strconv.FormatInt(a.Requests, 10),
			strconv.FormatInt(a.Tokens, 10),
			provider.CSVAmount(a.StandardCost),
		})
		total = total.Add(a.StandardCost)
	}
	_ = w.Write([]string{})
	// 明细来自封账快照，合计应与上面的 snapshot 完全一致；不一致说明快照写入有问题。
	_ = w.Write([]string{"items_total", "", "", "", "", provider.CSVAmount(total)})
}

// ---------- 供货商设置 ----------

// ProviderSettingsResponse 在设置基础上附带各分组的真实策略，供管理端只读核对。
//
// 这些策略字段只出现在管理端接口，绝不下发到供号商侧。
type ProviderSettingsResponse struct {
	service.ProviderSettings
	Groups []ProviderGroupPolicyView `json:"available_groups"`
}

// ProviderGroupPolicyView 是分组的真实策略，仅管理端可见。
type ProviderGroupPolicyView struct {
	ID                     int64   `json:"id"`
	Name                   string  `json:"name"`
	Platform               string  `json:"platform"`
	RateMultiplier         float64 `json:"rate_multiplier"`
	ContentReviewPolicy    string  `json:"content_review_policy"`
	SystemPromptPolicy     string  `json:"claude_oauth_system_prompt_policy"`
	Status                 string  `json:"status"`
	AccountCountForDisplay int     `json:"account_count,omitempty"`
	// ExistingPriorities 是该分组内非供号商账号已有的 priority 去重值。
	//
	// 设置页据此提示优先级冲突：调度里 priority 是硬门槛（只保留数值最小的那批），
	// 分组里混有不同 priority 的账号时，数值大的一边会被完全饿死且不报错。
	ExistingPriorities []int `json:"existing_priorities"`
}

// GetSettings 返回供货商设置与可选分组的真实策略。
// GET /api/v1/admin/provider-settings
func (h *ProviderHandler) GetSettings(c *gin.Context) {
	ctx := c.Request.Context()
	settings, err := h.settingService.GetProviderSettings(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	groups, err := h.adminService.GetAllGroups(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	prioritiesByGroup, err := h.accountRepo.DistinctNonProviderPrioritiesByGroup(ctx)
	if err != nil {
		// 优先级提示是辅助信息，取不到不该挡住整个设置页。
		slog.Warn("failed to load group priorities for provider settings", "error", err)
		prioritiesByGroup = map[int64][]int{}
	}

	views := make([]ProviderGroupPolicyView, 0, len(groups))
	for i := range groups {
		g := groups[i]
		if g.Platform != service.PlatformAnthropic {
			continue
		}
		existing := prioritiesByGroup[g.ID]
		if existing == nil {
			existing = []int{}
		}
		views = append(views, ProviderGroupPolicyView{
			ID:                  g.ID,
			Name:                g.Name,
			Platform:            g.Platform,
			RateMultiplier:      g.RateMultiplier,
			ContentReviewPolicy: g.AnthropicContentReviewPolicy(),
			SystemPromptPolicy:  g.AnthropicClaudeOAuthSystemPromptPolicy(),
			Status:              g.Status,
			ExistingPriorities:  existing,
		})
	}

	response.Success(c, ProviderSettingsResponse{ProviderSettings: settings, Groups: views})
}

// UpdateSettings 整体保存供货商设置。校验在 service 层，不依赖前端。
// PUT /api/v1/admin/provider-settings
func (h *ProviderHandler) UpdateSettings(c *gin.Context) {
	var req service.ProviderSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.settingService.SaveProviderSettings(c.Request.Context(), req); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	settings, err := h.settingService.GetProviderSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// GetTierAffectedCount 预览某档位当前的账号数，供「应用到存量」二次确认。
// GET /api/v1/admin/provider-settings/tiers/:tier/affected-count
func (h *ProviderHandler) GetTierAffectedCount(c *gin.Context) {
	tier := c.Param("tier")
	if tier == "" {
		response.BadRequest(c, "tier is required")
		return
	}
	count, err := h.accountRepo.CountByProviderTier(c.Request.Context(), tier)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"tier": tier, "affected": count})
}

// ApplyTierToExisting 把某档位最新参数回填到该档现有账号。
// POST /api/v1/admin/provider-settings/tiers/:tier/apply-existing
//
// 只增量合并档位相关的 extra 键，persona_* 等其它持久设置原样保留。
func (h *ProviderHandler) ApplyTierToExisting(c *gin.Context) {
	tier := c.Param("tier")
	if tier == "" {
		response.BadRequest(c, "tier is required")
		return
	}
	if tier == service.ProviderTierCustom {
		response.ErrorFrom(c, infraerrors.BadRequest("CUSTOM_TIER_NOT_BACKFILLABLE",
			"custom tier accounts have per-account parameters and cannot be bulk updated"))
		return
	}

	ctx := c.Request.Context()
	settings, err := h.settingService.GetProviderSettings(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	resolved, err := service.ResolveProviderTier(settings, tier, nil)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := h.adminService.ApplyTierToExistingProviderAccounts(ctx, resolved)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
