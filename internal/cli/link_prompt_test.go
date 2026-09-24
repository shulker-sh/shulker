package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
)

// scripted answers each question by its title, as a player at a terminal would, and records what
// was asked. A question with no answer fails the run, so a test also pins which questions come.
type scripted struct {
	answers map[string]string
	// marks answers a search by title: each step types its query, then marks one of the rows.
	marks map[string][]markStep
	asked []string
}

func (s *scripted) Pick(title string, choices []out.Choice, _ io.Reader) (string, error) {
	s.asked = append(s.asked, title)
	answer, ok := s.answers[title]
	if !ok {
		return "", fmt.Errorf("unexpected question %q", title)
	}
	if !slices.ContainsFunc(choices, func(c out.Choice) bool { return c.Value == answer }) {
		return "", fmt.Errorf("%q offers no %q among %v", title, answer, choices)
	}
	return answer, nil
}

func (s *scripted) Ask(title, _, placeholder string, _ io.Reader) (string, error) {
	s.asked = append(s.asked, title)
	answer, ok := s.answers[title]
	if !ok {
		return "", fmt.Errorf("unexpected question %q", title)
	}
	if answer == "" {
		return placeholder, nil
	}
	return answer, nil
}

// runAnswering runs at a terminal, answering from answers.
func (h *harness) runAnswering(t *testing.T, answers map[string]string, args ...string) (int, string, string, *scripted) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := h.newApp(&stdout, &stderr)
	s := &scripted{answers: answers}
	a.asker = s
	a.tty = func() bool { return true }
	code := a.run(context.Background(), args)
	return code, stdout.String(), stderr.String(), s
}

func multimcDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "multimc.cfg"), []byte("[General]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBareLinkAsksForTheLauncher(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	dir := multimcDir(t)

	code, stdout, stderr, s := h.runAnswering(t, map[string]string{
		"Which launcher?":             "multimc",
		"Where is MultiMC installed?": dir,
	}, "link")
	if code != 0 {
		t.Fatalf("bare link: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if want := []string{"Which launcher?", "Where is MultiMC installed?"}; !slices.Equal(s.asked, want) {
		t.Fatalf("asked %q, want %q", s.asked, want)
	}
	if !strings.Contains(stdout, "created instance pack") || !strings.Contains(stdout, "follows pack from "+h.dir) {
		t.Fatalf("a bare link reaches link multimc: %s", stdout)
	}
	cfg := filepath.Join(dir, "instances", "shulker-pack", launcher.PrismInstanceFile)
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("no MultiMC instance written: %v", err)
	}
	instances := readInstances(t, h)
	if len(instances) != 1 || instances[0].Launcher != "multimc" || instances[0].LauncherDir != dir {
		t.Fatalf("registry = %+v", instances)
	}
}

func TestLinkLaunchersOffered(t *testing.T) {
	h := newHarness(t)
	var offered []out.Choice
	a := h.newApp(io.Discard, io.Discard)
	a.tty = func() bool { return true }
	a.asker = offering(func(choices []out.Choice) { offered = choices })
	a.run(context.Background(), []string{"link"})
	var want []out.Choice
	for _, e := range launcher.All {
		want = append(want, out.Choice{Label: e.Title, Value: e.Name})
	}
	if !slices.Equal(offered, want) {
		t.Fatalf("offered %v, want %v", offered, want)
	}
}

// offering records the choices of the first question and escapes it.
type offering func([]out.Choice)

func (o offering) Pick(_ string, choices []out.Choice, _ io.Reader) (string, error) {
	o(choices)
	return "", out.ErrPickCancelled
}

func (o offering) Ask(string, string, string, io.Reader) (string, error) {
	return "", out.ErrPickCancelled
}

func (o offering) BrowseMarks(string, string, out.BrowseSource, io.Reader) ([]string, error) {
	return nil, out.ErrPickCancelled
}

func TestBareLinkIsTheGroupHelpWhenItCantAsk(t *testing.T) {
	h := newHarness(t)
	for _, flag := range []string{"--no-input", "--json"} {
		code, stdout, stderr, s := h.runAnswering(t, map[string]string{}, flag, "link")
		if code != 0 || !strings.Contains(stdout+stderr, "multimc") || len(s.asked) > 0 {
			t.Fatalf("%s link: exit %d, asked %q\n%s\n%s", flag, code, s.asked, stdout, stderr)
		}
	}
	code, stdout, stderr := h.run(t, "link")
	if code != 0 || !strings.Contains(stdout+stderr, "multimc") {
		t.Fatalf("off a terminal a bare link is its help: exit %d\n%s\n%s", code, stdout, stderr)
	}

	code, stdout, _ = h.run(t, "link", "prsm", "--json")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "usage" || !slices.Contains(e.Candidates, "prism") {
		t.Fatalf("a stray word is the unknown subcommand: exit %d %s", code, stdout)
	}
	code, stdout, _, s := h.runAnswering(t, map[string]string{}, "link", "prsm", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" || len(s.asked) > 0 {
		t.Fatalf("a stray word asks nothing: exit %d %s", code, stdout)
	}
}

func TestLinkMultiMCAsksWhereItIsInstalled(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	dir := multimcDir(t)
	code, stdout, stderr, s := h.runAnswering(t, map[string]string{"Where is MultiMC installed?": dir}, "link", "multimc")
	if code != 0 || !slices.Equal(s.asked, []string{"Where is MultiMC installed?"}) {
		t.Fatalf("link multimc: exit %d, asked %q\n%s\n%s", code, s.asked, stdout, stderr)
	}

	code, stdout, _, s = h.runAnswering(t, map[string]string{"Where is MultiMC installed?": dir}, "--no-input", "link", "multimc", "--json")
	if code == 0 || failureCode(t, stdout).Code != "launcher-dir-required" || len(s.asked) > 0 {
		t.Fatalf("--no-input: exit %d, asked %q %s", code, s.asked, stdout)
	}
}

func TestLinkNestsTheSourceItIsGiven(t *testing.T) {
	h := newHarness(t)
	root := shulkerInstances(t, h)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	elsewhere := t.TempDir()
	h.dir, elsewhere = elsewhere, h.dir

	code, stdout, stderr, s := h.runAnswering(t, map[string]string{}, "link", "shulker", elsewhere)
	if code != 0 || len(s.asked) > 0 {
		t.Fatalf("a named source asks nothing: exit %d, asked %q\n%s\n%s", code, s.asked, stdout, stderr)
	}
	if _, entry := onlyModpack(t, instanceManifest(t, filepath.Join(root, "pack"))); entry["source"] != elsewhere {
		t.Fatalf("modpack entry: %v", entry)
	}
}

func TestLinkAuthorsTheInstanceOutsideAProject(t *testing.T) {
	h := newHarness(t)
	root := shulkerInstances(t, h)
	code, stdout, stderr, s := h.runAnswering(t, map[string]string{
		"Start from an existing pack?": "",
		"Which Minecraft version?":     "*",
		"Add mods?":                    "yes",
		"Which mod loader?":            "fabric",
		"Which fabric version?":        "*",
		"What should it be called?":    "",
	}, "link", "shulker")
	if code != 0 {
		t.Fatalf("link shulker: exit %d\n%s\n%s", code, stdout, stderr)
	}
	want := []string{"Start from an existing pack?", "Which Minecraft version?", "Add mods?", "Which mod loader?", "Which fabric version?", "What should it be called?"}
	if !slices.Equal(s.asked, want) {
		t.Fatalf("asked %q, want %q", s.asked, want)
	}
	if !strings.Contains(stdout, "created instance fabric-26.2") || strings.Contains(stdout, "follows") {
		t.Fatalf("link output: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "shulker.json")); !os.IsNotExist(err) {
		t.Fatalf("the current directory is left alone: %v", err)
	}

	gameDir := filepath.Join(root, "fabric-26.2")
	m := instanceManifest(t, gameDir)
	client, _ := m["client"].(map[string]any)
	loaderKey, _ := m["loader"].(map[string]any)
	if m["name"] != "fabric-26.2" || client["name"] != "Fabric 26.2" || client["build"] != "." || m["minecraft"] != "26.2" || loaderKey["type"] != "fabric" {
		t.Fatalf("an authored instance is a project of its own: %v", m)
	}
	if requires, _ := m["requires"].(map[string]any); len(requires) != 0 {
		t.Fatalf("an authored instance follows no pack: %v", requires)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "shulker.lock")); err != nil {
		t.Fatalf("no lock: %v", err)
	}
	instances := readInstances(t, h)
	if len(instances) != 1 || instances[0].ID != "fabric-26.2" || instances[0].Source != gameDir {
		t.Fatalf("an authored instance syncs from itself: %+v", instances)
	}

	h.mustRun(t, "-C", gameDir, "add", "sodium")
	h.mustRun(t, "-C", gameDir, "sync")
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("an authored instance grows with add: %v", err)
	}
}

func TestLinkAuthorsAVanillaInstanceUnderTheNameGiven(t *testing.T) {
	h := newHarness(t)
	launcherDir := multimcDir(t)
	code, stdout, stderr, _ := h.runAnswering(t, map[string]string{
		"Start from an existing pack?": "",
		"Which Minecraft version?":     "26.2",
		"Add mods?":                    "none",
		"What should it be called?":    "Weekend",
	}, "link", "multimc", "--launcher-dir", launcherDir)
	if code != 0 {
		t.Fatalf("link multimc: exit %d\n%s\n%s", code, stdout, stderr)
	}
	instances := readInstances(t, h)
	if len(instances) != 1 || instances[0].ID != "weekend" || instances[0].Name != "Weekend" {
		t.Fatalf("registry = %+v", instances)
	}
	m := instanceManifest(t, instances[0].Dir)
	if m["minecraft"] != "26.2" || m["loader"] != nil || m["name"] != "weekend" {
		t.Fatalf("vanilla instance: %v", m)
	}

	_, _, _, s := h.runAnswering(t, map[string]string{
		"Start from an existing pack?": "",
		"Which Minecraft version?":     "26.2",
		"Add mods?":                    "none",
	}, "link", "multimc", "--launcher-dir", launcherDir, "--name", "Other")
	if slices.Contains(s.asked, "What should it be called?") {
		t.Fatalf("--name answers the name: %q", s.asked)
	}
}

func TestLinkStartsFromAnExistingPack(t *testing.T) {
	h := newHarness(t)
	root := shulkerInstances(t, h)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	pack := h.dir
	h.dir = t.TempDir()

	code, stdout, stderr, s := h.runAnswering(t, map[string]string{
		"Start from an existing pack?": "pack",
		"Which pack?":                  pack,
	}, "link", "shulker")
	if code != 0 {
		t.Fatalf("link shulker: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if want := []string{"Start from an existing pack?", "Which pack?"}; !slices.Equal(s.asked, want) {
		t.Fatalf("a pack decides the platform, so nothing more is asked: %q", s.asked)
	}
	if _, entry := onlyModpack(t, instanceManifest(t, filepath.Join(root, "pack"))); entry["source"] != pack {
		t.Fatalf("the pack is nested as the source: %v", entry)
	}
}

func TestLinkOutsideAProjectCantAuthorOffATerminal(t *testing.T) {
	h := newHarness(t)
	shulkerInstances(t, h)
	code, stdout, _ := h.run(t, "link", "shulker", "--json")
	if code == 0 || failureCode(t, stdout).Code != "manifest-not-found" {
		t.Fatalf("off a terminal: exit %d %s", code, stdout)
	}
}
