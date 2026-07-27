//go:build unit

package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// 供号商相关的仓储方法在既有测试替身上的空实现。
//
// 这些替身分散在多个测试文件里，只关心各自被测的那几个方法。把新增的接口方法集中
// 放在这里，既避免逐个文件插入噪声，也让「哪些替身还没实现供号商行为」一目了然。
// 需要断言供号商行为的测试应在自己的文件里覆写对应方法。

// ---- AccountRepository ----

func (m *accountRepoStub) ListByProvider(context.Context, int64) ([]Account, error) { return nil, nil }
func (m *accountRepoStub) ListByProviderTier(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (m *accountRepoStub) CountByProviderTier(context.Context, string) (int, error) { return 0, nil }
func (m *accountRepoStub) UpdateProviderTierParams(context.Context, int64, int, int, map[string]any) error {
	return nil
}

func (m *publicBatchImageAccountRepo) ListByProvider(context.Context, int64) ([]Account, error) {
	return nil, nil
}
func (m *publicBatchImageAccountRepo) ListByProviderTier(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (m *publicBatchImageAccountRepo) CountByProviderTier(context.Context, string) (int, error) {
	return 0, nil
}
func (m *publicBatchImageAccountRepo) UpdateProviderTierParams(context.Context, int64, int, int, map[string]any) error {
	return nil
}

func (m *mockAccountRepoForPlatform) ListByProvider(context.Context, int64) ([]Account, error) {
	return nil, nil
}
func (m *mockAccountRepoForPlatform) ListByProviderTier(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (m *mockAccountRepoForPlatform) CountByProviderTier(context.Context, string) (int, error) {
	return 0, nil
}
func (m *mockAccountRepoForPlatform) UpdateProviderTierParams(context.Context, int64, int, int, map[string]any) error {
	return nil
}

func (m *mockAccountRepoForGemini) ListByProvider(context.Context, int64) ([]Account, error) {
	return nil, nil
}
func (m *mockAccountRepoForGemini) ListByProviderTier(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (m *mockAccountRepoForGemini) CountByProviderTier(context.Context, string) (int, error) {
	return 0, nil
}
func (m *mockAccountRepoForGemini) UpdateProviderTierParams(context.Context, int64, int, int, map[string]any) error {
	return nil
}

func (m *batchAccountQueryRepo) ListByProvider(context.Context, int64) ([]Account, error) {
	return nil, nil
}
func (m *batchAccountQueryRepo) ListByProviderTier(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (m *batchAccountQueryRepo) CountByProviderTier(context.Context, string) (int, error) {
	return 0, nil
}
func (m *batchAccountQueryRepo) UpdateProviderTierParams(context.Context, int64, int, int, map[string]any) error {
	return nil
}

func (m *fullRebuildAccountRepo) ListByProvider(context.Context, int64) ([]Account, error) {
	return nil, nil
}
func (m *fullRebuildAccountRepo) ListByProviderTier(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (m *fullRebuildAccountRepo) CountByProviderTier(context.Context, string) (int, error) {
	return 0, nil
}
func (m *fullRebuildAccountRepo) UpdateProviderTierParams(context.Context, int64, int, int, map[string]any) error {
	return nil
}

// ---- UserRepository ----

func (m *userRepoStubForGroupUpdate) ListProviders(context.Context) ([]User, error) { return nil, nil }
func (m *userRepoStub) ListProviders(context.Context) ([]User, error)               { return nil, nil }
func (m *emailSyncRepoStub) ListProviders(context.Context) ([]User, error)          { return nil, nil }
func (m *mockUserRepo) ListProviders(context.Context) ([]User, error)               { return nil, nil }

// DistinctNonProviderPrioritiesByGroup 的空实现。返回空 map 表示「没有已有账号」，
// 设置页据此不会提示优先级冲突。

func (m *accountRepoStub) DistinctNonProviderPrioritiesByGroup(context.Context) (map[int64][]int, error) {
	return nil, nil
}
func (m *publicBatchImageAccountRepo) DistinctNonProviderPrioritiesByGroup(context.Context) (map[int64][]int, error) {
	return nil, nil
}
func (m *mockAccountRepoForPlatform) DistinctNonProviderPrioritiesByGroup(context.Context) (map[int64][]int, error) {
	return nil, nil
}
func (m *mockAccountRepoForGemini) DistinctNonProviderPrioritiesByGroup(context.Context) (map[int64][]int, error) {
	return nil, nil
}
func (m *batchAccountQueryRepo) DistinctNonProviderPrioritiesByGroup(context.Context) (map[int64][]int, error) {
	return nil, nil
}
func (m *fullRebuildAccountRepo) DistinctNonProviderPrioritiesByGroup(context.Context) (map[int64][]int, error) {
	return nil, nil
}

// ListByProviderPaged 的空实现。
func (m *accountRepoStub) ListByProviderPaged(context.Context, int64, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}
func (m *publicBatchImageAccountRepo) ListByProviderPaged(context.Context, int64, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}
func (m *mockAccountRepoForPlatform) ListByProviderPaged(context.Context, int64, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}
func (m *mockAccountRepoForGemini) ListByProviderPaged(context.Context, int64, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}
func (m *batchAccountQueryRepo) ListByProviderPaged(context.Context, int64, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}
func (m *fullRebuildAccountRepo) ListByProviderPaged(context.Context, int64, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}

// MinPriorityByGroup 的空实现。返回空 map 表示「分组里还没有账号」，
// 上号时会回落到设置里的默认优先级。

func (m *accountRepoStub) MinPriorityByGroup(context.Context) (map[int64]int, error) {
	return nil, nil
}
func (m *publicBatchImageAccountRepo) MinPriorityByGroup(context.Context) (map[int64]int, error) {
	return nil, nil
}
func (m *mockAccountRepoForPlatform) MinPriorityByGroup(context.Context) (map[int64]int, error) {
	return nil, nil
}
func (m *mockAccountRepoForGemini) MinPriorityByGroup(context.Context) (map[int64]int, error) {
	return nil, nil
}
func (m *batchAccountQueryRepo) MinPriorityByGroup(context.Context) (map[int64]int, error) {
	return nil, nil
}
func (m *fullRebuildAccountRepo) MinPriorityByGroup(context.Context) (map[int64]int, error) {
	return nil, nil
}
