package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 供号商对账查询。
//
// 四条不可违反的约束，改动这些 SQL 前务必读完：
//
//  1. 金额只允许 SUM(total_cost)。禁止改用 actual_cost 或 account_stats_cost：
//     那两者含分组倍率与账号倍率，一旦用上，平台自己设的分组倍率就会泄露给供号商，
//     而供号商面板的口径必须恒定为 1 倍率。
//
//  2. JOIN accounts 禁止追加 AND a.deleted_at IS NULL。供号商下线（软删除）账号后，
//     该账号本期已产生的金额必须继续计入，否则会凭空少付钱且没有任何报错。
//     这些是 raw SQL，不走 Ent 的软删除 interceptor，天然能查到已删账号 —— 这是有意为之，
//     不是疏漏。项目里 usage_log_repo_deleted_user_integration_test.go 有同类先例。
//
//  3. 按日聚合的时区参数必须来自 provider_settlement_timezone 设置，
//     禁止沿用请求里由浏览器注入的 timezone，否则管理员与供号商看到的每日明细
//     会按各自时区切分，总额一致但逐日对不上。
//
//  4. 区间条件必须用 providerWindowClause 而不是裸的 created_at BETWEEN。
//     usage_logs 由异步 worker 写入，created_at 是 worker 赋值而非 COMMIT 时刻，
//     存在「created_at 早、提交晚」的记录。只按 created_at 切区间会让这类记录
//     既不在已封金额里也进不了下一期，直接漏账。
//
// 金额一律扫进 decimal.Decimal：这是要拿去付钱的数字，不能中途降级为 float64。

// providerWindowClause 构造周期条件与参数。
//
// 命中两类记录：
//   - 落在本期 [start, end) 内的正常记录；
//   - created_at 早于本期起点、但 id 大于上期水位的迟到记录。
//     上期封账时记录了当时计入的最大 id，因此 id 大于水位即可断定「上期没算过」，
//     不会重复计入。
//
// 返回的 SQL 片段里参数占位从 $1 开始按 provider/start/end/watermark 顺序。
func providerWindowClause(alias string) string {
	return `
		` + alias + `.provider_user_id = $1
		AND (
			(ul.created_at >= $2 AND ul.created_at < $3)
			OR (ul.created_at < $2 AND ul.id > $4)
		)
		AND ul.created_at < $3`
}

func providerWindowArgs(providerUserID int64, w service.ProviderSettlementWindow) []any {
	return []any{providerUserID, w.Start, w.End, w.LastUsageID}
}

// GetProviderPeriodTotals 返回某供号商在给定周期内的 1 倍率合计。
//
// 同时返回本次覆盖到的最大 usage id（下期水位）与捕获到的迟到行数（用于告警）。
func (r *usageLogRepository) GetProviderPeriodTotals(
	ctx context.Context,
	providerUserID int64,
	w service.ProviderSettlementWindow,
) (*service.ProviderPeriodTotals, error) {
	query := `
		SELECT
			COUNT(*) AS requests,
			COALESCE(SUM(ul.input_tokens + ul.output_tokens
			           + ul.cache_creation_tokens + ul.cache_read_tokens), 0) AS tokens,
			COALESCE(SUM(ul.total_cost), 0) AS standard_cost,
			COUNT(DISTINCT ul.account_id) AS account_count,
			COALESCE(MAX(ul.id), 0) AS max_usage_id,
			COUNT(*) FILTER (WHERE ul.created_at < $2) AS late_rows
		FROM usage_logs ul
		JOIN accounts a ON a.id = ul.account_id
		WHERE ` + providerWindowClause("a")

	var out service.ProviderPeriodTotals
	if err := scanSingleRow(
		ctx, r.sql, query,
		providerWindowArgs(providerUserID, w),
		&out.Requests, &out.Tokens, &out.StandardCost,
		&out.AccountCount, &out.MaxUsageID, &out.LateRows,
	); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetProviderDailyBreakdown 返回按日聚合的 1 倍率明细。
// timezone 必须由调用方从设置读取，不接受请求参数。
func (r *usageLogRepository) GetProviderDailyBreakdown(
	ctx context.Context,
	providerUserID int64,
	w service.ProviderSettlementWindow,
	timezone string,
) ([]service.ProviderDailyUsage, error) {
	query := `
		SELECT
			TO_CHAR(ul.created_at AT TIME ZONE $5, 'YYYY-MM-DD') AS date,
			COUNT(*) AS requests,
			COALESCE(SUM(ul.input_tokens + ul.output_tokens
			           + ul.cache_creation_tokens + ul.cache_read_tokens), 0) AS tokens,
			COALESCE(SUM(ul.total_cost), 0) AS standard_cost
		FROM usage_logs ul
		JOIN accounts a ON a.id = ul.account_id
		WHERE ` + providerWindowClause("a") + `
		GROUP BY date
		ORDER BY date`

	args := append(providerWindowArgs(providerUserID, w), timezone)
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.ProviderDailyUsage, 0, 32)
	for rows.Next() {
		var item service.ProviderDailyUsage
		if err := rows.Scan(&item.Date, &item.Requests, &item.Tokens, &item.StandardCost); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// GetProviderAccountBreakdown 返回分账号的 1 倍率明细。
//
// 已下线（软删除）账号仍然出现在结果里并带 offline 标记 —— 取出来是为了在明细中标注，
// 不是拿来过滤。少了这些行，明细合计就会对不上总额。
func (r *usageLogRepository) GetProviderAccountBreakdown(
	ctx context.Context,
	providerUserID int64,
	w service.ProviderSettlementWindow,
) ([]service.ProviderAccountUsage, error) {
	query := `
		SELECT
			ul.account_id,
			COALESCE(MAX(a.name), '') AS account_name,
			BOOL_OR(a.deleted_at IS NOT NULL) AS offline,
			COUNT(*) AS requests,
			COALESCE(SUM(ul.input_tokens + ul.output_tokens
			           + ul.cache_creation_tokens + ul.cache_read_tokens), 0) AS tokens,
			COALESCE(SUM(ul.total_cost), 0) AS standard_cost
		FROM usage_logs ul
		JOIN accounts a ON a.id = ul.account_id
		WHERE ` + providerWindowClause("a") + `
		GROUP BY ul.account_id
		ORDER BY standard_cost DESC`

	rows, err := r.sql.QueryContext(ctx, query, providerWindowArgs(providerUserID, w)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.ProviderAccountUsage, 0, 32)
	for rows.Next() {
		var item service.ProviderAccountUsage
		if err := rows.Scan(
			&item.AccountID, &item.AccountName, &item.Offline,
			&item.Requests, &item.Tokens, &item.StandardCost,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// GetProviderSingleAccountTotals 返回单个账号在周期内的 1 倍率合计。
func (r *usageLogRepository) GetProviderSingleAccountTotals(
	ctx context.Context,
	accountID int64,
	w service.ProviderSettlementWindow,
) (*service.ProviderPeriodTotals, error) {
	const query = `
		SELECT
			COUNT(*) AS requests,
			COALESCE(SUM(ul.input_tokens + ul.output_tokens
			           + ul.cache_creation_tokens + ul.cache_read_tokens), 0) AS tokens,
			COALESCE(SUM(ul.total_cost), 0) AS standard_cost,
			COALESCE(MAX(ul.id), 0) AS max_usage_id
		FROM usage_logs ul
		WHERE ul.account_id = $1
			AND (
				(ul.created_at >= $2 AND ul.created_at < $3)
				OR (ul.created_at < $2 AND ul.id > $4)
			)
			AND ul.created_at < $3
	`
	var out service.ProviderPeriodTotals
	if err := scanSingleRow(
		ctx, r.sql, query,
		[]any{accountID, w.Start, w.End, w.LastUsageID},
		&out.Requests, &out.Tokens, &out.StandardCost, &out.MaxUsageID,
	); err != nil {
		return nil, err
	}
	// 单账号视角下「有用量的账号数」恒为 0 或 1，与周期合计的语义保持一致。
	if out.Requests > 0 {
		out.AccountCount = 1
	}
	return &out, nil
}

// GetProviderAccountTotalsBatch 批量返回多个账号在周期内的 1 倍率合计，
// 供账号列表页一次查完，避免 N+1。
func (r *usageLogRepository) GetProviderAccountTotalsBatch(
	ctx context.Context,
	providerUserID int64,
	w service.ProviderSettlementWindow,
) (map[int64]service.ProviderPeriodTotals, error) {
	breakdown, err := r.GetProviderAccountBreakdown(ctx, providerUserID, w)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]service.ProviderPeriodTotals, len(breakdown))
	for _, item := range breakdown {
		totals := service.ProviderPeriodTotals{
			Requests:     item.Requests,
			Tokens:       item.Tokens,
			StandardCost: item.StandardCost,
		}
		if item.Requests > 0 {
			totals.AccountCount = 1
		}
		out[item.AccountID] = totals
	}
	return out, nil
}
