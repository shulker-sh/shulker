package cli

import (
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
