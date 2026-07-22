package service

import (
	"strings"
	"time"
)

const fableAvailabilityEvidenceMaxAge = 15 * time.Minute

// hasFreshFableAvailabilityEvidence reports whether the latest passive 7d_oi
// snapshot is recent and still shows remaining Fable capacity. It is a
// preference signal only; unknown accounts remain eligible as fallback.
func (a *Account) hasFreshFableAvailabilityEvidence(now time.Time) bool {
	if a == nil || len(a.Extra) == 0 {
		return false
	}
	if _, ok := a.Extra["passive_usage_7d_oi_utilization"]; !ok {
		return false
	}
	utilization := parseExtraFloat64(a.Extra["passive_usage_7d_oi_utilization"])
	if utilization < 0 || utilization >= 1.0-1e-9 {
		return false
	}

	resetUnix := int64(parseExtraFloat64(a.Extra["passive_usage_7d_oi_reset"]))
	if resetUnix <= 0 || !time.Unix(resetUnix, 0).After(now) {
		return false
	}

	sampledRaw, ok := a.Extra["passive_usage_sampled_at"].(string)
	if !ok || strings.TrimSpace(sampledRaw) == "" {
		return false
	}
	sampledAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(sampledRaw))
	if err != nil || sampledAt.After(now.Add(time.Minute)) {
		return false
	}
	age := now.Sub(sampledAt)
	return age >= 0 && age <= fableAvailabilityEvidenceMaxAge
}
