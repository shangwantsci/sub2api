package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// 无 build tag 的测试替身上的供号商方法空实现。
// 带 unit tag 的替身见 provider_repo_stubs_unit_test.go。

func (m *contentModerationTestUserRepo) ListProviders(context.Context) ([]User, error) {
	return nil, nil
}

func (m *sessionWindowMockRepo) DistinctNonProviderPrioritiesByGroup(context.Context) (map[int64][]int, error) {
	return nil, nil
}

// ListByProviderPaged 的空实现。
func (m *sessionWindowMockRepo) ListByProviderPaged(context.Context, int64, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}

// MinPriorityByGroup 的空实现。返回空 map 表示「分组里还没有账号」，
// 上号时会回落到设置里的默认优先级。

func (m *sessionWindowMockRepo) MinPriorityByGroup(context.Context) (map[int64]int, error) {
	return nil, nil
}
