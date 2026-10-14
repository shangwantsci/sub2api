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
	"github.com/imroc/req/v3"
)

// nurturingPrompts 是养号用的英文日常对话内容库。
var nurturingPrompts = []string{
	// --- 日常生活 ---
	"What's a good recipe for pasta?",
	"What are some tips for better sleep?",
	"How do I organize my desk better?",
	"What are some easy houseplants to grow?",
	"Can you suggest a healthy breakfast idea?",
	"What's the best way to clean a stainless steel pan?",
	"How do I get rid of fruit flies in my kitchen?",
	"Recommend a good podcast for commuting",
	"What's a fun weekend activity that doesn't cost much?",
	"How do I make cold brew coffee at home?",
	"What are some good stretches to do after sitting all day?",
	"Suggest a quick dinner recipe with chicken",
	"How do I remove a coffee stain from a white shirt?",
	"What's the best way to store fresh herbs?",
	"Recommend a good movie to watch tonight",
	"How do I set up a basic home gym?",
	"What are some good travel destinations in Southeast Asia?",
	"How do I train my dog to stop barking?",
	"What's a good playlist for working out?",
	"How do I properly care for a cast iron skillet?",
	// --- 技术/编程 ---
	"Write a Python function to parse a CSV file",
	"Explain the difference between async and await in JavaScript",
	"What's the best way to handle errors in Go?",
	"How do I set up a virtual environment in Python?",
	"Explain what a REST API is in simple terms",
	"What's the difference between SQL and NoSQL databases?",
	"How do I reverse a linked list in Python?",
	"Explain the concept of dependency injection",
	"What's the difference between a stack and a queue?",
	"How do I write a basic Dockerfile?",
	"Explain what Git rebasing is and when to use it",
	"How do I read a JSON file in Node.js?",
	"What's the difference between TCP and UDP?",
	"Explain how HTTPS works in simple terms",
	"How do I profile a slow Python script?",
	// --- 科学/教育 ---
	"Can you explain how photosynthesis works?",
	"Tell me an interesting fact about space",
	"What's the difference between weather and climate?",
	"Explain the water cycle in simple terms",
	"How does a refrigerator work?",
	"What causes the Northern Lights?",
	"How do vaccines work at a cellular level?",
	"What's the theory of relativity in simple terms?",
	"How do black holes form?",
	"Why is the sky blue?",
	// --- 创意/写作 ---
	"Write a short poem about autumn leaves",
	"Suggest a creative writing prompt for a mystery story",
	"Help me come up with a catchy name for a bakery",
	"Write a haiku about the ocean",
	"Can you rewrite this sentence to sound more professional: 'I wanna talk about the thing'",
	"What are some tips for writing better emails?",
	"Help me brainstorm ideas for a birthday gift",
	"Write a short story opening set in a coffee shop",
	"Suggest five creative blog post titles about productivity",
	"How do I write a compelling book review?",
	// --- 工作/效率 ---
	"What's the Pomodoro technique and how do I use it?",
	"How do I prioritize tasks when everything seems urgent?",
	"Suggest a good note-taking system",
	"What are some tips for giving a better presentation?",
	"How do I write a concise project status update?",
}

// nurturingModels 是养号聊天使用的模型列表。
var nurturingModels = []string{
	"claude-sonnet-4-6",
	"claude-haiku-4-5-20251001",
}

// sendNurturingChat 用 sessionKey 在 claude.ai 发起一次轻量 Chat 对话。
// 使用 nurture() 传入的共享 web 客户端（含 CookieJar + 正确浏览器 header）。
// 遇到 401/403 时返回带标记的 error 供 nurture() 熔断。
func (s *AccountNurturingService) sendNurturingChat(ctx context.Context, account *Account, client *req.Client) error {
	if account == nil {
		return nil
	}

	sessionKey := strings.TrimSpace(account.GetCredential("session_key"))
	orgUUID := strings.TrimSpace(account.GetExtraString("org_uuid"))
	if sessionKey == "" || orgUUID == "" {
		return nil
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
		return classifyNurturingHTTPError(createResp.StatusCode, createResp.Header, createResp.Bytes(),
			fmt.Sprintf("create conversation: status %d", createResp.StatusCode))
	}

	prompt := nurturingPrompts[rand.Intn(len(nurturingPrompts))]

	model := nurturingModels[0]
	if rand.Intn(100) < 30 {
		model = nurturingModels[1]
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
			"model":          model,
			"timezone":       "America/New_York",
			"locale":         "en-US",
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
		return classifyNurturingHTTPError(completionResp.StatusCode, completionResp.Header, nil,
			fmt.Sprintf("send completion: status %d", completionResp.StatusCode))
	}

	if rand.Intn(100) < 15 {
		_, _ = io.Copy(io.Discard, completionResp.Body)
	} else {
		readSize := int64(1024 + rand.Intn(7168))
		_, _ = io.Copy(io.Discard, io.LimitReader(completionResp.Body, readSize))
	}

	logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d chat OK (model=%s)", account.ID, model)

	// 60% 概率删除对话，避免堆积只有一条消息的僵尸对话。
	time.Sleep(time.Duration(3+rand.Intn(6)) * time.Second)
	if rand.Intn(100) < 60 {
		deleteURL := fmt.Sprintf("https://claude.ai/api/organizations/%s/chat_conversations/%s", orgUUID, convUUID)
		delResp, delErr := client.R().
			SetContext(ctx).
			SetCookies(cookie).
			SetHeader("Content-Type", "application/json").
			Delete(deleteURL)
		if delErr != nil {
			logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d delete conv failed: %v", account.ID, delErr)
		} else if !delResp.IsSuccessState() {
			logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d delete conv status %d", account.ID, delResp.StatusCode)
		} else {
			logger.LegacyPrintf("service.nurturing", "[Nurturing] Account %d conv deleted", account.ID)
		}
	}

	return nil
}
