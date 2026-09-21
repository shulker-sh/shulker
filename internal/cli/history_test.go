package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
)

func TestHistoryRollback(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")
	jar := filepath.Join(h.dir, "mods", h.jars["sodium"].filename)
	if _, err := os.Stat(jar); err != nil {
		t.Fatal(err)
	}

	h.mustRun(t, "remove", "sodium")
	h.mustRun(t, "build")
	if _, err := os.Stat(jar); !os.IsNotExist(err) {
		t.Fatalf("sodium should be gone before the rollback: %v", err)
	}

	// The build after the remove keeps nothing of its own: the remove already
	// kept the state it was about to rewrite.
	stdout := h.mustRun(t, "history", "list")
	if !strings.Contains(stdout, "1) ") || !strings.Contains(stdout, "before remove") || strings.Contains(stdout, "before build") {
		t.Fatalf("history list: %s", stdout)
	}
	if stdout = h.mustRun(t, "history", "show", "1"); !strings.Contains(stdout, "History entry 1") || !strings.Contains(stdout, "+ sodium") {
		t.Fatalf("history show should offer sodium back: %s", stdout)
	}

	stdout = h.mustRun(t, "rollback")
	if !strings.Contains(stdout, "rolled back to") || !strings.Contains(stdout, "kept this state as history entry") {
		t.Fatalf("rollback output: %s", stdout)
	}
	if _, err := os.Stat(jar); err != nil {
		t.Fatalf("sodium should be back after the rollback: %v", err)
	}
	if manifest := readFile(t, filepath.Join(h.dir, "shulker.json")); !strings.Contains(manifest, "sodium") {
		t.Fatalf("the manifest should be restored too: %s", manifest)
	}
	// The rollback kept the state it replaced, so it can itself be undone.
	if stdout = h.mustRun(t, "history", "list"); !strings.Contains(stdout, "before a rollback") {
		t.Fatalf("the rollback should leave an entry of its own: %s", stdout)
	}
}

// An entry holds resource packs and shaders as well as mods, so the rows have to
// count them and the difference has to name them: a pack-only change once read
// as "restoring it would change nothing".
func TestHistoryCountsPacks(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "fresh-animations")
	h.mustRun(t, "shader", "add", "complementary-reimagined")
	h.mustRun(t, "install")
	h.mustRun(t, "remove", "fresh-animations")

	stdout := h.mustRun(t, "history", "list")
	if !strings.Contains(stdout, "1 resource pack") || !strings.Contains(stdout, "1 shader") {
		t.Fatalf("history list should count packs: %s", stdout)
	}
	if stdout = h.mustRun(t, "history", "show", "1"); !strings.Contains(stdout, "resource packs:") || !strings.Contains(stdout, "+ fresh-animations") {
		t.Fatalf("history show should offer the pack back: %s", stdout)
	}
}

func TestHistoryPruneAndWarning(t *testing.T) {
	h := newInPlace(t)
	h.editManifest(t, func(m map[string]any) { m["history"] = 1 })
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")

	_, stderr := h.mustRunStderr(t, "remove", "sodium")
	if !strings.Contains(stderr, "history entries are kept") || !strings.Contains(stderr, "shulker history prune") {
		t.Fatalf("a change over the limit should warn: %s", stderr)
	}
	entries, err := build.History(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("nothing but prune deletes: %+v", entries)
	}

	stdout := h.mustRun(t, "history", "prune")
	if !strings.Contains(stdout, "pruned 1 history entry") || !strings.Contains(stdout, "1 entry kept") {
		t.Fatalf("prune output: %s", stdout)
	}
	if entries, err = build.History(h.dir); err != nil || len(entries) != 1 {
		t.Fatalf("prune should leave the newest: %+v %v", entries, err)
	}
}

func TestHistoryKeepsEditsBeforeABuild(t *testing.T) {
	h := newInPlace(t)
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "x.txt"), "from the pack\n")
	h.mustRun(t, "install")
	if entries, err := build.History(h.dir); err != nil || len(entries) != 0 {
		t.Fatalf("a first build with nothing of yours at risk keeps nothing: %+v %v", entries, err)
	}

	writeFile(t, filepath.Join(h.dir, "config", "x.txt"), "mine\n")
	h.mustRun(t, "build")

	if stdout := h.mustRun(t, "history", "list"); !strings.Contains(stdout, "before build client") {
		t.Fatalf("a build over an edit should keep it: %s", stdout)
	}
	entries, err := build.History(h.dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries: %+v %v", entries, err)
	}
	kept := filepath.Join(build.HistoryPath(h.dir), entries[0].ID, "config", "x.txt")
	if got := readFile(t, kept); got != "mine\n" {
		t.Fatalf("the entry should hold the edit: %q", got)
	}
}

func TestHistoryNeedsAnInstance(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	code, stdout, _ := h.run(t, "--json", "history", "list")
	if e := failureCode(t, stdout); code == 0 || e.Code != "not-in-place" {
		t.Fatalf("history outside an instance: code=%d %+v", code, e)
	}
}

// A forced build overwrites what the player changed, so it is the build that most
// needs to keep the state first.
func TestForcedBuildKeepsTheDriftItOverwrites(t *testing.T) {
	h := newInPlace(t)
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "x.txt"), "from the pack\n")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "gone.txt"), "from the pack\n")
	h.mustRun(t, "install")
	writeFile(t, filepath.Join(h.dir, "config", "x.txt"), "mine\n")
	writeFile(t, filepath.Join(h.dir, "config", "gone.txt"), "mine too\n")
	if err := os.Remove(filepath.Join(h.dir, "overrides", "config", "gone.txt")); err != nil {
		t.Fatal(err)
	}

	h.mustRun(t, "build", "--force")
	if got := readFile(t, filepath.Join(h.dir, "config", "x.txt")); got != "from the pack\n" {
		t.Fatalf("force should overwrite the edit: %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "config", "gone.txt")); !os.IsNotExist(err) {
		t.Fatalf("force should remove the edited file the source dropped: %v", err)
	}
	entries, err := build.History(h.dir)
	if err != nil || len(entries) != 1 || entries[0].Reason != "build" {
		t.Fatalf("a forced build over edits should keep them: %+v %v", entries, err)
	}
	kept := filepath.Join(build.HistoryPath(h.dir), entries[0].ID, "config")
	if got := readFile(t, filepath.Join(kept, "x.txt")); got != "mine\n" {
		t.Fatalf("the entry should hold the overwritten edit: %q", got)
	}
	if got := readFile(t, filepath.Join(kept, "gone.txt")); got != "mine too\n" {
		t.Fatalf("the entry should hold the removed edit: %q", got)
	}
}

func TestForcedBuildKeepsAnOverwrittenOption(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "install")
	options := filepath.Join(h.dir, "options.txt")
	writeFile(t, options, strings.Replace(readFile(t, options), "tutorialStep:none", "tutorialStep:movement", 1))

	h.mustRun(t, "build", "--force")
	if got := readFile(t, options); !strings.Contains(got, "tutorialStep:none") {
		t.Fatalf("force should overwrite the edited key: %s", got)
	}
	entries, err := build.History(h.dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("a forced build over an edited key should keep it: %+v %v", entries, err)
	}
	kept := filepath.Join(build.HistoryPath(h.dir), entries[0].ID, "options.txt")
	if got := readFile(t, kept); !strings.Contains(got, "tutorialStep:movement") {
		t.Fatalf("the entry should hold the edited key: %s", got)
	}
}

func TestRollbackPrune(t *testing.T) {
	h := newInPlace(t)
	h.editManifest(t, func(m map[string]any) { m["history"] = 1 })
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")
	h.mustRun(t, "remove", "sodium")
	before, err := build.History(h.dir)
	if err != nil || len(before) != 2 {
		t.Fatalf("entries before the rollback: %+v %v", before, err)
	}

	var env struct {
		Data rollbackResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "--json", "rollback", "--prune")), &env); err != nil {
		t.Fatal(err)
	}
	res := env.Data
	if res.Entry.ID != before[0].ID || len(res.Pruned) != 2 || res.Pruned[0].ID != before[0].ID || res.Pruned[1].ID != before[1].ID {
		t.Fatalf("rollback --prune should drop both older entries: %+v", res)
	}
	after, err := build.History(h.dir)
	if err != nil || len(after) != 1 || after[0].ID != res.Snapshot || after[0].Reason != "rollback" {
		t.Fatalf("only the state the rollback replaced should be left: %+v %v", after, err)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the rollback should still restore sodium: %v", err)
	}
}

// Every entry holds a lock, since init writes one before anything can take an
// entry, so an entry without a readable one is damaged.
func TestHistoryShowOnAnEntryWithoutItsLock(t *testing.T) {
	for name, damage := range map[string]func(path string){
		"missing":    func(path string) { os.Remove(path) },
		"unreadable": func(path string) { writeFile(t, path, "{") },
	} {
		t.Run(name, func(t *testing.T) {
			h := newInPlace(t)
			h.mustRun(t, "add", "sodium")
			entries, err := build.History(h.dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("entries: %+v %v", entries, err)
			}
			damage(filepath.Join(build.HistoryPath(h.dir), entries[0].ID, "shulker.lock"))

			code, stdout, _ := h.run(t, "--json", "history", "show")
			if e := failureCode(t, stdout); code == 0 || e.Code != "history-invalid" || !strings.Contains(e.Message, entries[0].ID) {
				t.Fatalf("a damaged entry: code=%d %+v", code, e)
			}
		})
	}
}
