package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildPublicPoolHealthSnapshotAggregatesRecoveryAndRedacts(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	group := &Group{ID: 7, Name: "ccmax", Platform: PlatformAnthropic}
	otherGroup := &Group{ID: 8, Name: "team", Platform: PlatformAnthropic}
	reset1m := now.Add(1 * time.Minute)
	reset21m := now.Add(21 * time.Minute)

	accounts := []Account{
		{
			ID:          1,
			Name:        "alice@example.com",
			Platform:    PlatformAnthropic,
			Type:        AccountTypeSetupToken,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 5,
			Extra: map[string]any{
				"codex_5h_used_percent": 60.0,
				"codex_5h_reset_at":     reset1m.Format(time.RFC3339),
			},
			Groups: []*Group{group},
		},
		{
			ID:               2,
			Name:             "bob@example.com",
			Platform:         PlatformAnthropic,
			Type:             AccountTypeSetupToken,
			Status:           StatusActive,
			Schedulable:      true,
			Concurrency:      5,
			RateLimitResetAt: &reset1m,
			Extra: map[string]any{
				"codex_5h_used_percent": 100.0,
				"codex_5h_reset_at":     reset1m.Format(time.RFC3339),
			},
			Groups: []*Group{group},
		},
		{
			ID:                      3,
			Name:                    "carol@example.com",
			Platform:                PlatformAnthropic,
			Type:                    AccountTypeSetupToken,
			Status:                  StatusActive,
			Schedulable:             true,
			TempUnschedulableUntil:  &reset21m,
			TempUnschedulableReason: "proxy 154.29.158.193 returned carol@example.com challenge",
			Concurrency:             1,
			Extra:                   map[string]any{"codex_5h_used_percent": 90.0},
			Groups:                  []*Group{group},
			ProxyFallbackOriginName: poolHealthStringPtr("154.29.158.193"),
			ProxyFallbackOriginID:   poolHealthInt64Ptr(99),
		},
		{
			ID:           4,
			Name:         "dave@example.com",
			Platform:     PlatformAnthropic,
			Type:         AccountTypeSetupToken,
			Status:       StatusError,
			Schedulable:  true,
			ErrorMessage: "token rejected for dave@example.com via 10.0.0.2",
			Groups:       []*Group{group},
		},
		{
			ID:          5,
			Name:        "erin@example.com",
			Platform:    PlatformAnthropic,
			Type:        AccountTypeSetupToken,
			Status:      StatusActive,
			Schedulable: false,
			Groups:      []*Group{group},
		},
		{
			ID:          6,
			Name:        "frank@example.com",
			Platform:    PlatformAnthropic,
			Type:        AccountTypeSetupToken,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Extra:       map[string]any{"codex_5h_used_percent": 20.0},
			Groups:      []*Group{group},
		},
		{
			ID:          7,
			Name:        "external@example.com",
			Platform:    PlatformAnthropic,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Groups:      []*Group{group},
		},
		{
			ID:          8,
			Name:        "other-group@example.com",
			Platform:    PlatformAnthropic,
			Type:        AccountTypeSetupToken,
			Status:      StatusActive,
			Schedulable: true,
			Groups:      []*Group{otherGroup},
		},
	}

	loadMap := map[int64]*AccountLoadInfo{
		1: {AccountID: 1, CurrentConcurrency: 2, WaitingCount: 1},
		2: {AccountID: 2, CurrentConcurrency: 5},
		3: {AccountID: 3, CurrentConcurrency: 1},
		6: {AccountID: 6, CurrentConcurrency: 0},
	}

	snapshot := buildPublicPoolHealthSnapshot(accounts, loadMap, now, publicPoolHealthOptions{
		Platform:    PlatformAnthropic,
		GroupName:   "ccmax",
		AccountType: AccountTypeSetupToken,
		Horizon:     4 * time.Hour,
	})

	if snapshot.Accounts.Total != 6 {
		t.Fatalf("total accounts = %d, want 6", snapshot.Accounts.Total)
	}
	if snapshot.Accounts.Effective != 4 {
		t.Fatalf("effective accounts = %d, want 4", snapshot.Accounts.Effective)
	}
	if snapshot.Accounts.Available != 2 {
		t.Fatalf("available accounts = %d, want 2", snapshot.Accounts.Available)
	}
	if snapshot.Accounts.Blocked != 2 {
		t.Fatalf("blocked accounts = %d, want 2", snapshot.Accounts.Blocked)
	}
	if snapshot.Accounts.InUse != 8 {
		t.Fatalf("in-use slots = %d, want 8", snapshot.Accounts.InUse)
	}
	if snapshot.Accounts.Idle != 1 {
		t.Fatalf("idle accounts = %d, want 1", snapshot.Accounts.Idle)
	}
	if snapshot.Accounts.Exhausted != 1 {
		t.Fatalf("exhausted accounts = %d, want 1", snapshot.Accounts.Exhausted)
	}
	if snapshot.Accounts.Unavailable != 4 {
		t.Fatalf("unavailable accounts = %d, want 4", snapshot.Accounts.Unavailable)
	}
	if snapshot.Accounts.Measured != 4 {
		t.Fatalf("measured accounts = %d, want 4", snapshot.Accounts.Measured)
	}
	if snapshot.Capacity.TotalSlots != 14 {
		t.Fatalf("total slots = %d, want 14", snapshot.Capacity.TotalSlots)
	}
	if snapshot.Capacity.SchedulableSlots != 6 {
		t.Fatalf("schedulable slots = %d, want 6", snapshot.Capacity.SchedulableSlots)
	}
	if snapshot.Capacity.BusySlots != 3 {
		t.Fatalf("busy slots = %d, want 3", snapshot.Capacity.BusySlots)
	}
	if snapshot.Capacity.FreeSlots != 3 {
		t.Fatalf("free slots = %d, want 3", snapshot.Capacity.FreeSlots)
	}
	if snapshot.Capacity.RemainingPercent != 21.4 {
		t.Fatalf("remaining percent = %.1f, want 21.4", snapshot.Capacity.RemainingPercent)
	}
	if snapshot.Capacity.PoolLoadPercent != 50 {
		t.Fatalf("pool load percent = %.1f, want 50.0", snapshot.Capacity.PoolLoadPercent)
	}

	if len(snapshot.RecoveryBuckets) != 2 {
		t.Fatalf("recovery bucket count = %d, want 2", len(snapshot.RecoveryBuckets))
	}
	if snapshot.RecoveryBuckets[0].AfterSeconds != 60 || snapshot.RecoveryBuckets[0].Count != 1 {
		t.Fatalf("first recovery bucket = %+v, want after 60s count 1", snapshot.RecoveryBuckets[0])
	}
	if len(snapshot.RecoveryBuckets[0].Segments) != 1 ||
		snapshot.RecoveryBuckets[0].Segments[0].Unit != "x5" ||
		snapshot.RecoveryBuckets[0].Segments[0].Count != 1 {
		t.Fatalf("first recovery segments = %+v, want one x5 segment", snapshot.RecoveryBuckets[0].Segments)
	}
	if snapshot.RecoveryBuckets[1].AfterSeconds != int64(21*time.Minute/time.Second) || snapshot.RecoveryBuckets[1].Count != 1 {
		t.Fatalf("second recovery bucket = %+v, want after 21m count 1", snapshot.RecoveryBuckets[1])
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	leaked := string(encoded)
	for _, sensitive := range []string{
		"alice@example.com",
		"carol@example.com",
		"dave@example.com",
		"154.29.158.193",
		"10.0.0.2",
		"token rejected",
		"challenge",
	} {
		if strings.Contains(leaked, sensitive) {
			t.Fatalf("public snapshot leaked sensitive text %q in %s", sensitive, leaked)
		}
	}
}

func TestBuildPublicPoolHealthSnapshotUsesSchedulableCapacityWhenUsageUnknown(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	group := &Group{ID: 7, Name: "ccmax", Platform: PlatformAnthropic}

	accounts := []Account{
		{
			ID:          1,
			Platform:    PlatformAnthropic,
			Type:        AccountTypeSetupToken,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 3,
			Groups:      []*Group{group},
		},
		{
			ID:          2,
			Platform:    PlatformAnthropic,
			Type:        AccountTypeSetupToken,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Groups:      []*Group{group},
		},
	}

	loadMap := map[int64]*AccountLoadInfo{
		1: {AccountID: 1, CurrentConcurrency: 1},
	}

	snapshot := buildPublicPoolHealthSnapshot(accounts, loadMap, now, publicPoolHealthOptions{
		Platform:    PlatformAnthropic,
		GroupName:   "ccmax",
		AccountType: AccountTypeSetupToken,
		Horizon:     4 * time.Hour,
	})

	if snapshot.Accounts.Measured != 0 {
		t.Fatalf("measured accounts = %d, want 0", snapshot.Accounts.Measured)
	}
	if snapshot.Accounts.Available != 2 {
		t.Fatalf("available accounts = %d, want 2", snapshot.Accounts.Available)
	}
	if snapshot.Capacity.TotalSlots != 4 {
		t.Fatalf("total slots = %d, want 4", snapshot.Capacity.TotalSlots)
	}
	if snapshot.Capacity.FreeSlots != 3 {
		t.Fatalf("free slots = %d, want 3", snapshot.Capacity.FreeSlots)
	}
	if snapshot.Capacity.RemainingPercent != 75 {
		t.Fatalf("remaining percent = %.1f, want 75.0", snapshot.Capacity.RemainingPercent)
	}
	if snapshot.Capacity.PoolLoadPercent != 25 {
		t.Fatalf("pool load percent = %.1f, want 25.0", snapshot.Capacity.PoolLoadPercent)
	}
}

func poolHealthStringPtr(v string) *string {
	return &v
}

func poolHealthInt64Ptr(v int64) *int64 {
	return &v
}
