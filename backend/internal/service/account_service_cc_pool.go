package service

import (
	"context"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// ListClaudeSubscriptionAccounts 只返回 Claude 订阅账号（Anthropic OAuth/SetupToken）。
// 当前实现基于 ListAllWithFilters 做内存过滤/排序/分页，避免为一个池页面新增 SQL 分支。
func (s *AccountService) ListClaudeSubscriptionAccounts(
	ctx context.Context,
	params pagination.PaginationParams,
	status, search, sortBy, sortOrder string,
) ([]Account, int64, error) {
	if lister, ok := s.accountRepo.(interface {
		ListClaudeSubscriptionAccounts(context.Context, pagination.PaginationParams, string, string, string, string) ([]Account, int64, error)
	}); ok {
		return lister.ListClaudeSubscriptionAccounts(ctx, params, status, search, sortBy, sortOrder)
	}
	accounts, err := s.accountRepo.ListAllWithFilters(ctx, PlatformAnthropic, "", status, search, 0, "")
	if err != nil {
		return nil, 0, err
	}
	filtered := make([]Account, 0, len(accounts))
	for i := range accounts {
		if accounts[i].Transport() == TransportClaudeCode {
			filtered = append(filtered, accounts[i])
		}
	}
	desc := strings.EqualFold(strings.TrimSpace(sortOrder), "desc")
	switch strings.TrimSpace(sortBy) {
	case "priority":
		sort.SliceStable(filtered, func(i, j int) bool {
			if desc {
				return filtered[i].Priority > filtered[j].Priority
			}
			return filtered[i].Priority < filtered[j].Priority
		})
	case "created_at":
		sort.SliceStable(filtered, func(i, j int) bool {
			if desc {
				return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
			}
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		})
	default:
		sort.SliceStable(filtered, func(i, j int) bool {
			if desc {
				return filtered[i].Name > filtered[j].Name
			}
			return filtered[i].Name < filtered[j].Name
		})
	}
	total := int64(len(filtered))
	offset := params.Offset()
	if offset >= len(filtered) {
		return []Account{}, total, nil
	}
	end := offset + params.PageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], total, nil
}
