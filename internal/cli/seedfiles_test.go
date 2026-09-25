package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASeededFileKeepsThePlayersCopy(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.editManifest(t, func(m map[string]any) { m["seedFiles"] = []string{"config/seeded/*.json"} })
	seeded := filepath.Join(h.dir, "overrides", "config", "seeded", "a.json")
	plain := filepath.Join(h.dir, "overrides", "config", "plain.json")
	writeFile(t, seeded, "pack 1\n")
	writeFile(t, plain, "pack 1\n")
	h.mustRun(t, "build")
	built := filepath.Join(h.dir, "build", "client", "config", "seeded", "a.json")
	builtPlain := filepath.Join(h.dir, "build", "client", "config", "plain.json")

	writeFile(t, seeded, "pack 2\n")
	h.mustRun(t, "build")
	if got := readFile(t, built); got != "pack 2\n" {
		t.Fatalf("an untouched seeded file follows the pack: %q", got)
	}

	writeFile(t, built, "mine\n")
	writeFile(t, seeded, "pack 3\n")
	for range 2 {
		_, stderr := h.mustRunStderr(t, "build")
		if !strings.Contains(stderr, "config/seeded/a.json changed in the pack and in game; kept yours. Delete it to take the pack's, or shulker build --force for every file.") {
			t.Fatalf("a seeded file changed in both is kept with a note, on every build: %s", stderr)
		}
		if got := readFile(t, built); got != "mine\n" {
			t.Fatalf("the player's copy is kept: %q", got)
		}
	}

	writeFile(t, builtPlain, "mine\n")
	writeFile(t, plain, "pack 2\n")
	if code, _, stderr := h.run(t, "build"); code == 0 || !strings.Contains(stderr, "config/plain.json") {
		t.Fatalf("an unseeded file changed in both still conflicts: %d %s", code, stderr)
	}
	if err := os.Remove(builtPlain); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(built); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "build")
	if got := readFile(t, built); got != "pack 3\n" {
		t.Fatalf("a deleted seeded file is seeded again: %q", got)
	}

	writeFile(t, built, "mine\n")
	writeFile(t, seeded, "pack 4\n")
	h.mustRun(t, "build", "--force")
	if got := readFile(t, built); got != "pack 4\n" {
		t.Fatalf("--force writes the pack's copy: %q", got)
	}
}

func TestASeededFileInTheWayIsKept(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.editManifest(t, func(m map[string]any) { m["seedFiles"] = []string{"a.json"} })
	h.mustRun(t, "build")
	built := filepath.Join(h.dir, "build", "client", "config", "a.json")
	writeFile(t, built, "mine\n")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "a.json"), "pack\n")
	_, stderr := h.mustRunStderr(t, "build")
	if !strings.Contains(stderr, "config/a.json was not written by shulker; kept yours.") {
		t.Fatalf("a seeded file in the way is kept with a note: %s", stderr)
	}
	if got := readFile(t, built); got != "mine\n" {
		t.Fatalf("the player's copy is kept: %q", got)
	}
}

func TestSkipFilesWinsOverSeedFiles(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.editManifest(t, func(m map[string]any) {
		m["seedFiles"] = []string{"*.json"}
		m["skipFiles"] = []string{"*.json"}
	})
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "a.json"), "x\n")
	h.mustRun(t, "build")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "config", "a.json")); !os.IsNotExist(err) {
		t.Fatalf("a skipped file is left out even when seeded: %v", err)
	}
}
