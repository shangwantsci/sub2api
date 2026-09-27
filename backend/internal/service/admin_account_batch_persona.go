package service

import (
	"context"
	"fmt"
	"strings"
)

// BatchPersonaConfig 批量人格配置输入。
type BatchPersonaConfig struct {
	AccountIDs         []int64 `json:"account_ids"`          // 空 = 所有 Anthropic OAuth/SetupToken 账号
	Timezone           string  `json:"timezone"`             // IANA 时区，如 "America/New_York"，空 = 随机分配
	ActiveStartHour    int     `json:"active_start_hour"`    // 0..23
	ActiveEndHour      int     `json:"active_end_hour"`      // 1..24
	MaxConcurrency     int     `json:"max_concurrency"`      // 活跃时段并发
	OffPeakConcurrency int     `json:"off_peak_concurrency"` // 非活跃时段并发，0=自动
	DailyCap           int     `json:"daily_request_cap"`    // 0=不限
	RandomizeTimezone  bool    `json:"randomize_timezone"`   // true 时忽略 Timezone，从账号代理 IP 所在国家自动分配
}

// BatchApplyPersonaConfig 批量给账号写入 persona 配置。
// 已启用 persona 的账号会被跳过（不覆盖），除非 force=true。
func (s *adminServiceImpl) BatchApplyPersonaConfig(ctx context.Context, input BatchPersonaConfig, force bool) (applied int, skipped int, err error) {
	accounts, err := s.resolveBatchPersonaAccounts(ctx, input.AccountIDs)
	if err != nil {
		return 0, 0, err
	}

	for i := range accounts {
		account := &accounts[i]
		if account.IsPersonaEnabled() && !force {
			skipped++
			continue
		}

		updates := map[string]any{
			extraPersonaEnabled:            true,
			extraPersonaActiveStart:        input.ActiveStartHour,
			extraPersonaActiveEnd:          input.ActiveEndHour,
			extraPersonaMaxConcurrency:     input.MaxConcurrency,
			extraPersonaOffPeakConcurrency: input.OffPeakConcurrency,
			extraPersonaDailyCap:           input.DailyCap,
		}

		tz := strings.TrimSpace(input.Timezone)
		if input.RandomizeTimezone || tz == "" {
			// 按代理出口国家自动分配时区/locale（空 Timezone 或显式 randomize）。
			s.applyPersonaGeoDefaults(ctx, updates, account.ProxyID)
		} else {
			updates[extraPersonaTimezone] = tz
		}

		if err := s.UpdateAccountExtra(ctx, account.ID, updates); err != nil {
			return applied, skipped, fmt.Errorf("apply persona to account %d: %w", account.ID, err)
		}
		applied++
	}

	return applied, skipped, nil
}

func (s *adminServiceImpl) resolveBatchPersonaAccounts(ctx context.Context, accountIDs []int64) ([]Account, error) {
	if len(accountIDs) == 0 {
		listed, err := s.accountRepo.ListSchedulableByPlatform(ctx, PlatformAnthropic)
		if err != nil {
			return nil, fmt.Errorf("list anthropic accounts: %w", err)
		}
		out := make([]Account, 0, len(listed))
		for i := range listed {
			a := listed[i]
			if a.Type == AccountTypeOAuth || a.Type == AccountTypeSetupToken {
				out = append(out, a)
			}
		}
		return out, nil
	}

	loaded, err := s.accountRepo.GetByIDs(ctx, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("get accounts by ids: %w", err)
	}
	out := make([]Account, 0, len(loaded))
	for _, a := range loaded {
		if a != nil {
			out = append(out, *a)
		}
	}
	return out, nil
}
