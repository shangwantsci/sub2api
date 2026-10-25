package repository

import (
	"context"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ListClaudeSubscriptionAccounts 只查 Anthropic OAuth/SetupToken，SQL 级分页。
func (r *accountRepository) ListClaudeSubscriptionAccounts(
	ctx context.Context,
	params pagination.PaginationParams,
	status, search, sortBy, sortOrder string,
) ([]service.Account, int64, error) {
	q := r.accountListFilteredQuery(service.PlatformAnthropic, "", status, search, 0, "").
		Where(dbaccount.TypeIn(service.AccountTypeOAuth, service.AccountTypeSetupToken))
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	desc := strings.EqualFold(strings.TrimSpace(sortOrder), "desc")
	switch strings.TrimSpace(sortBy) {
	case "priority":
		if desc {
			q = q.Order(dbaccount.OrderOption(dbent.Desc(dbaccount.FieldPriority)))
		} else {
			q = q.Order(dbaccount.OrderOption(dbent.Asc(dbaccount.FieldPriority)))
		}
	case "created_at":
		if desc {
			q = q.Order(dbaccount.OrderOption(dbent.Desc(dbaccount.FieldCreatedAt)))
		} else {
			q = q.Order(dbaccount.OrderOption(dbent.Asc(dbaccount.FieldCreatedAt)))
		}
	default:
		if desc {
			q = q.Order(dbaccount.OrderOption(dbent.Desc(dbaccount.FieldName)))
		} else {
			q = q.Order(dbaccount.OrderOption(dbent.Asc(dbaccount.FieldName)))
		}
	}
	entities, err := q.Offset(params.Offset()).Limit(params.PageSize).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	accounts := make([]service.Account, 0, len(entities))
	for _, entity := range entities {
		if account := accountEntityToService(entity); account != nil {
			accounts = append(accounts, *account)
		}
	}
	return accounts, int64(total), nil
}
