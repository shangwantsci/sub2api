package service

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/tiktoken-go/tokenizer"
)

// ClaudeCodeUsage 是计费/记账用的归一化 usage。
//
// CC 的真实请求里带有它自己的 billing header、identity、默认 system（客户没传 system 时）
// 和 11 个工具，首次请求会产生一大笔 cache_creation（实测 2.1.309 约 19,205 tokens）。
// 这些都不是客户的内容：客户只为自己的文本输入和模型输出付费。
//
// 因此这里刻意：
//   - InputTokens = 客户 system + messages 的文本 token（本地 o200k 估算）；
//   - CacheCreationInputTokens / CacheReadInputTokens = 0；
//   - OutputTokens = 上游 result.usage.output_tokens；
//   - Raw 保留上游原始 usage，供审计/对账。
type ClaudeCodeUsage struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	Raw                      json.RawMessage
}

// NormalizeClaudeCodeUsage 把 CC 的 result usage 归一化成“只算客户输入”的账单口径。
func NormalizeClaudeCodeUsage(turn ClaudeCodeTurn, result *ClaudeCodeResult) ClaudeCodeUsage {
	usage := ClaudeCodeUsage{InputTokens: countClaudeCodeTurnTokens(turn)}
	if result == nil {
		return usage
	}
	usage.Raw = result.Usage

	if len(result.Usage) > 0 {
		var raw struct {
			OutputTokens int `json:"output_tokens"`
		}
		if err := json.Unmarshal(result.Usage, &raw); err == nil {
			usage.OutputTokens = raw.OutputTokens
		}
	}
	if usage.OutputTokens == 0 && strings.TrimSpace(result.Text) != "" {
		// 上游没给 output_tokens 时，用结果文本做近似兜底。
		usage.OutputTokens = countClaudeCodeTokens([]string{result.Text})
	}
	return usage
}

var (
	claudeCodeTokenizerOnce sync.Once
	claudeCodeTokenizer     tokenizer.Codec
	claudeCodeTokenizerErr  error
)

// claudeCodeTokenizer 懒加载一次 o200k 分词器。它不是 Anthropic 的分词器，
// 只用于按客户文本估算输入 token；首次加载失败会回退到 len/4 粗估。
func claudeCodeTokenizerCodec() (tokenizer.Codec, error) {
	claudeCodeTokenizerOnce.Do(func() {
		claudeCodeTokenizer, claudeCodeTokenizerErr = tokenizer.Get(tokenizer.O200kBase)
	})
	return claudeCodeTokenizer, claudeCodeTokenizerErr
}

func countClaudeCodeTurnTokens(turn ClaudeCodeTurn) int {
	texts := make([]string, 0, len(turn.Messages)+1)
	if strings.TrimSpace(turn.System) != "" {
		texts = append(texts, turn.System)
	}
	for _, message := range turn.Messages {
		if text := strings.TrimSpace(message.Text); text != "" {
			texts = append(texts, text)
		}
	}
	return countClaudeCodeTokens(texts)
}

func countClaudeCodeTokens(texts []string) int {
	codec, err := claudeCodeTokenizerCodec()
	total := 0
	for _, text := range texts {
		if text == "" {
			continue
		}
		if err == nil {
			if n, countErr := codec.Count(text); countErr == nil {
				total += n
				continue
			}
		}
		// 分词器不可用时用粗估，保证计费不中断。
		total += (len(text) + 3) / 4
	}
	return total
}
