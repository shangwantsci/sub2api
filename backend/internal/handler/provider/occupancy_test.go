//go:build unit

package provider

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func anthropicAccount(id int64, concurrency, sessions, rpm int) service.Account {
	extra := map[string]any{}
	if sessions > 0 {
		extra["max_sessions"] = sessions
	}
	if rpm > 0 {
		extra["base_rpm"] = rpm
	}
	return service.Account{
		ID:          id,
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeOAuth,
		Concurrency: concurrency,
		Extra:       extra,
	}
}

func TestSelectOccupancyTargetsSkipsDisabledLimits(t *testing.T) {
	accounts := []service.Account{
		anthropicAccount(1, 3, 3, 30),
		anthropicAccount(2, 0, 3, 30),
		anthropicAccount(3, 3, 0, 30),
		anthropicAccount(4, 3, 3, 0),
		{
			ID:          5,
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Concurrency: 8,
			Extra:       map[string]any{"max_sessions": 8, "base_rpm": 80},
		},
	}

	got := selectOccupancyTargets(accounts)

	require.Equal(t, []int64{1, 3, 4}, got.ConcurrencyIDs)
	require.Equal(t, []int64{1, 2, 3}, got.RPMIDs)
	require.Equal(t, []int64{1, 2, 4}, got.SessionIDs)
	require.Equal(t, 5*time.Minute, got.IdleTimeouts[1])
	require.NotContains(t, got.IdleTimeouts, int64(3))
	require.NotContains(t, got.ConcurrencyIDs, int64(2))
	require.NotContains(t, got.ConcurrencyIDs, int64(5))
	require.NotContains(t, got.RPMIDs, int64(5))
	require.NotContains(t, got.SessionIDs, int64(5))
}

func TestSelectOccupancyTargetsUsesAccountIdleTimeout(t *testing.T) {
	acc := anthropicAccount(7, 3, 3, 30)
	acc.Extra["session_idle_timeout_minutes"] = 12

	got := selectOccupancyTargets([]service.Account{acc})

	require.Equal(t, []int64{7}, got.SessionIDs)
	require.Equal(t, 12*time.Minute, got.IdleTimeouts[7])
}

func TestMergeOccupancyIntoViewsFillsCollectedValues(t *testing.T) {
	views := []AccountView{
		{ID: 1, Concurrency: 3, MaxSessions: 3, BaseRPM: 30},
		{ID: 2, Concurrency: 3, MaxSessions: 3, BaseRPM: 30},
	}

	mergeOccupancyIntoViews(views, map[int64]int{1: 1}, map[int64]int{1: 5}, map[int64]int{1: 2})

	require.Equal(t, 1, *views[0].CurrentConcurrency)
	require.Equal(t, 5, *views[0].CurrentRPM)
	require.Equal(t, 2, *views[0].ActiveSessions)
	require.Nil(t, views[1].CurrentConcurrency)
	require.Nil(t, views[1].CurrentRPM)
	require.Nil(t, views[1].ActiveSessions)
}

func TestMergeOccupancyIntoViewsSkipsZeroConcurrencyCap(t *testing.T) {
	views := []AccountView{
		{ID: 1, Concurrency: 0, MaxSessions: 3, BaseRPM: 30},
	}

	mergeOccupancyIntoViews(views, map[int64]int{1: 9}, map[int64]int{1: 4}, map[int64]int{1: 1})

	require.Nil(t, views[0].CurrentConcurrency)
	require.Equal(t, 4, *views[0].CurrentRPM)
	require.Equal(t, 1, *views[0].ActiveSessions)
}

func TestMergeOccupancyIntoViewsNilMapsLeaveFieldsNil(t *testing.T) {
	views := []AccountView{
		{ID: 1, Concurrency: 3, MaxSessions: 3, BaseRPM: 30},
	}

	mergeOccupancyIntoViews(views, nil, nil, nil)

	require.Nil(t, views[0].CurrentConcurrency)
	require.Nil(t, views[0].CurrentRPM)
	require.Nil(t, views[0].ActiveSessions)
}

func TestMergeOccupancyIntoViewsDoesNotLeakStrategy(t *testing.T) {
	views := []AccountView{
		{ID: 1, Concurrency: 3, MaxSessions: 3, BaseRPM: 30},
	}
	mergeOccupancyIntoViews(views, map[int64]int{1: 1}, map[int64]int{1: 5}, map[int64]int{1: 2})

	raw, err := json.Marshal(views[0])
	require.NoError(t, err)
	payload := string(raw)
	for _, needle := range []string{
		"rpm_strategy", "rpm_sticky_buffer", "session_idle_timeout",
		"load_factor", "platform", "tiered",
	} {
		require.NotContains(t, payload, needle, "occupancy merge must not expose %q; payload=%s", needle, payload)
	}
	require.Contains(t, payload, `"current_concurrency":1`)
	require.Contains(t, payload, `"current_rpm":5`)
	require.Contains(t, payload, `"active_sessions":2`)
}
