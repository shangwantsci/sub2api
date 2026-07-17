package claude

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// CalibratedProfileSchemaVersion 是本仓能解析的标定 profile JSON 结构版本。
// tools/cc-calibrate 产出的 profile 必须带相同 schema_version，否则 loader 拒绝加载
// 并回退到编译内置常量（constants.go）。升级结构时同步递增两侧。
const CalibratedProfileSchemaVersion = 1

// 标定 beta_rules 的 endpoint 维度。真身 /v1/messages 与 /v1/messages/count_tokens
// 的 anthropic-beta 集合不同，标定时分别归档，loader 按端点各取一套。
const (
	CalibratedEndpointMessages    = "messages"
	CalibratedEndpointCountTokens = "count_tokens"
)

// CalibratedProfile 是 tools/cc-calibrate 对真身 Claude Code CLI 抓包标定所得的
// 机器可读 profile。网关在启动/热加载时读取它，覆盖 constants.go 里所有硬编码的
// 出站字节（headers / anthropic-beta / cc_version），使"升级 CLI → 重跑标定 →
// 发布 profile"即可零改代码、零重部署地刷新伪装字节。
//
// 字段与 tools/cc-calibrate/extract-profile.js 的输出严格对齐。
type CalibratedProfile struct {
	SchemaVersion int                 `json:"schema_version"`
	CLIVersion    string              `json:"cli_version"`
	CapturedAt    string              `json:"captured_at"`
	Source        string              `json:"source"`
	Headers       CalibratedHeaders   `json:"headers"`
	BetaRules     map[string][]string `json:"beta_rules"`
	Guard         CalibratedGuard     `json:"guard"`
}

// CalibratedHeaders 是标定抓到的请求头模板与"真身不发"的头列表。
type CalibratedHeaders struct {
	// Template 是真身对所有模型统一发送的一组请求头（UA / x-stainless-* / x-app 等）。
	Template map[string]string `json:"template"`
	// Absent 是真身根本不发送的头（如 x-client-request-id）。loader 会主动删除它们，
	// 杜绝过度伪造构成的第三方特征。
	Absent []string `json:"absent"`
}

// CalibratedGuard 是指纹回归守卫的结果。SaltVerified=false 表示 salt/索引算法漂移，
// loader 必须拒绝该 profile（否则会把错误的 cc_version 指纹批量发出去导致封号）。
type CalibratedGuard struct {
	SaltVerified bool `json:"salt_verified"`
	Checked      int  `json:"checked"`
	OK           int  `json:"ok"`
}

// ParseCalibratedProfile 解析并校验一个标定 profile JSON。校验不通过返回错误，
// 调用方据此回退到编译内置常量。
func ParseCalibratedProfile(data []byte) (*CalibratedProfile, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("empty calibrated profile")
	}
	var p CalibratedProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse calibrated profile: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate 校验 profile 的自洽性。任何一条不满足都拒绝加载：
//   - schema_version 与本仓一致
//   - cc_version 是三段 semver
//   - headers.template.User-Agent 存在且包含 cc_version（UA 版本必须与 billing
//     cc_version 一致，否则被上游判第三方——这是最致命的漂移点）
//   - 指纹守卫通过（salt_verified=true 且至少校验过一次）
//   - 至少有一条 beta 规则
func (p *CalibratedProfile) Validate() error {
	if p == nil {
		return fmt.Errorf("nil calibrated profile")
	}
	if p.SchemaVersion != CalibratedProfileSchemaVersion {
		return fmt.Errorf("unsupported calibrated profile schema_version=%d (want %d)", p.SchemaVersion, CalibratedProfileSchemaVersion)
	}
	version := strings.TrimSpace(p.CLIVersion)
	if !isThreeSegmentSemver(version) {
		return fmt.Errorf("invalid calibrated cli_version %q (want X.Y.Z)", p.CLIVersion)
	}
	ua := strings.TrimSpace(p.Headers.Template[headerKeyUserAgent(p.Headers.Template)])
	if ua == "" {
		return fmt.Errorf("calibrated headers.template missing User-Agent")
	}
	if !strings.Contains(ua, version) {
		return fmt.Errorf("calibrated User-Agent %q does not contain cli_version %q (version drift would be flagged as third-party)", ua, version)
	}
	if !p.Guard.SaltVerified || p.Guard.Checked == 0 {
		return fmt.Errorf("calibrated fingerprint guard not verified (salt_verified=%v checked=%d); refusing to load", p.Guard.SaltVerified, p.Guard.Checked)
	}
	if len(p.BetaRules) == 0 {
		return fmt.Errorf("calibrated profile has no beta_rules")
	}
	return nil
}

// HeaderTemplate 返回请求头模板的一份拷贝（key 保持标定原始形态，应用时由调用方
// 走 resolveWireCasing 归一到 wire 形态）。
func (p *CalibratedProfile) HeaderTemplate() map[string]string {
	if p == nil {
		return nil
	}
	return cloneStringMap(p.Headers.Template)
}

// AbsentHeaders 返回真身不发送、应主动删除的头列表拷贝。
func (p *CalibratedProfile) AbsentHeaders() []string {
	if p == nil {
		return nil
	}
	return cloneStrings(p.Headers.Absent)
}

// Version 返回标定的 cc_version。
func (p *CalibratedProfile) Version() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.CLIVersion)
}

// UserAgent 返回标定 headers 模板里的 User-Agent。
func (p *CalibratedProfile) UserAgent() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.Headers.Template[headerKeyUserAgent(p.Headers.Template)])
}

// MessageBetas 解析 /v1/messages 端点、指定模型与请求特性下的 anthropic-beta 集合。
// 命中返回 (betas, true)；未标定到任何可用档位返回 (nil, false)，调用方回退到常量。
func (p *CalibratedProfile) MessageBetas(modelID string, hasTools, hasJSONSchema bool) ([]string, bool) {
	return p.lookupBetas(CalibratedEndpointMessages, modelID, hasTools, hasJSONSchema)
}

// CountTokensBetas 解析 /v1/messages/count_tokens 端点的 anthropic-beta 集合。
// count_tokens 不回退到 messages 端点（两者集合不同），未命中返回 (nil, false)。
func (p *CalibratedProfile) CountTokensBetas(modelID string, hasTools, hasJSONSchema bool) ([]string, bool) {
	return p.lookupBetas(CalibratedEndpointCountTokens, modelID, hasTools, hasJSONSchema)
}

// lookupBetas 在 profile 内部按"精确特性 → 逐步放宽 → 同族基线"回退链查找 beta 集合，
// 端点之间互不回退。
func (p *CalibratedProfile) lookupBetas(endpoint, modelID string, hasTools, hasJSONSchema bool) ([]string, bool) {
	if p == nil || len(p.BetaRules) == 0 {
		return nil, false
	}
	family := CalibratedFamilyOf(modelID)
	for _, feat := range calibratedFeatureCandidates(hasTools, hasJSONSchema) {
		key := endpoint + "|" + family + "|" + feat
		if betas, ok := p.BetaRules[key]; ok && len(betas) > 0 {
			return cloneStrings(betas), true
		}
	}
	return nil, false
}

// CalibratedFamilyOf 把模型 ID 归一到标定用的模型族，与 extract-profile.js 的 familyOf 对齐。
func CalibratedFamilyOf(modelID string) string {
	s := strings.ToLower(strings.TrimSpace(NormalizeModelID(modelID)))
	switch {
	case strings.Contains(s, "haiku"):
		return "haiku"
	case strings.Contains(s, "fable"):
		return "fable"
	case strings.Contains(s, "sonnet"):
		return "sonnet"
	default:
		return "opus"
	}
}

// calibratedFeatureCandidates 返回按优先级排序的特性 key 候选列表（越靠前越精确）。
// 与 extract-profile.js 的 featuresOf().sort().join('+') 对齐（"json_schema"<"tools"）。
func calibratedFeatureCandidates(hasTools, hasJSONSchema bool) []string {
	out := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	add := func(feats ...string) {
		sort.Strings(feats)
		key := strings.Join(feats, "+")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	switch {
	case hasTools && hasJSONSchema:
		add("json_schema", "tools")
		add("tools")
		add("json_schema")
	case hasTools:
		add("tools")
	case hasJSONSchema:
		add("json_schema")
	}
	add() // 基线（无特性），永远兜底最后一档
	return out
}

// headerKeyUserAgent 在 template 里大小写不敏感地找到 User-Agent 的实际 key。
func headerKeyUserAgent(template map[string]string) string {
	for k := range template {
		if strings.EqualFold(k, "User-Agent") {
			return k
		}
	}
	return "User-Agent"
}

// isThreeSegmentSemver 判断字符串是否形如 X.Y.Z（三段纯数字）。
func isThreeSegmentSemver(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
