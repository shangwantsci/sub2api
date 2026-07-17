package service

import "context"

// RPMCache RPM 计数器缓存接口
// 用于 Anthropic OAuth/SetupToken 账号的每分钟请求数限制
type RPMCache interface {
	// IncrementRPM 原子递增并返回当前分钟的计数
	// 使用 Redis 服务器时间确定 minute key，避免多实例时钟偏差
	IncrementRPM(ctx context.Context, accountID int64) (count int, err error)

	// GetRPM 获取当前分钟的 RPM 计数
	GetRPM(ctx context.Context, accountID int64) (count int, err error)

	// GetRPMBatch 批量获取多个账号的 RPM 计数（使用 Pipeline）
	GetRPMBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error)

	// IncrementAccountDaily 原子递增并返回账号在给定「日键」下的累计请求数。
	// dayKey 由调用方按人格时区计算（YYYYMMDD），从而跨人格本地午夜自动重置。
	// 用于人格日请求上限（persona_daily_request_cap）。
	IncrementAccountDaily(ctx context.Context, accountID int64, dayKey string) (count int, err error)

	// GetAccountDaily 获取账号在给定「日键」下的累计请求数。
	GetAccountDaily(ctx context.Context, accountID int64, dayKey string) (count int, err error)
}
