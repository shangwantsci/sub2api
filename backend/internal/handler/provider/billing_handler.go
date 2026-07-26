package provider

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// 供号商对账。金额一律是 1 倍率标准价（usage_logs.total_cost），
// 与平台自定的分组倍率彻底解耦：管理员改分组倍率不会改变这里的任何数字。

// GetCurrentBilling 返回当前待结算金额与明细。
// GET /api/v1/provider/billing/current
func (h *Handler) GetCurrentBilling(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	period, err := h.settlementService.GetCurrentPeriod(c.Request.Context(), providerID, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, period)
}

// GetCurrentPeriodInfo 返回当前结算周期的元信息，不含任何金额聚合。
// GET /api/v1/provider/billing/period
//
// 账号列表等页面只需要「本期从哪天开始」这一个上下文，为此去跑整套用量聚合
// （GetCurrentBilling 还会算按日明细）太重。这里只读最近一条结算单。
func (h *Handler) GetCurrentPeriodInfo(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	window, err := h.settlementService.CurrentWindow(ctx, providerID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	tz := service.DefaultProviderSettlementTimezone
	if h.settingService != nil {
		tz = h.settingService.GetProviderSettlementTimezone(ctx)
	}
	response.Success(c, gin.H{
		"period_start":        window.Start,
		"settlement_timezone": tz,
	})
}

// ListSettlements 分页返回历史结算单。
// GET /api/v1/provider/billing/settlements?page=1&page_size=20
//
// 走标准分页响应（items/total/page/page_size/pages）。结算历史是财务凭证，
// 供号商必须能确认自己看到的就是全部，之前那种截断到前 N 条且不告知总数的做法
// 会让更早的结算单静默消失。
//
// 结算时区不在这里返回：它是页面级信息，由当期对账接口一并给出。
func (h *Handler) ListSettlements(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	records, result, err := h.settlementService.ListSettlements(
		c.Request.Context(), providerID,
		pagination.PaginationParams{Page: page, PageSize: pageSize},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, records, result.Total, page, pageSize)
}

// ExportCurrentBilling 导出当期分账号明细 CSV。
// GET /api/v1/provider/billing/export
//
// CSV 输出完整精度金额（不做 1 位小数格式化），供对账核算使用。
func (h *Handler) ExportCurrentBilling(c *gin.Context) {
	providerID, ok := currentProviderID(c)
	if !ok {
		return
	}
	period, err := h.settlementService.GetCurrentPeriod(c.Request.Context(), providerID, false)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	filename := fmt.Sprintf("provider-%d-pending-%s.csv", providerID, time.Now().Format("20060102"))
	writeBillingCSV(c, filename, period.PeriodStart, period.AsOf, period.Accounts)
}

// writeBillingCSV 输出分账号明细 CSV。
func writeBillingCSV(
	c *gin.Context,
	filename string,
	periodStart, periodEnd time.Time,
	accounts []service.ProviderAccountUsage,
) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Status(http.StatusOK)

	// BOM 让 Excel 正确识别 UTF-8 中文。
	_, _ = c.Writer.WriteString("\xEF\xBB\xBF")

	w := csv.NewWriter(c.Writer)
	defer w.Flush()

	_ = w.Write([]string{"period_start", periodStart.Format(time.RFC3339)})
	_ = w.Write([]string{"period_end", periodEnd.Format(time.RFC3339)})
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
			// 账号名由供号商自填，可能以 = + - @ 开头触发表格公式，必须转义。
			CSVCell(a.AccountName),
			offline,
			strconv.FormatInt(a.Requests, 10),
			strconv.FormatInt(a.Tokens, 10),
			// 完整精度，不四舍五入。
			CSVAmount(a.StandardCost),
		})
		total = total.Add(a.StandardCost)
	}
	_ = w.Write([]string{})
	_ = w.Write([]string{"total", "", "", "", "", CSVAmount(total)})
}
