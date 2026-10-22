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

// resolveTierForApply 校验 :tier 并解析成模板参数；失败时已写好响应并返回 false。
//
// 预览与应用共用这一套校验，两者对「哪个档位可以回填」的判断因此完全一致：
// custom 档的账号各有各的参数，不能批量回填；其余档位必须是存在且对供货商开放的模板。
func (h *ProviderHandler) resolveTierForApply(c *gin.Context) (service.ResolvedProviderTier, bool) {
	tier := c.Param("tier")
	if tier == "" {
		response.BadRequest(c, "tier is required")
		return service.ResolvedProviderTier{}, false
	}
	if tier == service.ProviderTierCustom {
		response.ErrorFrom(c, infraerrors.BadRequest("CUSTOM_TIER_NOT_BACKFILLABLE",
			"custom tier accounts have per-account parameters and cannot be bulk updated"))
		return service.ResolvedProviderTier{}, false
	}

	settings, err := h.settingService.GetProviderSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return service.ResolvedProviderTier{}, false
	}
	resolved, err := service.ResolveProviderTier(settings, tier, nil)
	if err != nil {
		response.ErrorFrom(c, err)
		return service.ResolvedProviderTier{}, false
	}
	return resolved, true
}

// PreviewTierApplication 预览「应用到存量」：该档位有多少账号、其中多少会被改动、改动了哪几项。
// GET /api/v1/admin/provider-settings/tiers/:tier/apply-preview
//
// 只读，不写任何数据。响应 data：
//
//	{"tier":"3","matched":12,"will_change":5,"samples":[{"account_id":101,"name":"a@x.com",
//	  "changes":[{"field":"concurrency","from":10,"to":3},{"field":"load_factor","from":10,"to":3}]}]}
//
// changes[].field 取值固定为 concurrency / max_sessions / base_rpm / window_cost_limit /
// adaptive_throttle / load_factor，并按这个顺序出现。samples 只含将改动的账号，最多 20 个，
// 按账号 id 升序。管理员确认时把 will_change 原样带回 apply-existing 的 expected_changed。
func (h *ProviderHandler) PreviewTierApplication(c *gin.Context) {
	resolved, ok := h.resolveTierForApply(c)
	if !ok {
		return
	}
	preview, err := h.adminService.PreviewTierApplication(c.Request.Context(), resolved)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, preview)
}

// ApplyTierRequest 是「应用到存量」的请求体。
type ApplyTierRequest struct {
	// ExpectedChanged 是管理员在预览里确认过的将改动账号数（apply-preview 的 will_change）。
	// 必填；用指针区分「没带」与「带了 0」。
	ExpectedChanged *int `json:"expected_changed"`
}

// ApplyTierToExisting 把某档位最新参数回填到该档中参数与模板不同的账号。
// POST /api/v1/admin/provider-settings/tiers/:tier/apply-existing
//
// 请求体 {"expected_changed": 5} 必填（缺失、格式不对或为负 → 400 EXPECTED_CHANGED_REQUIRED）。
// 服务端重新计算将改动的账号数，与 expected_changed 不相等 → 409 PRESET_APPLY_CONFLICT 且不写任何数据；
// 相等才执行，且只写将改动的账号。响应 {"matched":12,"updated":5}。
//
// 走 BulkUpdate 的增量写，只改并发、load_factor（写成档位并发）两列与 max_sessions / base_rpm /
// window_cost_limit / adaptive_throttle 四个 extra 键，其余持久设置（nurturing_enabled 等）原样保留。
func (h *ProviderHandler) ApplyTierToExisting(c *gin.Context) {
	var req ApplyTierRequest
	// 空请求体（旧版前端不带 body）与格式错误一样，都等于没有给出确认过的数字，不能放行。
	if err := c.ShouldBindJSON(&req); err != nil || req.ExpectedChanged == nil || *req.ExpectedChanged < 0 {
		response.ErrorFrom(c, infraerrors.BadRequest("EXPECTED_CHANGED_REQUIRED",
			"expected_changed is required and must be a non-negative integer; use will_change from apply-preview"))
		return
	}

	resolved, ok := h.resolveTierForApply(c)
	if !ok {
		return
	}
	result, err := h.adminService.ApplyTierToExistingProviderAccounts(c.Request.Context(), resolved, *req.ExpectedChanged)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// GetCapacityPresets 返回容量档位模板表（设置缺失时返回种子值）。
// GET /api/v1/admin/settings/capacity-presets
func (h *ProviderHandler) GetCapacityPresets(c *gin.Context) {
	presets, err := h.settingService.GetCapacityPresets(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, presets)
}

// UpdateCapacityPresets 整体保存容量档位模板表。校验在 service 层，不依赖前端。
// PUT /api/v1/admin/settings/capacity-presets
//
// 已存在的 key 若仍有供货商账号的 provider_tier 记着它，不能被改名或删除。
// 改模板只影响之后套用的账号，存量账号要用「应用到存量」或批量编辑才会变。
func (h *ProviderHandler) UpdateCapacityPresets(c *gin.Context) {
	var req service.CapacityPresets
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	saved, err := h.settingService.SaveCapacityPresets(c.Request.Context(), req, h.accountRepo)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, saved)
}
