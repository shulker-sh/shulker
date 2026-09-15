package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func docsJSON(t *testing.T, wantExit int, args ...string) map[string]any {
	t.Helper()
	code, stdout, _ := run(t, append([]string{"docs", "--json"}, args...)...)
	if code != wantExit {
		t.Fatalf("docs %q: exit %d, want %d: %s", args, code, wantExit, stdout)
	}
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error != nil {
		return map[string]any{"code": env.Error.Code, "candidates": env.Error.Candidates}
	}
	return env.Data.(map[string]any)
}

func TestDocsIndexListsPages(t *testing.T) {
	pages := docsJSON(t, out.ExitOK)["pages"].([]any)
	if len(pages) != 5 || pages[0].(map[string]any)["name"] != "getting-started" {
		t.Fatalf("pages %v", pages)
	}
	code, stdout, _ := run(t, "docs")
	if code != out.ExitOK || !strings.Contains(stdout, "• cli ") || !strings.Contains(stdout, "$ shulker docs add") {
		t.Fatalf("exit %d, output %q", code, stdout)
	}
}

func TestDocsPrintsSectionMarkdown(t *testing.T) {
	data := docsJSON(t, out.ExitOK, "add")
	markdown, _ := data["markdown"].(string)
	if data["page"] != "cli" || data["heading"] != "shulker add" || !strings.HasPrefix(markdown, "### `shulker add`\n") || strings.Contains(markdown, "### `shulker remove`") {
		t.Fatalf("data %v", data)
	}
	_, stdout, _ := run(t, "docs", "add")
	if stdout != markdown {
		t.Fatalf("human output is not the plain markdown: %q", stdout)
	}
}

func TestDocsListsMatchesWithCommands(t *testing.T) {
	matches := docsJSON(t, out.ExitOK, "requireKey")["matches"].([]any)
	var commands []string
	for _, m := range matches {
		commands = append(commands, m.(map[string]any)["command"].(string))
	}
	joined := strings.Join(commands, "\n")
	if !strings.Contains(joined, "shulker docs manifest requirekey") || !strings.Contains(joined, "shulker docs lock requirekey") {
		t.Fatalf("commands %q", commands)
	}
}

func TestDocsSearch(t *testing.T) {
	data := docsJSON(t, out.ExitOK, "--search", "build", "DIRECTORY")
	matches := data["matches"].([]any)
	first := matches[0].(map[string]any)
	if data["query"] != "build DIRECTORY" || first["line"] == nil || first["command"] == "" {
		t.Fatalf("data %v", data)
	}
	if got := docsJSON(t, out.ExitOK, "--search", "qqqq-nothing")["matches"].([]any); len(got) != 0 {
		t.Fatalf("matches %v", got)
	}
	code, _, _ := run(t, "docs", "--search")
	if code != out.ExitUsage {
		t.Fatalf("--search without a phrase exited %d", code)
	}
}

func TestDocsTopicNotFound(t *testing.T) {
	data := docsJSON(t, out.ExitError, "qqqq-nothing")
	if data["code"] != "topic-not-found" || len(data["candidates"].([]string)) != 5 {
		t.Fatalf("data %v", data)
	}
}

func TestExcerptTrimsAroundTheMatch(t *testing.T) {
	theme := out.Theme{}
	line := strings.Repeat("a ", 100) + "see [`shulker pull [file...]`](https://shulker.sh/docs/cli#shulker-pull) for the build directory " + strings.Repeat("z ", 100)
	got := excerpt(theme, line, "build  directory")
	if !strings.Contains(got, "build directory") || strings.Contains(got, "https://") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("excerpt %q", got)
	}
	if n := len([]rune(got)); n > docsExcerptWidth+2 {
		t.Fatalf("excerpt is %d runes", n)
	}
	if got := excerpt(theme, "short", "absent"); got != "short" {
		t.Fatalf("excerpt without a match %q", got)
	}
}
