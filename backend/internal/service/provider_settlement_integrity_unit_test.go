//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// 结算是真金白银的操作，这组测试覆盖四件必须成立的事：
//   1. 并发结算不会产生覆盖同一区间的两张单（否则重复付款）；
//   2. 迟到写入不会漏账，也不会被重复计入；
//   3. 封账终点会退回冷却期，不会把还没落定的用量算进来；
//   4. 明细快照与结算单同事务写入，历史凭证不依赖会被清理的活表。

// ---- 测试替身 ----

type fakeSettlementRepo struct {
	mu sync.Mutex

	settled []ProviderSettlement
	items   map[int64][]ProviderAccountUsage
	nextID  int64

	// lockMu 模拟 pg_advisory_xact_lock：真正在 fn 执行期间互斥。
	lockMu sync.Mutex
	// lockCalls 记录加锁次数。若 Settle 不再走 WithProviderSettlementLock，
	// 这个计数会掉到 0，测试随即失败。
	lockCalls int
	// lockWait 放大临界区，让「读起点」与「写结算单」之间的竞态窗口足够大。
	lockWait time.Duration
}

func newFakeSettlementRepo() *fakeSettlementRepo {
	return &fakeSettlementRepo{items: map[int64][]ProviderAccountUsage{}, nextID: 1}
}

func (f *fakeSettlementRepo) WithProviderSettlementLock(
	ctx context.Context, _ int64, fn func(ctx context.Context) error,
) error {
	f.lockMu.Lock()
	defer f.lockMu.Unlock()

	f.mu.Lock()
	f.lockCalls++
	f.mu.Unlock()

	if f.lockWait > 0 {
		time.Sleep(f.lockWait)
	}
	return fn(ctx)
}

func (f *fakeSettlementRepo) lockCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lockCalls
}

func (f *fakeSettlementRepo) LatestSettled(_ context.Context, providerUserID int64) (*ProviderSettlement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out *ProviderSettlement
	for i := range f.settled {
		s := f.settled[i]
		if s.ProviderUserID != providerUserID || s.Status != ProviderSettlementStatusSettled {
			continue
		}
		if out == nil || s.PeriodEnd.After(out.PeriodEnd) {
			cp := s
			out = &cp
		}
	}
	return out, nil
}

func (f *fakeSettlementRepo) LatestAny(_ context.Context, _ int64) (*ProviderSettlement, error) {
	return nil, nil
}

func (f *fakeSettlementRepo) Create(_ context.Context, s *ProviderSettlement, items []ProviderAccountUsage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s.ID = f.nextID
	f.nextID++
	f.settled = append(f.settled, *s)
	f.items[s.ID] = append([]ProviderAccountUsage(nil), items...)
	return nil
}

func (f *fakeSettlementRepo) GetByID(_ context.Context, id int64) (*ProviderSettlement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.settled {
		if f.settled[i].ID == id {
			cp := f.settled[i]
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeSettlementRepo) ListByProvider(
	_ context.Context, providerUserID int64, params pagination.PaginationParams,
) ([]ProviderSettlement, *pagination.PaginationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := make([]ProviderSettlement, 0, len(f.settled))
	for i := len(f.settled) - 1; i >= 0; i-- {
		if f.settled[i].ProviderUserID == providerUserID {
			all = append(all, f.settled[i])
		}
	}
	total := int64(len(all))
	start := params.Offset()
	if start > len(all) {
		start = len(all)
	}
	end := start + params.Limit()
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], paginationResultForTest(total, params), nil
}

func paginationResultForTest(total int64, params pagination.PaginationParams) *pagination.PaginationResult {
	pages := int(total) / params.Limit()
	if int(total)%params.Limit() > 0 {
		pages++
	}
	return &pagination.PaginationResult{
		Total: total, Page: params.Page, PageSize: params.Limit(), Pages: pages,
	}
}

func (f *fakeSettlementRepo) ListItems(_ context.Context, settlementID int64) ([]ProviderAccountUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ProviderAccountUsage(nil), f.items[settlementID]...), nil
}

func (f *fakeSettlementRepo) Void(_ context.Context, id int64, _ int64, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.settled {
		if f.settled[i].ID == id && f.settled[i].Status == ProviderSettlementStatusSettled {
			f.settled[i].Status = ProviderSettlementStatusVoided
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeSettlementRepo) LatestSettledBatch(context.Context, []int64) (map[int64]ProviderSettlement, error) {
	return map[int64]ProviderSettlement{}, nil
}

func (f *fakeSettlementRepo) EarliestUnsettledStart(context.Context) (time.Time, error) {
	return time.Time{}, nil
}

// fakeUsageReader 模拟 usage_logs：每行带 createdAt 与自增 id，
// 用来复现「created_at 早、提交晚」的迟到写入。
type fakeUsageReader struct {
	rows []fakeUsageRow
}

type fakeUsageRow struct {
	id        int64
	createdAt time.Time
	accountID int64
	cost      string
}

func (f *fakeUsageReader) matching(w ProviderSettlementWindow) []fakeUsageRow {
	out := make([]fakeUsageRow, 0, len(f.rows))
	for _, r := range f.rows {
		// 与 providerWindowClause 的语义保持一致。
		inPeriod := !r.createdAt.Before(w.Start) && r.createdAt.Before(w.End)
		isLate := r.createdAt.Before(w.Start) && r.id > w.LastUsageID
		if (inPeriod || isLate) && r.createdAt.Before(w.End) {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeUsageReader) GetProviderPeriodTotals(
	_ context.Context, _ int64, w ProviderSettlementWindow,
) (*ProviderPeriodTotals, error) {
	rows := f.matching(w)
	out := &ProviderPeriodTotals{StandardCost: decimal.Zero}
	accounts := map[int64]struct{}{}
	for _, r := range rows {
		out.Requests++
		out.StandardCost = out.StandardCost.Add(decimal.RequireFromString(r.cost))
		if r.id > out.MaxUsageID {
			out.MaxUsageID = r.id
		}
		if r.createdAt.Before(w.Start) {
			out.LateRows++
		}
		accounts[r.accountID] = struct{}{}
	}
	out.AccountCount = len(accounts)
	return out, nil
}

func (f *fakeUsageReader) GetProviderAccountBreakdown(
	_ context.Context, _ int64, w ProviderSettlementWindow,
) ([]ProviderAccountUsage, error) {
	byAccount := map[int64]*ProviderAccountUsage{}
	for _, r := range f.matching(w) {
		item, ok := byAccount[r.accountID]
		if !ok {
			item = &ProviderAccountUsage{AccountID: r.accountID, StandardCost: decimal.Zero}
			byAccount[r.accountID] = item
		}
		item.Requests++
		item.StandardCost = item.StandardCost.Add(decimal.RequireFromString(r.cost))
	}
	out := make([]ProviderAccountUsage, 0, len(byAccount))
	for _, v := range byAccount {
		out = append(out, *v)
	}
	return out, nil
}

func (f *fakeUsageReader) GetProviderDailyBreakdown(
	context.Context, int64, ProviderSettlementWindow, string,
) ([]ProviderDailyUsage, error) {
	return nil, nil
}

func (f *fakeUsageReader) GetProviderSingleAccountTotals(
	context.Context, int64, ProviderSettlementWindow,
) (*ProviderPeriodTotals, error) {
	return &ProviderPeriodTotals{StandardCost: decimal.Zero}, nil
}

func (f *fakeUsageReader) GetProviderAccountTotalsBatch(
	context.Context, int64, ProviderSettlementWindow,
) (map[int64]ProviderPeriodTotals, error) {
	return map[int64]ProviderPeriodTotals{}, nil
}

type fakeProviderUserReader struct{ createdAt time.Time }

func (f *fakeProviderUserReader) GetByID(_ context.Context, id int64) (*User, error) {
	return &User{ID: id, CreatedAt: f.createdAt, IsProvider: true}, nil
}

func (f *fakeProviderUserReader) ListProviders(context.Context) ([]User, error) { return nil, nil }

// ---- 测试 ----

func newTestSettlementService(
	repo *fakeSettlementRepo, usage *fakeUsageReader, userCreatedAt time.Time,
) *ProviderSettlementService {
	return NewProviderSettlementService(
		repo, usage, &fakeProviderUserReader{createdAt: userCreatedAt}, nil, nil,
	)
}

func TestSettleIsSerializedPerProvider(t *testing.T) {
	base := time.Now().Add(-24 * time.Hour)
	repo := newFakeSettlementRepo()
	repo.lockWait = 5 * time.Millisecond
	usage := &fakeUsageReader{rows: []fakeUsageRow{
		{id: 1, createdAt: base.Add(time.Hour), accountID: 1, cost: "1.5"},
		{id: 2, createdAt: base.Add(2 * time.Hour), accountID: 1, cost: "2.5"},
	}}
	svc := newTestSettlementService(repo, usage, base)

	// 两个管理员同时点结算。若「读起点 → 聚合 → 插入」不在同一把锁内，
	// 两次都会从同一起点聚合出 4.0，产生两张覆盖同一区间的单，等于付两次钱。
	end := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.Settle(context.Background(), 42, 1, "", end)
		}()
	}
	wg.Wait()

	require.Equal(t, 2, repo.lockCallCount(),
		"每次结算都必须经过 WithProviderSettlementLock")

	total := decimal.Zero
	for _, s := range repo.settled {
		total = total.Add(s.StandardCost)
	}
	require.Equal(t, "4", total.String(),
		"两次结算的金额合计必须等于实际用量；等于 8 说明同一区间被结算了两次")

	// 起点必须在锁内重新读取：第二次结算的区间应当从第一次的终点开始，
	// 而不是与第一次重叠。
	if len(repo.settled) == 2 {
		require.False(t, repo.settled[1].PeriodStart.Before(repo.settled[0].PeriodEnd),
			"第二张结算单的区间与第一张重叠，说明起点是在锁外读的")
	}
}

func TestSettleCapturesLateCommittedRows(t *testing.T) {
	base := time.Now().Add(-24 * time.Hour)
	repo := newFakeSettlementRepo()
	usage := &fakeUsageReader{rows: []fakeUsageRow{
		{id: 1, createdAt: base.Add(time.Hour), accountID: 1, cost: "1.0"},
	}}
	svc := newTestSettlementService(repo, usage, base)

	firstEnd := base.Add(2 * time.Hour)
	first, err := svc.Settle(context.Background(), 42, 1, "", firstEnd)
	require.NoError(t, err)
	require.Equal(t, "1", first.StandardCost.String())
	require.Equal(t, int64(1), first.LastUsageID, "水位应记录本期计入的最大 usage id")

	// 一条迟到行：created_at 落在已封区间内，但当时还没提交，所以 id 更大。
	// 只按 created_at 切区间的话，这笔钱既不在上期也进不了下期，直接漏账。
	usage.rows = append(usage.rows, fakeUsageRow{
		id: 2, createdAt: base.Add(90 * time.Minute), accountID: 1, cost: "0.5",
	})

	second, err := svc.Settle(context.Background(), 42, 1, "", base.Add(3*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "0.5", second.StandardCost.String(), "迟到行必须在下一期被补计")
	require.Equal(t, int64(2), second.LastUsageID)

	// 再结一次不应该把同一笔重复计入。
	third, err := svc.Settle(context.Background(), 42, 1, "", base.Add(4*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "0", third.StandardCost.String(), "已计入的行不能被重复捕获")
}

func TestSettleWatermarkNeverGoesBackwards(t *testing.T) {
	base := time.Now().Add(-24 * time.Hour)
	repo := newFakeSettlementRepo()
	usage := &fakeUsageReader{rows: []fakeUsageRow{
		{id: 7, createdAt: base.Add(time.Hour), accountID: 1, cost: "1.0"},
	}}
	svc := newTestSettlementService(repo, usage, base)

	first, err := svc.Settle(context.Background(), 42, 1, "", base.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(7), first.LastUsageID)

	// 空周期：一行未计。水位若被重置成 0，上期已计入的旧行会在下期被重复捕获。
	second, err := svc.Settle(context.Background(), 42, 1, "", base.Add(3*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(7), second.LastUsageID, "空周期必须沿用上期水位，不能退回 0")
	require.Equal(t, "0", second.StandardCost.String())
}

func TestSettlePersistsBreakdownSnapshot(t *testing.T) {
	base := time.Now().Add(-24 * time.Hour)
	repo := newFakeSettlementRepo()
	usage := &fakeUsageReader{rows: []fakeUsageRow{
		{id: 1, createdAt: base.Add(time.Hour), accountID: 11, cost: "1.25"},
		{id: 2, createdAt: base.Add(time.Hour), accountID: 22, cost: "2.75"},
	}}
	svc := newTestSettlementService(repo, usage, base)

	record, err := svc.Settle(context.Background(), 42, 1, "", base.Add(2*time.Hour))
	require.NoError(t, err)

	// 明细必须落盘：usage_logs 默认 90 天后被硬删，靠回查活表会让历史凭证残缺。
	items, err := svc.GetSettlementBreakdown(context.Background(), record)
	require.NoError(t, err)
	require.Len(t, items, 2)

	sum := decimal.Zero
	for _, it := range items {
		sum = sum.Add(it.StandardCost)
	}
	require.Equal(t, record.StandardCost.String(), sum.String(),
		"快照明细合计必须与结算单总额完全一致")
}

func TestSettleKeepsFullDecimalPrecision(t *testing.T) {
	base := time.Now().Add(-24 * time.Hour)
	repo := newFakeSettlementRepo()
	// 每笔都是 float64 无法精确表示的值，逐笔累加会暴露二进制浮点误差。
	rows := make([]fakeUsageRow, 0, 1000)
	for i := 0; i < 1000; i++ {
		rows = append(rows, fakeUsageRow{
			id: int64(i + 1), createdAt: base.Add(time.Hour), accountID: 1, cost: "0.0000000001",
		})
	}
	usage := &fakeUsageReader{rows: rows}
	svc := newTestSettlementService(repo, usage, base)

	record, err := svc.Settle(context.Background(), 42, 1, "", base.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "0.0000001", record.StandardCost.String())
}

// 结算历史是财务凭证，分页必须给出 total，且翻页不重不漏。
// 旧实现是「截断到前 50 条且不告知总数」，更早的结算单会静默消失。
func TestListSettlementsPaginates(t *testing.T) {
	base := time.Now().Add(-100 * time.Hour)
	repo := newFakeSettlementRepo()
	usage := &fakeUsageReader{}
	svc := newTestSettlementService(repo, usage, base)

	// 造 7 期结算单。
	for i := 0; i < 7; i++ {
		usage.rows = append(usage.rows, fakeUsageRow{
			id: int64(i + 1), createdAt: base.Add(time.Duration(i) * time.Hour), accountID: 1, cost: "1.0",
		})
		_, err := svc.Settle(context.Background(), 42, 1, "", base.Add(time.Duration(i+1)*time.Hour))
		require.NoError(t, err)
	}

	first, result, err := svc.ListSettlements(context.Background(), 42,
		pagination.PaginationParams{Page: 1, PageSize: 3})
	require.NoError(t, err)
	require.Len(t, first, 3)
	require.Equal(t, int64(7), result.Total, "必须给出总数，否则供号商无从判断是否被截断")
	require.Equal(t, 3, result.Pages)

	second, _, err := svc.ListSettlements(context.Background(), 42,
		pagination.PaginationParams{Page: 2, PageSize: 3})
	require.NoError(t, err)
	require.Len(t, second, 3)

	last, _, err := svc.ListSettlements(context.Background(), 42,
		pagination.PaginationParams{Page: 3, PageSize: 3})
	require.NoError(t, err)
	require.Len(t, last, 1, "末页应只剩 1 条")

	// 三页合起来不重不漏。
	seen := map[int64]bool{}
	for _, page := range [][]ProviderSettlement{first, second, last} {
		for _, s := range page {
			require.False(t, seen[s.ID], "结算单 %d 在多页中重复出现", s.ID)
			seen[s.ID] = true
		}
	}
	require.Len(t, seen, 7, "翻完所有页必须覆盖全部结算单")

	// 越界页返回空而不是报错。
	beyond, _, err := svc.ListSettlements(context.Background(), 42,
		pagination.PaginationParams{Page: 99, PageSize: 3})
	require.NoError(t, err)
	require.Empty(t, beyond)
}

func TestVoidRequiresReason(t *testing.T) {
	base := time.Now().Add(-24 * time.Hour)
	repo := newFakeSettlementRepo()
	usage := &fakeUsageReader{rows: []fakeUsageRow{
		{id: 1, createdAt: base.Add(time.Hour), accountID: 1, cost: "1.0"},
	}}
	svc := newTestSettlementService(repo, usage, base)

	record, err := svc.Settle(context.Background(), 42, 1, "", base.Add(2*time.Hour))
	require.NoError(t, err)

	// 作废是抹掉一笔已确认应付款的操作，必须留下原因。
	require.Error(t, svc.Void(context.Background(), record.ID, 1, "   "))
	require.NoError(t, svc.Void(context.Background(), record.ID, 1, "转账失败，重新结算"))
}

func TestVoidedPeriodReturnsAmountToPending(t *testing.T) {
	base := time.Now().Add(-24 * time.Hour)
	repo := newFakeSettlementRepo()
	usage := &fakeUsageReader{rows: []fakeUsageRow{
		{id: 1, createdAt: base.Add(time.Hour), accountID: 1, cost: "3.5"},
	}}
	svc := newTestSettlementService(repo, usage, base)

	record, err := svc.Settle(context.Background(), 42, 1, "", base.Add(2*time.Hour))
	require.NoError(t, err)
	require.NoError(t, svc.Void(context.Background(), record.ID, 1, "重新结算"))

	// 作废后周期起点回到上上期，金额重新变成待结算。
	period, err := svc.GetCurrentPeriod(context.Background(), 42, false)
	require.NoError(t, err)
	require.Equal(t, "3.5", period.Totals.StandardCost.String())
}
