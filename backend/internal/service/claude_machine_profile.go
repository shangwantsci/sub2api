package service

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
)

// 机器档案写在账号 extra 里，创建时生成一次，之后不换。
// 平台固定为 linux，与出站 X-Stainless-OS 的校准值一致。
const (
	extraClaudeDeviceID     = "claude_device_id"
	extraClaudeEnvPlatform  = "claude_env_platform"
	extraClaudeEnvShell     = "claude_env_shell"
	extraClaudeEnvOSVersion = "claude_env_os_version"
	extraClaudeEnvHome      = "claude_env_home"

	claudeMachinePlatform = "linux"
)

var claudeMachineProfileKeys = []string{
	extraClaudeDeviceID,
	extraClaudeEnvPlatform,
	extraClaudeEnvShell,
	extraClaudeEnvOSVersion,
	extraClaudeEnvHome,
}

var claudeMachineShells = []string{"bash", "zsh"}

var claudeMachineOSVersions = []string{
	"Linux 6.8.0-generic",
	"Linux 6.6.0-generic",
	"Linux 6.1.0-generic",
	"Linux 5.15.0-generic",
}

var claudeMachineUsers = []string{
	"alex", "sam", "jordan", "taylor", "casey", "morgan",
	"riley", "drew", "quinn", "avery", "jamie", "cameron",
}

// ClaudeMachineProfile 是一个订阅账号对外呈现的固定电脑。
type ClaudeMachineProfile struct {
	DeviceID  string
	Platform  string
	Shell     string
	OSVersion string
	Home      string
}

// WorkingDir 是提示词里的工作目录，家目录下的 projects。
func (p ClaudeMachineProfile) WorkingDir() string {
	return strings.TrimRight(p.Home, "/") + "/projects"
}

func (p ClaudeMachineProfile) extraPatch() map[string]any {
	return map[string]any{
		extraClaudeDeviceID:     p.DeviceID,
		extraClaudeEnvPlatform:  p.Platform,
		extraClaudeEnvShell:     p.Shell,
		extraClaudeEnvOSVersion: p.OSVersion,
		extraClaudeEnvHome:      p.Home,
	}
}

func isClaudeDeviceID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

func validClaudeShell(shell string) bool {
	return shell == "bash" || shell == "zsh"
}

func validClaudeOSVersion(version string) bool {
	version = strings.TrimSpace(version)
	return strings.HasPrefix(version, "Linux ") &&
		!strings.ContainsAny(version, "\r\n") &&
		len(version) <= 80
}

func validClaudeHome(home string) bool {
	const prefix = "/home/"
	if !strings.HasPrefix(home, prefix) || strings.ContainsAny(home, "\r\n ") {
		return false
	}
	name := home[len(prefix):]
	if len(name) < 2 || len(name) > 20 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// ParseClaudeMachineProfile 读取已经写好的档案。缺字段或值不合法时返回 false。
func ParseClaudeMachineProfile(extra map[string]any) (ClaudeMachineProfile, bool) {
	p := ClaudeMachineProfile{
		DeviceID:  strings.ToLower(strings.TrimSpace(extraString(extra, extraClaudeDeviceID))),
		Platform:  strings.TrimSpace(extraString(extra, extraClaudeEnvPlatform)),
		Shell:     strings.TrimSpace(extraString(extra, extraClaudeEnvShell)),
		OSVersion: strings.TrimSpace(extraString(extra, extraClaudeEnvOSVersion)),
		Home:      strings.TrimSpace(extraString(extra, extraClaudeEnvHome)),
	}
	if !isClaudeDeviceID(p.DeviceID) || p.Platform != claudeMachinePlatform ||
		!validClaudeShell(p.Shell) || !validClaudeOSVersion(p.OSVersion) || !validClaudeHome(p.Home) {
		return ClaudeMachineProfile{}, false
	}
	return p, true
}

func extraString(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	v, ok := extra[key].(string)
	if !ok {
		return ""
	}
	return v
}

func indexFromDeviceID(deviceID, salt string, n int) int {
	sum := 0
	for i := 0; i < len(deviceID); i++ {
		sum = sum*33 + int(deviceID[i])
	}
	for i := 0; i < len(salt); i++ {
		sum = sum*33 + int(salt[i])
	}
	if sum < 0 {
		sum = -sum
	}
	return sum % n
}

// CompleteClaudeMachineProfile 补齐缺的字段。已有的合法设备号保持不变；
// adoptDeviceID 只在账号上还没有设备号时使用（通常是 Redis 里已有的 ClientID）。
func CompleteClaudeMachineProfile(extra map[string]any, adoptDeviceID string) ClaudeMachineProfile {
	if p, ok := ParseClaudeMachineProfile(extra); ok {
		return p
	}
	p := ClaudeMachineProfile{
		DeviceID:  strings.ToLower(strings.TrimSpace(extraString(extra, extraClaudeDeviceID))),
		Platform:  claudeMachinePlatform,
		Shell:     strings.TrimSpace(extraString(extra, extraClaudeEnvShell)),
		OSVersion: strings.TrimSpace(extraString(extra, extraClaudeEnvOSVersion)),
		Home:      strings.TrimSpace(extraString(extra, extraClaudeEnvHome)),
	}
	if !isClaudeDeviceID(p.DeviceID) {
		adopt := strings.ToLower(strings.TrimSpace(adoptDeviceID))
		if isClaudeDeviceID(adopt) {
			p.DeviceID = adopt
		} else {
			p.DeviceID = generateClientID()
		}
	}
	if !validClaudeShell(p.Shell) {
		p.Shell = claudeMachineShells[indexFromDeviceID(p.DeviceID, "shell", len(claudeMachineShells))]
	}
	if !validClaudeOSVersion(p.OSVersion) {
		p.OSVersion = claudeMachineOSVersions[indexFromDeviceID(p.DeviceID, "os", len(claudeMachineOSVersions))]
	}
	if !validClaudeHome(p.Home) {
		user := claudeMachineUsers[indexFromDeviceID(p.DeviceID, "user", len(claudeMachineUsers))]
		p.Home = "/home/" + user
	}
	return p
}

func applyClaudeMachineProfile(extra map[string]any) map[string]any {
	if _, ok := ParseClaudeMachineProfile(extra); ok {
		return extra
	}
	if extra == nil {
		extra = map[string]any{}
	}
	for k, v := range CompleteClaudeMachineProfile(extra, "").extraPatch() {
		extra[k] = v
	}
	return extra
}

// preserveClaudeMachineProfile 在整列替换 extra 时把没带上的档案键补回去。
func preserveClaudeMachineProfile(current, next map[string]any) {
	if current == nil || next == nil {
		return
	}
	for _, key := range claudeMachineProfileKeys {
		if v, ok := next[key].(string); ok && strings.TrimSpace(v) != "" {
			continue
		}
		if v, ok := current[key]; ok {
			next[key] = v
		}
	}
}

// ensureClaudeMachineProfile 保证 Anthropic OAuth / Setup Token 账号有一份档案。
// 已有档案直接返回。缺了就补写；写入失败时不改账号，调用方跳过环境替换。
func (s *GatewayService) ensureClaudeMachineProfile(ctx context.Context, account *Account) (ClaudeMachineProfile, bool) {
	if account == nil || !account.IsAnthropicOAuthOrSetupToken() {
		return ClaudeMachineProfile{}, false
	}
	if p, ok := ParseClaudeMachineProfile(account.Extra); ok {
		return p, true
	}
	adopt := ""
	if s != nil && s.identityService != nil {
		adopt = s.identityService.CachedClientID(ctx, account.ID)
	}
	p := CompleteClaudeMachineProfile(account.Extra, adopt)
	patch := map[string]any{}
	previous := map[string]any{}
	for k, v := range p.extraPatch() {
		var prev string
		exists := false
		if account.Extra != nil {
			if raw, ok := account.Extra[k]; ok {
				exists = true
				previous[k] = raw
				if s, ok := raw.(string); ok {
					prev = s
				}
			}
		}
		vs, _ := v.(string)
		if exists && prev == vs {
			continue
		}
		patch[k] = v
	}
	if len(patch) == 0 {
		return p, true
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	for k, v := range patch {
		account.Extra[k] = v
	}
	if s == nil || s.accountRepo == nil || account.ID <= 0 {
		rollbackClaudeMachinePatch(account.Extra, patch, previous)
		return ClaudeMachineProfile{}, false
	}
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, patch); err != nil {
		slog.Warn("claude_machine_profile_persist_failed", "account_id", account.ID, "error", err)
		rollbackClaudeMachinePatch(account.Extra, patch, previous)
		return ClaudeMachineProfile{}, false
	}
	return p, true
}

func rollbackClaudeMachinePatch(extra map[string]any, patch, previous map[string]any) {
	for k := range patch {
		if prev, ok := previous[k]; ok {
			extra[k] = prev
			continue
		}
		delete(extra, k)
	}
}

func alignClaudeDeviceID(ctx context.Context, s *GatewayService, account *Account, fp *Fingerprint) {
	if s == nil || account == nil || fp == nil {
		return
	}
	deviceID := strings.ToLower(strings.TrimSpace(account.GetExtraString(extraClaudeDeviceID)))
	if s.identityService == nil {
		if isClaudeDeviceID(deviceID) {
			fp.ClientID = deviceID
		}
		return
	}
	s.identityService.AlignClientID(ctx, account.ID, fp, deviceID)
}

func pinClaudeMachineHeaders(req *http.Request) {
	if req == nil {
		return
	}
	setHeaderRaw(req.Header, "X-Stainless-OS", "Linux")
	setHeaderRaw(req.Header, "X-Stainless-Arch", "x64")
}
