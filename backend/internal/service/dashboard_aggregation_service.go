package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
)

const (
	defaultDashboardAggregationTimeout         = 2 * time.Minute
	defaultDashboardAggregationBackfillTimeout = 30 * time.Minute
	dashboardAggregationRetentionInterval      = 6 * time.Hour

	// dashboardAggregationLeaderLockKey gates the periodic scheduled aggregation so
	// that only one instance runs it per cycle in a multi-replica deployment.
	dashboardAggregationLeaderLockKey = "dashboard:aggregation:leader"
	// dashboardAggregationLeaderLockTTL must exceed the job's worst-case runtime
	// (defaultDashboardAggregationTimeout) so the lock never expires mid-run.
	dashboardAggregationLeaderLockTTL = 5 * time.Minute
)

var (
	// ErrDashboardBackfillDisabled 当配置禁用回填时返回。
	ErrDashboardBackfillDisabled = errors.New("仪表盘聚合回填已禁用")
	// ErrDashboardBackfillTooLarge 当回填跨度超过限制时返回。
	ErrDashboardBackfillTooLarge   = errors.New("回填时间跨度过大")
	errDashboardAggregationRunning = errors.New("聚合作业正在运行")
)

// DashboardAggregationRepository 定义仪表盘预聚合仓储接口。
type DashboardAggregationRepository interface {
	AggregateRange(ctx context.Context, start, end time.Time) error
	// RecomputeRange 重新计算指定时间范围内的聚合数据（包含活跃用户等派生表）。
	// 设计目的：当 usage_logs 被批量删除/回滚后，确保聚合表可恢复一致性。
	RecomputeRange(ctx context.Context, start, end time.Time) error
	GetAggregationWatermark(ctx context.Context) (time.Time, error)
	UpdateAggregationWatermark(ctx context.Context, aggregatedAt time.Time) error
	CleanupAggregates(ctx context.Context, hourlyCutoff, dailyCutoff time.Time) error
	CleanupUsageLogs(ctx context.Context, cutoff time.Time) error
	CleanupUsageBillingDedup(ctx context.Context, cutoff time.Time) error
	EnsureUsageLogsPartitions(ctx context.Context, now time.Time) error
}

// DashboardAggregationService 负责定时聚合与回填。
type DashboardAggregationService struct {
	repo                 DashboardAggregationRepository
	timingWheel          *TimingWheelService
	cfg                  config.DashboardAggregationConfig
	running              int32
	lastRetentionCleanup atomic.Value // time.Time

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string

	// settlementGuard 用于在删除 usage_logs 前，把保留截止点夹到「最早未结算周期起点」之前。
	// 供号商的待结算金额是直接查 usage_logs 算出来的，删掉未结算区间的行会让应付金额
	// 静默缩水，且没有任何报错。nil 表示未接入（例如测试），此时不做额外保护。
	settlementGuard ProviderSettlementRetentionGuard
}

// ProviderSettlementRetentionGuard 提供「哪些 usage 行还不能删」的下界。
type ProviderSettlementRetentionGuard interface {
	// EarliestUnsettledStart 返回所有供号商中最早的未结算周期起点。
	// 返回零值表示没有需要保护的数据。
	EarliestUnsettledStart(ctx context.Context) (time.Time, error)
}

// NewDashboardAggregationService 创建聚合服务。
func NewDashboardAggregationService(repo DashboardAggregationRepository, timingWheel *TimingWheelService, cfg *config.Config) *DashboardAggregationService {
	var aggCfg config.DashboardAggregationConfig
	if cfg != nil {
		aggCfg = cfg.DashboardAgg
	}
	return &DashboardAggregationService{
		repo:        repo,
		timingWheel: timingWheel,
		cfg:         aggCfg,
		instanceID:  uuid.NewString(),
	}
}

// SetLeaderLock injects the leader-lock cache and DB used to elect a single
// instance for the periodic scheduled aggregation. When both are nil the job runs
// ungated (single-instance / test behavior).
func (s *DashboardAggregationService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

// SetProviderSettlementGuard 注入供号商结算保护。必须在启动清理作业前调用。
func (s *DashboardAggregationService) SetProviderSettlementGuard(guard ProviderSettlementRetentionGuard) {
	if s == nil {
		return
	}
	s.settlementGuard = guard
}

// ProviderSettlementFloor 返回所有供号商中最早的未结算周期起点。
//
// 第二个返回值为 false 表示守卫已注入但查不出来，调用方必须保守处理：删 usage_logs
// 不可逆，未结算区间的行一旦没了，应付金额就静默缩水，而且当期还没封账、
// provider_settlement_items 里没有快照，事后无从重算。
//
// 未注入守卫时返回零值 + true（没有需要保护的区间），与定时清理的既有行为一致。
func (s *DashboardAggregationService) ProviderSettlementFloor(ctx context.Context) (time.Time, bool) {
	if s == nil || s.settlementGuard == nil {
		return time.Time{}, true
	}
	earliest, err := s.settlementGuard.EarliestUnsettledStart(ctx)
	if err != nil {
		return time.Time{}, false
	}
	return earliest, true
}

// clampUsageCutoffForSettlement 把 usage_logs 的保留截止点夹到未结算区间之前。
//
// 读取失败时返回零值（放弃本轮清理）而不是原截止点：宁可多留一轮数据，
// 也不能在不确定的情况下删掉可能还没结算的应付流水。
func (s *DashboardAggregationService) clampUsageCutoffForSettlement(
	ctx context.Context,
	cutoff time.Time,
) (time.Time, bool) {
	earliest, ok := s.ProviderSettlementFloor(ctx)
	if !ok {
		logger.LegacyPrintf("service.dashboard_aggregation",
			"[DashboardAggregation] 无法确定供号商未结算区间，跳过本轮 usage_logs 清理")
		return time.Time{}, false
	}
	if earliest.IsZero() || cutoff.Before(earliest) {
		return cutoff, true
	}
	logger.LegacyPrintf("service.dashboard_aggregation",
		"[DashboardAggregation] usage_logs 保留截止点被供号商未结算区间夹紧: %s -> %s",
		cutoff.Format(time.RFC3339), earliest.Format(time.RFC3339))
	return earliest, true
}

// Start 启动定时聚合作业（重启生效配置）。
func (s *DashboardAggregationService) Start() {
	if s == nil || s.repo == nil || s.timingWheel == nil {
		return
	}
	if !s.cfg.Enabled {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 聚合作业已禁用")
		return
	}

	interval := time.Duration(s.cfg.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}

	if s.cfg.RecomputeDays > 0 {
		go s.recomputeRecentDays()
	}

	s.timingWheel.ScheduleRecurring("dashboard:aggregation", interval, func() {
		s.runScheduledAggregation()
	})
	logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 聚合作业启动 (interval=%v, lookback=%ds)", interval, s.cfg.LookbackSeconds)
	if !s.cfg.BackfillEnabled {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 回填已禁用，如需补齐保留窗口以外历史数据请手动回填")
	}
}

// TriggerBackfill 触发回填（异步）。
func (s *DashboardAggregationService) TriggerBackfill(start, end time.Time) error {
	if s == nil || s.repo == nil {
		return errors.New("聚合服务未初始化")
	}
	if !s.cfg.BackfillEnabled {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 回填被拒绝: backfill_enabled=false")
		return ErrDashboardBackfillDisabled
	}
	if !end.After(start) {
		return errors.New("回填时间范围无效")
	}
	if s.cfg.BackfillMaxDays > 0 {
		maxRange := time.Duration(s.cfg.BackfillMaxDays) * 24 * time.Hour
		if end.Sub(start) > maxRange {
			return ErrDashboardBackfillTooLarge
		}
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultDashboardAggregationBackfillTimeout)
		defer cancel()
		if err := s.backfillRange(ctx, start, end); err != nil {
			logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 回填失败: %v", err)
		}
	}()
	return nil
}

// TriggerRecomputeRange 触发指定范围的重新计算（异步）。
// 与 TriggerBackfill 不同：
// - 不依赖 backfill_enabled（这是内部一致性修复）
// - 不更新 watermark（避免影响正常增量聚合游标）
func (s *DashboardAggregationService) TriggerRecomputeRange(start, end time.Time) error {
	if s == nil || s.repo == nil {
		return errors.New("聚合服务未初始化")
	}
	if !s.cfg.Enabled {
		return errors.New("聚合服务已禁用")
	}
	if !end.After(start) {
		return errors.New("重新计算时间范围无效")
	}

	go func() {
		const maxRetries = 3
		for i := 0; i < maxRetries; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), defaultDashboardAggregationBackfillTimeout)
			err := s.recomputeRange(ctx, start, end)
			cancel()
			if err == nil {
				return
			}
			if !errors.Is(err, errDashboardAggregationRunning) {
				logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 重新计算失败: %v", err)
				return
			}
			time.Sleep(5 * time.Second)
		}
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 重新计算放弃: 聚合作业持续占用")
	}()
	return nil
}

func (s *DashboardAggregationService) recomputeRecentDays() {
	days := s.cfg.RecomputeDays
	if days <= 0 {
		return
	}
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -days)

	ctx, cancel := context.WithTimeout(context.Background(), defaultDashboardAggregationBackfillTimeout)
	defer cancel()
	if err := s.backfillRange(ctx, start, now); err != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 启动重算失败: %v", err)
		return
	}
}

func (s *DashboardAggregationService) recomputeRange(ctx context.Context, start, end time.Time) error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return errDashboardAggregationRunning
	}
	defer atomic.StoreInt32(&s.running, 0)

	jobStart := time.Now().UTC()
	if err := s.repo.RecomputeRange(ctx, start, end); err != nil {
		return err
	}
	logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 重新计算完成 (start=%s end=%s duration=%s)",
		start.UTC().Format(time.RFC3339),
		end.UTC().Format(time.RFC3339),
		time.Since(jobStart).String(),
	)
	return nil
}

func (s *DashboardAggregationService) runScheduledAggregation() {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&s.running, 0)

	jobStart := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), defaultDashboardAggregationTimeout)
	defer cancel()

	// Multi-instance guard: only the leader runs the periodic aggregation; peers
	// skip this cycle to avoid N× redundant GROUP BY queries and watermark races.
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, dashboardAggregationLeaderLockKey, s.instanceID, dashboardAggregationLeaderLockTTL)
	if !ok {
		return
	}
	defer release()

	now := time.Now().UTC()
	last, err := s.repo.GetAggregationWatermark(ctx)
	if err != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 读取水位失败: %v", err)
		last = time.Unix(0, 0).UTC()
	}

	lookback := time.Duration(s.cfg.LookbackSeconds) * time.Second
	epoch := time.Unix(0, 0).UTC()
	start := last.Add(-lookback)
	if !last.After(epoch) {
		retentionDays := s.cfg.Retention.UsageLogsDays
		if retentionDays <= 0 {
			retentionDays = 1
		}
		start = truncateToDayUTC(now.AddDate(0, 0, -retentionDays))
	} else if start.After(now) {
		start = now.Add(-lookback)
	}

	if err := s.aggregateRange(ctx, start, now); err != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 聚合失败: %v", err)
		return
	}

	updateErr := s.repo.UpdateAggregationWatermark(ctx, now)
	if updateErr != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 更新水位失败: %v", updateErr)
	}
	slog.Debug("[DashboardAggregation] 聚合完成",
		"start", start.Format(time.RFC3339),
		"end", now.Format(time.RFC3339),
		"duration", time.Since(jobStart).String(),
		"watermark_updated", updateErr == nil,
	)

	s.maybeCleanupRetention(ctx, now)
}

func (s *DashboardAggregationService) backfillRange(ctx context.Context, start, end time.Time) error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return errDashboardAggregationRunning
	}
	defer atomic.StoreInt32(&s.running, 0)

	jobStart := time.Now().UTC()
	startUTC := start.UTC()
	endUTC := end.UTC()
	if !endUTC.After(startUTC) {
		return errors.New("回填时间范围无效")
	}

	cursor := truncateToDayUTC(startUTC)
	for cursor.Before(endUTC) {
		windowEnd := cursor.Add(24 * time.Hour)
		if windowEnd.After(endUTC) {
			windowEnd = endUTC
		}
		if err := s.aggregateRange(ctx, cursor, windowEnd); err != nil {
			return err
		}
		cursor = windowEnd
	}

	updateErr := s.repo.UpdateAggregationWatermark(ctx, endUTC)
	if updateErr != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 更新水位失败: %v", updateErr)
	}
	logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 回填聚合完成 (start=%s end=%s duration=%s watermark_updated=%t)",
		startUTC.Format(time.RFC3339),
		endUTC.Format(time.RFC3339),
		time.Since(jobStart).String(),
		updateErr == nil,
	)

	s.maybeCleanupRetention(ctx, endUTC)
	return nil
}

func (s *DashboardAggregationService) aggregateRange(ctx context.Context, start, end time.Time) error {
	if !end.After(start) {
		return nil
	}
	if err := s.repo.EnsureUsageLogsPartitions(ctx, end); err != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 分区检查失败: %v", err)
	}
	return s.repo.AggregateRange(ctx, start, end)
}

func (s *DashboardAggregationService) maybeCleanupRetention(ctx context.Context, now time.Time) {
	lastAny := s.lastRetentionCleanup.Load()
	if lastAny != nil {
		if last, ok := lastAny.(time.Time); ok && now.Sub(last) < dashboardAggregationRetentionInterval {
			return
		}
	}

	hourlyCutoff := now.AddDate(0, 0, -s.cfg.Retention.HourlyDays)
	dailyCutoff := now.AddDate(0, 0, -s.cfg.Retention.DailyDays)
	usageCutoff := now.AddDate(0, 0, -s.cfg.Retention.UsageLogsDays)
	dedupCutoff := now.AddDate(0, 0, -s.cfg.Retention.UsageBillingDedupDays)

	aggErr := s.repo.CleanupAggregates(ctx, hourlyCutoff, dailyCutoff)
	if aggErr != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 聚合保留清理失败: %v", aggErr)
	}
	var usageErr error
	if safeCutoff, ok := s.clampUsageCutoffForSettlement(ctx, usageCutoff); ok {
		usageErr = s.repo.CleanupUsageLogs(ctx, safeCutoff)
		if usageErr != nil {
			logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] usage_logs 保留清理失败: %v", usageErr)
		}
	}
	dedupErr := s.repo.CleanupUsageBillingDedup(ctx, dedupCutoff)
	if dedupErr != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] usage_billing_dedup 保留清理失败: %v", dedupErr)
	}
	if aggErr == nil && usageErr == nil && dedupErr == nil {
		s.lastRetentionCleanup.Store(now)
	}
}

func truncateToDayUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
