package service

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
)

// nurturingPrompts 是养号用的随机日常对话内容库。
var nurturingPrompts = []string{
	"What's a good recipe for pasta?",
	"Can you explain how photosynthesis works?",
	"What are some tips for better sleep?",
	"Tell me an interesting fact about space",
	"How do I organize my desk better?",
	"What's the difference between weather and climate?",
	"Suggest a good book to read this weekend",
	"How does a refrigerator work?",
	"What are some easy houseplants to grow?",
	"Explain the water cycle in simple terms",
}

// sendNurturingChat 用 sessionKey 在 claude.ai 发起一次轻量 Chat 对话。
// 步骤：创建 conversation -> 发送一条消息 -> 读取 SSE 前 1KB -> 关闭。
// 整个过程耗时约几秒，不需要解析完整响应。
func (s *AccountNurturingService) sendNurturingChat(ctx context.Context, account *Account) error {
	if account == nil {
		return nil
	}

	sessionKey := strings.TrimSpace(account.GetCredential("session_key"))
	orgUUID := strings.TrimSpace(account.GetExtraString("org_uuid"))
	if sessionKey == "" || orgUUID == "" {
		return nil
	}

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	client, err := createNurturingClient(proxyURL)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	cookie := &http.Cookie{Name: "sessionKey", Value: sessionKey}
	convUUID := uuid.New().String()

	createURL := fmt.Sprintf("https://claude.ai/api/organizations/%s/chat_conversations", orgUUID)
	createResp, err := client.R().
		SetContext(ctx).
		SetCookies(cookie).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]any{
			"name": "",
			"uuid": convUUID,
		}).
		Post(createURL)
	if err != nil {
		return fmt.Errorf("create conversation: %w", err)
	}
	if !createResp.IsSuccessState() {
		return fmt.Errorf("create conversation: status %d", createResp.StatusCode)
	}

	prompt := nurturingPrompts[rand.Intn(len(nurturingPrompts))]
	persona := account.GetPersonaEnvelope()
	timezone := strings.TrimSpace(persona.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	locale := strings.TrimSpace(persona.Locale)
	if locale == "" {
		locale = "en-US"
	}

	chatCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	completionURL := fmt.Sprintf(
		"https://claude.ai/api/organizations/%s/chat_conversations/%s/completion",
		orgUUID, convUUID,
	)
	completionResp, err := client.R().
		SetContext(chatCtx).
		SetCookies(cookie).
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "text/event-stream").
		SetBody(map[string]any{
			"prompt":         prompt,
			"model":          "claude-sonnet-4-6",
			"timezone":       timezone,
			"locale":         locale,
			"rendering_mode": "messages",
		}).
		DisableAutoReadResponse().
		Post(completionURL)
	if err != nil {
		return fmt.Errorf("send completion: %w", err)
	}
	defer func() {
		if completionResp != nil && completionResp.Body != nil {
			_ = completionResp.Body.Close()
		}
	}()

	if completionResp.StatusCode < 200 || completionResp.StatusCode >= 300 {
		return fmt.Errorf("send completion: status %d", completionResp.StatusCode)
	}

	// 只读前 1KB SSE 留下会话痕迹，无需解析完整流。
	_, _ = io.Copy(io.Discard, io.LimitReader(completionResp.Body, 1024))

	logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d chat OK", account.ID)
	return nil
}
