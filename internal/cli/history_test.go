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
	rows := tableRows(stdout)
	if len(rows) != 2 || rows[0]["#"] != "1" || rows[0]["Before"] != "remove" || rows[1]["Before"] != "add" {
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
	if rows := tableRows(h.mustRun(t, "history", "list")); len(rows) == 0 || rows[0]["Before"] != "a rollback" {
		t.Fatalf("the rollback should leave an entry of its own: %+v", rows)
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
	if rows := tableRows(stdout); len(rows) != 3 || squash(rows[0]["Contents"]) != "0mods,1resourcepack,1shader" {
		t.Fatalf("history list should count packs: %s", stdout)
	}
	if stdout = h.mustRun(t, "history", "show", "1"); !strings.Contains(stdout, "resource packs:") || !strings.Contains(stdout, "+ fresh-animations") {
		t.Fatalf("history show should offer the pack back: %s", stdout)
	}
}

func TestHistoryCountsDatapacks(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "terralith")
	h.mustRun(t, "install")
	h.mustRun(t, "remove", "terralith")

	if stdout := h.mustRun(t, "history", "list"); !strings.Contains(stdout, "1 datapack") {
		t.Fatalf("history list should count datapacks: %s", stdout)
	}
	if stdout := h.mustRun(t, "history", "show", "1"); !strings.Contains(stdout, "datapacks: 1") {
		t.Fatalf("history show should count datapacks: %s", stdout)
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

func TestHistoryNeedsAnInstance(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	code, stdout, _ := h.run(t, "--json", "history", "list")
	if e := failureCode(t, stdout); code == 0 || e.Code != "not-in-place" {
		t.Fatalf("history outside an instance: code=%d %+v", code, e)
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
