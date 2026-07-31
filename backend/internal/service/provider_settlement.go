package service

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/shopspring/decimal"
)

// 供号商结算（封账）。
//
// 结算不删改 usage_logs 任何一行：每次结算只插入一条不可变的结算单，
// 「当前待结算」= 最近一条 status='settled' 的 period_end 之后产生的 total_cost。
// 效果与「清零重新累积」完全一致，但数据零损失、可回溯、误操作可作废。
//
// 之所以不做物理清零：usage_logs 是平台自身客户扣费与收入统计的同一张事实表，
// 同一账号既归属供号商（成本 total_cost）又服务平台客户（收入 actual_cost），
// 清掉供号商金额等于连带清掉平台自己的账，且争议时拿不出任何凭证。

const (
	// ProviderSettlementStatusSettled 已结算。
	ProviderSettlementStatusSettled = "settled"
	// ProviderSettlementStatusVoided 已作废。作废后该期金额回到待结算。
	ProviderSettlementStatusVoided = "voided"
)

// 金额一律用 shopspring/decimal，从 SQL 扫描到 JSON 全程十进制精确。
//
// decimal.Decimal 的 MarshalJSON 默认带引号，即序列化成 JSON 字符串而非数字。
// 这是刻意的：JSON 数字在 JS 侧会被解析成 float64，十进制精度当场丢失。
// 前端拿到字符串后用 BigInt 做精确求和，只在最终展示时收敛到 1 位小数。

// ProviderPeriodTotals 是一个周期内的 1 倍率合计。
type ProviderPeriodTotals struct {
	Requests     int64           `json:"requests"`
	Tokens       int64           `json:"tokens"`
	StandardCost decimal.Decimal `json:"standard_cost"`
	AccountCount int             `json:"account_count"`
	// MaxUsageID 是本次聚合覆盖到的最大 usage_logs.id，封账时作为水位记录下来。
	MaxUsageID int64 `json:"-"`
	// LateRows 是本次捕获到的上期漏网行数，非零说明写入延迟超过了冷却期，需要关注。
	LateRows int64 `json:"-"`
}

// ProviderDailyUsage 是按日聚合的 1 倍率明细。
type ProviderDailyUsage struct {
	Date         string          `json:"date"`
	Requests     int64           `json:"requests"`
	Tokens       int64           `json:"tokens"`
	StandardCost decimal.Decimal `json:"standard_cost"`
}

// ProviderAccountUsage 是分账号的 1 倍率明细。
// Offline 为 true 表示该账号已被供号商下线，其金额仍计入本期。
type ProviderAccountUsage struct {
	AccountID    int64           `json:"account_id"`
	AccountName  string          `json:"account_name"`
	Offline      bool            `json:"offline"`
	Requests     int64           `json:"requests"`
	Tokens       int64           `json:"tokens"`
	StandardCost decimal.Decimal `json:"standard_cost"`
	// MaxUsageID / LateRows 供汇总出周期合计用，不下发给供号商：
	// 水位与迟到行数是平台内部的结算机制。
	MaxUsageID int64 `json:"-"`
	LateRows   int64 `json:"-"`
}

// aggregateProviderTotals 从分账号明细汇总出周期合计。
//
// 刻意不再单独查一次总额。两次独立查询之间提交的行会进明细却不进金额，于是导出的
// 对账凭证比结算单金额高；更糟的是那些行下期还会因为 id > 水位被再捕获一次，
// 等于同一笔付两次。管理端面板的一致性校验也会因此把结算按钮永久禁用。
// 让两者同源，这类偏差就不存在了。
func aggregateProviderTotals(items []ProviderAccountUsage) ProviderPeriodTotals {
	out := ProviderPeriodTotals{StandardCost: decimal.Zero}
	for i := range items {
		item := items[i]
		out.Requests += item.Requests
		out.Tokens += item.Tokens
		out.StandardCost = out.StandardCost.Add(item.StandardCost)
		out.LateRows += item.LateRows
		if item.MaxUsageID > out.MaxUsageID {
			out.MaxUsageID = item.MaxUsageID
		}
		if item.Requests > 0 {
			out.AccountCount++
		}
	}
	return out
}

// ProviderSettlement 是一条结算单。
type ProviderSettlement struct {
	ID             int64           `json:"id"`
	ProviderUserID int64           `json:"provider_user_id"`
	PeriodStart    time.Time       `json:"period_start"`
	PeriodEnd      time.Time       `json:"period_end"`
	StandardCost   decimal.Decimal `json:"standard_cost"`
	Requests       int64           `json:"requests"`
	Tokens         int64           `json:"tokens"`
	AccountCount   int             `json:"account_count"`
	// LastUsageID 是本期计入的最大 usage_logs.id，下一期据此捕获迟到行。
	LastUsageID int64      `json:"-"`
	Status      string     `json:"status"`
	SettledAt   time.Time  `json:"settled_at"`
	SettledBy   *int64     `json:"settled_by,omitempty"`
	VoidedAt    *time.Time `json:"voided_at,omitempty"`
	VoidedBy    *int64     `json:"voided_by,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ProviderSettlementWindow 描述一次聚合要覆盖的范围。
//
// Start/End 是 created_at 的半开区间；LastUsageID 是上一期的水位，
// 用于额外捕获「created_at 早于 Start 但当时尚未提交」的迟到行。
type ProviderSettlementWindow struct {
	Start       time.Time
	End         time.Time
	LastUsageID int64
}

// ProviderSettlementRepository 是结算单的持久化端口。
type ProviderSettlementRepository interface {
	// LatestSettled 返回最近一条已结算记录；无记录时返回 (nil, nil)。
	LatestSettled(ctx context.Context, providerUserID int64) (*ProviderSettlement, error)
	// LatestAny 返回最近一条记录（含已作废）；无记录时返回 (nil, nil)。
	LatestAny(ctx context.Context, providerUserID int64) (*ProviderSettlement, error)
	// Create 插入结算单，并在同一事务内落盘分账号明细快照。
	Create(ctx context.Context, s *ProviderSettlement, items []ProviderAccountUsage) error
	GetByID(ctx context.Context, id int64) (*ProviderSettlement, error)
	// ListByProvider 分页返回结算历史，同时给出 total —— 结算历史是财务凭证，
	// 供号商要能确认看到的就是全部，而不是被静默截断。
	ListByProvider(ctx context.Context, providerUserID int64, params pagination.PaginationParams) ([]ProviderSettlement, *pagination.PaginationResult, error)
	// ListItems 返回某期的分账号明细快照。
	ListItems(ctx context.Context, settlementID int64) ([]ProviderAccountUsage, error)
	// Void 把指定结算单置为已作废；已经作废时返回 false。
	Void(ctx context.Context, id int64, voidedBy int64, reason string) (bool, error)
	// LatestSettledBatch 批量取多个供号商的最近结算记录，避免列表页 N+1。
	LatestSettledBatch(ctx context.Context, providerUserIDs []int64) (map[int64]ProviderSettlement, error)
	// WithProviderSettlementLock 在事务内取得该供号商的排他锁后执行 fn。
	// 用 PostgreSQL 事务级 advisory lock，锁随事务提交/回滚自动释放。
	WithProviderSettlementLock(ctx context.Context, providerUserID int64, fn func(ctx context.Context) error) error
	// EarliestUnsettledStart 返回所有供号商中最早的未结算周期起点，
	// 供清理任务判断哪些 usage 行还不能删。无供号商时返回零值。
	EarliestUnsettledStart(ctx context.Context) (time.Time, error)
}

// ProviderUsageReader 是对账聚合的只读端口。实现见 usage_log_repo_provider.go。
type ProviderUsageReader interface {
	GetProviderPeriodTotals(ctx context.Context, providerUserID int64, w ProviderSettlementWindow) (*ProviderPeriodTotals, error)
	GetProviderDailyBreakdown(ctx context.Context, providerUserID int64, w ProviderSettlementWindow, timezone string) ([]ProviderDailyUsage, error)
	GetProviderAccountBreakdown(ctx context.Context, providerUserID int64, w ProviderSettlementWindow) ([]ProviderAccountUsage, error)
	GetProviderSingleAccountTotals(ctx context.Context, accountID int64, w ProviderSettlementWindow) (*ProviderPeriodTotals, error)
	GetProviderAccountTotalsBatch(ctx context.Context, providerUserID int64, w ProviderSettlementWindow) (map[int64]ProviderPeriodTotals, error)
}

// ProviderUserReader 读取供号商用户，用于列表与周期起点回落。
type ProviderUserReader interface {
	GetByID(ctx context.Context, id int64) (*User, error)
	ListProviders(ctx context.Context) ([]User, error)
}

// ProviderSettlementService 提供待结算计算与封账操作。
type ProviderSettlementService struct {
	settlements ProviderSettlementRepository
	usage       ProviderUsageReader
	users       ProviderUserReader
	accounts    AccountRepository
	settings    *SettingService
}

func NewProviderSettlementService(
	settlements ProviderSettlementRepository,
	usage ProviderUsageReader,
	users ProviderUserReader,
	accounts AccountRepository,
	settings *SettingService,
) *ProviderSettlementService {
	return &ProviderSettlementService{
		settlements: settlements,
		usage:       usage,
		users:       users,
		accounts:    accounts,
		settings:    settings,
	}
}

// ProviderCurrentPeriod 是「当前待结算」视图。
type ProviderCurrentPeriod struct {
	ProviderUserID int64                  `json:"provider_user_id"`
	PeriodStart    time.Time              `json:"period_start"`
	AsOf           time.Time              `json:"as_of"`
	Totals         ProviderPeriodTotals   `json:"totals"`
	Accounts       []ProviderAccountUsage `json:"accounts"`
	Daily          []ProviderDailyUsage   `json:"daily"`
	Timezone       string                 `json:"settlement_timezone"`
}

// CurrentPeriodStart 计算当前结算周期的起点。
//
// 取最近一条 status='settled' 的 period_end；没有则回落供号商注册时间。
// 作废最近一期后，这个查询自然会取到上上期的 period_end，金额随之回到待结算。
func (s *ProviderSettlementService) CurrentPeriodStart(ctx context.Context, providerUserID int64) (time.Time, error) {
	w, err := s.currentWindowStart(ctx, providerUserID)
	if err != nil {
		return time.Time{}, err
	}
	return w.Start, nil
}

// CurrentWindow 返回「从当前周期起点到此刻」的查询窗口。
//
// 供账号列表等只读视图使用：终点取当前时刻而非可封账时刻，
// 供号商应当能立刻看到刚产生的用量。
func (s *ProviderSettlementService) CurrentWindow(
	ctx context.Context,
	providerUserID int64,
) (ProviderSettlementWindow, error) {
	base, err := s.currentWindowStart(ctx, providerUserID)
	if err != nil {
		return ProviderSettlementWindow{}, err
	}
	base.End = time.Now()
	return base, nil
}

// currentWindowStart 返回当前周期的起点与上期水位。
func (s *ProviderSettlementService) currentWindowStart(
	ctx context.Context,
	providerUserID int64,
) (ProviderSettlementWindow, error) {
	latest, err := s.settlements.LatestSettled(ctx, providerUserID)
	if err != nil {
		return ProviderSettlementWindow{}, err
	}
	if latest != nil {
		return ProviderSettlementWindow{Start: latest.PeriodEnd, LastUsageID: latest.LastUsageID}, nil
	}
	user, err := s.users.GetByID(ctx, providerUserID)
	if err != nil {
		return ProviderSettlementWindow{}, err
	}
	if user == nil {
		return ProviderSettlementWindow{}, infraerrors.NotFound("PROVIDER_NOT_FOUND", "provider not found")
	}
	// 首期没有上期水位；供号商注册之前不可能有归属于他的用量。
	return ProviderSettlementWindow{Start: user.CreatedAt, LastUsageID: 0}, nil
}

// settleableEnd 返回「此刻可以安全封账到哪里」。
//
// usage_logs 由异步 worker 写入，created_at 是 worker 赋值而非 COMMIT 时刻，
// 因此刚过去的一小段时间内还可能有记录尚未提交。封账终点往回退一个冷却期，
// 让这段窗口内的写入先落定，避免刚封完账就出现漏网行。
//
// 冷却期只是把漏网概率压到极低，不是证明；真正兜底的是 last_usage_id 水位。
func (s *ProviderSettlementService) settleableEnd(ctx context.Context, now time.Time) time.Time {
	cooldown := DefaultProviderSettlementCooldown
	if s.settings != nil {
		cooldown = s.settings.GetProviderSettlementCooldown(ctx)
	}
	return now.Add(-cooldown)
}

// GetCurrentPeriod 返回当前待结算金额与明细。
//
// 这是「看」的视图，终点取当前时刻而不是可封账时刻：供号商应当能看到刚刚产生的用量，
// 否则会疑惑为什么调用了却不计数。真正封账时才退回冷却期，见 settleableEnd。
func (s *ProviderSettlementService) GetCurrentPeriod(
	ctx context.Context,
	providerUserID int64,
	includeDaily bool,
) (*ProviderCurrentPeriod, error) {
	base, err := s.currentWindowStart(ctx, providerUserID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	window := ProviderSettlementWindow{Start: base.Start, End: now, LastUsageID: base.LastUsageID}

	tz := DefaultProviderSettlementTimezone
	if s.settings != nil {
		tz = s.settings.GetProviderSettlementTimezone(ctx)
	}

	// 本期金额从分账号明细汇总，两者同源。分开查在活跃供号商身上必然对不上——
	// 两次查询之间还在产生新请求——管理端面板的一致性校验会因此把结算按钮禁用。
	accounts, err := s.usage.GetProviderAccountBreakdown(ctx, providerUserID, window)
	if err != nil {
		return nil, err
	}
	totals := aggregateProviderTotals(accounts)

	out := &ProviderCurrentPeriod{
		ProviderUserID: providerUserID,
		PeriodStart:    base.Start,
		AsOf:           now,
		Totals:         totals,
		Accounts:       accounts,
		Timezone:       tz,
	}
	if includeDaily {
		daily, dErr := s.usage.GetProviderDailyBreakdown(ctx, providerUserID, window, tz)
		if dErr != nil {
			return nil, dErr
		}
		out.Daily = daily
	}
	return out, nil
}

// Settle 对一个供号商封账。
//
// 全程在一个事务内、持有该供号商的 advisory lock 后完成「取起点 → 聚合 → 插入」。
// 不加锁的话两个管理员同时点结算会各自从同一起点聚合，产生两张覆盖同一区间的结算单，
// 直接导致重复付款。
//
// periodEnd 由调用方传入，批量结算时多个供号商共用同一个时间戳，保证一次月结的
// 所有结算单周期对齐。零值表示用「当前时刻减去冷却期」。
func (s *ProviderSettlementService) Settle(
	ctx context.Context,
	providerUserID int64,
	settledBy int64,
	notes string,
	periodEnd time.Time,
) (*ProviderSettlement, error) {
	if periodEnd.IsZero() {
		periodEnd = s.settleableEnd(ctx, time.Now())
	}

	var record *ProviderSettlement
	err := s.settlements.WithProviderSettlementLock(ctx, providerUserID, func(ctx context.Context) error {
		// 起点必须在锁内重新读取：锁外读到的可能已被并发结算推进。
		base, err := s.currentWindowStart(ctx, providerUserID)
		if err != nil {
			return err
		}
		if !periodEnd.After(base.Start) {
			return infraerrors.BadRequest("EMPTY_SETTLEMENT_PERIOD",
				"settlement period end must be after the period start; the cooldown window may not have elapsed yet")
		}

		window := ProviderSettlementWindow{
			Start:       base.Start,
			End:         periodEnd,
			LastUsageID: base.LastUsageID,
		}
		// 结算金额与明细快照必须同源。分开查的话，两次查询之间提交的行会进快照却不进
		// 金额：导出的对账凭证比结算单高，管理员按明细付款就多付了；更糟的是那些行
		// 下期还会因为 id > 水位被再捕获一次，同一笔付两次。
		items, err := s.usage.GetProviderAccountBreakdown(ctx, providerUserID, window)
		if err != nil {
			return err
		}
		totals := aggregateProviderTotals(items)

		if totals.LateRows > 0 {
			// 迟到行说明写入延迟超过了冷却期。金额没有丢（本期已补计），
			// 但冷却期可能设得太短，值得关注。
			slog.Warn("provider settlement caught late-committed usage rows",
				"provider_user_id", providerUserID,
				"late_rows", totals.LateRows,
				"previous_watermark", base.LastUsageID)
		}

		// 水位只能前进，不能后退：本期若一行未计，沿用上期水位，
		// 否则会让已计入的旧行在下期被重复捕获。
		watermark := base.LastUsageID
		if totals.MaxUsageID > watermark {
			watermark = totals.MaxUsageID
		}

		record = &ProviderSettlement{
			ProviderUserID: providerUserID,
			PeriodStart:    base.Start,
			PeriodEnd:      periodEnd,
			StandardCost:   totals.StandardCost,
			Requests:       totals.Requests,
			Tokens:         totals.Tokens,
			AccountCount:   totals.AccountCount,
			LastUsageID:    watermark,
			Status:         ProviderSettlementStatusSettled,
			SettledAt:      time.Now(),
			Notes:          strings.TrimSpace(notes),
		}
		if settledBy > 0 {
			record.SettledBy = &settledBy
		}
		// 明细与结算单同事务落盘，历史凭证不再依赖 usage_logs 活表。
		return s.settlements.Create(ctx, record, items)
	})
	if err != nil {
		return nil, err
	}
	return record, nil
}

// ProviderBatchSettleResult 是批量结算的结果。
type ProviderBatchSettleResult struct {
	PeriodEnd    time.Time            `json:"period_end"`
	Settled      []ProviderSettlement `json:"settled"`
	Skipped      []int64              `json:"skipped"`
	Failed       map[int64]string     `json:"failed,omitempty"`
	TotalCost    decimal.Decimal      `json:"total_cost"`
	TotalCount   int                  `json:"total_count"`
	SettlementTZ string               `json:"settlement_timezone"`
}

// periodIsEmpty 报告某供号商到 periodEnd 为止是否完全没有可结算的用量。
func (s *ProviderSettlementService) periodIsEmpty(
	ctx context.Context,
	providerUserID int64,
	periodEnd time.Time,
) (bool, error) {
	base, err := s.currentWindowStart(ctx, providerUserID)
	if err != nil {
		return false, err
	}
	if !periodEnd.After(base.Start) {
		return true, nil
	}
	totals, err := s.usage.GetProviderPeriodTotals(ctx, providerUserID, ProviderSettlementWindow{
		Start:       base.Start,
		End:         periodEnd,
		LastUsageID: base.LastUsageID,
	})
	if err != nil {
		return false, err
	}
	return totals.Requests == 0 && totals.StandardCost.IsZero(), nil
}

// SettleBatch 批量封账。共用同一个 periodEnd，让一次月结的周期整齐对齐。
//
// 空周期（无请求且金额为 0）直接跳过而不是「先插入再作废」：后者会在账本里留下
// 一串没有意义的作废单，还会干扰「只能作废最近一期」的判定。
func (s *ProviderSettlementService) SettleBatch(
	ctx context.Context,
	providerUserIDs []int64,
	settledBy int64,
	notes string,
) (*ProviderBatchSettleResult, error) {
	periodEnd := s.settleableEnd(ctx, time.Now())
	out := &ProviderBatchSettleResult{
		PeriodEnd: periodEnd,
		Settled:   make([]ProviderSettlement, 0, len(providerUserIDs)),
		Skipped:   make([]int64, 0),
		Failed:    make(map[int64]string),
		TotalCost: decimal.Zero,
	}
	if s.settings != nil {
		out.SettlementTZ = s.settings.GetProviderSettlementTimezone(ctx)
	}

	for _, id := range providerUserIDs {
		// 先按各自的真实周期起点预筛，避免为完全没有用量的供号商产生空单。
		// 这里只是预筛；Settle 内部仍会在锁里重新读起点并重新聚合。
		if empty, checkErr := s.periodIsEmpty(ctx, id, periodEnd); checkErr == nil && empty {
			out.Skipped = append(out.Skipped, id)
			continue
		}

		record, err := s.Settle(ctx, id, settledBy, notes, periodEnd)
		if err != nil {
			out.Failed[id] = err.Error()
			continue
		}
		if record.Requests == 0 && record.StandardCost.IsZero() {
			out.Skipped = append(out.Skipped, id)
			continue
		}
		out.Settled = append(out.Settled, *record)
		out.TotalCost = out.TotalCost.Add(record.StandardCost)
	}
	out.TotalCount = len(out.Settled)
	return out, nil
}

// Void 作废一条结算单，金额回到待结算。
//
// 只允许作废最近一条已结算记录：周期是链式的（下一期起点 = 上一期终点），
// 作废中间某期会让周期链断裂并造成金额重复或遗漏。
func (s *ProviderSettlementService) Void(
	ctx context.Context,
	settlementID int64,
	voidedBy int64,
	reason string,
) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return infraerrors.BadRequest("VOID_REASON_REQUIRED", "a reason is required to void a settlement")
	}

	record, err := s.settlements.GetByID(ctx, settlementID)
	if err != nil {
		return err
	}
	if record == nil {
		return infraerrors.NotFound("SETTLEMENT_NOT_FOUND", "settlement not found")
	}

	// 与 Settle 共用同一把锁：否则「作废最近一期」与「结算新一期」交错时，
	// 可能作废掉一个已经不是最新的单，把周期链断开。
	return s.settlements.WithProviderSettlementLock(ctx, record.ProviderUserID, func(ctx context.Context) error {
		// 锁内重新读取状态，锁外读到的可能已过期。
		current, err := s.settlements.GetByID(ctx, settlementID)
		if err != nil {
			return err
		}
		if current == nil {
			return infraerrors.NotFound("SETTLEMENT_NOT_FOUND", "settlement not found")
		}
		if current.Status == ProviderSettlementStatusVoided {
			return infraerrors.BadRequest("SETTLEMENT_ALREADY_VOIDED", "settlement is already voided")
		}
		latest, err := s.settlements.LatestSettled(ctx, current.ProviderUserID)
		if err != nil {
			return err
		}
		if latest == nil || latest.ID != current.ID {
			return infraerrors.BadRequest("SETTLEMENT_NOT_LATEST",
				"only the most recent settlement can be voided, otherwise the period chain would break")
		}
		ok, err := s.settlements.Void(ctx, settlementID, voidedBy, reason)
		if err != nil {
			return err
		}
		if !ok {
			return infraerrors.BadRequest("SETTLEMENT_ALREADY_VOIDED", "settlement is already voided")
		}
		return nil
	})
}

// ListSettlements 返回结算历史（含已作废）。
func (s *ProviderSettlementService) ListSettlements(
	ctx context.Context,
	providerUserID int64,
	params pagination.PaginationParams,
) ([]ProviderSettlement, *pagination.PaginationResult, error) {
	return s.settlements.ListByProvider(ctx, providerUserID, params)
}

// GetSettlement 取单条结算单。
func (s *ProviderSettlementService) GetSettlement(ctx context.Context, id int64) (*ProviderSettlement, error) {
	record, err := s.settlements.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, infraerrors.NotFound("SETTLEMENT_NOT_FOUND", "settlement not found")
	}
	return record, nil
}

// GetSettlementBreakdown 返回某一期的分账号明细，供导出对账凭证。
//
// 读的是封账时落盘的快照，不再回查 usage_logs：活表默认 90 天后被保留策略硬删，
// 靠重算会让历史凭证残缺，而结算单上的总额还在，两者对不上无法解释。
func (s *ProviderSettlementService) GetSettlementBreakdown(
	ctx context.Context,
	settlement *ProviderSettlement,
) ([]ProviderAccountUsage, error) {
	if settlement == nil {
		return nil, infraerrors.NotFound("SETTLEMENT_NOT_FOUND", "settlement not found")
	}
	return s.settlements.ListItems(ctx, settlement.ID)
}

// ProviderSummary 是管理端对账列表的一行。
type ProviderSummary struct {
	UserID           int64            `json:"user_id"`
	Email            string           `json:"email"`
	Username         string           `json:"username"`
	Status           string           `json:"status"`
	CreatedAt        time.Time        `json:"created_at"`
	AccountsTotal    int              `json:"accounts_total"`
	AccountsActive   int              `json:"accounts_active"`
	AccountsPaused   int              `json:"accounts_paused"`
	PeriodStart      time.Time        `json:"period_start"`
	PendingRequests  int64            `json:"pending_requests"`
	PendingTokens    int64            `json:"pending_tokens"`
	PendingCost      decimal.Decimal  `json:"pending_cost"`
	LastSettledAt    *time.Time       `json:"last_settled_at,omitempty"`
	LastSettledCost  *decimal.Decimal `json:"last_settled_cost,omitempty"`
	LastSettlementID *int64           `json:"last_settlement_id,omitempty"`
}

// ProviderSummaryList 是管理端对账列表及其汇总。
type ProviderSummaryList struct {
	Providers        []ProviderSummary `json:"providers"`
	TotalProviders   int               `json:"total_providers"`
	PendingProviders int               `json:"pending_providers"`
	TotalPendingCost decimal.Decimal   `json:"total_pending_cost"`
	Timezone         string            `json:"settlement_timezone"`
}

// ListProviderSummaries 汇总所有供号商的待结算状态。
func (s *ProviderSettlementService) ListProviderSummaries(ctx context.Context) (*ProviderSummaryList, error) {
	users, err := s.users.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := &ProviderSummaryList{
		Providers:        make([]ProviderSummary, 0, len(users)),
		TotalPendingCost: decimal.Zero,
	}
	if s.settings != nil {
		out.Timezone = s.settings.GetProviderSettlementTimezone(ctx)
	}

	ids := make([]int64, 0, len(users))
	for i := range users {
		ids = append(ids, users[i].ID)
	}
	latestByProvider, err := s.settlements.LatestSettledBatch(ctx, ids)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	for i := range users {
		u := users[i]
		row := ProviderSummary{
			UserID:    u.ID,
			Email:     u.Email,
			Username:  u.Username,
			Status:    u.Status,
			CreatedAt: u.CreatedAt,
		}

		start := u.CreatedAt
		watermark := int64(0)
		if latest, ok := latestByProvider[u.ID]; ok {
			start = latest.PeriodEnd
			watermark = latest.LastUsageID
			settledAt := latest.SettledAt
			cost := latest.StandardCost
			id := latest.ID
			row.LastSettledAt = &settledAt
			row.LastSettledCost = &cost
			row.LastSettlementID = &id
		}
		row.PeriodStart = start

		totals, tErr := s.usage.GetProviderPeriodTotals(ctx, u.ID, ProviderSettlementWindow{
			Start:       start,
			End:         now,
			LastUsageID: watermark,
		})
		if tErr != nil {
			return nil, tErr
		}
		row.PendingRequests = totals.Requests
		row.PendingTokens = totals.Tokens
		row.PendingCost = totals.StandardCost

		accounts, aErr := s.accounts.ListByProvider(ctx, u.ID)
		if aErr == nil {
			row.AccountsTotal = len(accounts)
			for j := range accounts {
				if accounts[j].Schedulable && accounts[j].Status == StatusActive {
					row.AccountsActive++
				} else {
					row.AccountsPaused++
				}
			}
		}

		if row.PendingCost.IsPositive() || row.PendingRequests > 0 {
			out.PendingProviders++
		}
		out.TotalPendingCost = out.TotalPendingCost.Add(row.PendingCost)
		out.Providers = append(out.Providers, row)
	}

	sort.SliceStable(out.Providers, func(i, j int) bool {
		return out.Providers[i].PendingCost.GreaterThan(out.Providers[j].PendingCost)
	})
	out.TotalProviders = len(out.Providers)
	return out, nil
}
