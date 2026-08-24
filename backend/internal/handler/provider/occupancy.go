package provider

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// occupancyTargets 是当页账号里值得去 Redis 查占用的子集。
//
// 非 Anthropic OAuth/SetupToken、以及对应上限 ≤0 的项都跳过：
// 前者供号商账号本不该出现，作防御；后者要么限制未启用，要么并发不限
// （调度器不写槽位，读回来恒为 0）。
type occupancyTargets struct {
	ConcurrencyIDs []int64
	RPMIDs         []int64
	SessionIDs     []int64
	IdleTimeouts   map[int64]time.Duration
}

func selectOccupancyTargets(accounts []service.Account) occupancyTargets {
	out := occupancyTargets{
		IdleTimeouts: make(map[int64]time.Duration),
	}
	for i := range accounts {
		acc := &accounts[i]
		if !acc.IsAnthropicOAuthOrSetupToken() {
			continue
		}
		if acc.Concurrency > 0 {
			out.ConcurrencyIDs = append(out.ConcurrencyIDs, acc.ID)
		}
		if acc.GetBaseRPM() > 0 {
			out.RPMIDs = append(out.RPMIDs, acc.ID)
		}
		if acc.GetMaxSessions() > 0 {
			out.SessionIDs = append(out.SessionIDs, acc.ID)
			out.IdleTimeouts[acc.ID] = time.Duration(acc.GetSessionIdleTimeoutMinutes()) * time.Minute
		}
	}
	return out
}

func mergeOccupancyIntoViews(views []AccountView, conc, rpm, sessions map[int64]int) {
	for i := range views {
		v := &views[i]
		if v.Concurrency > 0 {
			if n, ok := conc[v.ID]; ok {
				v.CurrentConcurrency = intPtr(n)
			}
		}
		if v.BaseRPM > 0 {
			if n, ok := rpm[v.ID]; ok {
				v.CurrentRPM = intPtr(n)
			}
		}
		if v.MaxSessions > 0 {
			if n, ok := sessions[v.ID]; ok {
				v.ActiveSessions = intPtr(n)
			}
		}
	}
}

func intPtr(n int) *int { return &n }

// attachRuntimeOccupancy 按当页账号批量读 Redis 占用并写回视图。
// Redis 失败时对应字段保持 nil，列表本身不因此 500。
func (h *Handler) attachRuntimeOccupancy(ctx context.Context, accounts []service.Account, views []AccountView) {
	targets := selectOccupancyTargets(accounts)

	var conc map[int64]int
	if h.concurrencyService != nil && len(targets.ConcurrencyIDs) > 0 {
		if cc, err := h.concurrencyService.GetAccountConcurrencyBatch(ctx, targets.ConcurrencyIDs); err == nil {
			conc = cc
		}
	}

	var rpmCounts map[int64]int
	if h.rpmCache != nil && len(targets.RPMIDs) > 0 {
		if counts, err := h.rpmCache.GetRPMBatch(ctx, targets.RPMIDs); err == nil {
			rpmCounts = counts
		}
	}

	var sessions map[int64]int
	if h.sessionLimitCache != nil && len(targets.SessionIDs) > 0 {
		if counts, err := h.sessionLimitCache.GetActiveSessionCountBatch(ctx, targets.SessionIDs, targets.IdleTimeouts); err == nil {
			sessions = counts
		}
	}

	mergeOccupancyIntoViews(views, conc, rpmCounts, sessions)
}
