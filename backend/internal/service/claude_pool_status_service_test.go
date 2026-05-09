//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestParseDerouterClaudePoolStatusHTML_SelectsMaxClaudeCoefficient(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	html := `
		<table>
			<tr><td>GPT 5.2</td><td>$1.00</td><td>$2.00</td><td>95%</td><td>1.50x</td></tr>
			<tr><td>Claude Opus 4.7</td><td>$1.14</td><td>$5.72</td><td>4%</td><td>0.80×</td></tr>
			<tr><td>Claude Sonnet 4.6</td><td>$0.86</td><td>$4.29</td><td>68%</td><td>1.20×</td></tr>
			<tr><td>Claude Haiku 4.5</td><td>$0.23</td><td>$1.14</td><td>44</td><td>%</td><td>1.00×</td></tr>
		</table>`

	snapshot, err := ParseDerouterClaudePoolStatusHTML(html, now)

	require.NoError(t, err)
	require.Equal(t, 3, len(snapshot.Models))
	require.Equal(t, 1.20, snapshot.Coefficient)
	require.Equal(t, 68.0, snapshot.LoadPercent)
	require.Equal(t, 32.0, snapshot.IdlePercent)
	require.Equal(t, "Claude Sonnet 4.6", snapshot.SelectedModel)
	require.Equal(t, now, snapshot.UpdatedAt)
}

func TestParseDerouterClaudePoolStatusHTML_RejectsMissingClaudeRows(t *testing.T) {
	_, err := ParseDerouterClaudePoolStatusHTML(`<table><tr><td>GPT 5.2</td><td>$1</td></tr></table>`, time.Now())
	require.Error(t, err)
}

func TestClaudePoolStatusServiceDynamicMultiplier(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	svc := NewClaudePoolStatusService(&config.Config{
		Gateway: config.GatewayConfig{
			ClaudePoolPricing: config.ClaudePoolPricingConfig{
				Enabled:           true,
				GroupName:         "claude满血默认",
				BaseCoefficient:   0.8,
				StaleAfterSeconds: 1800,
			},
		},
	})
	svc.now = func() time.Time { return now }
	svc.snapshot = ClaudePoolSnapshot{
		Status:      ClaudePoolStatusFresh,
		Coefficient: 1.2,
		UpdatedAt:   now.Add(-time.Minute),
	}

	require.Equal(t, 2.4, svc.ApplyDynamicMultiplier(1.6, "claude满血默认"))
	require.Equal(t, 2.1, svc.ApplyDynamicMultiplier(1.4, "claude满血默认"))
	require.Equal(t, 1.6, svc.ApplyDynamicMultiplier(1.6, "other"))
	require.Equal(t, 0.0, svc.ApplyDynamicMultiplier(0, "claude满血默认"))
}

func TestClaudePoolStatusServiceDynamicMultiplierFallsBackWhenStale(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	svc := NewClaudePoolStatusService(&config.Config{
		Gateway: config.GatewayConfig{
			ClaudePoolPricing: config.ClaudePoolPricingConfig{
				Enabled:           true,
				GroupName:         "claude满血默认",
				BaseCoefficient:   0.8,
				StaleAfterSeconds: 1800,
			},
		},
	})
	svc.now = func() time.Time { return now }
	svc.snapshot = ClaudePoolSnapshot{
		Status:      ClaudePoolStatusFresh,
		Coefficient: 1.5,
		UpdatedAt:   now.Add(-31 * time.Minute),
	}

	require.Equal(t, 1.6, svc.ApplyDynamicMultiplier(1.6, "claude满血默认"))
	require.Equal(t, ClaudePoolStatusStale, svc.PublicStatus().Status)
}
