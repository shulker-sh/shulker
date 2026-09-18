package cli

import (
	"strings"
	"testing"
)

func TestBuildTakesASidePositionally(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	twoSided(t, h)
	h.mustRun(t, "install")

	if stdout := h.mustRun(t, "build"); !strings.Contains(stdout, "built client") || !strings.Contains(stdout, "built server") {
		t.Fatalf("no argument builds every declared side: %s", stdout)
	}
	if stdout := h.mustRun(t, "build", "server"); strings.Contains(stdout, "built client") || !strings.Contains(stdout, "built server") {
		t.Fatalf("an argument builds that side alone: %s", stdout)
	}

	code, stdout, _ := h.run(t, "build", "nope", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" || e.Message != `"nope" is not a side` || strings.Join(e.Candidates, ",") != "client,server" {
		t.Fatalf("not a side: exit %d %s", code, stdout)
	}

	h.editManifest(t, func(m map[string]any) { delete(m, "server") })
	code, stdout, stderr := h.run(t, "build", "server", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-side" || e.Message != "shulker.json declares no server" {
		t.Fatalf("undeclared side: exit %d %s", code, stdout)
	}
	if _, _, stderr = h.run(t, "build", "server"); !strings.Contains(stderr, `add "server": {} to shulker.json`) {
		t.Fatalf("no-side hint: %s", stderr)
	}
}

func TestDiffIntoNeedsOneSide(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	twoSided(t, h)
	h.mustRun(t, "install")

	code, stdout, _ := h.run(t, "diff", "--into", t.TempDir(), "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "into-target" {
		t.Fatalf("--into across both sides: exit %d %s", code, stdout)
	}
	h.mustRun(t, "diff", "client", "--into", t.TempDir())
}
