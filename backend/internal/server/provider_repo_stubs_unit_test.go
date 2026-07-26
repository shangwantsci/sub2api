//go:build unit

package server_test

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 供号商相关的仓储方法在既有测试替身上的空实现。

func (m *stubAccountRepo) ListByProvider(context.Context, int64) ([]service.Account, error) {
	return nil, nil
}
func (m *stubAccountRepo) ListByProviderTier(context.Context, string) ([]service.Account, error) {
	return nil, nil
}
func (m *stubAccountRepo) CountByProviderTier(context.Context, string) (int, error) {
	return 0, nil
}
func (m *stubAccountRepo) UpdateProviderTierParams(context.Context, int64, int, int, map[string]any) error {
	return nil
}

func (m *stubUserRepo) ListProviders(context.Context) ([]service.User, error) { return nil, nil }

// DistinctNonProviderPrioritiesByGroup 的空实现。返回空 map 表示「没有已有账号」，
// 设置页据此不会提示优先级冲突。

func (m *stubAccountRepo) DistinctNonProviderPrioritiesByGroup(context.Context) (map[int64][]int, error) {
	return nil, nil
}

// ListByProviderPaged 的空实现。
func (m *stubAccountRepo) ListByProviderPaged(context.Context, int64, pagination.PaginationParams) ([]service.Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}
