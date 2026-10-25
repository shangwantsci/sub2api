package service

import (
	"context"
	"encoding/json"
)

// ClaudeCodeForwardResult 是一次 CC 请求的完整结果：实际发给 CC 的 turn、
// 上游 result，以及按“只算客户输入”归一化后的 usage。
type ClaudeCodeForwardResult struct {
	Turn   ClaudeCodeTurn
	Result *ClaudeCodeResult
	Usage  ClaudeCodeUsage
}

// RunRequest 是网关层调用 CC 运输的入口：
// 解析客户端 body → 校验/映射 → 进程池执行（可流式回调）→ 归一化 usage。
func (p *ClaudeCodePool) RunRequest(
	ctx context.Context,
	account *Account,
	body []byte,
	model string,
	onEvent func(json.RawMessage) error,
) (*ClaudeCodeForwardResult, error) {
	return p.runRequest(ctx, account, body, model, onEvent, false)
}

// RunRequestReserved 供 scheduler 已用 TryAcquire 预占槽位的路径使用。
func (p *ClaudeCodePool) RunRequestReserved(
	ctx context.Context,
	account *Account,
	body []byte,
	model string,
	onEvent func(json.RawMessage) error,
) (*ClaudeCodeForwardResult, error) {
	return p.runRequest(ctx, account, body, model, onEvent, true)
}

func (p *ClaudeCodePool) runRequest(
	ctx context.Context,
	account *Account,
	body []byte,
	model string,
	onEvent func(json.RawMessage) error,
	reserved bool,
) (*ClaudeCodeForwardResult, error) {
	turn, err := BuildClaudeCodeTurn(body, model)
	if err != nil {
		return nil, newClaudeCodeRequestError(err)
	}
	return p.RunTurn(ctx, account, turn, onEvent, reserved)
}

// RunTurn 执行一个已构建的 turn；runtime 的 session/resume 路径直接使用。
func (p *ClaudeCodePool) RunTurn(ctx context.Context, account *Account, turn ClaudeCodeTurn, onEvent func(json.RawMessage) error, reserved bool) (*ClaudeCodeForwardResult, error) {
	var result *ClaudeCodeResult
	var err error
	if reserved {
		result, err = p.RunWithEventsReserved(ctx, account, turn, onEvent)
	} else {
		result, err = p.RunWithEvents(ctx, account, turn, onEvent)
	}
	if err != nil {
		return nil, classifyClaudeCodeRunError(err)
	}
	return &ClaudeCodeForwardResult{
		Turn:   turn,
		Result: result,
		Usage:  NormalizeClaudeCodeUsage(turn, result),
	}, nil
}
