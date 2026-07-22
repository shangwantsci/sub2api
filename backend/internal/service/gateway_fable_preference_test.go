//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fableEvidenceExtra(now time.Time, utilization float64, sampledAt time.Time) map[string]any {
	return map[string]any{
		"passive_usage_7d_oi_utilization": utilization,
		"passive_usage_7d_oi_reset":       now.Add(24 * time.Hour).Unix(),
		"passive_usage_sampled_at":        sampledAt.UTC().Format(time.RFC3339Nano),
	}
}

func TestAccountHasFreshFableAvailabilityEvidence(t *testing.T) {
	now := time.Now().UTC()

	require.True(t, (&Account{Extra: fableEvidenceExtra(now, 0.83, now.Add(-time.Minute))}).hasFreshFableAvailabilityEvidence(now))
	require.False(t, (&Account{Extra: fableEvidenceExtra(now, 1.0, now.Add(-time.Minute))}).hasFreshFableAvailabilityEvidence(now))
	require.False(t, (&Account{Extra: fableEvidenceExtra(now, 0.2, now.Add(-16*time.Minute))}).hasFreshFableAvailabilityEvidence(now))
	require.False(t, (&Account{Extra: map[string]any{}}).hasFreshFableAvailabilityEvidence(now))

	expired := fableEvidenceExtra(now, 0.2, now.Add(-time.Minute))
	expired["passive_usage_7d_oi_reset"] = now.Add(-time.Minute).Unix()
	require.False(t, (&Account{Extra: expired}).hasFreshFableAvailabilityEvidence(now))
}

func TestSelectDefaultLoadAwareCandidatePrefersFreshFableEvidence(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-time.Hour)
	recent := now.Add(-time.Minute)
	unknown := &Account{ID: 1, Priority: 1, LastUsedAt: &old}
	known := &Account{
		ID:         2,
		Priority:   1,
		LastUsedAt: &recent,
		Extra:      fableEvidenceExtra(now, 0.4, recent),
	}
	accounts := []accountWithLoad{
		{account: unknown, loadInfo: &AccountLoadInfo{AccountID: 1, LoadRate: 0}},
		{account: known, loadInfo: &AccountLoadInfo{AccountID: 2, LoadRate: 50}},
	}

	selected := selectDefaultLoadAwareCandidate(accounts, "claude-fable-5", false, false)
	require.NotNil(t, selected)
	require.Equal(t, int64(2), selected.account.ID)

	selected = selectDefaultLoadAwareCandidate(accounts, "claude-opus-4-6", false, false)
	require.NotNil(t, selected)
	require.Equal(t, int64(1), selected.account.ID, "non-Fable scheduling must retain load/LRU behavior")
}

func TestSelectDefaultLoadAwareCandidatePreservesPriorityBeforeFableEvidence(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-time.Hour)
	recent := now.Add(-time.Minute)
	highPriorityUnknown := &Account{ID: 1, Priority: 1, LastUsedAt: &old}
	lowPriorityKnown := &Account{
		ID:         2,
		Priority:   2,
		LastUsedAt: &recent,
		Extra:      fableEvidenceExtra(now, 0.2, recent),
	}
	accounts := []accountWithLoad{
		{account: highPriorityUnknown, loadInfo: &AccountLoadInfo{AccountID: 1}},
		{account: lowPriorityKnown, loadInfo: &AccountLoadInfo{AccountID: 2}},
	}

	selected := selectDefaultLoadAwareCandidate(accounts, "claude-fable-5", false, false)
	require.NotNil(t, selected)
	require.Equal(t, int64(1), selected.account.ID)
}
