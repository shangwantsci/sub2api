package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbsettlement "github.com/Wei-Shaw/sub2api/ent/providersettlement"
	dbsettlementitem "github.com/Wei-Shaw/sub2api/ent/providersettlementitem"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type providerSettlementRepository struct {
	client *dbent.Client
	sql    sqlExecutor
}

// NewProviderSettlementRepository 创建结算单仓储。
func NewProviderSettlementRepository(client *dbent.Client, sqlDB *sql.DB) service.ProviderSettlementRepository {
	return &providerSettlementRepository{client: client, sql: sqlDB}
}

func settlementEntityToService(m *dbent.ProviderSettlement) *service.ProviderSettlement {
	if m == nil {
		return nil
	}
	out := &service.ProviderSettlement{
		ID:             m.ID,
		ProviderUserID: m.ProviderUserID,
		PeriodStart:    m.PeriodStart,
		PeriodEnd:      m.PeriodEnd,
		StandardCost:   m.StandardCost,
		Requests:       m.Requests,
		Tokens:         m.Tokens,
		AccountCount:   m.AccountCount,
		LastUsageID:    m.LastUsageID,
		Status:         m.Status,
		SettledAt:      m.SettledAt,
		SettledBy:      m.SettledBy,
		VoidedAt:       m.VoidedAt,
		VoidedBy:       m.VoidedBy,
		CreatedAt:      m.CreatedAt,
	}
	if m.Notes != nil {
		out.Notes = *m.Notes
	}
	return out
}

// WithProviderSettlementLock 在一个事务内取得该供号商的排他 advisory lock 后执行 fn。
//
// 用 PostgreSQL 事务级 advisory lock（pg_advisory_xact_lock）而不是 Redis 锁：
// 结算是财务操作，需要与数据写入处于同一事务边界，锁随 COMMIT/ROLLBACK 自动释放，
// 不存在 TTL 过期或 fail-open 导致的双结算。复用仓库既有的 lockRepositoryScopedKeys。
//
// fn 内的所有仓储调用都会通过 context 复用同一事务。
func (r *providerSettlementRepository) WithProviderSettlementLock(
	ctx context.Context,
	providerUserID int64,
	fn func(ctx context.Context) error,
) error {
	// 已处于外层事务时直接复用，锁与业务仍在同一事务边界内。
	if dbent.TxFromContext(ctx) != nil {
		return r.lockAndRun(ctx, providerUserID, fn)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		if !errors.Is(err, dbent.ErrTxStarted) {
			return err
		}
		return r.lockAndRun(ctx, providerUserID, fn)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := r.lockAndRun(txCtx, providerUserID, fn); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *providerSettlementRepository) lockAndRun(
	ctx context.Context,
	providerUserID int64,
	fn func(ctx context.Context) error,
) error {
	client := clientFromContext(ctx, r.client)
	release, err := lockRepositoryScopedKeys(
		ctx,
		client,
		txAwareSQLExecutor(ctx, r.sql, r.client),
		fmt.Sprintf("provider_settlement:user:%d", providerUserID),
	)
	if err != nil {
		return err
	}
	defer release()
	return fn(ctx)
}

func (r *providerSettlementRepository) LatestSettled(
	ctx context.Context,
	providerUserID int64,
) (*service.ProviderSettlement, error) {
	m, err := clientFromContext(ctx, r.client).ProviderSettlement.Query().
		Where(
			dbsettlement.ProviderUserIDEQ(providerUserID),
			dbsettlement.StatusEQ(service.ProviderSettlementStatusSettled),
		).
		Order(dbent.Desc(dbsettlement.FieldPeriodEnd), dbent.Desc(dbsettlement.FieldID)).
		First(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return settlementEntityToService(m), nil
}

func (r *providerSettlementRepository) LatestAny(
	ctx context.Context,
	providerUserID int64,
) (*service.ProviderSettlement, error) {
	m, err := clientFromContext(ctx, r.client).ProviderSettlement.Query().
		Where(dbsettlement.ProviderUserIDEQ(providerUserID)).
		Order(dbent.Desc(dbsettlement.FieldID)).
		First(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return settlementEntityToService(m), nil
}

// Create 插入结算单并在同一事务内落盘分账号明细快照。
//
// 明细必须与结算单原子写入：只写总额不写明细，等于让历史凭证依赖会被清理的活表。
func (r *providerSettlementRepository) Create(
	ctx context.Context,
	s *service.ProviderSettlement,
	items []service.ProviderAccountUsage,
) error {
	if s == nil {
		return nil
	}
	client := clientFromContext(ctx, r.client)

	builder := client.ProviderSettlement.Create().
		SetProviderUserID(s.ProviderUserID).
		SetPeriodStart(s.PeriodStart).
		SetPeriodEnd(s.PeriodEnd).
		SetStandardCost(s.StandardCost).
		SetRequests(s.Requests).
		SetTokens(s.Tokens).
		SetAccountCount(s.AccountCount).
		SetLastUsageID(s.LastUsageID).
		SetStatus(s.Status).
		SetNillableSettledBy(s.SettledBy)
	if !s.SettledAt.IsZero() {
		builder.SetSettledAt(s.SettledAt)
	}
	if s.Notes != "" {
		builder.SetNotes(s.Notes)
	}
	created, err := builder.Save(ctx)
	if err != nil {
		return err
	}
	s.ID = created.ID
	s.CreatedAt = created.CreatedAt
	s.SettledAt = created.SettledAt

	if len(items) == 0 {
		return nil
	}
	bulk := make([]*dbent.ProviderSettlementItemCreate, 0, len(items))
	for i := range items {
		item := items[i]
		bulk = append(bulk, client.ProviderSettlementItem.Create().
			SetSettlementID(created.ID).
			SetAccountID(item.AccountID).
			SetAccountName(item.AccountName).
			SetOffline(item.Offline).
			SetRequests(item.Requests).
			SetTokens(item.Tokens).
			SetStandardCost(item.StandardCost))
	}
	if _, err := client.ProviderSettlementItem.CreateBulk(bulk...).Save(ctx); err != nil {
		return fmt.Errorf("persist settlement items: %w", err)
	}
	return nil
}

// ListItems 返回某期的分账号明细快照。
func (r *providerSettlementRepository) ListItems(
	ctx context.Context,
	settlementID int64,
) ([]service.ProviderAccountUsage, error) {
	rows, err := clientFromContext(ctx, r.client).ProviderSettlementItem.Query().
		Where(dbsettlementitem.SettlementIDEQ(settlementID)).
		Order(dbent.Desc(dbsettlementitem.FieldStandardCost)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.ProviderAccountUsage, 0, len(rows))
	for _, m := range rows {
		out = append(out, service.ProviderAccountUsage{
			AccountID:    m.AccountID,
			AccountName:  m.AccountName,
			Offline:      m.Offline,
			Requests:     m.Requests,
			Tokens:       m.Tokens,
			StandardCost: m.StandardCost,
		})
	}
	return out, nil
}

func (r *providerSettlementRepository) GetByID(ctx context.Context, id int64) (*service.ProviderSettlement, error) {
	m, err := clientFromContext(ctx, r.client).ProviderSettlement.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return settlementEntityToService(m), nil
}

// ListByProvider 分页返回某供号商的结算历史，按时间倒序。
//
// 必须返回 total：结算历史是财务凭证，供号商要能确认「这就是全部」，
// 而不是被静默截断到前 N 条。
func (r *providerSettlementRepository) ListByProvider(
	ctx context.Context,
	providerUserID int64,
	params pagination.PaginationParams,
) ([]service.ProviderSettlement, *pagination.PaginationResult, error) {
	q := clientFromContext(ctx, r.client).ProviderSettlement.Query().
		Where(dbsettlement.ProviderUserIDEQ(providerUserID))

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	rows, err := q.
		Order(dbent.Desc(dbsettlement.FieldID)).
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	out := make([]service.ProviderSettlement, 0, len(rows))
	for _, m := range rows {
		if converted := settlementEntityToService(m); converted != nil {
			out = append(out, *converted)
		}
	}
	return out, paginationResultFromTotal(int64(total), params), nil
}

// Void 把结算单置为已作废。
//
// 条件更新只命中仍是 settled 的行，受影响行数为 0 表示已被并发作废。
// 作废原因写入独立的 void_reason 语义位置（notes 保留原结算备注不动），
// 避免破坏原始审计信息。
func (r *providerSettlementRepository) Void(
	ctx context.Context,
	id int64,
	voidedBy int64,
	reason string,
) (bool, error) {
	client := clientFromContext(ctx, r.client)
	builder := client.ProviderSettlement.Update().
		Where(
			dbsettlement.IDEQ(id),
			dbsettlement.StatusEQ(service.ProviderSettlementStatusSettled),
		).
		SetStatus(service.ProviderSettlementStatusVoided).
		SetVoidedAt(time.Now()).
		SetVoidReason(reason)
	if voidedBy > 0 {
		builder.SetVoidedBy(voidedBy)
	}
	affected, err := builder.Save(ctx)
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *providerSettlementRepository) LatestSettledBatch(
	ctx context.Context,
	providerUserIDs []int64,
) (map[int64]service.ProviderSettlement, error) {
	out := make(map[int64]service.ProviderSettlement, len(providerUserIDs))
	if len(providerUserIDs) == 0 {
		return out, nil
	}
	rows, err := clientFromContext(ctx, r.client).ProviderSettlement.Query().
		Where(
			dbsettlement.ProviderUserIDIn(providerUserIDs...),
			dbsettlement.StatusEQ(service.ProviderSettlementStatusSettled),
		).
		Order(dbent.Asc(dbsettlement.FieldPeriodEnd), dbent.Asc(dbsettlement.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	// 升序遍历，后写的覆盖先写的，最终留下每个供号商 period_end 最大的那条。
	for _, m := range rows {
		if converted := settlementEntityToService(m); converted != nil {
			out[converted.ProviderUserID] = *converted
		}
	}
	return out, nil
}

// EarliestUnsettledStart 返回所有供号商中最早的未结算周期起点。
//
// 清理任务据此判断哪些 usage 行还不能删：早于该时刻的行都已被封账并快照，
// 删掉不影响任何待结算金额。没有供号商时返回零值，表示无需保护。
func (r *providerSettlementRepository) EarliestUnsettledStart(ctx context.Context) (time.Time, error) {
	client := clientFromContext(ctx, r.client)

	providers, err := client.User.Query().
		Where(dbuser.IsProviderEQ(true)).
		Select(dbuser.FieldID, dbuser.FieldCreatedAt).
		All(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if len(providers) == 0 {
		return time.Time{}, nil
	}

	ids := make([]int64, 0, len(providers))
	for _, u := range providers {
		ids = append(ids, u.ID)
	}
	settled, err := r.LatestSettledBatch(ctx, ids)
	if err != nil {
		return time.Time{}, err
	}

	var earliest time.Time
	for _, u := range providers {
		start := u.CreatedAt
		if latest, ok := settled[u.ID]; ok {
			start = latest.PeriodEnd
		}
		if earliest.IsZero() || start.Before(earliest) {
			earliest = start
		}
	}
	return earliest, nil
}
