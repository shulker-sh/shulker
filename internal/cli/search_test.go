package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func searchJSON(t *testing.T, h *harness, args ...string) map[string]any {
	t.Helper()
	stdout := h.mustRun(t, append([]string{"search", "--json"}, args...)...)
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK {
		t.Fatalf("envelope: %+v", env)
	}
	return env.Data.(map[string]any)
}

func TestSearchMergesProvidersIntoOneTable(t *testing.T) {
	h := newHarness(t)
	stdout := h.mustRun(t, "search", "sodium")
	for _, want := range []string{
		"Name", "Slug", "Type", "Side", "Source", "Downloads",
		"Sodium", "sodium", "mod", "client", "both", "379.6M",
		"Add one:", "$ shulker add <slug>",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "AANobbMI") || strings.Contains(stdout, "SODIUM") {
		t.Errorf("a merged row printed its ids or its second name:\n%s", stdout)
	}
	verbose := h.mustRun(t, "search", "sodium", "-v")
	for _, want := range []string{"Modrinth ID", "CurseForge Downloads", "AANobbMI", "394468", "228.1M", "151.4M"} {
		if !strings.Contains(verbose, want) {
			t.Errorf("-v: missing %q in:\n%s", want, verbose)
		}
	}
}

func TestSearchJSONCarriesEveryHit(t *testing.T) {
	h := newHarness(t)
	data := searchJSON(t, h, "fresh animations")
	if data["query"] != "fresh animations" {
		t.Errorf("query %v", data["query"])
	}
	results := data["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results %v", results)
	}
	r := results[0].(map[string]any)
	if r["slug"] != "fresh-animations" || r["title"] != "Fresh Animations" || r["type"] != "resourcepack" ||
		r["side"] != "client" || r["downloads"].(float64) != 9_000_000 {
		t.Errorf("result %v", r)
	}
	hits := r["providers"].([]any)
	if len(hits) != 2 {
		t.Fatalf("providers %v", hits)
	}
	first, second := hits[0].(map[string]any), hits[1].(map[string]any)
	if first["provider"] != "modrinth" || first["id"] != "50dA9Sha" || first["downloads"].(float64) != 5_000_000 {
		t.Errorf("modrinth hit %v", first)
	}
	if second["provider"] != "curseforge" || second["id"] != "600000" || second["title"] != "FRESH-ANIMATIONS" {
		t.Errorf("curseforge hit %v", second)
	}
}

func TestSearchNarrowsByTypeAndLimit(t *testing.T) {
	h := newHarness(t)
	stdout := h.mustRun(t, "search", "fresh", "--type", "resourcepack")
	if !strings.Contains(stdout, "Fresh Animations") || strings.Contains(stdout, "Sodium") {
		t.Errorf("--type resourcepack: %s", stdout)
	}
	results := searchJSON(t, h, "a", "--limit", "1")["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("--limit 1 over two providers: %v", results)
	}
	if r := results[0].(map[string]any); r["slug"] != "fabric-api" || len(r["providers"].([]any)) != 2 {
		t.Errorf("--limit 1 kept %v, want each provider's first result, merged", r)
	}
}

func TestSearchOneProvider(t *testing.T) {
	h := newHarness(t)
	stdout := h.mustRun(t, "search", "sodium", "--provider", "modrinth", "-v")
	if !strings.Contains(stdout, "AANobbMI") || strings.Contains(stdout, "Source") || strings.Contains(stdout, "CurseForge") {
		t.Errorf("--provider modrinth: %s", stdout)
	}
	code, _, stderr := h.run(t, "search", "sodium", "--provider", "nope")
	if code == 0 || !strings.Contains(stderr, "--provider takes one of modrinth, curseforge") {
		t.Errorf("exit %d, stderr %s", code, stderr)
	}
}

func TestSearchWithoutCurseForge(t *testing.T) {
	h := newHarness(t)
	h.noCurseForge = true
	stdout := h.mustRun(t, "search", "sodium", "-v")
	if !strings.Contains(stdout, "AANobbMI") || strings.Contains(stdout, "CurseForge") {
		t.Errorf("search with no key: %s", stdout)
	}
	code, stdout, _ := h.run(t, "search", "sodium", "--provider", "curseforge", "--json")
	if code == 0 || !strings.Contains(stdout, `"provider-unavailable"`) {
		t.Errorf("exit %d, stdout %s", code, stdout)
	}
}

func TestSearchWarnsWhenOneProviderFails(t *testing.T) {
	h := newHarness(t)
	h.cfSearchFails = true
	stdout, stderr := h.mustRunStderr(t, "search", "sodium", "-v")
	if !strings.Contains(stdout, "AANobbMI") || strings.Contains(stdout, "CurseForge") {
		t.Errorf("stdout %s", stdout)
	}
	if !strings.Contains(stderr, "! curseforge search sodium") {
		t.Errorf("stderr %s", stderr)
	}
	code, _, stderr := h.run(t, "search", "sodium", "--provider", "curseforge")
	if code == 0 || !strings.Contains(stderr, "curseforge search sodium") {
		t.Errorf("exit %d, stderr %s", code, stderr)
	}
}

func TestSearchFindsNothing(t *testing.T) {
	h := newHarness(t)
	stdout := h.mustRun(t, "search", "nothingatall")
	if !strings.Contains(stdout, `No projects match "nothingatall"`) || strings.Contains(stdout, "shulker add") {
		t.Errorf("empty search: %s", stdout)
	}
	if results := searchJSON(t, h, "nothingatall")["results"]; len(results.([]any)) != 0 {
		t.Errorf("results %v", results)
	}
}

func TestSearchNeedsWords(t *testing.T) {
	h := newHarness(t)
	h.tty = true
	for _, args := range [][]string{{"search"}, {"search", "--no-input"}} {
		code, _, stderr := h.run(t, args...)
		if code == 0 || !strings.Contains(stderr, "Missing at least one argument") || !strings.Contains(stderr, "[words...]") {
			t.Errorf("%v: exit %d, stderr %s", args, code, stderr)
		}
	}
	code, _, stderr := h.run(t, "search", "sodium", "--limit", "0")
	if code == 0 || !strings.Contains(stderr, "--limit takes a number of results to print") {
		t.Errorf("exit %d, stderr %s", code, stderr)
	}
}

func TestSearchWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	before := projectFiles(t, h.dir)
	h.mustRun(t, "search", "sodium")
	if after := projectFiles(t, h.dir); after != before {
		t.Errorf("search changed the project:\nbefore %s\nafter  %s", before, after)
	}
}

// projectFiles is every file in the project with its bytes, so a command that
// was meant to write nothing can be held to it.
func projectFiles(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.WriteString(path + " " + string(data) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestSearchFailsOnAScopeItCantOpen(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "search", "sodium")
	if e := runError(t, h, "search", "sodium", "-C", h.dir); e.Code != "manifest-not-found" {
		t.Errorf("-C on a folder with no project: %+v", e)
	}
	h.dir = ""
	if e := runError(t, h, "search", "sodium", "-i", "nosuch"); e.Code != "no-instances" {
		t.Errorf("-i naming no instance: %+v", e)
	}
}
