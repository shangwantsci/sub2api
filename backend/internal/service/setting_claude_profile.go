package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

// ClaudeCalibratedProfileStatus 是标定 profile 的只读展示状态（供管理端 UI）。
type ClaudeCalibratedProfileStatus struct {
	Published     bool     `json:"published"`
	Valid         bool     `json:"valid"`
	CLIVersion    string   `json:"cli_version"`
	CapturedAt    string   `json:"captured_at"`
	Source        string   `json:"source"`
	UserAgent     string   `json:"user_agent"`
	SaltVerified  bool     `json:"salt_verified"`
	GuardChecked  int      `json:"guard_checked"`
	GuardOK       int      `json:"guard_ok"`
	BetaRuleKeys  []string `json:"beta_rule_keys"`
	AbsentHeaders []string `json:"absent_headers"`
	Raw           string   `json:"raw"`
	Error         string   `json:"error,omitempty"`
}

// cachedClaudeCalibratedProfile 缓存已加载的标定 profile。profile 为 nil 表示
// "未发布/无效/守卫未过"，网关热路径据此回退到编译内置常量。
type cachedClaudeCalibratedProfile struct {
	profile   *claude.CalibratedProfile
	expiresAt int64 // unix nano
}

const claudeCalibratedProfileCacheTTL = 60 * time.Second
const claudeCalibratedProfileErrorTTL = 5 * time.Second
const claudeCalibratedProfileDBTimeout = 5 * time.Second
const claudeCalibratedProfileSFKey = "claude_calibrated_profile"

// GetClaudeCalibratedProfile 返回当前已加载的标定 profile；未发布/解析失败/守卫未过
// 均返回 nil（调用方回退到编译内置常量）。进程内 atomic.Value 缓存（60s TTL），
// singleflight 防缓存过期时的 thundering herd。这是网关伪装热路径读取项。
func (s *SettingService) GetClaudeCalibratedProfile(ctx context.Context) *claude.CalibratedProfile {
	if s == nil || s.settingRepo == nil {
		return nil
	}
	if cached, ok := s.claudeCalibratedProfileCache.Load().(*cachedClaudeCalibratedProfile); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.profile
		}
	}
	result, _, _ := s.claudeCalibratedProfileSF.Do(claudeCalibratedProfileSFKey, func() (any, error) {
		if cached, ok := s.claudeCalibratedProfileCache.Load().(*cachedClaudeCalibratedProfile); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return cached, nil
			}
		}
		if ctx == nil {
			ctx = context.Background()
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claudeCalibratedProfileDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyClaudeCodeCalibratedProfile)
		if err != nil {
			// 未发布是常态（新装/未接标定），用完整 TTL 缓存 nil；真实 DB 错误用短 TTL 快速重试。
			ttl := claudeCalibratedProfileCacheTTL
			if !errors.Is(err, ErrSettingNotFound) {
				slog.Warn("failed to get claude calibrated profile setting", "error", err)
				ttl = claudeCalibratedProfileErrorTTL
			}
			entry := &cachedClaudeCalibratedProfile{profile: nil, expiresAt: time.Now().Add(ttl).UnixNano()}
			s.claudeCalibratedProfileCache.Store(entry)
			return entry, nil
		}
		if strings.TrimSpace(raw) == "" {
			entry := &cachedClaudeCalibratedProfile{profile: nil, expiresAt: time.Now().Add(claudeCalibratedProfileCacheTTL).UnixNano()}
			s.claudeCalibratedProfileCache.Store(entry)
			return entry, nil
		}
		profile, parseErr := claude.ParseCalibratedProfile([]byte(raw))
		if parseErr != nil {
			// 已发布但无效（例如守卫未过 / schema 漂移）：绝不发出去，回退常量，短 TTL 重试。
			slog.Warn("stored claude calibrated profile is invalid; falling back to built-in constants", "error", parseErr)
			entry := &cachedClaudeCalibratedProfile{profile: nil, expiresAt: time.Now().Add(claudeCalibratedProfileErrorTTL).UnixNano()}
			s.claudeCalibratedProfileCache.Store(entry)
			return entry, nil
		}
		entry := &cachedClaudeCalibratedProfile{profile: profile, expiresAt: time.Now().Add(claudeCalibratedProfileCacheTTL).UnixNano()}
		s.claudeCalibratedProfileCache.Store(entry)
		return entry, nil
	})
	if entry, ok := result.(*cachedClaudeCalibratedProfile); ok && entry != nil {
		return entry.profile
	}
	return nil
}

// PublishClaudeCalibratedProfile 校验并发布一个标定 profile JSON。校验不通过（schema
// 漂移 / cc_version 与 UA 不一致 / 指纹守卫未过 / 无 beta 规则）直接返回错误，不写入，
// 保证错误字节永远不会进入热路径。成功后立即刷新进程内缓存，下次请求即生效。
func (s *SettingService) PublishClaudeCalibratedProfile(ctx context.Context, raw []byte) (*claude.CalibratedProfile, error) {
	if s == nil || s.settingRepo == nil {
		return nil, fmt.Errorf("setting service unavailable")
	}
	profile, err := claude.ParseCalibratedProfile(raw)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claudeCalibratedProfileDBTimeout)
	defer cancel()
	if err := s.settingRepo.Set(dbCtx, SettingKeyClaudeCodeCalibratedProfile, strings.TrimSpace(string(raw))); err != nil {
		return nil, fmt.Errorf("persist calibrated profile: %w", err)
	}
	s.claudeCalibratedProfileCache.Store(&cachedClaudeCalibratedProfile{
		profile:   profile,
		expiresAt: time.Now().Add(claudeCalibratedProfileCacheTTL).UnixNano(),
	})
	return profile, nil
}

// ClearClaudeCalibratedProfile 清除已发布的标定 profile，网关回退到编译内置常量。
func (s *SettingService) ClearClaudeCalibratedProfile(ctx context.Context) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claudeCalibratedProfileDBTimeout)
	defer cancel()
	if err := s.settingRepo.Set(dbCtx, SettingKeyClaudeCodeCalibratedProfile, ""); err != nil {
		return fmt.Errorf("clear calibrated profile: %w", err)
	}
	s.claudeCalibratedProfileCache.Store(&cachedClaudeCalibratedProfile{
		profile:   nil,
		expiresAt: time.Now().Add(claudeCalibratedProfileCacheTTL).UnixNano(),
	})
	return nil
}

// GetClaudeCalibratedProfileStatus 返回标定 profile 的只读展示状态。未发布 → Published=false；
// 已发布但校验失败（守卫未过 / schema 漂移）→ Published=true, Valid=false + Error，网关此时
// 回退到编译内置常量。
func (s *SettingService) GetClaudeCalibratedProfileStatus(ctx context.Context) ClaudeCalibratedProfileStatus {
	status := ClaudeCalibratedProfileStatus{BetaRuleKeys: []string{}, AbsentHeaders: []string{}}
	if s == nil || s.settingRepo == nil {
		return status
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claudeCalibratedProfileDBTimeout)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyClaudeCodeCalibratedProfile)
	if err != nil || strings.TrimSpace(raw) == "" {
		return status
	}
	status.Published = true
	status.Raw = raw
	profile, parseErr := claude.ParseCalibratedProfile([]byte(raw))
	if parseErr != nil {
		status.Valid = false
		status.Error = parseErr.Error()
		return status
	}
	status.Valid = true
	status.CLIVersion = profile.Version()
	status.CapturedAt = profile.CapturedAt
	status.Source = profile.Source
	status.UserAgent = profile.UserAgent()
	status.SaltVerified = profile.Guard.SaltVerified
	status.GuardChecked = profile.Guard.Checked
	status.GuardOK = profile.Guard.OK
	status.AbsentHeaders = profile.AbsentHeaders()
	if status.AbsentHeaders == nil {
		status.AbsentHeaders = []string{}
	}
	keys := make([]string, 0, len(profile.BetaRules))
	for k := range profile.BetaRules {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	status.BetaRuleKeys = keys
	return status
}

// invalidateClaudeCalibratedProfileCache 强制下次读取重新从 DB 加载。
func (s *SettingService) invalidateClaudeCalibratedProfileCache() {
	if s == nil {
		return
	}
	s.claudeCalibratedProfileSF.Forget(claudeCalibratedProfileSFKey)
	s.claudeCalibratedProfileCache.Store(&cachedClaudeCalibratedProfile{profile: nil, expiresAt: 0})
}
