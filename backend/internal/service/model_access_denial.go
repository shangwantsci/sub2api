package service

import (
	"context"
	"strings"
)

const (
	modelAccessDenialsKey                     = "model_access_denials"
	anthropicFableCreditsRequiredDenialReason = "anthropic_fable_credits_required"
)

// ModelAccessDenial records a model entitlement failure that has no reset time.
// Unlike model_rate_limits, this state remains active until an explicit account
// test succeeds or an administrator clears it.
type ModelAccessDenial struct {
	Reason           string `json:"reason"`
	ErrorCode        string `json:"error_code,omitempty"`
	DisabledReason   string `json:"disabled_reason,omitempty"`
	Model            string `json:"model,omitempty"`
	ModelDisplayName string `json:"model_display_name,omitempty"`
	ObservedAt       string `json:"observed_at"`
}

// ModelAccessDenialStore is an optional repository capability. The production
// repository implements it with atomic nested JSONB updates; test/lightweight
// repositories can fall back to AccountRepository.UpdateExtra.
type ModelAccessDenialStore interface {
	SetModelAccessDenial(ctx context.Context, id int64, scope string, denial ModelAccessDenial) error
	ClearModelAccessDenial(ctx context.Context, id int64, scope string) error
}

func (a *Account) isModelAccessDeniedWithContext(ctx context.Context, requestedModel string) bool {
	if a == nil {
		return false
	}
	for _, key := range a.modelRateLimitKeysForRequest(ctx, requestedModel) {
		if a.hasModelAccessDenialForKey(key) {
			return true
		}
	}
	return false
}

func (a *Account) hasModelAccessDenialForKey(scope string) bool {
	if a == nil || a.Extra == nil || strings.TrimSpace(scope) == "" {
		return false
	}
	rawDenials, ok := a.Extra[modelAccessDenialsKey].(map[string]any)
	if !ok {
		return false
	}
	raw, exists := rawDenials[scope]
	if !exists || raw == nil {
		return false
	}
	switch value := raw.(type) {
	case map[string]any:
		return len(value) > 0
	case ModelAccessDenial:
		return strings.TrimSpace(value.Reason) != ""
	default:
		return true
	}
}

func persistModelAccessDenial(
	ctx context.Context,
	repo AccountRepository,
	account *Account,
	scope string,
	denial ModelAccessDenial,
) error {
	scope = strings.TrimSpace(scope)
	if repo == nil || account == nil || scope == "" {
		return nil
	}
	if store, ok := repo.(ModelAccessDenialStore); ok {
		if err := store.SetModelAccessDenial(ctx, account.ID, scope, denial); err != nil {
			return err
		}
		setLocalModelAccessDenial(account, scope, denial)
		return nil
	}

	denials := cloneModelAccessDenials(account.Extra)
	denials[scope] = modelAccessDenialToMap(denial)
	if err := repo.UpdateExtra(ctx, account.ID, map[string]any{modelAccessDenialsKey: denials}); err != nil {
		return err
	}
	setLocalModelAccessDenial(account, scope, denial)
	return nil
}

func clearModelAccessDenial(
	ctx context.Context,
	repo AccountRepository,
	account *Account,
	scope string,
) error {
	scope = strings.TrimSpace(scope)
	if repo == nil || account == nil || scope == "" {
		return nil
	}
	if store, ok := repo.(ModelAccessDenialStore); ok {
		if err := store.ClearModelAccessDenial(ctx, account.ID, scope); err != nil {
			return err
		}
		clearLocalModelAccessDenial(account, scope)
		return nil
	}

	denials := cloneModelAccessDenials(account.Extra)
	delete(denials, scope)
	if err := repo.UpdateExtra(ctx, account.ID, map[string]any{modelAccessDenialsKey: denials}); err != nil {
		return err
	}
	clearLocalModelAccessDenial(account, scope)
	return nil
}

func cloneModelAccessDenials(extra map[string]any) map[string]any {
	result := map[string]any{}
	if extra == nil {
		return result
	}
	if existing, ok := extra[modelAccessDenialsKey].(map[string]any); ok {
		for key, value := range existing {
			result[key] = value
		}
	}
	return result
}

func setLocalModelAccessDenial(account *Account, scope string, denial ModelAccessDenial) {
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	denials := cloneModelAccessDenials(account.Extra)
	denials[scope] = modelAccessDenialToMap(denial)
	account.Extra[modelAccessDenialsKey] = denials
}

func clearLocalModelAccessDenial(account *Account, scope string) {
	if account == nil || account.Extra == nil {
		return
	}
	denials := cloneModelAccessDenials(account.Extra)
	delete(denials, scope)
	account.Extra[modelAccessDenialsKey] = denials
}

func modelAccessDenialToMap(denial ModelAccessDenial) map[string]any {
	result := map[string]any{
		"reason":      denial.Reason,
		"observed_at": denial.ObservedAt,
	}
	if denial.ErrorCode != "" {
		result["error_code"] = denial.ErrorCode
	}
	if denial.DisabledReason != "" {
		result["disabled_reason"] = denial.DisabledReason
	}
	if denial.Model != "" {
		result["model"] = denial.Model
	}
	if denial.ModelDisplayName != "" {
		result["model_display_name"] = denial.ModelDisplayName
	}
	return result
}
