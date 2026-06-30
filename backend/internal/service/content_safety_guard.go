package service

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tidwall/gjson"
)

const (
	ContentSafetyGuardModeOff   = "off"
	ContentSafetyGuardModeWarn  = "warn"
	ContentSafetyGuardModeBlock = "block"

	ContentSafetyActionSkip  = "skip"
	ContentSafetyActionAllow = "allow"
	ContentSafetyActionWarn  = "warn"
	ContentSafetyActionBlock = "block"

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
	contentSafetyBlockMessage     = "请求违反使用政策，已被拦截"
	contentSafetySettingsCacheTTL = 10 * time.Second
)

type ContentSafetyGuardSettings struct {
	Enabled bool
	Mode    string
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
	Category string `json:"category"`
	Severity string `json:"severity"`
	Reason   string `json:"reason"`
}

type ContentSafetyDecision struct {
	Allowed        bool                   `json:"allowed"`
	Blocked        bool                   `json:"blocked"`
	Flagged        bool                   `json:"flagged"`
	Action         string                 `json:"action"`
	Mode           string                 `json:"mode"`
	Message        string                 `json:"message"`
	PrimaryFinding ContentSafetyFinding   `json:"primary_finding"`
	Findings       []ContentSafetyFinding `json:"findings"`
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
		Enabled: true,
		Mode:    ContentSafetyGuardModeBlock,
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
	})
	if err != nil {
		return settings, err
	}
	if raw, ok := values[SettingKeyEnableContentSafetyFilter]; ok && strings.TrimSpace(raw) != "" {
		settings.Enabled = strings.TrimSpace(raw) == "true"
	}
	settings.Mode = NormalizeContentSafetyGuardMode(values[SettingKeyContentSafetyGuardMode])
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
	decision.Mode = settings.Mode
	if !decision.Flagged {
		decision.Action = ContentSafetyActionAllow
		decision.Allowed = true
		decision.Blocked = false
		return decision
	}
	if settings.Mode == ContentSafetyGuardModeWarn {
		decision.Action = ContentSafetyActionWarn
		decision.Allowed = true
		decision.Blocked = false
		decision.Message = ""
		return decision
	}
	decision.Action = ContentSafetyActionBlock
	decision.Allowed = false
	decision.Blocked = true
	decision.Message = contentSafetyBlockMessage
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
	text := ExtractContentSafetyText(input.Protocol, input.Body)
	findings := classifyContentSafetyText(text)
	decision := &ContentSafetyDecision{
		Allowed:  true,
		Action:   ContentSafetyActionAllow,
		Findings: findings,
	}
	if len(findings) == 0 {
		return decision
	}
	primary := findings[0]
	for _, finding := range findings[1:] {
		if contentSafetySeverityRank(finding.Severity) > contentSafetySeverityRank(primary.Severity) {
			primary = finding
		}
	}
	decision.Flagged = true
	decision.Blocked = true
	decision.Allowed = false
	decision.Action = ContentSafetyActionBlock
	decision.Message = contentSafetyBlockMessage
	decision.PrimaryFinding = primary
	return decision
}

func ExtractContentSafetyText(protocol string, body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	var parts []string
	switch protocol {
	case ContentModerationProtocolAnthropicMessages:
		collectContentSafetyValue(gjson.GetBytes(body, "system"), &parts)
		collectAnthropicContentSafetyMessages(gjson.GetBytes(body, "messages"), &parts)
	case ContentModerationProtocolOpenAIChat:
		collectOpenAIChatContentSafetyMessages(gjson.GetBytes(body, "messages"), &parts)
	case ContentModerationProtocolOpenAIResponses:
		collectContentSafetyValue(gjson.GetBytes(body, "instructions"), &parts)
		collectResponsesContentSafetyInput(gjson.GetBytes(body, "input"), &parts)
	default:
		collectContentSafetyValue(gjson.GetBytes(body, "system"), &parts)
		collectContentSafetyValue(gjson.GetBytes(body, "instructions"), &parts)
		collectAnthropicContentSafetyMessages(gjson.GetBytes(body, "messages"), &parts)
		collectOpenAIChatContentSafetyMessages(gjson.GetBytes(body, "messages"), &parts)
		collectResponsesContentSafetyInput(gjson.GetBytes(body, "input"), &parts)
	}
	return normalizeContentModerationText(strings.Join(parts, "\n"))
}

func collectAnthropicContentSafetyMessages(messages gjson.Result, parts *[]string) {
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, item gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
		if role == "user" || role == "system" || role == "tool" {
			collectContentSafetyValue(item.Get("content"), parts)
		}
		return true
	})
}

func collectOpenAIChatContentSafetyMessages(messages gjson.Result, parts *[]string) {
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, item gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
		switch role {
		case "user", "system", "developer", "tool":
			collectContentSafetyValue(item.Get("content"), parts)
		}
		return true
	})
}

func collectResponsesContentSafetyInput(input gjson.Result, parts *[]string) {
	switch {
	case !input.Exists():
		return
	case input.Type == gjson.String:
		addContentSafetyText(parts, input.String())
	case input.IsArray():
		input.ForEach(func(_, item gjson.Result) bool {
			collectResponsesContentSafetyItem(item, parts)
			return true
		})
	case input.IsObject():
		collectResponsesContentSafetyItem(input, parts)
	}
}

func collectResponsesContentSafetyItem(item gjson.Result, parts *[]string) {
	role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
	typ := strings.ToLower(strings.TrimSpace(item.Get("type").String()))
	if role == "assistant" {
		return
	}
	if role == "" && typ != "input_text" && typ != "message" && typ != "function_call_output" {
		return
	}
	collectContentSafetyValue(item.Get("content"), parts)
	collectContentSafetyValue(item.Get("text"), parts)
	collectContentSafetyValue(item.Get("output"), parts)
}

func collectContentSafetyValue(value gjson.Result, parts *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		addContentSafetyText(parts, value.String())
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectContentSafetyValue(item, parts)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		switch typ {
		case "image", "input_image", "image_url":
			return
		}
		collectContentSafetyValue(value.Get("text"), parts)
		collectContentSafetyValue(value.Get("content"), parts)
		collectContentSafetyValue(value.Get("output"), parts)
	}
}

func addContentSafetyText(parts *[]string, text string) {
	text = strings.TrimSpace(text)
	if text == "" || isAnthropicSystemReminderText(text) {
		return
	}
	*parts = append(*parts, text)
}

func classifyContentSafetyText(text string) []ContentSafetyFinding {
	text = normalizeContentModerationText(text)
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	findings := make([]ContentSafetyFinding, 0, 4)
	add := func(category string, severity string, reason string) {
		for _, existing := range findings {
			if existing.Category == category {
				return
			}
		}
		findings = append(findings, ContentSafetyFinding{Category: category, Severity: severity, Reason: reason})
	}

	if hasChildSexualSafetyRisk(lower) {
		add(ContentSafetyCategoryChildSafety, ContentSafetySeverityCritical, "child sexual exploitation or sexualization")
	}
	if hasPolicyBypassRisk(lower) {
		add(ContentSafetyCategoryPolicyBypass, ContentSafetySeverityHigh, "platform or safety rule bypass request")
	}
	if hasSexualExplicitRisk(lower) && !hasLegitimateSexualEducationContext(lower) {
		add(ContentSafetyCategorySexualExplicit, ContentSafetySeverityHigh, "explicit sexual generation request")
	}
	if hasFraudRisk(lower) && !hasBenignAnalysisContext(lower) {
		add(ContentSafetyCategoryFraud, ContentSafetySeverityHigh, "fraud, phishing, forgery, or coordinated deception")
	}
	if hasIllegalActivityRisk(lower) && !hasBenignAnalysisContext(lower) {
		add(ContentSafetyCategoryIllegalActivity, ContentSafetySeverityHigh, "illegal transaction or evasion request")
	}
	if hasCyberAbuseRisk(lower) && !hasDefensiveCyberContext(lower) {
		add(ContentSafetyCategoryCyberAbuse, ContentSafetySeverityHigh, "unauthorized cyber abuse request")
	}
	if hasWeaponsRisk(lower) && !hasBenignAnalysisContext(lower) {
		add(ContentSafetyCategoryWeapons, ContentSafetySeverityHigh, "weapon or dangerous material instructions")
	}
	if hasPrivacyAbuseRisk(lower) && !hasBenignPrivacyContext(lower) {
		add(ContentSafetyCategoryPrivacyAbuse, ContentSafetySeverityHigh, "privacy or identity abuse request")
	}
	if hasSelfHarmRisk(lower) && !hasSelfHarmSupportContext(lower) {
		add(ContentSafetyCategorySelfHarm, ContentSafetySeverityHigh, "self-harm encouragement or instructions")
	}
	if hasViolenceHateRisk(lower) && !hasBenignAnalysisContext(lower) {
		add(ContentSafetyCategoryViolenceHate, ContentSafetySeverityHigh, "violent, hateful, or extremist request")
	}

	return findings
}

func hasChildSexualSafetyRisk(text string) bool {
	return containsAny(text, []string{
		"未成年", "未成年人", "儿童", "小孩", "幼女", "幼男", "萝莉", "正太", "child", "children", "minor", "underage", "teen",
	}) && containsAny(text, []string{
		"色情", "性化", "性行为", "性交", "裸照", "裸体", "露骨", "csam", "porn", "sexual", "nude", "explicit",
	})
}

func hasPolicyBypassRisk(text string) bool {
	return containsAny(text, []string{"绕过", "越狱", "忽略", "禁用", "bypass", "jailbreak", "ignore", "disable"}) &&
		containsAny(text, []string{"使用政策", "安全限制", "安全规则", "平台规则", "anthropic", "claude", "policy", "safety", "guardrail", "moderation"})
}

func hasSexualExplicitRisk(text string) bool {
	return containsAny(text, []string{"色情", "性交", "做爱", "口交", "肛交", "乱伦", "兽交", "恋物", "性幻想", "露骨", "porn", "sex scene", "explicit sex", "incest", "bestiality", "fetish"}) &&
		containsAny(text, []string{"写", "生成", "描写", "详细", "角色扮演", "扮演", "故事", "create", "write", "generate", "roleplay", "describe", "graphic"})
}

func hasFraudRisk(text string) bool {
	return containsAny(text, []string{"钓鱼", "骗取", "诈骗", "欺诈", "伪造文件", "伪造证件", "虚假评论", "刷好评", "假评论", "phishing", "scam", "steal credentials", "fake review", "forge", "forged document"}) &&
		containsAny(text, []string{"生成", "创建", "写", "制作", "骗取", "窃取", "批量", "帮我", "create", "generate", "write", "build", "steal", "trick"})
}

func hasIllegalActivityRisk(text string) bool {
	return containsAny(text, []string{"冰毒", "海洛因", "可卡因", "芬太尼", "贩毒", "人口贩卖", "卖淫", "嫖娼", "伪造护照", "伪造身份证", "洗钱", "meth", "heroin", "cocaine", "fentanyl", "trafficking", "prostitution", "money laundering", "fake passport"}) &&
		containsAny(text, []string{"购买", "出售", "交易", "渠道", "躲避", "规避", "运输", "buy", "sell", "trade", "evade", "ship", "avoid detection"})
}

func hasCyberAbuseRisk(text string) bool {
	if containsAny(text, []string{"勒索软件", "恶意软件", "木马", "僵尸网络", "钓鱼套件", "ransomware", "malware", "trojan", "botnet", "credential stealer", "phishing kit"}) &&
		containsAny(text, []string{"帮我写", "帮我生成", "生成", "制作", "开发", "编写", "payload", "窃取", "加密目标", "感染", "传播", "投放", "免杀", "绕过杀软", "持久化", "write", "generate", "build", "create", "develop", "deploy", "steal", "encrypt files", "evade antivirus", "persistence"}) {
		return true
	}
	return containsAny(text, []string{"漏洞", "exploit", "payload", "ddos", "sql注入", "sql injection", "xss", "webshell", "后门", "backdoor"}) &&
		containsAny(text, []string{"未授权", "入侵", "攻击", "绕过", "窃取", "批量扫描", "自动入侵", "利用", "unauthorized", "hack", "attack", "bypass", "steal", "exfiltrate", "mass scan"})
}

func hasWeaponsRisk(text string) bool {
	return containsAny(text, []string{"炸弹", "爆炸物", "枪支", "武器", "毒气", "生物武器", "化学武器", "放射性", "核材料", "bomb", "explosive", "gun", "weapon", "chemical weapon", "biological weapon", "radioactive", "nuclear"}) &&
		containsAny(text, []string{"制作", "制造", "改装", "提高杀伤", "配方", "步骤", "教程", "build", "make", "manufacture", "modify", "recipe", "step-by-step"})
}

func hasPrivacyAbuseRisk(text string) bool {
	return containsAny(text, []string{"盗取隐私", "开盒", "人肉", "窃取个人信息", "身份证号", "非法监控", "监控伴侣", "冒充", "dox", "doxx", "steal personal data", "impersonate", "illegal surveillance", "spy on"}) &&
		containsAny(text, []string{"帮我", "如何", "写", "生成", "收集", "追踪", "定位", "create", "how to", "track", "collect", "steal"})
}

func hasSelfHarmRisk(text string) bool {
	return containsAny(text, []string{"自杀", "自残", "割腕", "厌食", "暴食", "suicide", "self-harm", "kill myself", "cut myself", "eating disorder"}) &&
		containsAny(text, []string{"方法", "步骤", "教程", "鼓励", "劝我", "最快", "无痛", "怎么做", "method", "instructions", "encourage", "how to", "painless"})
}

func hasViolenceHateRisk(text string) bool {
	violence := containsAny(text, []string{"杀死", "屠杀", "灭绝", "袭击", "恐怖袭击", "加入isis", "圣战", "kill all", "exterminate", "massacre", "terror attack", "join isis", "jihad"})
	hateTarget := containsAny(text, []string{"黑人", "犹太人", "穆斯林", "同性恋", "移民", "女性", "残疾人", "black people", "jews", "muslims", "gay people", "immigrants", "women", "disabled people"})
	return violence && (hateTarget || containsAny(text, []string{"宣言", "宣传", "招募", "manifesto", "propaganda", "recruit"}))
}

func hasLegitimateSexualEducationContext(text string) bool {
	return containsAny(text, []string{"医学", "健康", "性教育", "法律", "合规", "科普", "治疗", "medical", "health", "sex education", "legal", "compliance", "educational", "treatment"}) &&
		!containsAny(text, []string{"色情", "角色扮演", "露骨", "性幻想", "porn", "roleplay", "graphic"})
}

func hasDefensiveCyberContext(text string) bool {
	return containsAny(text, []string{"防御", "修复", "检测", "加固", "审计", "防护", "缓解", "日志分析", "应急响应", "清除", "分析", "防范", "defensive", "fix", "patch", "detect", "mitigate", "hardening", "audit", "blue team", "incident response", "checklist", "clean", "remove", "forensics", "analyze"}) &&
		!containsAny(text, []string{"未授权", "自动入侵", "骗取", "窃取", "绕过杀软", "免杀", "持久化", "unauthorized", "steal", "evade antivirus", "persistence"})
}

func hasBenignAnalysisContext(text string) bool {
	return containsAny(text, []string{"新闻", "历史", "分析", "研究", "合规", "法律", "科普", "预防", "识别", "防范", "news", "history", "analysis", "research", "compliance", "legal", "educational", "prevent", "identify"}) &&
		!containsAny(text, []string{"帮我制作", "帮我生成", "教我制作", "购买", "出售", "骗取", "入侵", "build me", "create a", "steal", "buy", "sell"})
}

func hasBenignPrivacyContext(text string) bool {
	return containsAny(text, []string{"隐私合规", "数据保护", "防止", "识别", "privacy compliance", "data protection", "prevent", "detect"}) &&
		!containsAny(text, []string{"盗取", "冒充", "监控", "steal", "impersonate", "spy"})
}

func hasSelfHarmSupportContext(text string) bool {
	return containsAny(text, []string{"求助", "帮我稳定", "支持资源", "热线", "不想伤害自己", "crisis", "support", "hotline", "help me stay safe", "resources"}) &&
		!containsAny(text, []string{"方法", "步骤", "教程", "最快", "无痛", "method", "instructions", "painless"})
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
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
