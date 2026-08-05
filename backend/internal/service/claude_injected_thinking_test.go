package service

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// profile 里的 budget 与真身抓包时的 max_tokens 成对，客户端自带的 max_tokens
// 可能远小于它，必须收敛后再注入，否则凑出上游必然 400 的组合。
func TestResolveMimicThinkingBudget(t *testing.T) {
	tests := []struct {
		name          string
		defaultBudget int
		maxTokens     int64
		want          int
	}{
		{name: "profile 无 budget", defaultBudget: 0, maxTokens: 4096, want: 0},
		{name: "max_tokens 缺失时沿用抓包原值", defaultBudget: 31999, maxTokens: 0, want: 31999},
		{name: "max_tokens 大于 budget 用原值", defaultBudget: 31999, maxTokens: 32000, want: 31999},
		{name: "max_tokens 远大于 budget 用原值", defaultBudget: 31999, maxTokens: 64000, want: 31999},
		{name: "max_tokens 等于 budget 需收敛", defaultBudget: 31999, maxTokens: 31999, want: 31998},
		{name: "max_tokens 小于 budget 收敛到 max-1", defaultBudget: 31999, maxTokens: 4096, want: 4095},
		{name: "刚好容纳 API 下限", defaultBudget: 31999, maxTokens: 1025, want: 1024},
		{name: "低于 API 下限则不注入", defaultBudget: 31999, maxTokens: 1024, want: 0},
		{name: "极小 max_tokens 不注入", defaultBudget: 31999, maxTokens: 64, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveMimicThinkingBudget(tt.defaultBudget, tt.maxTokens)
			require.Equal(t, tt.want, got)
			if got > 0 && tt.maxTokens > 0 {
				require.Less(t, int64(got), tt.maxTokens, "budget 必须严格小于 max_tokens")
			}
			if got > 0 {
				require.GreaterOrEqual(t, got, minThinkingBudgetTokens, "budget 不得低于 API 下限")
			}
		})
	}
}

func TestStripInjectedThinkingFromResponseBody(t *testing.T) {
	t.Run("摘掉推理块后 text 成为首块", func(t *testing.T) {
		body := []byte(`{"content":[` +
			`{"type":"thinking","thinking":"","signature":"CAIS"},` +
			`{"type":"text","text":"3973"}]}`)

		got, stripped := stripInjectedThinkingFromResponseBody(body)

		require.True(t, stripped)
		content := gjson.GetBytes(got, "content")
		require.Equal(t, 1, len(content.Array()))
		require.Equal(t, "text", content.Array()[0].Get("type").String())
		require.Equal(t, "3973", content.Array()[0].Get("text").String())
	})

	t.Run("redacted_thinking 同样摘掉", func(t *testing.T) {
		body := []byte(`{"content":[{"type":"redacted_thinking","data":"x"},{"type":"text","text":"hi"}]}`)

		got, stripped := stripInjectedThinkingFromResponseBody(body)

		require.True(t, stripped)
		require.Equal(t, 1, len(gjson.GetBytes(got, "content").Array()))
	})

	t.Run("保留 tool_use 等其它块", func(t *testing.T) {
		body := []byte(`{"content":[` +
			`{"type":"thinking","thinking":"","signature":"CAIS"},` +
			`{"type":"text","text":"hi"},` +
			`{"type":"tool_use","id":"tu_1","name":"get_weather","input":{}}]}`)

		got, stripped := stripInjectedThinkingFromResponseBody(body)

		require.True(t, stripped)
		content := gjson.GetBytes(got, "content").Array()
		require.Equal(t, 2, len(content))
		require.Equal(t, "text", content[0].Get("type").String())
		require.Equal(t, "tool_use", content[1].Get("type").String())
	})

	t.Run("没有推理块时原样返回", func(t *testing.T) {
		body := []byte(`{"content":[{"type":"text","text":"hi"}]}`)

		got, stripped := stripInjectedThinkingFromResponseBody(body)

		require.False(t, stripped)
		require.JSONEq(t, string(body), string(got))
	})

	// 边界 2：长推理耗尽 max_tokens 时响应里可能只有推理块，摘完会让 content 变空。
	// 保留原样，客户端才能从 stop_reason 看出被截断。
	t.Run("摘完会为空时保留原样", func(t *testing.T) {
		body := []byte(`{"content":[{"type":"thinking","thinking":"","signature":"CAIS"}],"stop_reason":"max_tokens"}`)

		got, stripped := stripInjectedThinkingFromResponseBody(body)

		require.False(t, stripped)
		require.JSONEq(t, string(body), string(got))
	})

	t.Run("content 不是数组时原样返回", func(t *testing.T) {
		body := []byte(`{"type":"error","error":{"type":"invalid_request_error"}}`)

		got, stripped := stripInjectedThinkingFromResponseBody(body)

		require.False(t, stripped)
		require.JSONEq(t, string(body), string(got))
	})
}

// mustEvent 把 SSE data 行解析成 processSSEEvent 内部使用的 map 形态。
func mustEvent(t *testing.T, raw string) map[string]any {
	t.Helper()
	var event map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &event))
	return event
}

func TestInjectedThinkingStreamFilter(t *testing.T) {
	t.Run("丢弃推理块事件并把后续 index 前移", func(t *testing.T) {
		f := &injectedThinkingStreamFilter{}

		// 上游 index=0 的推理块：三个事件全部丢弃
		for _, raw := range []string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"x"}}`,
			`{"type":"content_block_stop","index":0}`,
		} {
			event := mustEvent(t, raw)
			drop, _ := f.apply(event["type"].(string), event)
			require.True(t, drop, raw)
		}

		// 上游 index=1 的文本块：保留并前移为 index=0
		for _, raw := range []string{
			`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"3973"}}`,
			`{"type":"content_block_stop","index":1}`,
		} {
			event := mustEvent(t, raw)
			drop, changed := f.apply(event["type"].(string), event)
			require.False(t, drop, raw)
			require.True(t, changed, raw)
			require.Equal(t, 0, event["index"], raw)
		}
	})

	t.Run("非 content_block 事件不受影响", func(t *testing.T) {
		f := &injectedThinkingStreamFilter{}

		for _, raw := range []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
			`{"type":"message_delta","usage":{"output_tokens":5}}`,
			`{"type":"message_stop"}`,
		} {
			event := mustEvent(t, raw)
			drop, changed := f.apply(event["type"].(string), event)
			require.False(t, drop, raw)
			require.False(t, changed, raw)
		}
	})

	t.Run("没有推理块时 index 保持原样", func(t *testing.T) {
		f := &injectedThinkingStreamFilter{}

		event := mustEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		drop, changed := f.apply("content_block_start", event)

		require.False(t, drop)
		require.False(t, changed)
		require.Equal(t, float64(0), event["index"])
	})

	t.Run("推理块出现在中段时按前置丢弃数前移", func(t *testing.T) {
		f := &injectedThinkingStreamFilter{}

		first := mustEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		drop, changed := f.apply("content_block_start", first)
		require.False(t, drop)
		require.False(t, changed)

		mid := mustEvent(t, `{"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":""}}`)
		drop, _ = f.apply("content_block_start", mid)
		require.True(t, drop)

		last := mustEvent(t, `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"tu_1"}}`)
		drop, changed = f.apply("content_block_start", last)
		require.False(t, drop)
		require.True(t, changed)
		require.Equal(t, 1, last["index"], "index=2 前面丢了一块，应前移为 1")
	})

	t.Run("连续两个推理块后文本前移两位", func(t *testing.T) {
		f := &injectedThinkingStreamFilter{}

		for i := 0; i < 2; i++ {
			event := mustEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
			event["index"] = float64(i)
			drop, _ := f.apply("content_block_start", event)
			require.True(t, drop)
		}

		text := mustEvent(t, `{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`)
		drop, changed := f.apply("content_block_start", text)

		require.False(t, drop)
		require.True(t, changed)
		require.Equal(t, 0, text["index"])
	})
}

// 首次实现只在一个 normalizeClaudeOAuthRequestBody 调用点打标记，主转发路径漏掉，
// 线上表现为剥离完全不生效。这里锁住判定语义，路径覆盖由下面的调用点测试保证。
func TestMarkInjectedThinkingIfAdded(t *testing.T) {
	tests := []struct {
		name   string
		before string
		after  string
		want   bool
	}{
		{
			name:   "客户端没带、改写后有 -> 标记",
			before: `{"model":"claude-opus-5","max_tokens":1024}`,
			after:  `{"model":"claude-opus-5","max_tokens":1024,"thinking":{"type":"adaptive"}}`,
			want:   true,
		},
		{
			name:   "客户端自己带了 -> 不标记",
			before: `{"model":"claude-opus-5","thinking":{"type":"enabled","budget_tokens":1024}}`,
			after:  `{"model":"claude-opus-5","thinking":{"type":"enabled","budget_tokens":1024}}`,
			want:   false,
		},
		{
			name:   "客户端带了 disabled、网关未改 -> 不标记",
			before: `{"model":"claude-opus-5","thinking":{"type":"disabled"}}`,
			after:  `{"model":"claude-opus-5","thinking":{"type":"disabled"}}`,
			want:   false,
		},
		{
			name:   "两侧都没有 -> 不标记",
			before: `{"model":"claude-sonnet-4-5-20250929","max_tokens":1024}`,
			after:  `{"model":"claude-sonnet-4-5-20250929","max_tokens":1024}`,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			markInjectedThinkingIfAdded(c, []byte(tt.before), []byte(tt.after))
			require.Equal(t, tt.want, recalledClaudeMimicInjectedThinking(c))
		})
	}

	t.Run("nil context 不 panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			markInjectedThinkingIfAdded(nil, []byte(`{}`), []byte(`{"thinking":{"type":"adaptive"}}`))
		})
	})
}

// 每个会把响应回给客户端的转发路径都必须调用 markInjectedThinkingIfAdded，
// 否则响应侧摘不掉注入的 thinking block。新增转发路径时这条会失败，提醒补上。
func TestAllForwardPathsMarkInjectedThinking(t *testing.T) {
	for _, path := range []string{
		"gateway_forward.go",
		"gateway_claude_oauth_body.go",
	} {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Contains(t, string(src), "markInjectedThinkingIfAdded(",
				"%s 调用了 normalizeClaudeOAuthRequestBody 却没打注入标记", path)
		})
	}
}

func TestRecalledClaudeMimicInjectedThinking(t *testing.T) {
	t.Run("未标记时为 false", func(t *testing.T) {
		require.False(t, recalledClaudeMimicInjectedThinking(fakeGinContext{}))
	})

	t.Run("标记后为 true", func(t *testing.T) {
		c := fakeGinContext{claudeMimicInjectedThinkingKey: true}
		require.True(t, recalledClaudeMimicInjectedThinking(c))
	})

	t.Run("值类型不对时为 false", func(t *testing.T) {
		c := fakeGinContext{claudeMimicInjectedThinkingKey: "yes"}
		require.False(t, recalledClaudeMimicInjectedThinking(c))
	})
}

// fakeGinContext 实现 recalledClaudeMimicInjectedThinking 所需的最小接口。
type fakeGinContext map[string]any

func (f fakeGinContext) Get(key string) (any, bool) {
	value, ok := f[key]
	return value, ok
}
