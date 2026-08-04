package service

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/tiktoken-go/tokenizer"
)

// 非 Claude Code 客户端走 Claude OAuth 账号时，网关会把 system 整体换成 Claude Code
// 形态的身份 blocks。其中**不带 cache_control** 的那几块会直接计进上游返回的
// usage.input_tokens，客户端因此看到一个比自己实际输入更大的数字。
//
// 本文件把这部分注入量从「回给客户端的」usage.input_tokens 里扣掉。
//
// 两条边界必须守住：
//
//  1. 带 cache_control 的 block 落在 cache_creation_input_tokens / cache_read_input_tokens
//     维度，本来就不在 input_tokens 里 —— 扣它等于凭空抹掉客户的真实 token。
//     （默认 3-block 形态里体量最大的提示词扩充块正是带 cache_control 的。）
//  2. 只改回给客户端的展示值。计费与审计始终使用上游原始 usage，
//     调用方拿到的 *TokenUsage 不受影响。
//
// 已知残留：客户请求带 system 时，网关会把它迁移成 user/assistant 消息对，其中
// assistant 的 ack（约 9 tokens）同样计入 input_tokens，当前不扣。要扣得安全需在
// 迁移处按结构变化记录，不能按文本匹配（合法对话可能恰好命中相同 ack）。
const claudeMimicInjectedInputTokensKey = "claude_mimic_injected_input_tokens"

var (
	claudeInjectedCodecOnce sync.Once
	claudeInjectedCodec     tokenizer.Codec
	claudeInjectedCodecErr  error
)

// claudeInjectedTokenizer 懒加载并全局复用 tokenizer。
// o200k 不是 Anthropic 的分词器，但这里只用来量固定的几十 token 英文身份块，
// 绝对误差在几个 token 量级。
func claudeInjectedTokenizer() (tokenizer.Codec, error) {
	claudeInjectedCodecOnce.Do(func() {
		claudeInjectedCodec, claudeInjectedCodecErr = tokenizer.Get(tokenizer.O200kBase)
	})
	return claudeInjectedCodec, claudeInjectedCodecErr
}

// claudeOAuthBillableInputTokensEnabled 读取全局开关（进程内 60s 缓存）。
func (s *GatewayService) claudeOAuthBillableInputTokensEnabled(ctx context.Context) bool {
	if s == nil || s.settingService == nil {
		return false
	}
	return s.settingService.GetClaudeOAuthBillableInputTokensEnabled(ctx)
}

// claudeOAuthBillableInputTokensOverride 返回注入量的标定覆盖值（0 = 用本地估算）。
func (s *GatewayService) claudeOAuthBillableInputTokensOverride(ctx context.Context) int {
	if s == nil || s.settingService == nil {
		return 0
	}
	return s.settingService.GetClaudeOAuthBillableInputTokensOverride(ctx)
}

// resolveClaudeMimicInjectedInputTokens 得出本次请求应扣除的注入量。
//
// 先用本地估算判断「有没有注入」——这一步必须保留：估出 0 说明 system 里没有
// 不带 cache_control 的注入块，此时即便配了覆盖值也不能扣。
// 确认有注入后，若管理员填了标定值则以其为准：本地走 o200k，与 Anthropic 口径存在
// 系统性偏差（线上实测 identity_only 两块，本地估 42 而官方计 28），
// 注入内容形态固定，一次标定即可长期生效。
func (s *GatewayService) resolveClaudeMimicInjectedInputTokens(ctx context.Context, body []byte) int {
	injected := countClaudeMimicInjectedInputTokens(body)
	if injected <= 0 {
		return 0
	}
	if override := s.claudeOAuthBillableInputTokensOverride(ctx); override > 0 {
		return override
	}
	return injected
}

// countClaudeMimicInjectedInputTokens 统计最终 wire body 里会落进 usage.input_tokens
// 的注入 system block token 数。
//
// 伪装路径下客户原始 system 已被迁移进 messages、system 字段整体替换为注入 blocks，
// 因此这里数出来的就是纯注入量。非伪装路径不得调用 —— 那时 system 是客户自己的内容。
func countClaudeMimicInjectedInputTokens(body []byte) int {
	system := gjson.GetBytes(body, "system")
	if !system.IsArray() {
		return 0
	}
	codec, err := claudeInjectedTokenizer()
	if err != nil {
		return 0
	}

	total := 0
	system.ForEach(func(_, block gjson.Result) bool {
		// 带 cache_control 的块计入 cache 维度，不在 input_tokens 里，绝不能扣。
		if block.Get("cache_control").Exists() {
			return true
		}
		text := block.Get("text").String()
		if text == "" {
			return true
		}
		if n, err := codec.Count(text); err == nil {
			total += n
		}
		return true
	})
	return total
}

// rememberClaudeMimicInjectedInputTokens 记录本次请求的注入量，供响应阶段扣除。
func rememberClaudeMimicInjectedInputTokens(c *gin.Context, tokens int) {
	if c != nil && tokens > 0 {
		c.Set(claudeMimicInjectedInputTokensKey, tokens)
	}
}

// recalledClaudeMimicInjectedInputTokens 返回本次请求注入进 usage.input_tokens 的量。
func recalledClaudeMimicInjectedInputTokens(c *gin.Context) int {
	if c == nil {
		return 0
	}
	value, ok := c.Get(claudeMimicInjectedInputTokensKey)
	if !ok {
		return 0
	}
	tokens, ok := value.(int)
	if !ok {
		return 0
	}
	return tokens
}

// deductClaudeMimicInjectedInputTokens 返回扣除注入量后的 input_tokens。
// 任何不确定（没有注入、扣不动、会扣成非正数）都返回 ok=false，保持官方原值。
func deductClaudeMimicInjectedInputTokens(officialTokens, injectedTokens int) (int, bool) {
	if injectedTokens <= 0 {
		return 0, false
	}
	// 注入量不可能达到甚至超过上游总量。一旦如此说明估算不可信，原样透传。
	if injectedTokens >= officialTokens {
		return 0, false
	}
	return officialTokens - injectedTokens, true
}

// rewriteInputTokensInJSON 就地改写 JSON 里某个 usage 路径下的 input_tokens。
// path 为 usage 对象的 gjson 路径，例如 "usage" 或 "message.usage"。
func rewriteInputTokensInJSON(body []byte, path string, injectedTokens int) ([]byte, int, int, bool) {
	field := path + ".input_tokens"
	official := gjson.GetBytes(body, field)
	if !official.Exists() || official.Type != gjson.Number {
		return body, 0, 0, false
	}
	officialTokens := int(official.Int())

	billable, ok := deductClaudeMimicInjectedInputTokens(officialTokens, injectedTokens)
	if !ok {
		return body, 0, 0, false
	}
	next, err := sjson.SetBytes(body, field, billable)
	if err != nil {
		return body, 0, 0, false
	}
	return next, officialTokens, billable, true
}

// rewriteInputTokensInMap 改写 SSE 事件解出的 usage map（流式路径）。
func rewriteInputTokensInMap(usage map[string]any, injectedTokens int) (int, int, bool) {
	raw, ok := usage["input_tokens"]
	if !ok {
		return 0, 0, false
	}
	officialTokens, ok := numericToInt(raw)
	if !ok {
		return 0, 0, false
	}
	billable, ok := deductClaudeMimicInjectedInputTokens(officialTokens, injectedTokens)
	if !ok {
		return 0, 0, false
	}
	usage["input_tokens"] = billable
	return officialTokens, billable, true
}

// numericToInt 处理 JSON 解码后可能出现的 float64/json.Number/int 形态。
func numericToInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}
