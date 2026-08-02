//go:build unit

package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 供号商相关的仓储方法在既有测试替身上的空实现（unit tag 部分）。

func (m *grokCredentialHandlerRepo) ListByProvider(context.Context, int64) ([]service.Account, error) {
	return nil, nil
}
func (m *grokCredentialHandlerRepo) ListByProviderTier(context.Context, string) ([]service.Account, error) {
	return nil, nil
}
func (m *grokCredentialHandlerRepo) CountByProviderTier(context.Context, string) (int, error) {
	return 0, nil
}
func (m *grokCredentialHandlerRepo) UpdateProviderTierParams(context.Context, int64, int, int, map[string]any) error {
	return nil
}

func (m *grokCredentialHandlerRepo) UpdateProviderAccountTier(context.Context, int64, string, int, int, map[string]any) error {
	return nil
}

func (m *userHandlerRepoStub) ListProviders(context.Context) ([]service.User, error) {
	return nil, nil
}

// DistinctNonProviderPrioritiesByGroup 的空实现。返回空 map 表示「没有已有账号」，
// 设置页据此不会提示优先级冲突。

func (m *grokCredentialHandlerRepo) DistinctNonProviderPrioritiesByGroup(context.Context) (map[int64][]int, error) {
	return nil, nil
}

// ListByProviderPaged 的空实现。
func (m *grokCredentialHandlerRepo) ListByProviderPaged(context.Context, int64, pagination.PaginationParams) ([]service.Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}

// MinPriorityByGroup 的空实现。返回空 map 表示「分组里还没有账号」，
// 上号时会回落到设置里的默认优先级。

func (m *grokCredentialHandlerRepo) MinPriorityByGroup(context.Context) (map[int64]int, error) {
	return nil, nil
}
