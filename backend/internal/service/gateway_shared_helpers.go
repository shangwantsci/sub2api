package service

import (
	"crypto/sha256"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// deleteJSONPathBytes 删除 JSON 路径；路径不存在/删除失败时返回原 body 与 false。
func deleteJSONPathBytes(body []byte, path string) ([]byte, bool) {
	next, err := sjson.DeleteBytes(body, path)
	if err != nil {
		return body, false
	}
	return next, true
}

// generateSessionUUID 由种子生成稳定的 UUIDv4（OpenAI 兼容层 session_id 隔离用）。
func generateSessionUUID(seed string) string {
	if seed == "" {
		return uuid.NewString()
	}
	hash := sha256.Sum256([]byte(seed))
	bytes := hash[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

type cacheControlPath struct {
	path string
	log  string
}

func collectCacheControlPaths(body []byte) (invalidThinking []cacheControlPath, messagePaths []string, toolPaths []string, systemPaths []string) {
	system := gjson.GetBytes(body, "system")
	if system.IsArray() {
		sysIndex := 0
		system.ForEach(func(_, item gjson.Result) bool {
			if item.Get("cache_control").Exists() {
				path := fmt.Sprintf("system.%d.cache_control", sysIndex)
				if item.Get("type").String() == "thinking" {
					invalidThinking = append(invalidThinking, cacheControlPath{
						path: path,
						log:  "[Warning] Removed illegal cache_control from thinking block in system",
					})
				} else {
					systemPaths = append(systemPaths, path)
				}
			}
			sysIndex++
			return true
		})
	}

	messages := gjson.GetBytes(body, "messages")
	if messages.IsArray() {
		msgIndex := 0
		messages.ForEach(func(_, msg gjson.Result) bool {
			content := msg.Get("content")
			if content.IsArray() {
				contentIndex := 0
				content.ForEach(func(_, item gjson.Result) bool {
					if item.Get("cache_control").Exists() {
						path := fmt.Sprintf("messages.%d.content.%d.cache_control", msgIndex, contentIndex)
						if item.Get("type").String() == "thinking" {
							invalidThinking = append(invalidThinking, cacheControlPath{
								path: path,
								log:  fmt.Sprintf("[Warning] Removed illegal cache_control from thinking block in messages[%d].content[%d]", msgIndex, contentIndex),
							})
						} else {
							messagePaths = append(messagePaths, path)
						}
					}
					contentIndex++
					return true
				})
			}
			msgIndex++
			return true
		})
	}

	tools := gjson.GetBytes(body, "tools")
	if tools.IsArray() {
		toolIndex := 0
		tools.ForEach(func(_, tool gjson.Result) bool {
			if tool.Get("cache_control").Exists() {
				toolPaths = append(toolPaths, fmt.Sprintf("tools.%d.cache_control", toolIndex))
			}
			toolIndex++
			return true
		})
	}

	return invalidThinking, messagePaths, toolPaths, systemPaths
}

// enforceCacheControlLimit 强制执行 cache_control 块数量限制（最多 4 个）。
// 超限时优先移除工具断点，再移除 messages 断点，最后才移除 system 断点。
func enforceCacheControlLimit(body []byte) []byte {
	if len(body) == 0 {
		return body
	}

	invalidThinking, messagePaths, toolPaths, systemPaths := collectCacheControlPaths(body)
	out := body
	modified := false

	for _, item := range invalidThinking {
		if !gjson.GetBytes(out, item.path).Exists() {
			continue
		}
		next, ok := deleteJSONPathBytes(out, item.path)
		if !ok {
			continue
		}
		out = next
		modified = true
		logger.LegacyPrintf("service.gateway", "%s", item.log)
	}

	count := len(messagePaths) + len(toolPaths) + len(systemPaths)
	if count <= maxCacheControlBlocks {
		if modified {
			return out
		}
		return body
	}

	remaining := count - maxCacheControlBlocks
	for i := len(toolPaths) - 1; i >= 0 && remaining > 0; i-- {
		path := toolPaths[i]
		if !gjson.GetBytes(out, path).Exists() {
			continue
		}
		next, ok := deleteJSONPathBytes(out, path)
		if !ok {
			continue
		}
		out = next
		modified = true
		remaining--
	}

	for _, path := range messagePaths {
		if remaining <= 0 {
			break
		}
		if !gjson.GetBytes(out, path).Exists() {
			continue
		}
		next, ok := deleteJSONPathBytes(out, path)
		if !ok {
			continue
		}
		out = next
		modified = true
		remaining--
	}

	for i := len(systemPaths) - 1; i >= 0 && remaining > 0; i-- {
		path := systemPaths[i]
		if !gjson.GetBytes(out, path).Exists() {
			continue
		}
		next, ok := deleteJSONPathBytes(out, path)
		if !ok {
			continue
		}
		out = next
		modified = true
		remaining--
	}

	if modified {
		return out
	}
	return body
}
