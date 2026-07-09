package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPublicPoolHealthPlatform    = PlatformAnthropic
	defaultPublicPoolHealthGroupName   = "ccmax"
	defaultPublicPoolHealthAccountType = AccountTypeSetupToken
	defaultPublicPoolHealthHorizon     = 4 * time.Hour
)

type PublicPoolHealthRequest struct {
	Platform    string
	GroupName   string
	AccountType string
	Horizon     time.Duration
}

type PublicPoolHealthSnapshot struct {
	UpdatedAt            time.Time                        `json:"updated_at"`
	Platform             string                           `json:"platform"`
	GroupName            string                           `json:"group_name"`
	AccountType          string                           `json:"account_type"`
	HorizonSeconds       int64                            `json:"horizon_seconds"`
	Accounts             PublicPoolHealthAccounts         `json:"accounts"`
	Capacity             PublicPoolHealthCapacity         `json:"capacity"`
	Health               PublicPoolHealthStatus           `json:"health"`
	UnavailableBreakdown []PublicPoolHealthBreakdown      `json:"unavailable_breakdown"`
	RecoveryBuckets      []PublicPoolHealthRecoveryBucket `json:"recovery_buckets"`
}

type PublicPoolHealthAccounts struct {
	Total       int `json:"total"`
	Effective   int `json:"effective"`
	Available   int `json:"available"`
	Blocked     int `json:"blocked"`
	Measured    int `json:"measured"`
	InUse       int `json:"in_use"`
	Idle        int `json:"idle"`
	Exhausted   int `json:"exhausted"`
	Unavailable int `json:"unavailable"`
}

type PublicPoolHealthCapacity struct {
	RemainingPercent   float64 `json:"remaining_percent"`
	PoolLoadPercent    float64 `json:"pool_load_percent"`
	Waiting            int     `json:"waiting"`
	TotalSlots         int     `json:"total_slots"`
	SchedulableSlots   int     `json:"schedulable_slots"`
	BusySlots          int     `json:"busy_slots"`
	FreeSlots          int     `json:"free_slots"`
	OvercommittedSlots int     `json:"overcommitted_slots"`
}

type PublicPoolHealthStatus struct {
	Level               string `json:"level"`
	Label               string `json:"label"`
	Message             string `json:"message"`
	NextRecoverySeconds *int64 `json:"next_recovery_seconds,omitempty"`
}

type PublicPoolHealthBreakdown struct {
	Reason string `json:"reason"`
	Label  string `json:"label"`
	Count  int    `json:"count"`
}

type PublicPoolHealthRecoveryBucket struct {
	AfterSeconds int64                             `json:"after_seconds"`
	Label        string                            `json:"label"`
	Count        int                               `json:"count"`
	Segments     []PublicPoolHealthRecoverySegment `json:"segments"`
}

type PublicPoolHealthRecoverySegment struct {
	Unit  string `json:"unit"`
	Count int    `json:"count"`
}

type publicPoolHealthOptions struct {
	Platform    string
	GroupName   string
	AccountType string
	Horizon     time.Duration
}

type poolHealthRecoveryDraft struct {
	count   int
	segment map[int]int
}

func (s *OpsService) GetPublicPoolHealth(ctx context.Context, req PublicPoolHealthRequest) (*PublicPoolHealthSnapshot, error) {
	opts := normalizePublicPoolHealthOptions(publicPoolHealthOptions{
		Platform:    req.Platform,
		GroupName:   req.GroupName,
		AccountType: req.AccountType,
		Horizon:     req.Horizon,
	})

	accounts, err := s.listAllAccountsForOps(ctx, opts.Platform, nil)
	if err != nil {
		return nil, err
	}
	loadMap := s.getAccountsLoadMapBestEffort(ctx, accounts)

	return buildPublicPoolHealthSnapshot(accounts, loadMap, time.Now().UTC(), opts), nil
}

func buildPublicPoolHealthSnapshot(accounts []Account, loadMap map[int64]*AccountLoadInfo, now time.Time, opts publicPoolHealthOptions) *PublicPoolHealthSnapshot {
	opts = normalizePublicPoolHealthOptions(opts)
	now = now.UTC()

	snapshot := &PublicPoolHealthSnapshot{
		UpdatedAt:      now,
		Platform:       opts.Platform,
		GroupName:      opts.GroupName,
		AccountType:    opts.AccountType,
		HorizonSeconds: int64(opts.Horizon.Seconds()),
	}

	breakdown := map[string]int{}
	recoveryDrafts := map[int64]*poolHealthRecoveryDraft{}
	recoveryDeadline := now.Add(opts.Horizon)

	for _, account := range accounts {
		if !poolHealthAccountMatches(account, opts) {
			continue
		}

		snapshot.Accounts.Total++
		loadFactor := account.EffectiveLoadFactor()
		snapshot.Capacity.TotalSlots += loadFactor

		effective, _ := poolHealthEffectiveState(account, now)
		if load := loadMap[account.ID]; load != nil {
			if load.CurrentConcurrency > 0 {
				snapshot.Accounts.InUse += load.CurrentConcurrency
			}
			if load.WaitingCount > 0 {
				snapshot.Capacity.Waiting += load.WaitingCount
			}
		}

		usedPercent, measured := poolHealthAccountUsagePercent(account)
		if measured {
			snapshot.Accounts.Measured++
		}

		if effective {
			snapshot.Accounts.Effective++
		}

		unavailableReason := poolHealthUnavailableReason(account, usedPercent, measured, now)
		if unavailableReason != "" {
			snapshot.Accounts.Unavailable++
			breakdown[unavailableReason]++
			if poolHealthRuntimeBlockedReason(unavailableReason) {
				snapshot.Accounts.Blocked++
			}
		}

		blockers := poolHealthAccountBlockers(account, usedPercent, measured, now)
		if unavailableReason == "rate_limited" || unavailableReason == "quota_exhausted" || (measured && usedPercent >= 99.5) {
			snapshot.Accounts.Exhausted++
		}

		if len(blockers) > 0 {
			if recoveryAt, ok := poolHealthRecoveryAt(blockers); ok && recoveryAt.After(now) && !recoveryAt.After(recoveryDeadline) {
				afterSeconds := ceilDurationSeconds(recoveryAt.Sub(now), time.Minute)
				addPoolHealthRecoveryDraft(recoveryDrafts, afterSeconds, loadFactor)
			}
		}

		if unavailableReason != "" {
			continue
		}

		snapshot.Accounts.Available++
		snapshot.Capacity.SchedulableSlots += loadFactor

		currentBusy := 0
		if load := loadMap[account.ID]; load != nil {
			currentBusy = load.CurrentConcurrency + load.WaitingCount
		}
		snapshot.Capacity.BusySlots += currentBusy
		if free := loadFactor - currentBusy; free > 0 {
			snapshot.Capacity.FreeSlots += free
		} else if free < 0 {
			snapshot.Capacity.OvercommittedSlots += -free
		}

		if load := loadMap[account.ID]; load == nil || load.CurrentConcurrency <= 0 {
			snapshot.Accounts.Idle++
		}
	}

	if snapshot.Capacity.TotalSlots > 0 {
		snapshot.Capacity.RemainingPercent = roundTo1Decimal(float64(snapshot.Capacity.FreeSlots) / float64(snapshot.Capacity.TotalSlots) * 100)
	}
	if snapshot.Capacity.SchedulableSlots > 0 {
		pressure := float64(snapshot.Capacity.BusySlots) / float64(snapshot.Capacity.SchedulableSlots) * 100
		snapshot.Capacity.PoolLoadPercent = roundTo1Decimal(pressure)
	}
	snapshot.UnavailableBreakdown = buildPoolHealthBreakdown(breakdown)
	snapshot.RecoveryBuckets = buildPoolHealthRecoveryBuckets(recoveryDrafts)
	snapshot.Health = buildPoolHealthStatus(snapshot.Capacity.RemainingPercent, snapshot.Capacity.PoolLoadPercent, snapshot.RecoveryBuckets, snapshot.Accounts.Total)

	return snapshot
}

func normalizePublicPoolHealthOptions(opts publicPoolHealthOptions) publicPoolHealthOptions {
	opts.Platform = strings.TrimSpace(opts.Platform)
	if opts.Platform == "" {
		opts.Platform = defaultPublicPoolHealthPlatform
	}
	opts.GroupName = strings.TrimSpace(opts.GroupName)
	if opts.GroupName == "" {
		opts.GroupName = defaultPublicPoolHealthGroupName
	}
	opts.AccountType = strings.TrimSpace(opts.AccountType)
	if opts.AccountType == "" {
		opts.AccountType = defaultPublicPoolHealthAccountType
	}
	if opts.Horizon <= 0 {
		opts.Horizon = defaultPublicPoolHealthHorizon
	}
	return opts
}

func poolHealthAccountMatches(account Account, opts publicPoolHealthOptions) bool {
	if opts.Platform != "" && account.Platform != opts.Platform {
		return false
	}
	if opts.AccountType != "" && account.Type != opts.AccountType {
		return false
	}
	if opts.GroupName == "" {
		return true
	}
	for _, group := range account.Groups {
		if group == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(group.Name), opts.GroupName) {
			return true
		}
	}
	return false
}

func poolHealthEffectiveState(account Account, now time.Time) (bool, string) {
	if account.Status == StatusError {
		return false, "error"
	}
	if account.Status != StatusActive {
		return false, "inactive"
	}
	if !account.Schedulable {
		return false, "manual_disabled"
	}
	if account.AutoPauseOnExpired && account.ExpiresAt != nil && !now.Before(*account.ExpiresAt) {
		return false, "expired"
	}
	return true, ""
}

func poolHealthUnavailableReason(account Account, usedPercent float64, measured bool, now time.Time) string {
	if account.Status == StatusError {
		return "error"
	}
	if account.Status != StatusActive {
		return "inactive"
	}
	if !account.Schedulable {
		return "manual_disabled"
	}
	if account.AutoPauseOnExpired && account.ExpiresAt != nil && !now.Before(*account.ExpiresAt) {
		return "expired"
	}
	if account.RateLimitResetAt != nil && now.Before(*account.RateLimitResetAt) {
		return "rate_limited"
	}
	if account.OverloadUntil != nil && now.Before(*account.OverloadUntil) {
		return "overloaded"
	}
	if account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil) {
		return "temp_unschedulable"
	}
	if measured && usedPercent >= 99.5 && account.SessionWindowStatus == "rejected" {
		return "quota_exhausted"
	}
	return ""
}

func poolHealthRuntimeBlockedReason(reason string) bool {
	switch reason {
	case "rate_limited", "overloaded", "temp_unschedulable", "quota_exhausted":
		return true
	default:
		return false
	}
}

func poolHealthAccountUsagePercent(account Account) (float64, bool) {
	used5h, ok5h := poolHealthExtraFloat(account.Extra, "codex_5h_used_percent")
	used7d, ok7d := poolHealthExtraFloat(account.Extra, "codex_7d_used_percent")
	sessionWindow, okSession := poolHealthExtraFloat(account.Extra, "session_window_utilization")
	passive7d, okPassive7d := poolHealthExtraFloat(account.Extra, "passive_usage_7d_utilization")
	if okSession && sessionWindow <= 1 {
		sessionWindow *= 100
	}
	if okPassive7d && passive7d <= 1 {
		passive7d *= 100
	}
	if !ok5h && !ok7d && !okSession && !okPassive7d {
		switch account.SessionWindowStatus {
		case "rejected":
			return 100, true
		case "allowed_warning":
			return 80, true
		default:
			return 0, false
		}
	}
	usedPercent := math.Max(used5h, used7d)
	if okSession {
		usedPercent = math.Max(usedPercent, sessionWindow)
	}
	if okPassive7d {
		usedPercent = math.Max(usedPercent, passive7d)
	}
	return clampFloat64(usedPercent, 0, 100), true
}

func poolHealthQuotaResetAt(account Account, window string, now time.Time) (time.Time, bool) {
	if window == "session" && account.SessionWindowEnd != nil && account.SessionWindowEnd.After(now) {
		return account.SessionWindowEnd.UTC(), true
	}
	if resetAt, ok := poolHealthExtraTime(account.Extra, "session_window_end"); ok && resetAt.After(now) {
		return resetAt.UTC(), true
	}
	return time.Time{}, false
}

func poolHealthAccountBlockers(account Account, usedPercent float64, measured bool, now time.Time) []time.Time {
	blockers := make([]time.Time, 0, 4)
	appendFuture := func(t *time.Time) {
		if t != nil && t.After(now) {
			blockers = append(blockers, t.UTC())
		}
	}

	appendFuture(account.RateLimitResetAt)
	appendFuture(account.OverloadUntil)
	appendFuture(account.TempUnschedulableUntil)

	if measured && usedPercent >= 99.5 {
		if resetAt, ok := poolHealthUsageResetAt(account, now); ok {
			blockers = append(blockers, resetAt.UTC())
		} else if resetAt, ok := poolHealthQuotaResetAt(account, "session", now); ok {
			blockers = append(blockers, resetAt.UTC())
		}
	}

	return blockers
}

func poolHealthUsageResetAt(account Account, now time.Time) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, window := range []string{"5h", "7d"} {
		used, ok := poolHealthExtraFloat(account.Extra, "codex_"+window+"_used_percent")
		if !ok || used < 99.5 {
			continue
		}
		resetAt, ok := poolHealthExtraTime(account.Extra, "codex_"+window+"_reset_at")
		if !ok {
			resetAt, ok = poolHealthResetAfterTime(account.Extra, window, now)
		}
		if !ok || !resetAt.After(now) {
			continue
		}
		if !found || resetAt.After(latest) {
			latest = resetAt
			found = true
		}
	}
	return latest, found
}

func poolHealthResetAfterTime(extra map[string]any, window string, now time.Time) (time.Time, bool) {
	seconds, ok := poolHealthExtraFloat(extra, "codex_"+window+"_reset_after_seconds")
	if !ok || seconds <= 0 {
		return time.Time{}, false
	}
	base := now
	if updatedAt, ok := poolHealthExtraTime(extra, "codex_usage_updated_at"); ok {
		base = updatedAt
	}
	return base.Add(time.Duration(seconds) * time.Second), true
}

func poolHealthRecoveryAt(blockers []time.Time) (time.Time, bool) {
	var out time.Time
	for _, blocker := range blockers {
		if blocker.IsZero() {
			continue
		}
		if out.IsZero() || blocker.After(out) {
			out = blocker
		}
	}
	return out, !out.IsZero()
}

func addPoolHealthRecoveryDraft(drafts map[int64]*poolHealthRecoveryDraft, afterSeconds int64, loadFactor int) {
	if afterSeconds <= 0 {
		return
	}
	if loadFactor <= 0 {
		loadFactor = 1
	}
	draft := drafts[afterSeconds]
	if draft == nil {
		draft = &poolHealthRecoveryDraft{segment: map[int]int{}}
		drafts[afterSeconds] = draft
	}
	draft.count++
	draft.segment[loadFactor]++
}

func buildPoolHealthRecoveryBuckets(drafts map[int64]*poolHealthRecoveryDraft) []PublicPoolHealthRecoveryBucket {
	afterValues := make([]int64, 0, len(drafts))
	for after := range drafts {
		afterValues = append(afterValues, after)
	}
	sort.Slice(afterValues, func(i, j int) bool { return afterValues[i] < afterValues[j] })

	buckets := make([]PublicPoolHealthRecoveryBucket, 0, len(afterValues))
	for _, after := range afterValues {
		draft := drafts[after]
		factors := make([]int, 0, len(draft.segment))
		for factor := range draft.segment {
			factors = append(factors, factor)
		}
		sort.Slice(factors, func(i, j int) bool { return factors[i] > factors[j] })

		segments := make([]PublicPoolHealthRecoverySegment, 0, len(factors))
		for _, factor := range factors {
			segments = append(segments, PublicPoolHealthRecoverySegment{
				Unit:  fmt.Sprintf("x%d", factor),
				Count: draft.segment[factor],
			})
		}

		buckets = append(buckets, PublicPoolHealthRecoveryBucket{
			AfterSeconds: after,
			Label:        formatPoolHealthAfterLabel(after),
			Count:        draft.count,
			Segments:     segments,
		})
	}
	return buckets
}

func buildPoolHealthBreakdown(counts map[string]int) []PublicPoolHealthBreakdown {
	reasons := make([]string, 0, len(counts))
	for reason, count := range counts {
		if count > 0 {
			reasons = append(reasons, reason)
		}
	}
	sort.Strings(reasons)

	out := make([]PublicPoolHealthBreakdown, 0, len(reasons))
	for _, reason := range reasons {
		out = append(out, PublicPoolHealthBreakdown{
			Reason: reason,
			Label:  poolHealthReasonLabel(reason),
			Count:  counts[reason],
		})
	}
	return out
}

func buildPoolHealthStatus(remainingPercent, loadPercent float64, recoveryBuckets []PublicPoolHealthRecoveryBucket, total int) PublicPoolHealthStatus {
	status := PublicPoolHealthStatus{
		Level:   "healthy",
		Label:   "健康",
		Message: "池子容量充足",
	}
	if total == 0 {
		status.Level = "empty"
		status.Label = "无数据"
		status.Message = "暂无可展示的账号数据"
		return status
	}
	switch {
	case remainingPercent < 15 || loadPercent >= 90:
		status.Level = "tight"
		status.Label = "吃紧"
		status.Message = "池子当前压力较高"
	case remainingPercent < 35 || loadPercent >= 75:
		status.Level = "watch"
		status.Label = "偏紧"
		status.Message = "池子需要持续观察"
	default:
		status.Level = "healthy"
		status.Label = "健康"
		status.Message = "池子容量充足"
	}
	if len(recoveryBuckets) > 0 {
		next := recoveryBuckets[0].AfterSeconds
		status.NextRecoverySeconds = &next
		status.Message = "下一批账号约 " + formatPoolHealthDurationText(next) + " 后恢复"
	}
	return status
}

func poolHealthReasonLabel(reason string) string {
	switch reason {
	case "error":
		return "异常"
	case "expired":
		return "已过期"
	case "inactive":
		return "未启用"
	case "manual_disabled":
		return "手动停调度"
	case "rate_limited":
		return "限流/打满"
	case "overloaded":
		return "过载"
	case "temp_unschedulable":
		return "临时不可调度"
	case "quota_exhausted":
		return "额度打满"
	default:
		return reason
	}
}

func formatPoolHealthAfterLabel(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds 后", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm 后", minutes)
	}
	hours := minutes / 60
	remainMinutes := minutes % 60
	if remainMinutes == 0 {
		return fmt.Sprintf("%dh 后", hours)
	}
	return fmt.Sprintf("%dh%dm 后", hours, remainMinutes)
}

func formatPoolHealthDurationText(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%d 秒", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%d 分钟", minutes)
	}
	hours := minutes / 60
	remainMinutes := minutes % 60
	if remainMinutes == 0 {
		return fmt.Sprintf("%d 小时", hours)
	}
	return fmt.Sprintf("%d 小时 %d 分钟", hours, remainMinutes)
}

func ceilDurationSeconds(d time.Duration, unit time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	if unit <= 0 {
		return int64(math.Ceil(d.Seconds()))
	}
	units := int64(math.Ceil(float64(d) / float64(unit)))
	return int64((time.Duration(units) * unit).Seconds())
}

func poolHealthExtraFloat(extra map[string]any, key string) (float64, bool) {
	if len(extra) == 0 {
		return 0, false
	}
	raw, ok := extra[key]
	if !ok || raw == nil {
		return 0, false
	}
	return parseExtraFloat64(raw), true
}

func poolHealthExtraTime(extra map[string]any, key string) (time.Time, bool) {
	if len(extra) == 0 {
		return time.Time{}, false
	}
	raw, ok := extra[key]
	if !ok || raw == nil {
		return time.Time{}, false
	}
	switch v := raw.(type) {
	case time.Time:
		return v.UTC(), true
	case string:
		return parsePoolHealthTimeString(v)
	case json.Number:
		return poolHealthUnixTime(v.String())
	case float64:
		return poolHealthUnixTime(strconv.FormatInt(int64(v), 10))
	case int64:
		return time.Unix(v, 0).UTC(), true
	case int:
		return time.Unix(int64(v), 0).UTC(), true
	default:
		return parsePoolHealthTimeString(fmt.Sprint(v))
	}
}

func parsePoolHealthTimeString(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), true
	}
	return poolHealthUnixTime(v)
}

func poolHealthUnixTime(v string) (time.Time, bool) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0).UTC(), true
}

func roundTo1Decimal(v float64) float64 {
	return math.Round(v*10) / 10
}
