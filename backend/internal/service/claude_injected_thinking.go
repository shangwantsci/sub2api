package service

import (
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// 伪装路径下，客户端没要求 thinking 时网关仍会按 profile 注入 thinking（sonnet/opus
// 为 adaptive，haiku 为 enabled+budget）。上游据此返回一个额外的 thinking block，
// 它排在 content[0]，内容为空、真实推理加密在 signature 里：
//
//	content[0]  {"type":"thinking","thinking":"","signature":"CAIS..."}
//	content[1]  {"type":"text","text":"3973"}
//
// 对客户端而言这块没有信息价值，却改变了响应形态：官方在客户端未请求 thinking 时
// content[0] 就是 text。按 type 分派的客户端（官方 SDK）不受影响，但按下标取首块、
// 或无差别拼接各块 text 字段的实现会拿到空串或多出一个前导换行。
//
// 本文件把网关自己注入的 thinking block 从响应里摘掉，使响应回到客户端请求时的形态。
// 与 claude_billable_input_tokens.go 同属「剥离网关注入物」，一个管 usage，一个管 content。
//
// 三条边界必须守住：
//
//  1. 只处理网关注入的情形。客户端自己请求了 thinking 时必须原样透传 ——
//     那是它要的数据，signature 还需回传给上游做多轮校验。
//  2. 剥离后 content 不能为空。长推理耗尽 max_tokens 时响应里可能只有 thinking block，
//     此时整个 content 都会被摘空；保留原样才能让客户端从 stop_reason 看出被截断。
//  3. 只动回给客户端的响应体，不碰计费与审计用的 usage。
const claudeMimicInjectedThinkingKey = "claude_mimic_injected_thinking"

// rememberClaudeMimicInjectedThinking 标记本次请求的 thinking 字段由网关注入。
// 仅当改写前客户端自己没带 thinking、改写后出现了 thinking 时才应调用。
func rememberClaudeMimicInjectedThinking(c *gin.Context) {
	if c != nil {
		c.Set(claudeMimicInjectedThinkingKey, true)
	}
}

// recalledClaudeMimicInjectedThinking 返回本次请求的 thinking 是否由网关注入。
func recalledClaudeMimicInjectedThinking(c interface {
	Get(string) (any, bool)
}) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(claudeMimicInjectedThinkingKey)
	if !ok {
		return false
	}
	injected, _ := value.(bool)
	return injected
}

// isThinkingBlockType 判断一个 content block 是否属于推理块。
// redacted_thinking 是安全过滤后的变体，同样由注入触发，一并处理。
func isThinkingBlockType(blockType string) bool {
	return blockType == "thinking" || blockType == "redacted_thinking"
}

// stripInjectedThinkingFromResponseBody 从非流式响应体里摘掉推理块。
//
// 返回 (原 body, false) 的情形：content 不是数组、没有推理块、
// 或摘完会导致 content 为空（见文件头边界 2）。
func stripInjectedThinkingFromResponseBody(body []byte) ([]byte, bool) {
	content := gjson.GetBytes(body, "content")
	if !content.IsArray() {
		return body, false
	}

	kept := make([][]byte, 0, len(content.Array()))
	removed := false
	content.ForEach(func(_, block gjson.Result) bool {
		if isThinkingBlockType(block.Get("type").String()) {
			removed = true
			return true
		}
		kept = append(kept, []byte(block.Raw))
		return true
	})

	if !removed || len(kept) == 0 {
		return body, false
	}

	next, err := sjson.SetRawBytes(body, "content", buildJSONArrayRaw(kept))
	if err != nil {
		return body, false
	}
	return next, true
}

// injectedThinkingStreamFilter 在流式响应里丢弃推理块的事件，并把其后各块的
// index 前移，使客户端看到一段连续编号的 content block 序列。
//
// 事件序列（上游 → 客户端）：
//
//	content_block_start index=0 type=thinking   丢弃
//	content_block_delta index=0                 丢弃
//	content_block_stop  index=0                 丢弃
//	content_block_start index=1 type=text       改写为 index=0
//	content_block_delta index=1                 改写为 index=0
//	content_block_stop  index=1                 改写为 index=0
//
// 不重排 index 会在客户端留下从 1 开始的空洞：官方 SDK 按 index 定位 content 数组，
// 跳号会取到不存在的下标。
//
// 零值可用，非并发安全 —— 每个流一个实例，由单个事件循环串行调用。
type injectedThinkingStreamFilter struct {
	droppedIndexes map[int]bool
}

// shiftFor 返回 index 应前移的量，即排在它之前、已被丢弃的块数。
// 按「小于它的丢弃数」计算而非简单累加，这样推理块出现在中段也能算对。
func (f *injectedThinkingStreamFilter) shiftFor(index int) int {
	shift := 0
	for dropped := range f.droppedIndexes {
		if dropped < index {
			shift++
		}
	}
	return shift
}

// apply 处理单个已解析的 SSE 事件。
//
// drop=true 表示该事件属于被摘掉的推理块，调用方不应再下发；
// changed=true 表示事件的 index 已被就地改写，调用方需重新序列化。
func (f *injectedThinkingStreamFilter) apply(eventType string, event map[string]any) (drop bool, changed bool) {
	switch eventType {
	case "content_block_start":
		index, ok := sseEventIndex(event)
		if !ok {
			return false, false
		}
		blockType := ""
		if contentBlock, ok := event["content_block"].(map[string]any); ok {
			blockType, _ = contentBlock["type"].(string)
		}
		if isThinkingBlockType(blockType) {
			if f.droppedIndexes == nil {
				f.droppedIndexes = make(map[int]bool, 1)
			}
			f.droppedIndexes[index] = true
			return true, false
		}
		return false, f.rewriteIndex(event, index)

	case "content_block_delta", "content_block_stop":
		index, ok := sseEventIndex(event)
		if !ok {
			return false, false
		}
		if f.droppedIndexes[index] {
			return true, false
		}
		return false, f.rewriteIndex(event, index)
	}

	return false, false
}

// rewriteIndex 就地前移事件的 index，返回是否发生改写。
func (f *injectedThinkingStreamFilter) rewriteIndex(event map[string]any, index int) bool {
	shift := f.shiftFor(index)
	if shift == 0 {
		return false
	}
	event["index"] = index - shift
	return true
}
