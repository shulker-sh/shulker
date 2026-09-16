package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

type heldLockView struct {
	Mods map[string]struct {
		VersionNumber string `json:"versionNumber"`
	} `json:"mods"`
}

func heldVersion(t *testing.T, h *harness, id string) string {
	t.Helper()
	var l heldLockView
	h.readJSON(t, "shulker.lock", &l)
	return l.Mods[id].VersionNumber
}

// needsNewAPI gives sodium a jar that no longer works with the fabric-api the
// lock holds, which is what makes the add ask for a move.
func needsNewAPI(t *testing.T, h *harness) {
	t.Helper()
	h.jars["sodium"] = makeJarWith(t, "sodium", h.jars["sodium"].filename, "client", `"depends":{"fabricloader":">=0.17","fabric-api":">=2.0.0"}`)
}

// heldProject locks fabric-api at 1.0.0 and only then lets a newer one exist, so
// anything that moves it has to be the add under test.
func heldProject(t *testing.T, h *harness) {
	t.Helper()
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "fabric-api")
	h.newerAPI = true
}

func TestAddHoldsTheVersionsTheLockAlreadyPins(t *testing.T) {
	h := newHarness(t)
	heldProject(t, h)

	h.mustRun(t, "add", "sodium")

	if got := heldVersion(t, h, "fabric-api"); !strings.HasPrefix(got, "1.0.0") {
		t.Fatalf("adding sodium should hold fabric-api at 1.0.0, got %s", got)
	}
}

func TestAddRefusesToMoveAHeldDependency(t *testing.T) {
	h := newHarness(t)
	needsNewAPI(t, h)
	heldProject(t, h)

	code, stdout, _ := h.run(t, "add", "sodium", "--json")

	e := failureCode(t, stdout)
	if code == 0 || e.Code != "deps-held" {
		t.Fatalf("add should refuse to move a held dependency: exit %d %s", code, stdout)
	}
	if len(e.Items) != 1 || !strings.Contains(e.Items[0], "requires fabric-api >=2.0.0") || !strings.Contains(e.Items[0], "held at 1.0.0") {
		t.Fatalf("the refusal should name the held version and what needs it: %v", e.Items)
	}
	if got := heldVersion(t, h, "fabric-api"); !strings.HasPrefix(got, "1.0.0") {
		t.Fatalf("a refused add should leave the lock alone, got fabric-api %s", got)
	}
	if got := heldVersion(t, h, "sodium"); got != "" {
		t.Fatalf("a refused add should lock nothing, got sodium %s", got)
	}
}

func TestAddWithDepsMovesTheHeldDependency(t *testing.T) {
	h := newHarness(t)
	needsNewAPI(t, h)
	heldProject(t, h)

	h.mustRun(t, "add", "sodium", "--with-deps")

	if got := heldVersion(t, h, "fabric-api"); !strings.HasPrefix(got, "2.0.0") {
		t.Fatalf("--with-deps should move fabric-api, got %s", got)
	}
	if got := heldVersion(t, h, "sodium"); got == "" {
		t.Fatal("--with-deps should still add the mod that needed the move")
	}
}

func TestWithDepsListsAModpacksDependencyAsItMovesIt(t *testing.T) {
	h := newHarness(t)
	needsNewAPI(t, h)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "fabric-api")
	lockedPack(t, h, filepath.Join(h.dir, "base"), `"fabric-api": {}`)
	h.mustRun(t, "remove", "fabric-api")
	h.mustRun(t, "modpack", "add", "./base")
	h.newerAPI = true

	_, stderr := h.mustRunStderr(t, "add", "sodium", "--with-deps")

	if !strings.Contains(stderr, "no longer follows modpack base") {
		t.Fatalf("moving a modpack's dependency should say it stops following the modpack: %s", stderr)
	}
	var m struct {
		Requires map[string]any `json:"requires"`
	}
	h.readJSON(t, "shulker.json", &m)
	if _, listed := m.Requires["fabric-api"]; !listed {
		t.Fatalf("the moved dependency should be listed in shulker.json: %v", m.Requires)
	}
	if got := heldVersion(t, h, "fabric-api"); !strings.HasPrefix(got, "2.0.0") {
		t.Fatalf("--with-deps should move the modpack's dependency, got %s", got)
	}

	h.mustRun(t, "lock")

	if got := heldVersion(t, h, "fabric-api"); !strings.HasPrefix(got, "2.0.0") {
		t.Fatalf("the modpack should not copy its version back over the move, got %s", got)
	}
}

func TestAnIgnoredProblemDoesNotHoldUpAnAdd(t *testing.T) {
	h := newHarness(t)
	needsNewAPI(t, h)
	heldProject(t, h)
	h.mustRun(t, "ignore", "sodium", "fabric-api", "--rule", "depends", "--declared", ">=2.0.0", "--note", "works on 1.x")

	h.mustRun(t, "add", "sodium")

	if got := heldVersion(t, h, "fabric-api"); !strings.HasPrefix(got, "1.0.0") {
		t.Fatalf("an ignored problem should leave the held version alone, got %s", got)
	}
}
