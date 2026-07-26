package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 无 build tag 的测试替身上的供号商方法空实现。
// 带 unit tag 的替身见 provider_repo_stubs_unit_test.go。

func (m *openAIWSFailoverHandlerAccountRepoStub) ListByProvider(context.Context, int64) ([]service.Account, error) {
	return nil, nil
}
func (m *openAIWSFailoverHandlerAccountRepoStub) ListByProviderTier(context.Context, string) ([]service.Account, error) {
	return nil, nil
}
func (m *openAIWSFailoverHandlerAccountRepoStub) CountByProviderTier(context.Context, string) (int, error) {
	return 0, nil
}
func (m *openAIWSFailoverHandlerAccountRepoStub) UpdateProviderTierParams(context.Context, int64, int, int, map[string]any) error {
	return nil
}

func (m *openAIWSUsageHandlerAccountRepoStub) ListByProvider(context.Context, int64) ([]service.Account, error) {
	return nil, nil
}
func (m *openAIWSUsageHandlerAccountRepoStub) ListByProviderTier(context.Context, string) ([]service.Account, error) {
	return nil, nil
}
func (m *openAIWSUsageHandlerAccountRepoStub) CountByProviderTier(context.Context, string) (int, error) {
	return 0, nil
}
func (m *openAIWSUsageHandlerAccountRepoStub) UpdateProviderTierParams(context.Context, int64, int, int, map[string]any) error {
	return nil
}

// DistinctNonProviderPrioritiesByGroup 的空实现。返回空 map 表示「没有已有账号」，
// 设置页据此不会提示优先级冲突。

func (m *openAIWSFailoverHandlerAccountRepoStub) DistinctNonProviderPrioritiesByGroup(context.Context) (map[int64][]int, error) {
	return nil, nil
}
func (m *openAIWSUsageHandlerAccountRepoStub) DistinctNonProviderPrioritiesByGroup(context.Context) (map[int64][]int, error) {
	return nil, nil
}

// ListByProviderPaged 的空实现。
func (m *openAIWSFailoverHandlerAccountRepoStub) ListByProviderPaged(context.Context, int64, pagination.PaginationParams) ([]service.Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}
func (m *openAIWSUsageHandlerAccountRepoStub) ListByProviderPaged(context.Context, int64, pagination.PaginationParams) ([]service.Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}
