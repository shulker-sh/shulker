package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAddWithNamesNotFoundAddsNothingAndNamesThem(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	lockBefore, err := os.ReadFile(filepath.Join(h.dir, "shulker.lock"))
	if err != nil {
		t.Fatal(err)
	}

	code, jsonOut, _ := h.run(t, "--json", "add", "sodium", "this-mod-does-not-exist", "sodum")

	e := failureCode(t, jsonOut)
	if code == 0 || e.Code != "mod-not-found" || !slices.Equal(e.Items, []string{"this-mod-does-not-exist", "sodum"}) {
		t.Fatalf("exit %d: %+v", code, e)
	}
	if lockAfter, _ := os.ReadFile(filepath.Join(h.dir, "shulker.lock")); string(lockAfter) != string(lockBefore) {
		t.Fatal("nothing is written while a name misses")
	}
	_, stdout, stderr := h.run(t, "add", "sodium", "this-mod-does-not-exist", "sodum")
	human := stdout + stderr
	for _, want := range []string{"Nothing was added: 2 mods were not found", "$ shulker add sodium", "Or skip the ones not found by adding --skip-missing"} {
		if !strings.Contains(human, want) {
			t.Errorf("missing %q in:\n%s", want, human)
		}
	}
}

func TestAddSkipMissingAddsTheRestAndWarnsPerName(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")

	code, stdout, stderr := h.run(t, "add", "--skip-missing", "sodium", "sodum")

	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "sodum was not found") {
		t.Errorf("no warning for sodum:\n%s%s", stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "sodum is already in the pack") {
		t.Errorf("a skipped name isn't in the pack:\n%s", stdout)
	}
	data, err := os.ReadFile(filepath.Join(h.dir, "shulker.lock"))
	if err != nil || !strings.Contains(string(data), `"sodium"`) {
		t.Fatalf("sodium is locked: %v", err)
	}
}
