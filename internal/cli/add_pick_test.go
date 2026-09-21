package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

type markStep struct{ query, value string }

// BrowseMarks types each step's query and marks the row it names; a title with no steps is
// escaped.
func (s *scripted) BrowseMarks(title, _ string, src out.BrowseSource, _ io.Reader) ([]string, error) {
	s.asked = append(s.asked, title)
	steps, ok := s.marks[title]
	if !ok {
		return nil, fmt.Errorf("unexpected question %q", title)
	}
	if len(steps) == 0 {
		return nil, out.ErrPickCancelled
	}
	var marked []string
	for _, step := range steps {
		src.SetQuery(step.query)
		rows := src.Rows()
		if !slices.ContainsFunc(rows, func(c out.Choice) bool { return c.Value == step.value }) {
			return nil, fmt.Errorf("%q for %q offers no %q among %v", title, step.query, step.value, rows)
		}
		marked = append(marked, step.value)
	}
	return marked, nil
}

// runMarking runs at a terminal, answering each search from marks.
func (h *harness) runMarking(t *testing.T, marks map[string][]markStep, args ...string) (int, string, string, *scripted) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := h.newApp(&stdout, &stderr)
	s := &scripted{marks: marks}
	a.asker = s
	a.tty = func() bool { return true }
	code := a.run(context.Background(), args)
	return code, stdout.String(), stderr.String(), s
}

func TestBareAddMarksSeveralAndAddsThemInOneRelock(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "install")
	before := historyCount(t, h.dir)
	code, stdout, stderr, s := h.runMarking(t, map[string][]markStep{
		"Add which mods?": {{"sodium", "modrinth:AANobbMI"}, {"fabric api", "modrinth:P7dR8mSH"}},
	}, "add")
	if code != 0 {
		t.Fatalf("bare add: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if !slices.Equal(s.asked, []string{"Add which mods?"}) {
		t.Fatalf("asked %q", s.asked)
	}
	m := h.readManifest(t)
	for _, id := range []string{"sodium", "fabric-api"} {
		if _, ok := m.Requires[id]; !ok {
			t.Errorf("%s not added: %+v", id, m.Requires)
		}
	}
	if after := historyCount(t, h.dir); after != before+1 {
		t.Errorf("both mods should land in one relock: history %d -> %d", before, after)
	}
}

func TestBareAddTakesEachRowFromItsOwnProvider(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	code, stdout, stderr, _ := h.runMarking(t, map[string][]markStep{
		"Add which mods?": {{"sodium", "curseforge:394468"}},
	}, "add")
	if code != 0 {
		t.Fatalf("bare add: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if m := h.readManifest(t); m.Requires["sodium"].Provider != "curseforge" {
		t.Errorf("a CurseForge row should add from CurseForge: %+v", m.Requires)
	}
}

func TestBareAddSearchesTheTypeItAdds(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	code, stdout, stderr, _ := h.runMarking(t, map[string][]markStep{
		"Add which resource packs?": {{"fresh", "modrinth:50dA9Sha"}},
	}, "resourcepack", "add")
	if code != 0 {
		t.Fatalf("bare resourcepack add: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if _, ok := h.readManifest(t).Requires["fresh-animations"]; !ok {
		t.Errorf("fresh-animations not added")
	}
}

func TestAddAsksOnlyForWhatIsMissing(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	if code, stdout, stderr, s := h.runMarking(t, nil, "add", "sodium"); code != 0 || len(s.asked) != 0 {
		t.Fatalf("add with an argument should ask nothing: exit %d, asked %q\n%s\n%s", code, s.asked, stdout, stderr)
	}
	for _, args := range [][]string{{"add", "--no-input"}, {"add", "--json"}, {"modpack", "add"}, {"add", "--type", "modpack"}} {
		code, stdout, stderr, s := h.runMarking(t, nil, args...)
		if code == 0 || len(s.asked) != 0 || !strings.Contains(stdout+stderr, "missing at least one argument") {
			t.Errorf("%v should keep the missing-argument error: exit %d, asked %q\n%s\n%s", args, code, s.asked, stdout, stderr)
		}
	}
}

func TestEscapingBareAddAddsNothing(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	before := projectFiles(t, h.dir)
	code, stdout, stderr, _ := h.runMarking(t, map[string][]markStep{"Add which mods?": nil}, "add")
	if code != out.ExitInterrupted {
		t.Errorf("escaping should end interrupted: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if after := projectFiles(t, h.dir); after != before {
		t.Errorf("escaping changed the project")
	}
}
