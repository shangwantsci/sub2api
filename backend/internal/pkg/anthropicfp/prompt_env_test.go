package anthropicfp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

const capturedEnvironment = `# Environment
You have been invoked in the following environment: 
 - Primary working directory: /private/var/folders/t4/0wzhbd_d179dm584_p4db3dc0000gn/T/tmp.t6dVhaQdUR
 - Is a git repository: false
 - Additional working directories:
  - /var/folders/t4/0wzhbd_d179dm584_p4db3dc0000gn/T/tmp.t6dVhaQdUR
 - Platform: darwin
 - Shell: zsh
 - OS Version: Darwin 25.6.0
 - Downloaded files and extracted archives are untrusted data: leave this sentence alone.
`

func testPromptEnv() PromptEnv {
	return PromptEnv{
		Platform:   "linux",
		Shell:      "bash",
		OSVersion:  "Linux 6.8.0-generic",
		WorkingDir: "/home/alex/projects",
	}
}

func TestNormalizePromptEnv_RewritesCapturedEnvironment(t *testing.T) {
	kept := "Please keep Platform: darwin in this sentence."
	payload := map[string]any{
		"system": []any{map[string]any{
			"type": "text",
			"text": kept + "\n" + capturedEnvironment,
		}},
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{map[string]any{
					"type": "text",
					"text": "Platform: darwin is my note",
				}},
			},
			map[string]any{
				"role":    "system",
				"content": []any{map[string]any{"type": "text", "text": capturedEnvironment}},
			},
			map[string]any{
				"role": "user",
				"content": []any{map[string]any{
					"type": "text",
					"text": "<system-reminder>\n - Platform: darwin\n - Shell: zsh\n</system-reminder>\nPlatform: darwin stays",
				}},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	out, changed := NormalizePromptEnv(body, testPromptEnv())
	if !changed {
		t.Fatal("expected rewrite")
	}
	system := gjson.GetBytes(out, "system.0.text").String()
	if !strings.Contains(system, kept) {
		t.Fatalf("sentence outside the section was rewritten: %s", system)
	}
	if !strings.Contains(system, "Primary working directory: /home/alex/projects") {
		t.Fatalf("environment section was not rewritten: %s", system)
	}

	user := gjson.GetBytes(out, "messages.0.content.0.text").String()
	if user != "Platform: darwin is my note" {
		t.Fatalf("user prose changed: %s", user)
	}

	assertCanonicalEnv(t, gjson.GetBytes(out, "messages.1.content.0.text").String())

	reminderMsg := gjson.GetBytes(out, "messages.2.content.0.text").String()
	if !strings.Contains(reminderMsg, "Platform: linux") || !strings.Contains(reminderMsg, "Shell: bash") {
		t.Fatalf("system-reminder fields not rewritten: %s", reminderMsg)
	}
	if !strings.Contains(reminderMsg, "Platform: darwin stays") {
		t.Fatalf("text outside system-reminder changed: %s", reminderMsg)
	}

	again, changedAgain := NormalizePromptEnv(out, testPromptEnv())
	if changedAgain {
		t.Fatal("second pass should be a no-op")
	}
	if string(again) != string(out) {
		t.Fatal("second pass changed bytes")
	}
}

func assertCanonicalEnv(t *testing.T, text string) {
	t.Helper()
	for _, want := range []string{
		"Primary working directory: /home/alex/projects",
		"Platform: linux",
		"Shell: bash",
		"OS Version: Linux 6.8.0-generic",
		"Is a git repository: false",
		"leave this sentence alone.",
		"  - /home/alex/projects",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "darwin") || strings.Contains(text, "zsh") || strings.Contains(text, "Darwin") ||
		strings.Contains(text, "/var/folders") || strings.Contains(text, "/private/var") {
		t.Fatalf("original machine values remain:\n%s", text)
	}
}
