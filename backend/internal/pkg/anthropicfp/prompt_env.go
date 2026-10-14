package anthropicfp

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// PromptEnv 是要写进系统提示环境小节的固定值。
type PromptEnv struct {
	Platform   string
	Shell      string
	OSVersion  string
	WorkingDir string
}

func (e PromptEnv) usable() bool {
	return e.Platform != "" && e.Shell != "" && e.OSVersion != "" && e.WorkingDir != ""
}

var (
	environmentHeading  = regexp.MustCompile(`(?m)^# Environment\b`)
	nextMarkdownHeading = regexp.MustCompile(`(?m)^# `)
	primaryWorkingDirRe = regexp.MustCompile(`Primary working directory:[ \t]*\S+`)
	platformLineRe      = regexp.MustCompile(`Platform:[ \t]*\S+`)
	shellLineRe         = regexp.MustCompile(`Shell:[ \t]*\S+`)
	osVersionLineRe     = regexp.MustCompile(`OS Version:[ \t]*[^\n]+`)
	pathBulletRe        = regexp.MustCompile(`^([ \t]+-[ \t]+)(/\S+)\s*$`)
	systemReminderBlock = regexp.MustCompile(`(?s)(<system-reminder>)([\s\S]*?)(</system-reminder>)`)
)

// NormalizePromptEnv 把 Claude Code 写进系统提示的环境小节换成固定档案。
// 顶层 system，以及 messages 里 role=system 的文本，只改 `# Environment` 小节。
// 用户和助手消息只改 `<system-reminder>` 里再次出现的同样字段。
// 没有这些字段时原样返回。
func NormalizePromptEnv(body []byte, env PromptEnv) ([]byte, bool) {
	if len(body) == 0 || !env.usable() {
		return body, false
	}
	out := body
	changed := false

	sys := gjson.GetBytes(out, "system")
	if sys.Exists() {
		switch {
		case sys.Type == gjson.String:
			normalized := rewriteEnvironmentSections(sys.String(), env)
			if normalized != sys.String() {
				if next, err := sjson.SetBytes(out, "system", normalized); err == nil {
					out = next
					changed = true
				}
			}
		case sys.IsArray():
			idx := 0
			sys.ForEach(func(_, item gjson.Result) bool {
				if item.Get("type").String() == "text" {
					t := item.Get("text")
					if t.Exists() && t.Type == gjson.String {
						normalized := rewriteEnvironmentSections(t.String(), env)
						if normalized != t.String() {
							path := fmt.Sprintf("system.%d.text", idx)
							if next, err := sjson.SetBytes(out, path, normalized); err == nil {
								out = next
								changed = true
							}
						}
					}
				}
				idx++
				return true
			})
		}
	}

	messages := gjson.GetBytes(out, "messages")
	if messages.IsArray() {
		msgIdx := -1
		messages.ForEach(func(_, msg gjson.Result) bool {
			msgIdx++
			role := msg.Get("role").String()
			content := msg.Get("content")
			if !content.Exists() {
				return true
			}
			switch {
			case content.Type == gjson.String:
				normalized := rewriteMessageEnvironment(role, content.String(), env)
				if normalized != content.String() {
					path := fmt.Sprintf("messages.%d.content", msgIdx)
					if next, err := sjson.SetBytes(out, path, normalized); err == nil {
						out = next
						changed = true
					}
				}
			case content.IsArray():
				contentIdx := -1
				content.ForEach(func(_, block gjson.Result) bool {
					contentIdx++
					if block.Get("type").String() != "text" {
						return true
					}
					t := block.Get("text")
					if !t.Exists() || t.Type != gjson.String {
						return true
					}
					normalized := rewriteMessageEnvironment(role, t.String(), env)
					if normalized != t.String() {
						path := fmt.Sprintf("messages.%d.content.%d.text", msgIdx, contentIdx)
						if next, err := sjson.SetBytes(out, path, normalized); err == nil {
							out = next
							changed = true
						}
					}
					return true
				})
			}
			return true
		})
	}

	if !changed {
		return body, false
	}
	return out, true
}

func rewriteEnvironmentSections(text string, env PromptEnv) string {
	if !strings.Contains(text, "# Environment") {
		return text
	}
	var b strings.Builder
	rest := text
	for {
		loc := environmentHeading.FindStringIndex(rest)
		if loc == nil {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:loc[0]])
		tail := rest[loc[1]:]
		end := len(tail)
		if m := nextMarkdownHeading.FindStringIndex(tail); m != nil {
			end = m[0]
		}
		section := rest[loc[0] : loc[1]+end]
		b.WriteString(rewriteEnvironmentFields(section, env))
		rest = tail[end:]
	}
	return b.String()
}

// role=system 的消息是 Claude Code 放进 messages 数组的系统提示，环境小节在这里。
// 用户和助手消息只处理 system-reminder，避免改到用户正文。
func rewriteMessageEnvironment(role, text string, env PromptEnv) string {
	if role == "system" {
		return rewriteEnvironmentSections(text, env)
	}
	return rewriteSystemReminderEnvironment(text, env)
}

func rewriteSystemReminderEnvironment(text string, env PromptEnv) string {
	if !strings.Contains(text, "<system-reminder>") {
		return text
	}
	return systemReminderBlock.ReplaceAllStringFunc(text, func(block string) string {
		parts := systemReminderBlock.FindStringSubmatch(block)
		if len(parts) != 4 {
			return block
		}
		inner := rewriteEnvironmentSections(parts[2], env)
		inner = rewriteEnvironmentFields(inner, env)
		if inner == parts[2] {
			return block
		}
		return parts[1] + inner + parts[3]
	})
}

func rewriteEnvironmentFields(section string, env PromptEnv) string {
	out := primaryWorkingDirRe.ReplaceAllString(section, "Primary working directory: "+env.WorkingDir)
	out = platformLineRe.ReplaceAllString(out, "Platform: "+env.Platform)
	out = shellLineRe.ReplaceAllString(out, "Shell: "+env.Shell)
	out = osVersionLineRe.ReplaceAllString(out, "OS Version: "+strings.TrimRight(env.OSVersion, " \t"))
	if strings.Contains(out, "Additional working directories:") {
		out = rewriteAdditionalWorkingDirs(out, env.WorkingDir)
	}
	return out
}

func rewriteAdditionalWorkingDirs(section, workingDir string) string {
	lines := strings.Split(section, "\n")
	inAdditional := false
	for i, line := range lines {
		if strings.Contains(line, "Additional working directories:") {
			inAdditional = true
			continue
		}
		if !inAdditional {
			continue
		}
		if pathBulletRe.MatchString(line) {
			lines[i] = pathBulletRe.ReplaceAllString(line, "${1}"+workingDir)
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		inAdditional = false
	}
	return strings.Join(lines, "\n")
}
