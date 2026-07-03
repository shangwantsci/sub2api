package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

const (
	ContentSafetyGuardModeOff   = "off"
	ContentSafetyGuardModeWarn  = "warn"
	ContentSafetyGuardModeBlock = "block"

	ContentSafetyActionSkip    = "skip"
	ContentSafetyActionAllow   = "allow"
	ContentSafetyActionObserve = "observe"
	ContentSafetyActionWarn    = "warn"
	ContentSafetyActionBlock   = "block"

	ContentSafetyConfidenceLow    = "low"
	ContentSafetyConfidenceMedium = "medium"
	ContentSafetyConfidenceHigh   = "high"

	ContentSafetySeverityMedium   = "medium"
	ContentSafetySeverityHigh     = "high"
	ContentSafetySeverityCritical = "critical"

	ContentSafetyCategoryIllegalActivity = "illegal_activity"
	ContentSafetyCategoryCyberAbuse      = "cyber_abuse"
	ContentSafetyCategoryWeapons         = "weapons_dangerous_materials"
	ContentSafetyCategoryViolenceHate    = "violence_hate_extremism"
	ContentSafetyCategoryPrivacyAbuse    = "privacy_identity_abuse"
	ContentSafetyCategoryChildSafety     = "child_safety"
	ContentSafetyCategorySelfHarm        = "self_harm"
	ContentSafetyCategoryFraud           = "fraud_misinformation"
	ContentSafetyCategorySexualExplicit  = "sexual_explicit"
	ContentSafetyCategoryPolicyBypass    = "policy_bypass"
)

const (
	contentSafetyBlockMessage     = "请求违反使用政策，已被拦截。"
	contentSafetySettingsCacheTTL = 10 * time.Second
)

type ContentSafetyGuardSettings struct {
	Enabled             bool
	Mode                string
	LogRedactedEvidence bool
}

type ContentSafetyGuard struct {
	settingService   *SettingService
	settingsCache    atomic.Value
	settingsMu       sync.Mutex
	settingsCacheTTL time.Duration
	now              func() time.Time
}

type contentSafetySettingsCacheEntry struct {
	settings  ContentSafetyGuardSettings
	expiresAt time.Time
}

type ContentSafetyInput struct {
	Protocol string
	Body     []byte
}

type ContentSafetyCheckInput struct {
	Protocol string
	Body     []byte
}

type ContentSafetyFinding struct {
	Category   string                `json:"category"`
	Severity   string                `json:"severity"`
	Confidence string                `json:"confidence,omitempty"`
	Action     string                `json:"action,omitempty"`
	Reason     string                `json:"reason"`
	Evidence   ContentSafetyEvidence `json:"evidence,omitempty"`
}

type ContentSafetyEvidence struct {
	Source  string `json:"source,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
	Hash    string `json:"hash,omitempty"`
}

type ContentSafetyDecision struct {
	Allowed             bool                   `json:"allowed"`
	Blocked             bool                   `json:"blocked"`
	Flagged             bool                   `json:"flagged"`
	Action              string                 `json:"action"`
	Mode                string                 `json:"mode"`
	Message             string                 `json:"message"`
	PrimaryFinding      ContentSafetyFinding   `json:"primary_finding"`
	Findings            []ContentSafetyFinding `json:"findings"`
	LogRedactedEvidence bool                   `json:"-"`
}

type contentSafetyTextFragment struct {
	Source string
	Text   string
}

func NewContentSafetyGuard(settingService *SettingService) *ContentSafetyGuard {
	return &ContentSafetyGuard{
		settingService:   settingService,
		settingsCacheTTL: contentSafetySettingsCacheTTL,
		now:              time.Now,
	}
}

func DefaultContentSafetyGuardSettings() ContentSafetyGuardSettings {
	return ContentSafetyGuardSettings{
		Enabled:             true,
		Mode:                ContentSafetyGuardModeBlock,
		LogRedactedEvidence: true,
	}
}

func NormalizeContentSafetyGuardMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ContentSafetyGuardModeOff:
		return ContentSafetyGuardModeOff
	case ContentSafetyGuardModeWarn:
		return ContentSafetyGuardModeWarn
	case ContentSafetyGuardModeBlock, "":
		return ContentSafetyGuardModeBlock
	default:
		return ContentSafetyGuardModeBlock
	}
}

func ContentSafetyCategoryLabel(category string) string {
	switch category {
	case ContentSafetyCategoryIllegalActivity:
		return "违法活动或受管制交易"
	case ContentSafetyCategoryCyberAbuse:
		return "未授权网络攻击或恶意代码"
	case ContentSafetyCategoryWeapons:
		return "武器、爆炸物或危险材料"
	case ContentSafetyCategoryViolenceHate:
		return "暴力、仇恨或极端主义"
	case ContentSafetyCategoryPrivacyAbuse:
		return "隐私侵犯、身份滥用或冒充"
	case ContentSafetyCategoryChildSafety:
		return "未成年人安全风险"
	case ContentSafetyCategorySelfHarm:
		return "自伤、自杀或有害身心行为"
	case ContentSafetyCategoryFraud:
		return "欺诈、钓鱼、伪造或误导性信息"
	case ContentSafetyCategorySexualExplicit:
		return "露骨性内容"
	case ContentSafetyCategoryPolicyBypass:
		return "规避安全策略或平台限制"
	default:
		return "内容安全风险"
	}
}

func (s *SettingService) GetContentSafetyGuardSettings(ctx context.Context) ContentSafetyGuardSettings {
	settings, err := s.loadContentSafetyGuardSettings(ctx)
	if err != nil {
		return DefaultContentSafetyGuardSettings()
	}
	return settings
}

func (s *SettingService) loadContentSafetyGuardSettings(ctx context.Context) (ContentSafetyGuardSettings, error) {
	settings := DefaultContentSafetyGuardSettings()
	if s == nil || s.settingRepo == nil {
		return settings, nil
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyEnableContentSafetyFilter,
		SettingKeyContentSafetyGuardMode,
		SettingKeyContentSafetyLogRedactedEvidence,
	})
	if err != nil {
		return settings, err
	}
	if raw, ok := values[SettingKeyEnableContentSafetyFilter]; ok && strings.TrimSpace(raw) != "" {
		settings.Enabled = strings.TrimSpace(raw) == "true"
	}
	settings.Mode = NormalizeContentSafetyGuardMode(values[SettingKeyContentSafetyGuardMode])
	if raw, ok := values[SettingKeyContentSafetyLogRedactedEvidence]; ok && strings.TrimSpace(raw) != "" {
		settings.LogRedactedEvidence = !isFalseSettingValue(raw)
	}
	return settings, nil
}

func (g *ContentSafetyGuard) Check(ctx context.Context, input ContentSafetyCheckInput) *ContentSafetyDecision {
	if g == nil {
		return &ContentSafetyDecision{Allowed: true, Action: ContentSafetyActionSkip}
	}
	settings := g.cachedContentSafetyGuardSettings(ctx)
	settings.Mode = NormalizeContentSafetyGuardMode(settings.Mode)
	if !settings.Enabled || settings.Mode == ContentSafetyGuardModeOff {
		return &ContentSafetyDecision{Allowed: true, Action: ContentSafetyActionSkip, Mode: settings.Mode}
	}
	decision := EvaluateContentSafety(ContentSafetyInput{Protocol: input.Protocol, Body: input.Body})
	applyContentSafetyDecisionMode(decision, settings.Mode)
	decision.LogRedactedEvidence = settings.LogRedactedEvidence
	return decision
}

func (g *ContentSafetyGuard) cachedContentSafetyGuardSettings(ctx context.Context) ContentSafetyGuardSettings {
	if g == nil {
		return DefaultContentSafetyGuardSettings()
	}
	now := time.Now
	if g.now != nil {
		now = g.now
	}
	currentTime := now()
	if cached, ok := g.loadCachedContentSafetyGuardSettings(); ok && currentTime.Before(cached.expiresAt) {
		return cached.settings
	}

	g.settingsMu.Lock()
	defer g.settingsMu.Unlock()

	if cached, ok := g.loadCachedContentSafetyGuardSettings(); ok && currentTime.Before(cached.expiresAt) {
		return cached.settings
	}

	settings := DefaultContentSafetyGuardSettings()
	var err error
	if g.settingService != nil {
		settings, err = g.settingService.loadContentSafetyGuardSettings(ctx)
	}
	if err != nil {
		if cached, ok := g.loadCachedContentSafetyGuardSettings(); ok {
			return cached.settings
		}
		return DefaultContentSafetyGuardSettings()
	}
	settings.Mode = NormalizeContentSafetyGuardMode(settings.Mode)

	ttl := g.settingsCacheTTL
	if ttl <= 0 {
		ttl = contentSafetySettingsCacheTTL
	}
	g.settingsCache.Store(contentSafetySettingsCacheEntry{
		settings:  settings,
		expiresAt: currentTime.Add(ttl),
	})
	return settings
}

func (g *ContentSafetyGuard) loadCachedContentSafetyGuardSettings() (contentSafetySettingsCacheEntry, bool) {
	if g == nil {
		return contentSafetySettingsCacheEntry{}, false
	}
	raw := g.settingsCache.Load()
	if raw == nil {
		return contentSafetySettingsCacheEntry{}, false
	}
	cached, ok := raw.(contentSafetySettingsCacheEntry)
	return cached, ok
}

func EvaluateContentSafety(input ContentSafetyInput) *ContentSafetyDecision {
	var findings []ContentSafetyFinding
	for _, fragment := range extractContentSafetyTextFragments(input.Protocol, input.Body) {
		findings = mergeContentSafetyFindings(findings, classifyContentSafetyFragment(fragment))
	}
	decision := &ContentSafetyDecision{
		Allowed:  true,
		Action:   ContentSafetyActionAllow,
		Findings: findings,
	}
	if len(findings) == 0 {
		return decision
	}
	applyContentSafetyDecisionMode(decision, ContentSafetyGuardModeBlock)
	return decision
}

func applyContentSafetyDecisionMode(decision *ContentSafetyDecision, mode string) {
	if decision == nil {
		return
	}
	mode = NormalizeContentSafetyGuardMode(mode)
	decision.Mode = mode
	if len(decision.Findings) == 0 || mode == ContentSafetyGuardModeOff {
		decision.Allowed = true
		decision.Blocked = false
		decision.Flagged = false
		if mode == ContentSafetyGuardModeOff {
			decision.Action = ContentSafetyActionSkip
		} else {
			decision.Action = ContentSafetyActionAllow
		}
		decision.Message = ""
		decision.PrimaryFinding = ContentSafetyFinding{}
		return
	}

	for i := range decision.Findings {
		decision.Findings[i].Action = findingActionFor(mode, decision.Findings[i])
	}
	primary := primaryContentSafetyFinding(decision.Findings)
	decision.PrimaryFinding = primary
	decision.Flagged = true
	decision.Action = primary.Action
	decision.Blocked = primary.Action == ContentSafetyActionBlock
	decision.Allowed = !decision.Blocked
	if decision.Blocked {
		decision.Message = contentSafetyBlockMessage
	} else {
		decision.Message = ""
	}
}

func findingActionFor(mode string, finding ContentSafetyFinding) string {
	if mode == ContentSafetyGuardModeOff {
		return ContentSafetyActionSkip
	}
	switch finding.Confidence {
	case ContentSafetyConfidenceLow:
		return ContentSafetyActionObserve
	case ContentSafetyConfidenceMedium:
		return ContentSafetyActionWarn
	case ContentSafetyConfidenceHigh:
		if mode == ContentSafetyGuardModeWarn {
			return ContentSafetyActionWarn
		}
		return ContentSafetyActionBlock
	default:
		if mode == ContentSafetyGuardModeWarn {
			return ContentSafetyActionWarn
		}
		return ContentSafetyActionBlock
	}
}

func ExtractContentSafetyText(protocol string, body []byte) string {
	return normalizeContentModerationText(strings.Join(ExtractContentSafetyTextFragments(protocol, body), "\n"))
}

func ExtractContentSafetyTextFragments(protocol string, body []byte) []string {
	fragments := extractContentSafetyTextFragments(protocol, body)
	out := make([]string, 0, len(fragments))
	for _, fragment := range fragments {
		out = append(out, fragment.Text)
	}
	return out
}

func extractContentSafetyTextFragments(protocol string, body []byte) []contentSafetyTextFragment {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return nil
	}
	var parts []contentSafetyTextFragment
	switch protocol {
	case ContentModerationProtocolAnthropicMessages:
		collectContentSafetyValue(gjson.GetBytes(body, "system"), "system", &parts)
		collectAnthropicContentSafetyMessages(gjson.GetBytes(body, "messages"), &parts)
	case ContentModerationProtocolOpenAIChat:
		collectOpenAIChatContentSafetyMessages(gjson.GetBytes(body, "messages"), &parts)
	case ContentModerationProtocolOpenAIResponses:
		collectContentSafetyValue(gjson.GetBytes(body, "instructions"), "instructions", &parts)
		collectResponsesContentSafetyInput(gjson.GetBytes(body, "input"), &parts)
	default:
		collectContentSafetyValue(gjson.GetBytes(body, "system"), "system", &parts)
		collectContentSafetyValue(gjson.GetBytes(body, "instructions"), "instructions", &parts)
		collectAnthropicContentSafetyMessages(gjson.GetBytes(body, "messages"), &parts)
		collectOpenAIChatContentSafetyMessages(gjson.GetBytes(body, "messages"), &parts)
		collectResponsesContentSafetyInput(gjson.GetBytes(body, "input"), &parts)
	}
	out := make([]contentSafetyTextFragment, 0, len(parts))
	for _, part := range parts {
		part.Text = normalizeContentModerationText(part.Text)
		if part.Text != "" {
			out = append(out, part)
		}
	}
	return out
}

func mergeContentSafetyFindings(existing []ContentSafetyFinding, additions []ContentSafetyFinding) []ContentSafetyFinding {
	for _, addition := range additions {
		seen := false
		for _, current := range existing {
			if current.Category == addition.Category {
				seen = true
				break
			}
		}
		if !seen {
			existing = append(existing, addition)
		}
	}
	return existing
}

func collectAnthropicContentSafetyMessages(messages gjson.Result, parts *[]contentSafetyTextFragment) {
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(index, item gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
		if role == "user" || role == "system" || role == "tool" {
			collectContentSafetyValue(item.Get("content"), fmt.Sprintf("messages[%s].content", index.String()), parts)
		}
		return true
	})
}

func collectOpenAIChatContentSafetyMessages(messages gjson.Result, parts *[]contentSafetyTextFragment) {
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(index, item gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
		switch role {
		case "user", "system", "developer", "tool":
			collectContentSafetyValue(item.Get("content"), fmt.Sprintf("messages[%s].content", index.String()), parts)
		}
		return true
	})
}

func collectResponsesContentSafetyInput(input gjson.Result, parts *[]contentSafetyTextFragment) {
	switch {
	case !input.Exists():
		return
	case input.Type == gjson.String:
		addContentSafetyText(parts, "input", input.String())
	case input.IsArray():
		input.ForEach(func(index, item gjson.Result) bool {
			collectResponsesContentSafetyItem(item, fmt.Sprintf("input[%s]", index.String()), parts)
			return true
		})
	case input.IsObject():
		collectResponsesContentSafetyItem(input, "input", parts)
	}
}

func collectResponsesContentSafetyItem(item gjson.Result, source string, parts *[]contentSafetyTextFragment) {
	role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
	typ := strings.ToLower(strings.TrimSpace(item.Get("type").String()))
	if role == "assistant" {
		return
	}
	if role == "" && typ != "input_text" && typ != "message" && typ != "function_call_output" {
		return
	}
	collectContentSafetyValue(item.Get("content"), source+".content", parts)
	collectContentSafetyValue(item.Get("text"), source+".text", parts)
	collectContentSafetyValue(item.Get("output"), source+".output", parts)
}

func collectContentSafetyValue(value gjson.Result, source string, parts *[]contentSafetyTextFragment) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		addContentSafetyText(parts, source, value.String())
	case value.IsArray():
		value.ForEach(func(index, item gjson.Result) bool {
			collectContentSafetyValue(item, fmt.Sprintf("%s[%s]", source, index.String()), parts)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		switch typ {
		case "image", "input_image", "image_url":
			return
		}
		collectContentSafetyValue(value.Get("text"), source+".text", parts)
		collectContentSafetyValue(value.Get("content"), source+".content", parts)
		collectContentSafetyValue(value.Get("output"), source+".output", parts)
	}
}

func addContentSafetyText(parts *[]contentSafetyTextFragment, source string, text string) {
	text = strings.TrimSpace(text)
	if text == "" || isAnthropicSystemReminderText(text) {
		return
	}
	*parts = append(*parts, contentSafetyTextFragment{Source: source, Text: text})
}

func classifyContentSafetyText(text string) []ContentSafetyFinding {
	return classifyContentSafetyFragment(contentSafetyTextFragment{Text: text})
}

func classifyContentSafetyFragment(fragment contentSafetyTextFragment) []ContentSafetyFinding {
	text := fragment.Text
	text = normalizeContentModerationText(text)
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	findings := make([]ContentSafetyFinding, 0, 4)
	add := func(category string, severity string, confidence string, reason string) {
		for _, existing := range findings {
			if existing.Category == category {
				return
			}
		}
		evidence := ContentSafetyEvidence{}
		if confidence != ContentSafetyConfidenceLow {
			evidence = contentSafetyEvidenceForFragment(category, contentSafetyTextFragment{
				Source: fragment.Source,
				Text:   text,
			})
		}
		findings = append(findings, ContentSafetyFinding{
			Category:   category,
			Severity:   severity,
			Confidence: confidence,
			Action:     findingActionFor(ContentSafetyGuardModeBlock, ContentSafetyFinding{Confidence: confidence}),
			Reason:     reason,
			Evidence:   evidence,
		})
	}

	if hasChildSexualSafetyRisk(lower) {
		add(ContentSafetyCategoryChildSafety, ContentSafetySeverityCritical, ContentSafetyConfidenceHigh, "child sexual exploitation or sexualization")
	} else if hasChildSexualSafetyObserveRisk(lower) {
		add(ContentSafetyCategoryChildSafety, ContentSafetySeverityMedium, ContentSafetyConfidenceLow, "ambiguous child-safety term in benign technical context")
	}
	if hasPolicyBypassRisk(lower) {
		add(ContentSafetyCategoryPolicyBypass, ContentSafetySeverityHigh, ContentSafetyConfidenceHigh, "platform or safety rule bypass request")
	}
	if hasSexualExplicitRisk(lower) && !hasLegitimateSexualEducationContext(lower) {
		add(ContentSafetyCategorySexualExplicit, ContentSafetySeverityHigh, ContentSafetyConfidenceHigh, "explicit sexual generation request")
	}
	if hasFraudWarningRisk(lower) {
		add(ContentSafetyCategoryFraud, ContentSafetySeverityMedium, ContentSafetyConfidenceMedium, "ambiguous fraud or deception training context")
	}
	if hasFraudRisk(lower) && !hasBenignAnalysisContext(lower) && !hasFraudWarningRisk(lower) {
		add(ContentSafetyCategoryFraud, ContentSafetySeverityHigh, ContentSafetyConfidenceHigh, "fraud, phishing, forgery, or coordinated deception")
	}
	if hasIllegalActivityRisk(lower) && !hasBenignAnalysisContext(lower) {
		add(ContentSafetyCategoryIllegalActivity, ContentSafetySeverityHigh, ContentSafetyConfidenceHigh, "illegal transaction or evasion request")
	}
	if hasCyberAbuseRisk(lower) && !hasDefensiveCyberContext(lower) {
		add(ContentSafetyCategoryCyberAbuse, ContentSafetySeverityHigh, ContentSafetyConfidenceHigh, "unauthorized cyber abuse request")
	}
	if hasWeaponsRisk(lower) && !hasBenignAnalysisContext(lower) {
		add(ContentSafetyCategoryWeapons, ContentSafetySeverityHigh, ContentSafetyConfidenceHigh, "weapon or dangerous material instructions")
	}
	if hasPrivacyAbuseRisk(lower) && !hasBenignPrivacyContext(lower) {
		add(ContentSafetyCategoryPrivacyAbuse, ContentSafetySeverityHigh, ContentSafetyConfidenceHigh, "privacy or identity abuse request")
	}
	if hasSelfHarmRisk(lower) && !hasSelfHarmSupportContext(lower) {
		add(ContentSafetyCategorySelfHarm, ContentSafetySeverityHigh, ContentSafetyConfidenceHigh, "self-harm encouragement or instructions")
	}
	if hasViolenceHateRisk(lower) && !hasBenignAnalysisContext(lower) {
		add(ContentSafetyCategoryViolenceHate, ContentSafetySeverityHigh, ContentSafetyConfidenceHigh, "violent, hateful, or extremist request")
	}

	return findings
}

func hasChildSexualSafetyRisk(text string) bool {
	childTerms := []string{"未成年", "未成年人", "儿童", "小孩", "幼女", "幼男", "萝莉", "正太", "child", "children", "minor", "underage", "teen", "teenage"}
	sexualTerms := []string{"色情", "性化", "性行为", "性交", "裸照", "裸体", "露骨", "csam", "porn", "pornographic", "pornography", "sexual", "sexualized", "nude", "explicit sexual", "sexually explicit", "sexual content"}
	if containsAny(text, []string{"csam"}) {
		return true
	}
	for _, window := range contentSafetyWindowsAroundWithTerms(text, childTerms, 80, 140) {
		if containsAny(window.Text, sexualTerms) && !isBenignContext(ContentSafetyCategoryChildSafety, window.Term, window.Text) {
			return true
		}
	}
	return false
}

func hasChildSexualSafetyObserveRisk(text string) bool {
	childTerms := []string{"child", "children", "minor", "teen", "teenage"}
	sexualTerms := []string{"sexual", "sexual content", "explicit sexual", "sexually explicit", "porn", "nude"}
	for _, window := range contentSafetyWindowsAroundWithTerms(text, childTerms, 80, 140) {
		if containsAny(window.Text, sexualTerms) && isBenignContext(ContentSafetyCategoryChildSafety, window.Term, window.Text) {
			return true
		}
	}
	return false
}

func hasPolicyBypassRisk(text string) bool {
	for _, window := range contentSafetyWindowsAround(text, []string{"绕过", "越狱", "忽略", "禁用", "无视", "bypass", "jailbreak", "ignore", "disable", "override", "circumvent"}, 80, 120) {
		if containsAny(window, []string{"使用政策", "安全策略", "安全限制", "安全规则", "平台规则", "usage policy", "content policy", "platform policy", "model policy", "anthropic policy", "claude policy", "acceptable use", "safety policy", "safety rule", "guardrail", "moderation"}) &&
			containsAny(window, []string{"请", "帮我", "教我", "如何", "怎么", "怎样", "输出", "回答", "不要遵守", "不遵守", "无视", "忽略", "禁用", "help me", "how to", "tell me", "show me", "reveal", "output", "answer", "do not follow", "don't follow", "ignore", "disable", "override"}) {
			return true
		}
	}
	return false
}

func hasSexualExplicitRisk(text string) bool {
	sexualTerms := []string{"色情", "性交", "做爱", "口交", "肛交", "乱伦", "兽交", "恋物", "性幻想", "露骨", "porn", "pornographic", "pornography", "sex scene", "explicit sex", "incest", "bestiality", "fetish"}
	actionTerms := []string{"写", "生成", "描写", "详细", "角色扮演", "扮演", "故事", "create", "creating", "write", "generate", "generating", "roleplay", "describe", "graphic"}
	for _, window := range contentSafetyWindowsAround(text, sexualTerms, 80, 140) {
		if containsAny(window, actionTerms) {
			return true
		}
	}
	return false
}

func hasFraudRisk(text string) bool {
	fraudTerms := []string{"钓鱼", "骗取", "诈骗", "欺诈", "伪造文件", "伪造证件", "虚假评论", "刷好评", "假评论", "phishing", "scam", "scams", "steal credentials", "fake review", "fake reviews", "forged document", "forged documents", "forge document", "forge a document", "forge passport", "forge a passport", "forge signature", "forge a signature"}
	actionTerms := []string{"生成", "创建", "写", "制作", "骗取", "窃取", "批量", "帮我", "create", "creating", "generate", "generating", "write", "build", "building", "steal", "stealing", "trick"}
	for _, window := range contentSafetyWindowsAround(text, fraudTerms, 80, 140) {
		if containsAny(window, actionTerms) {
			return true
		}
	}
	return false
}

func hasFraudWarningRisk(text string) bool {
	fraudTerms := []string{"虚假评论", "假评论", "fake review", "fake reviews", "forged document", "forged documents", "scam", "scams"}
	trainingTerms := []string{"检测", "识别", "规则", "训练", "样例", "测试", "员工培训", "detection", "detect", "identify", "rule", "training", "sample", "samples", "test", "testing", "awareness"}
	actionTerms := []string{"生成", "创建", "写", "制作", "create", "creating", "generate", "generating", "write", "build", "building"}
	for _, window := range contentSafetyWindowsAround(text, fraudTerms, 80, 140) {
		if containsAny(window, trainingTerms) && containsAny(window, actionTerms) &&
			!containsAny(window, []string{"骗取", "窃取", "steal", "stealing", "capture credentials", "collect credentials"}) {
			return true
		}
	}
	return false
}

func hasIllegalActivityRisk(text string) bool {
	illegalTerms := []string{"冰毒", "海洛因", "可卡因", "芬太尼", "贩毒", "人口贩卖", "卖淫", "嫖娼", "伪造护照", "伪造身份证", "洗钱", "meth", "heroin", "cocaine", "fentanyl", "human trafficking", "sex trafficking", "drug trafficking", "prostitution", "money laundering", "fake passport"}
	actionTerms := []string{"购买", "出售", "交易", "渠道", "躲避", "规避", "运输", "buy", "buying", "sell", "selling", "trade", "trading", "evade", "evading", "ship", "shipping", "avoid detection"}
	for _, window := range contentSafetyWindowsAround(text, illegalTerms, 80, 140) {
		if containsAny(window, actionTerms) {
			return true
		}
	}
	return false
}

func hasCyberAbuseRisk(text string) bool {
	for _, window := range contentSafetyWindowsAround(text, []string{"勒索软件", "恶意软件", "木马", "僵尸网络", "钓鱼套件", "ransomware", "malware", "trojan", "botnet", "credential stealer", "phishing kit"}, 80, 140) {
		if containsAny(window, []string{"帮我写", "帮我生成", "生成", "制作", "开发", "编写", "payload", "窃取", "加密目标", "感染", "传播", "投放", "免杀", "绕过杀软", "持久化", "write", "generate", "generating", "build", "building", "create", "creating", "develop", "deploy", "steal", "stealing", "encrypt files", "evade antivirus", "persistence"}) {
			return true
		}
	}
	for _, window := range contentSafetyWindowsAround(text, []string{"漏洞", "exploit", "payload", "ddos", "sql注入", "sql injection", "xss", "webshell", "后门", "backdoor"}, 80, 140) {
		if containsAny(window, []string{"未授权", "入侵", "攻击", "绕过", "窃取", "批量扫描", "自动入侵", "利用", "unauthorized", "hack", "attack", "bypass", "steal", "exfiltrate", "mass scan"}) {
			return true
		}
	}
	return false
}

func hasWeaponsRisk(text string) bool {
	weaponTerms := []string{"炸弹", "爆炸物", "枪支", "武器", "毒气", "生物武器", "化学武器", "放射性", "核材料", "bomb", "explosive", "gun", "weapon", "chemical weapon", "biological weapon", "radioactive", "nuclear"}
	actionTerms := []string{"制作", "制造", "改装", "提高杀伤", "配方", "步骤", "教程", "build", "building", "make", "making", "manufacture", "manufacturing", "modify", "recipe", "step-by-step"}
	for _, window := range contentSafetyWindowsAround(text, weaponTerms, 80, 140) {
		if containsAny(window, actionTerms) {
			return true
		}
	}
	return false
}

func hasPrivacyAbuseRisk(text string) bool {
	privacyTerms := []string{"盗取隐私", "开盒", "人肉", "窃取个人信息", "身份证号", "非法监控", "监控伴侣", "冒充", "dox", "doxx", "doxing", "doxxing", "steal personal data", "impersonate", "illegal surveillance", "spy on"}
	actionTerms := []string{"帮我", "如何", "写", "生成", "收集", "追踪", "定位", "create", "creating", "how to", "track", "tracking", "collect", "collecting", "steal", "stealing"}
	for _, window := range contentSafetyWindowsAround(text, privacyTerms, 80, 140) {
		if containsAny(window, actionTerms) {
			return true
		}
	}
	return false
}

func hasSelfHarmRisk(text string) bool {
	selfHarmTerms := []string{"自杀", "自残", "割腕", "厌食", "暴食", "suicide", "self-harm", "self harm", "kill myself", "cut myself", "eating disorder"}
	actionTerms := []string{"方法", "步骤", "教程", "鼓励", "劝我", "最快", "无痛", "怎么做", "method", "methods", "instructions", "encourage", "how to", "painless"}
	for _, window := range contentSafetyWindowsAround(text, selfHarmTerms, 80, 140) {
		if containsAny(window, actionTerms) {
			return true
		}
	}
	return false
}

func hasViolenceHateRisk(text string) bool {
	violenceTerms := []string{"杀死", "屠杀", "灭绝", "袭击", "恐怖袭击", "加入isis", "圣战", "kill all", "exterminate", "massacre", "terror attack", "join isis", "jihad"}
	hateTargets := []string{"黑人", "犹太人", "穆斯林", "同性恋", "移民", "女性", "残疾人", "black people", "jews", "muslims", "gay people", "immigrants", "women", "disabled people"}
	extremismTerms := []string{"宣言", "宣传", "招募", "manifesto", "propaganda", "recruit", "recruiting"}
	for _, window := range contentSafetyWindowsAround(text, violenceTerms, 80, 140) {
		if containsAny(window, hateTargets) || containsAny(window, extremismTerms) {
			return true
		}
	}
	return false
}

func hasLegitimateSexualEducationContext(text string) bool {
	return containsAny(text, []string{"医学", "健康", "性教育", "法律", "合规", "科普", "治疗", "medical", "health", "sex education", "legal", "compliance", "educational", "treatment"}) &&
		!containsAny(text, []string{"色情", "角色扮演", "露骨", "性幻想", "porn", "roleplay", "graphic"})
}

func hasDefensiveCyberContext(text string) bool {
	return containsAny(text, []string{"防御", "修复", "检测", "加固", "审计", "防护", "缓解", "日志分析", "应急响应", "清除", "分析", "防范", "defensive", "fix", "patch", "detect", "detection", "mitigate", "hardening", "audit", "blue team", "incident response", "checklist", "clean", "remove", "forensics", "analyze", "analysis"}) &&
		!containsAny(text, []string{"未授权", "自动入侵", "骗取", "窃取", "绕过杀软", "免杀", "持久化", "unauthorized", "steal", "evade antivirus", "persistence"})
}

func hasBenignAnalysisContext(text string) bool {
	return containsAny(text, []string{"新闻", "历史", "分析", "研究", "合规", "法律", "科普", "预防", "识别", "防范", "检测", "规则", "训练", "样例", "测试", "news", "history", "analysis", "research", "compliance", "legal", "educational", "prevent", "prevention", "identify", "detect", "detection", "rule", "training", "sample", "samples", "test", "testing"}) &&
		!containsAny(text, []string{"帮我制作", "帮我生成", "教我制作", "购买", "出售", "骗取", "入侵", "build me", "create a", "steal", "buy", "sell"})
}

func hasBenignPrivacyContext(text string) bool {
	return containsAny(text, []string{"隐私合规", "数据保护", "防止", "识别", "脱敏", "校验", "验证", "测试", "privacy compliance", "data protection", "prevent", "detect", "mask", "redact", "validate", "validation", "test"}) &&
		!containsAny(text, []string{"盗取", "冒充", "监控", "steal", "impersonate", "spy"})
}

func hasSelfHarmSupportContext(text string) bool {
	return containsAny(text, []string{"求助", "帮我稳定", "支持资源", "热线", "不想伤害自己", "crisis", "support", "hotline", "help me stay safe", "resources"}) &&
		!containsAny(text, []string{"方法", "步骤", "教程", "最快", "无痛", "method", "instructions", "painless"})
}

func isBenignContext(category string, matchedTerm string, window string) bool {
	term := strings.ToLower(strings.TrimSpace(matchedTerm))
	text := strings.ToLower(window)
	switch category {
	case ContentSafetyCategoryChildSafety:
		switch term {
		case "child", "children":
			return containsAny(text, []string{"child process", "child_process", "parent/child process", "child thread", "child shell", "child pid", "child job"})
		case "minor":
			return containsAny(text, []string{"minor version", "minor versions", "minor release", "minor releases", "major/minor/patch", "semver", "semantic version", "upgrade"})
		}
	case ContentSafetyCategoryCyberAbuse:
		if term == "payload" {
			return containsAny(text, []string{"request payload", "response payload", "test payload", "marketing payload", "analytics payload"})
		}
	case ContentSafetyCategoryWeapons:
		if term == "gun" {
			return containsAny(text, []string{"gunicorn"})
		}
		if term == "bomb" {
			return containsAny(text, []string{"bombastic"})
		}
	case ContentSafetyCategorySelfHarm, ContentSafetyCategoryViolenceHate:
		return containsAny(text, []string{"kill process", "kill -9", "process killed", "job killed"})
	case ContentSafetyCategoryPolicyBypass:
		if term == "bypass" {
			return containsAny(text, []string{"bypass cache", "bypass proxy", "bypass localhost"})
		}
	case ContentSafetyCategoryFraud:
		if strings.HasPrefix(term, "forge") {
			return containsAny(text, []string{"forge ahead"})
		}
	}
	return false
}

var (
	contentSafetyEvidenceCredentialWithValuePattern = regexp.MustCompile(`(?i)\b(api\s*key|apikey|token|secret|authorization|bearer)\s+[\w./:+=-]{8,}`)
	contentSafetyEvidenceAssignmentSecretPattern    = regexp.MustCompile(`(?i)\b(password|passwd|token|secret|api[_ -]?key|authorization)\s*[:=]\s*[^,\s，]+`)
	contentSafetyEvidenceAPIKeyPattern              = regexp.MustCompile(`(?i)\b(sk-[a-z0-9][a-z0-9_-]{12,}|sk-ant-[a-z0-9_-]{12,})\b`)
	contentSafetyEvidenceEmailPattern               = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	contentSafetyEvidencePhonePattern               = regexp.MustCompile(`\b1[3-9]\d{9}\b`)
	contentSafetyEvidenceIDNumberPattern            = regexp.MustCompile(`\b\d{17}[\dXx]\b`)
	contentSafetyEvidenceLongNumberPattern          = regexp.MustCompile(`\b\d{12,}\b`)
	contentSafetyEvidenceURLQueryPattern            = regexp.MustCompile(`https?://[^\s?]+?\?[^\s，,]+`)
)

func contentSafetyEvidenceForFragment(category string, fragment contentSafetyTextFragment) ContentSafetyEvidence {
	text := normalizeContentModerationText(fragment.Text)
	if text == "" {
		return ContentSafetyEvidence{}
	}
	sum := sha256.Sum256([]byte(fragment.Source + "\n" + text))
	hash := hex.EncodeToString(sum[:])
	return ContentSafetyEvidence{
		Source:  strings.TrimSpace(fragment.Source),
		Excerpt: redactContentSafetyEvidence(contentSafetyExcerptForCategory(category, text)),
		Hash:    hash,
	}
}

func contentSafetyExcerptForCategory(category string, text string) string {
	text = normalizeContentModerationText(text)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	if match, ok := firstContentSafetyTermMatch(lower, contentSafetyEvidenceAnchors(category)); ok {
		anchorRune := utf8.RuneCountInString(lower[:match.Start])
		anchorLen := utf8.RuneCountInString(lower[match.Start:match.End])
		return contentSafetySliceRunes(text, anchorRune-40, anchorRune+anchorLen+80)
	}
	return contentSafetySliceRunes(text, 0, 120)
}

func contentSafetyEvidenceAnchors(category string) []string {
	switch category {
	case ContentSafetyCategoryIllegalActivity:
		return []string{"冰毒", "海洛因", "可卡因", "芬太尼", "贩毒", "人口贩卖", "卖淫", "嫖娼", "伪造护照", "伪造身份证", "洗钱", "meth", "heroin", "cocaine", "fentanyl", "human trafficking", "sex trafficking", "drug trafficking", "prostitution", "money laundering", "fake passport"}
	case ContentSafetyCategoryCyberAbuse:
		return []string{"勒索软件", "恶意软件", "木马", "僵尸网络", "钓鱼套件", "漏洞", "sql注入", "后门", "ransomware", "malware", "trojan", "botnet", "credential stealer", "phishing kit", "exploit", "payload", "ddos", "sql injection", "backdoor"}
	case ContentSafetyCategoryWeapons:
		return []string{"炸弹", "爆炸物", "枪支", "武器", "毒气", "生物武器", "化学武器", "放射性", "核材料", "bomb", "explosive", "gun", "weapon", "chemical weapon", "biological weapon", "radioactive", "nuclear"}
	case ContentSafetyCategoryViolenceHate:
		return []string{"杀死", "屠杀", "灭绝", "袭击", "恐怖袭击", "加入isis", "圣战", "kill all", "exterminate", "massacre", "terror attack", "join isis", "jihad"}
	case ContentSafetyCategoryPrivacyAbuse:
		return []string{"盗取隐私", "开盒", "人肉", "窃取个人信息", "身份证号", "非法监控", "监控伴侣", "冒充", "dox", "doxx", "steal personal data", "impersonate", "illegal surveillance", "spy on"}
	case ContentSafetyCategoryChildSafety:
		return []string{"未成年", "未成年人", "儿童", "小孩", "幼女", "幼男", "萝莉", "正太", "child", "children", "minor", "underage", "teen", "teenage"}
	case ContentSafetyCategorySelfHarm:
		return []string{"自杀", "自残", "割腕", "厌食", "暴食", "suicide", "self-harm", "kill myself", "cut myself", "eating disorder"}
	case ContentSafetyCategoryFraud:
		return []string{"钓鱼", "骗取", "诈骗", "欺诈", "伪造文件", "伪造证件", "虚假评论", "刷好评", "假评论", "phishing", "scam", "steal credentials", "fake review", "fake reviews", "forged document", "forged documents", "forge document", "forge a document", "forge passport", "forge a passport", "forge signature", "forge a signature"}
	case ContentSafetyCategorySexualExplicit:
		return []string{"色情", "性交", "做爱", "口交", "肛交", "乱伦", "兽交", "恋物", "性幻想", "露骨", "porn", "pornographic", "pornography", "sex scene", "explicit sex", "incest", "bestiality", "fetish"}
	case ContentSafetyCategoryPolicyBypass:
		return []string{"绕过", "越狱", "忽略", "禁用", "无视", "bypass", "jailbreak", "ignore", "disable", "override", "circumvent"}
	default:
		return nil
	}
}

func contentSafetySliceRunes(text string, start int, end int) string {
	runes := []rune(text)
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return ""
	}
	prefix := ""
	if start > 0 {
		prefix = "..."
	}
	suffix := ""
	if end < len(runes) {
		suffix = "..."
	}
	return prefix + string(runes[start:end]) + suffix
}

func redactContentSafetyEvidence(text string) string {
	text = contentSafetyEvidenceURLQueryPattern.ReplaceAllStringFunc(text, func(value string) string {
		if idx := strings.Index(value, "?"); idx >= 0 {
			return value[:idx+1] + "****"
		}
		return "****"
	})
	text = contentSafetyEvidenceCredentialWithValuePattern.ReplaceAllStringFunc(text, func(value string) string {
		fields := strings.Fields(value)
		if len(fields) == 0 {
			return "****"
		}
		return strings.Join(fields[:len(fields)-1], " ") + " ****"
	})
	text = contentSafetyEvidenceAssignmentSecretPattern.ReplaceAllStringFunc(text, func(value string) string {
		if idx := strings.IndexAny(value, ":="); idx >= 0 {
			return strings.TrimSpace(value[:idx+1]) + "****"
		}
		return "****"
	})
	text = contentSafetyEvidenceAPIKeyPattern.ReplaceAllString(text, "****")
	text = contentSafetyEvidenceEmailPattern.ReplaceAllString(text, "****")
	text = contentSafetyEvidenceIDNumberPattern.ReplaceAllString(text, "****")
	text = contentSafetyEvidencePhonePattern.ReplaceAllString(text, "****")
	text = contentSafetyEvidenceLongNumberPattern.ReplaceAllString(text, "****")
	return text
}

func containsAny(text string, needles []string) bool {
	return len(findContentSafetyTermMatches(text, needles)) > 0
}

func contentSafetyWindowsAround(text string, needles []string, before int, after int) []string {
	var windows []string
	for _, window := range contentSafetyWindowsAroundWithTerms(text, needles, before, after) {
		windows = append(windows, window.Text)
	}
	return windows
}

type contentSafetyTermWindow struct {
	Term string
	Text string
}

func contentSafetyWindowsAroundWithTerms(text string, needles []string, before int, after int) []contentSafetyTermWindow {
	var windows []contentSafetyTermWindow
	for _, match := range findContentSafetyTermMatches(text, needles) {
		start := match.Start - before
		if start < 0 {
			start = 0
		}
		end := match.End + after
		if end > len(text) {
			end = len(text)
		}
		windows = append(windows, contentSafetyTermWindow{Term: match.Term, Text: text[start:end]})
	}
	return windows
}

type contentSafetyTermMatch struct {
	Term  string
	Start int
	End   int
}

func firstContentSafetyTermMatch(text string, terms []string) (contentSafetyTermMatch, bool) {
	for _, term := range terms {
		matches := findContentSafetyTermMatches(text, []string{term})
		if len(matches) > 0 {
			return matches[0], true
		}
	}
	return contentSafetyTermMatch{}, false
}

func findContentSafetyTermMatches(text string, terms []string) []contentSafetyTermMatch {
	if text == "" || len(terms) == 0 {
		return nil
	}
	matches := make([]contentSafetyTermMatch, 0, 2)
	for _, rawTerm := range terms {
		term := strings.ToLower(strings.TrimSpace(rawTerm))
		if term == "" {
			continue
		}
		offset := 0
		for offset <= len(text) {
			idx := strings.Index(text[offset:], term)
			if idx < 0 {
				break
			}
			start := offset + idx
			end := start + len(term)
			if contentSafetyTermHasBoundary(text, term, start, end) {
				matches = append(matches, contentSafetyTermMatch{Term: term, Start: start, End: end})
			}
			offset = end
		}
	}
	return matches
}

func contentSafetyTermHasBoundary(text string, term string, start int, end int) bool {
	if !contentSafetyTermNeedsASCIIBoundary(term) {
		return true
	}
	if start > 0 && isContentSafetyASCIIWordByte(text[start-1]) {
		return false
	}
	if end < len(text) && isContentSafetyASCIIWordByte(text[end]) {
		return false
	}
	return true
}

func contentSafetyTermNeedsASCIIBoundary(term string) bool {
	for i := 0; i < len(term); i++ {
		if isContentSafetyASCIIWordByte(term[i]) {
			return true
		}
	}
	return false
}

func isContentSafetyASCIIWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func contentSafetySeverityRank(severity string) int {
	switch severity {
	case ContentSafetySeverityCritical:
		return 3
	case ContentSafetySeverityHigh:
		return 2
	case ContentSafetySeverityMedium:
		return 1
	default:
		return 0
	}
}

func primaryContentSafetyFinding(findings []ContentSafetyFinding) ContentSafetyFinding {
	if len(findings) == 0 {
		return ContentSafetyFinding{}
	}
	primary := findings[0]
	for _, finding := range findings[1:] {
		if contentSafetyFindingRank(finding) > contentSafetyFindingRank(primary) {
			primary = finding
		}
	}
	return primary
}

func contentSafetyFindingRank(finding ContentSafetyFinding) int {
	return contentSafetyActionRank(finding.Action)*100 + contentSafetySeverityRank(finding.Severity)*10 + contentSafetyConfidenceRank(finding.Confidence)
}

func contentSafetyActionRank(action string) int {
	switch action {
	case ContentSafetyActionBlock:
		return 3
	case ContentSafetyActionWarn:
		return 2
	case ContentSafetyActionObserve:
		return 1
	default:
		return 0
	}
}

func contentSafetyConfidenceRank(confidence string) int {
	switch confidence {
	case ContentSafetyConfidenceHigh:
		return 3
	case ContentSafetyConfidenceMedium:
		return 2
	case ContentSafetyConfidenceLow:
		return 1
	default:
		return 0
	}
}
