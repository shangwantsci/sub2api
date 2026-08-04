package service

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func TestCountClaudeMimicInjectedInputTokensSkipsCachedBlocks(t *testing.T) {
	// 默认 3-block 形态：体量最大的扩充块带 cache_control，落在 cache_creation/cache_read
	// 维度，绝不能计入 input_tokens 的扣除量。
	body := []byte(`{
		"system": [
			{"type": "text", "text": "cc_version=2.1.206; cc_entrypoint=sdk-cli;"},
			{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."},
			{"type": "text", "text": "A very long expansion prompt that must not be deducted.", "cache_control": {"type": "ephemeral", "ttl": "5m"}}
		],
		"messages": [{"role": "user", "content": "hi"}]
	}`)

	got := countClaudeMimicInjectedInputTokens(body)

	codec, err := claudeInjectedTokenizer()
	if err != nil {
		t.Fatal(err)
	}
	var want int
	for _, s := range []string{
		"cc_version=2.1.206; cc_entrypoint=sdk-cli;",
		"You are Claude Code, Anthropic's official CLI for Claude.",
	} {
		n, err := codec.Count(s)
		if err != nil {
			t.Fatal(err)
		}
		want += n
	}
	if got != want {
		t.Fatalf("got %d, want %d (cached block must be excluded)", got, want)
	}

	// 反向确认：把带 cache_control 的那块算进来会明显更大。
	cachedOnly, _ := codec.Count("A very long expansion prompt that must not be deducted.")
	if cachedOnly == 0 {
		t.Fatal("test setup: cached block should be non-empty")
	}
	if got >= want+cachedOnly {
		t.Fatal("cached block leaked into the deduction")
	}
}

func TestCountClaudeMimicInjectedInputTokensIdentityOnly(t *testing.T) {
	// identity_only 形态：两块都不带 cache_control，都落在 input_tokens。
	blocks, err := buildClaudeOAuthSystemPromptBlocksJSON(
		[]byte(`{"model":"claude-fable-5","messages":[{"role":"user","content":"hi"}]}`),
		"hi", "", claudeOAuthIdentityOnlyBlocksConfig,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("identity_only should yield 2 blocks, got %d", len(blocks))
	}
	for i, b := range blocks {
		if gjson.GetBytes(b, "cache_control").Exists() {
			t.Fatalf("identity_only block[%d] must not carry cache_control", i)
		}
	}

	raw := make([]json.RawMessage, 0, len(blocks))
	for _, b := range blocks {
		raw = append(raw, b)
	}
	system, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"system":` + string(system) + `,"messages":[{"role":"user","content":"hi"}]}`)

	got := countClaudeMimicInjectedInputTokens(body)
	if got <= 0 {
		t.Fatalf("expected identity_only injection to be counted, got %d", got)
	}
	// 身份两块体量很小，远低于带 cache_control 的扩充块（约 2539）。
	if got > 200 {
		t.Fatalf("identity_only injection unexpectedly large: %d tokens", got)
	}
	t.Logf("identity_only injected into input_tokens: %d tokens", got)
}

func TestCountClaudeMimicInjectedInputTokensNonArraySystem(t *testing.T) {
	// 裸字符串 system 只出现在未伪装路径，不该被当成注入量。
	body := []byte(`{"system":"You are a helpful assistant.","messages":[]}`)
	if got := countClaudeMimicInjectedInputTokens(body); got != 0 {
		t.Fatalf("non-array system must yield 0, got %d", got)
	}
	if got := countClaudeMimicInjectedInputTokens([]byte(`{"messages":[]}`)); got != 0 {
		t.Fatalf("missing system must yield 0, got %d", got)
	}
}

func TestRememberInjectedInputTokensOverwritesNotAccumulates(t *testing.T) {
	// 重试会重建上游请求并再次记录，必须覆盖而非累加。
	c := &gin.Context{}
	if got := recalledClaudeMimicInjectedInputTokens(c); got != 0 {
		t.Fatalf("empty context should yield 0, got %d", got)
	}

	rememberClaudeMimicInjectedInputTokens(c, 41)
	rememberClaudeMimicInjectedInputTokens(c, 43)
	if got := recalledClaudeMimicInjectedInputTokens(c); got != 43 {
		t.Fatalf("got %d, want 43 (overwrite, not accumulate)", got)
	}
}

func TestDeductClaudeMimicInjectedInputTokens(t *testing.T) {
	tests := []struct {
		name         string
		official     int
		injected     int
		wantBillable int
		wantDeducted bool
	}{
		{"normal", 5127, 41, 5086, true},
		{"no injection", 5127, 0, 0, false},
		{"negative injection", 5127, -5, 0, false},
		{"injection equals official", 41, 41, 0, false},
		{"injection exceeds official", 30, 41, 0, false},
		{"leaves exactly one", 42, 41, 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			billable, ok := deductClaudeMimicInjectedInputTokens(tc.official, tc.injected)
			if ok != tc.wantDeducted {
				t.Fatalf("deducted: got %v, want %v", ok, tc.wantDeducted)
			}
			if ok && billable != tc.wantBillable {
				t.Fatalf("billable: got %d, want %d", billable, tc.wantBillable)
			}
		})
	}
}

func TestRewriteInputTokensInJSON(t *testing.T) {
	body := []byte(`{"id":"msg_1","usage":{"input_tokens":5127,"output_tokens":12,"cache_read_input_tokens":2539}}`)

	next, official, billable, ok := rewriteInputTokensInJSON(body, "usage", 41)
	if !ok {
		t.Fatal("expected rewrite to apply")
	}
	if official != 5127 || billable != 5086 {
		t.Fatalf("got official=%d billable=%d", official, billable)
	}
	if got := gjson.GetBytes(next, "usage.input_tokens").Int(); got != 5086 {
		t.Fatalf("input_tokens: got %d, want 5086", got)
	}
	// 其余 usage 字段必须原样保留，尤其 cache 维度。
	if got := gjson.GetBytes(next, "usage.cache_read_input_tokens").Int(); got != 2539 {
		t.Fatalf("cache_read_input_tokens must be untouched, got %d", got)
	}
	if got := gjson.GetBytes(next, "usage.output_tokens").Int(); got != 12 {
		t.Fatalf("output_tokens must be untouched, got %d", got)
	}

	// 缺字段或不可扣时保持原样。
	if _, _, _, ok := rewriteInputTokensInJSON([]byte(`{"usage":{}}`), "usage", 41); ok {
		t.Fatal("missing input_tokens must not rewrite")
	}
	if _, _, _, ok := rewriteInputTokensInJSON(body, "usage", 99999); ok {
		t.Fatal("oversized injection must not rewrite")
	}
}

func TestRewriteInputTokensInMap(t *testing.T) {
	usage := map[string]any{"input_tokens": float64(5127), "cache_read_input_tokens": float64(2539)}

	official, billable, ok := rewriteInputTokensInMap(usage, 41)
	if !ok {
		t.Fatal("expected rewrite to apply")
	}
	if official != 5127 || billable != 5086 {
		t.Fatalf("got official=%d billable=%d", official, billable)
	}
	if usage["input_tokens"] != 5086 {
		t.Fatalf("input_tokens: got %v, want 5086", usage["input_tokens"])
	}
	if usage["cache_read_input_tokens"] != float64(2539) {
		t.Fatalf("cache_read_input_tokens must be untouched, got %v", usage["cache_read_input_tokens"])
	}

	if _, _, ok := rewriteInputTokensInMap(map[string]any{}, 41); ok {
		t.Fatal("missing input_tokens must not rewrite")
	}
}

func TestStreamingUsagePatchIsTakenBeforeDeduction(t *testing.T) {
	// 计费口径必须取自上游原始 usage：extractSSEUsagePatch 先跑，改写后跑。
	// 顺序颠倒会把扣减后的数字记进账，这里锁死该不变式。
	svc := &GatewayService{}
	event := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"usage": map[string]any{"input_tokens": float64(5127)},
		},
	}

	patch := svc.extractSSEUsagePatch(event)
	if patch == nil || patch.inputTokens != 5127 {
		t.Fatalf("billing patch must capture the upstream value, got %+v", patch)
	}

	usage := event["message"].(map[string]any)["usage"].(map[string]any)
	if _, billable, ok := rewriteInputTokensInMap(usage, 41); !ok || billable != 5086 {
		t.Fatalf("display rewrite failed: billable=%d ok=%v", billable, ok)
	}
	// 改写之后，先前取到的计费快照不受影响。
	if patch.inputTokens != 5127 {
		t.Fatalf("billing snapshot was mutated to %d", patch.inputTokens)
	}
}
