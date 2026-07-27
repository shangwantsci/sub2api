package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/tidwall/gjson"
)

// fingerprintSalt 是计算 cc_version 后缀指纹的盐值。
//
// 来源：与 Parrot src/transform/cc_mimicry.py 的 FINGERPRINT_SALT 完全一致；
// 这是真实 Claude Code CLI 抓包推导出的常量，改动会导致 fp 与 CLI 不一致，
// 进一步触发 Anthropic 的第三方检测。
const fingerprintSalt = "59cf53e54c78"

// computeClaudeCodeFingerprint 复刻真实 Claude Code CLI 的 cc_version 指纹算法：
//
//  1. 取 messages 中第一条非 synthetic role=user 的纯文本（首块 text）
//  2. 按 JavaScript 字符串（UTF-16 code unit）语义取第 4、7、20 位（不足以 '0' 补齐）
//  3. SHA256(SALT + chars + cc_version) 取 hex 前 3 字符
//
// 算法已直接从官方 Claude Code 2.1.211 native binary 验证：
//
//	[4,7,20].map(i => text[i] || "0").join("")
//
// 不能按 UTF-8 byte 索引，否则中文/emoji 首轮会产生错误 fp。
// 任何偏差都会导致 cc_version=X.Y.Z.{fp} 在上游侧与真实 CLI 不一致。
func computeClaudeCodeFingerprint(body []byte, version string) string {
	return computeClaudeCodeFingerprintFromText(extractFirstUserText(body), version)
}

func computeClaudeCodeFingerprintFromText(firstText, version string) string {
	indices := []int{4, 7, 20}
	units := utf16.Encode([]rune(firstText))
	var chars strings.Builder
	for _, i := range indices {
		if i >= len(units) {
			chars.WriteByte('0')
			continue
		}
		unit := units[i]
		// Node's crypto.update(string) UTF-8 encodes a lone surrogate as U+FFFD.
		if unit >= 0xD800 && unit <= 0xDFFF {
			chars.WriteRune(utf8.RuneError)
			continue
		}
		chars.WriteRune(rune(unit))
	}
	sum := sha256.Sum256([]byte(fingerprintSalt + chars.String() + version))
	return hex.EncodeToString(sum[:])[:3]
}

// extractFirstUserText 提取当前 body 中第一条 role=user 消息的首段 text。
//
// 新格式 system→messages 迁移不再携带 wire marker，因此绝不能在这里按固定 assistant
// ack 猜测并跳过消息：真实对话可能恰好具有相同形态。迁移调用方必须在改写前保存真实
// 首轮，并通过显式参数交给 billing / metadata 计算。旧标签仍仅作为历史格式兼容。
func extractFirstUserText(body []byte) string {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return ""
	}
	for _, msg := range messages.Array() {
		if msg.Get("role").String() != "user" {
			continue
		}
		text := firstTextBlockOfMessage(msg)
		if strings.HasPrefix(text, legacyMigratedSystemPromptLabel) {
			continue
		}
		return text
	}
	return ""
}

// inferBillingFingerprintSourceText 仅用于缺少显式迁移上下文的兼容路径：用 body 内已经
// 存在的 cc_version 指纹反证哪条 user 文本曾被用于计算。生产 mimic 路径会显式传入
// 改写前首轮，不依赖这个 12-bit 指纹推断。多个候选碰撞时保守返回第一条，避免把真实
// 对话误判为 synthetic；迁移路径的碰撞由显式上下文彻底规避。
func inferBillingFingerprintSourceText(body []byte) (string, bool) {
	version, wantFP := "", ""
	system := gjson.GetBytes(body, "system")
	if system.IsArray() {
		for _, block := range system.Array() {
			text := block.Get("text").String()
			if !strings.HasPrefix(strings.TrimSpace(text), "x-anthropic-billing-header:") {
				continue
			}
			match := ccVersionWithFpRe.FindStringSubmatch(text)
			if len(match) == 3 {
				version, wantFP = match[1], match[2]
				break
			}
		}
	}
	if version == "" || wantFP == "" {
		return "", false
	}

	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return "", false
	}
	for _, msg := range messages.Array() {
		if msg.Get("role").String() != "user" {
			continue
		}
		text := firstTextBlockOfMessage(msg)
		if strings.HasPrefix(text, legacyMigratedSystemPromptLabel) {
			continue
		}
		if computeClaudeCodeFingerprintFromText(text, version) == wantFP {
			return text, true
		}
	}
	return "", false
}

func firstTextBlockOfMessage(msg gjson.Result) string {
	content := msg.Get("content")
	if content.Type == gjson.String {
		return content.String()
	}
	if !content.IsArray() {
		return ""
	}
	text := ""
	content.ForEach(func(_, block gjson.Result) bool {
		if block.Get("type").String() != "text" {
			return true
		}
		text = block.Get("text").String()
		return false
	})
	return text
}

// buildBillingAttributionText 构造 system 数组的 billing attribution 文本。
//
// 形态对齐真实 Claude Code CLI：
//
//	x-anthropic-billing-header: cc_version=2.1.206.{fp}; cc_entrypoint=sdk-cli;
//
// 注意：新版 Claude Code CLI 已不再发送 cch=... 签名字段（见 issue #3358）。我们
// 随之去掉了 cch 段——继续注入它反而会让伪装请求偏离真实 CLI 流量。cc_version +
// cc_entrypoint=sdk-cli 仍保留：它们是客户端识别（claude_code_validator）与 Anthropic
// 第一方判定都依赖的稳定信号。
//
// 此 block 不带 cache_control（与真实 CLI 一致；cache breakpoint 由后续的
// Claude Code prompt block 承担）。
func buildBillingAttributionText(body []byte, cliVersion string) (string, error) {
	return buildBillingAttributionTextFromText(extractFirstUserText(body), cliVersion)
}

func buildBillingAttributionTextFromText(firstUserText, cliVersion string) (string, error) {
	if cliVersion == "" {
		return "", fmt.Errorf("cliVersion required")
	}
	profile := claude.DefaultClaudeCodeMimicryProfile()
	fp := computeClaudeCodeFingerprintFromText(firstUserText, cliVersion)
	return fmt.Sprintf(
		"x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=%s;",
		cliVersion, fp, profile.BillingEntrypoint,
	), nil
}
