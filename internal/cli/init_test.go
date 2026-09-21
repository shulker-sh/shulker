package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestInitChecksTheTarget(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run(t, "init", "--yes", "--loader", "fabric", "--side", "weird", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || len(e.Candidates) != 2 {
		t.Fatalf("unknown side: code=%d %s", code, stdout)
	}
}

func TestFailedInitLeavesNothingBehind(t *testing.T) {
	h := newHarness(t)
	if code, _, _ := h.run(t, "init", "--yes", "--loader", "fabric", "--name", "..."); code == 0 {
		t.Fatal("an invalid name must fail")
	}
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("a failed init left %s behind", e.Name())
	}
}

// --no-input is the mode and init --yes is its alias on this command, so both spellings,
// and a run off a terminal that passes neither, create the same project and print the same lines.
func TestInitWithoutAnswersTakesTheDefaults(t *testing.T) {
	h := newHarness(t)
	var first, firstManifest string
	for _, args := range [][]string{{"init", "--yes"}, {"init", "--no-input"}, {"init"}} {
		dir := t.TempDir()
		stdout := h.mustRun(t, append(args, "--name", "pack", "-C", dir)...)
		data, err := os.ReadFile(filepath.Join(dir, "shulker.json"))
		if err != nil {
			t.Fatal(err)
		}
		if first == "" {
			first, firstManifest = stdout, string(data)
			if !strings.Contains(stdout, "created shulker.json") {
				t.Fatalf("%v: %s", args, stdout)
			}
			continue
		}
		if stdout != first {
			t.Fatalf("%v prints\n%s\nwant\n%s", args, stdout, first)
		}
		if string(data) != firstManifest {
			t.Fatalf("%v writes\n%s\nwant\n%s", args, data, firstManifest)
		}
	}
}

func TestNoInputIsGlobal(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--no-input", "--name", "pack", "--loader", "fabric")
	if out := h.mustRun(t, "list", "--no-input", "--json"); !strings.Contains(out, `"ok": true`) {
		t.Fatalf("list --no-input: %s", out)
	}
	code, stdout, _ := h.run(t, "init", "--no-input", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "manifest-exists" {
		t.Fatalf("a second init: exit %d, %s", code, stdout)
	}
}
