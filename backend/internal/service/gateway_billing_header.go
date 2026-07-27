package service

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ccVersionWithFpRe matches the full cc_version=X.Y.Z.{fp} form (fp = 3 hex chars),
// which is what the mimic billing block always emits. The fp DEPENDS on the version
// (sha256(salt+chars+version)[:3]), so when the version changes the fp must be
// recomputed — otherwise the block carries a fp for the old version and gets flagged.
var ccVersionWithFpRe = regexp.MustCompile(`cc_version=(\d+\.\d+\.\d+)\.([0-9a-f]{3})`)

// ccVersionInBillingRe matches the bare semver part of cc_version (X.Y.Z), used as a
// fallback for any billing block that lacks the fp suffix.
var ccVersionInBillingRe = regexp.MustCompile(`cc_version=\d+\.\d+\.\d+`)

// syncBillingHeaderVersion rewrites cc_version in x-anthropic-billing-header system
// text blocks to match the version extracted from userAgent, RECOMPUTING the fingerprint
// suffix for the new version. This is what lets the calibrated-profile loader bump the
// CLI version (via the profile's User-Agent) with zero code changes: the billing block
// follows the emitted UA version and stays internally consistent (cc_version + fp).
// Only touches system array blocks whose text starts with "x-anthropic-billing-header".
func syncBillingHeaderVersion(body []byte, userAgent string) []byte {
	return syncBillingHeaderVersionWithFirstUserText(body, userAgent, extractFirstUserText(body))
}

// syncBillingHeaderVersionWithFirstUserText 使用调用方在 system→messages 改写前保存的
// 真实首轮文本重算 fp。新 wire 格式没有内部 marker，禁止从固定 ack 结构反推 synthetic
// 消息，否则合法对话撞上该文本时会静默得到错误的 cc_version 后缀。
func syncBillingHeaderVersionWithFirstUserText(body []byte, userAgent, firstUserText string) []byte {
	version := ExtractCLIVersion(userAgent)
	if version == "" {
		return body
	}

	systemResult := gjson.GetBytes(body, "system")
	if !systemResult.Exists() || !systemResult.IsArray() {
		return body
	}

	idx := 0
	systemResult.ForEach(func(_, item gjson.Result) bool {
		text := item.Get("text")
		if text.Exists() && text.Type == gjson.String &&
			strings.HasPrefix(text.String(), "x-anthropic-billing-header") {
			original := text.String()
			var newText string
			if ccVersionWithFpRe.MatchString(original) {
				fp := computeClaudeCodeFingerprintFromText(firstUserText, version)
				newText = ccVersionWithFpRe.ReplaceAllString(original, "cc_version="+version+"."+fp)
			} else {
				newText = ccVersionInBillingRe.ReplaceAllString(original, "cc_version="+version)
			}
			if newText != original {
				if updated, err := sjson.SetBytes(body, fmt.Sprintf("system.%d.text", idx), newText); err == nil {
					body = updated
				}
			}
		}
		idx++
		return true
	})

	return body
}
