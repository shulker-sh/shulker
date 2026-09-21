package cli

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestHeldDependencyAsksToMoveIt(t *testing.T) {
	h := newHarness(t)
	needsNewAPI(t, h)
	heldProject(t, h)

	code, stdout, stderr, s := h.runAnswering(t, map[string]string{"Move it?": "yes"}, "add", "sodium")

	if code != 0 {
		t.Fatalf("yes should move the held dependency: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if want := []string{"Move it?"}; !slices.Equal(s.asked, want) {
		t.Fatalf("asked %q, want %q", s.asked, want)
	}
	if !strings.Contains(stderr, "requires fabric-api >=2.0.0") {
		t.Fatalf("the rows print before the question: %s", stderr)
	}
	if got := heldVersion(t, h, "fabric-api"); !strings.HasPrefix(got, "2.0.0") {
		t.Fatalf("yes does what --with-deps does, got fabric-api %s", got)
	}
	if got := heldVersion(t, h, "sodium"); got == "" {
		t.Fatal("yes still adds the mod that needed the move")
	}
}

func TestDecliningTheMoveKeepsTheRefusal(t *testing.T) {
	h := newHarness(t)
	needsNewAPI(t, h)
	heldProject(t, h)

	code, _, stderr, _ := h.runAnswering(t, map[string]string{"Move it?": "no"}, "add", "sodium")

	if code == 0 || !strings.Contains(stderr, "shulker add sodium --with-deps") {
		t.Fatalf("no should end on the deps-held refusal and its nudge: exit %d\n%s", code, stderr)
	}
	if got := heldVersion(t, h, "fabric-api"); !strings.HasPrefix(got, "1.0.0") {
		t.Fatalf("no should leave the lock alone, got fabric-api %s", got)
	}
	if got := heldVersion(t, h, "sodium"); got != "" {
		t.Fatalf("no should lock nothing, got sodium %s", got)
	}
}

func TestLockedPackOnAnotherMinecraftAsksToUnlock(t *testing.T) {
	h, dir := projectWithLockedPack(t, "base")
	editPackLock(t, dir, func(m map[string]any) { m["minecraft"] = "26.1" })

	question := "Unlock base and resolve its mods for Minecraft 26.2?"
	code, stdout, stderr, s := h.runAnswering(t, map[string]string{question: "yes"}, "modpack", "add", "./base")

	if code != 0 {
		t.Fatalf("yes should unlock the modpack: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if want := []string{question}; !slices.Equal(s.asked, want) {
		t.Fatalf("asked %q, want %q", s.asked, want)
	}
	var m struct {
		Requires map[string]struct {
			Locked *bool `json:"locked"`
		} `json:"requires"`
	}
	h.readJSON(t, "shulker.json", &m)
	if got := m.Requires["base"].Locked; got == nil || *got {
		t.Fatalf("yes records \"locked\": false on the entry, got %v", got)
	}
	var l modpackLockView
	h.readJSON(t, "shulker.lock", &l)
	if l.Modpacks["base"].Locked {
		t.Fatalf("the lock should not record the modpack as locked: %+v", l.Modpacks["base"])
	}
	if l.Mods["sodium"].Modpack != "" || heldVersion(t, h, "sodium") == "" {
		t.Fatalf("an unlocked modpack's mods are resolved here: %+v", l.Mods["sodium"])
	}
}

func TestDecliningTheUnlockKeepsTheMismatch(t *testing.T) {
	h, dir := projectWithLockedPack(t, "base")
	editPackLock(t, dir, func(m map[string]any) { m["minecraft"] = "26.1" })

	code, _, stderr, _ := h.runAnswering(t, map[string]string{"Unlock base and resolve its mods for Minecraft 26.2?": "no"}, "modpack", "add", "./base")

	if code == 0 || !strings.Contains(stderr, "locked modpack base is built for minecraft 26.1") {
		t.Fatalf("no should end on the modpack-mismatch refusal: exit %d\n%s", code, stderr)
	}
	var m struct {
		Requires map[string]any `json:"requires"`
	}
	h.readJSON(t, "shulker.json", &m)
	if _, added := m.Requires["base"]; added {
		t.Fatalf("no should add nothing: %v", m.Requires)
	}
}

func TestLoaderRequiredAsksForTheLoader(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")

	code, stdout, stderr, s := h.runAnswering(t, map[string]string{"Which mod loader?": "fabric"}, "add", "sodium")

	if code != 0 {
		t.Fatalf("the add should continue once a loader is picked: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if want := []string{"Which mod loader?"}; !slices.Equal(s.asked, want) {
		t.Fatalf("asked %q, want %q", s.asked, want)
	}
	m, l := readProject(t, h.dir)
	if m.Loader.Type != "fabric" || m.Loader.Version != "" || l.Loader.Type != "fabric" || l.Loader.Version != "0.17.3" || len(m.Mods()) != 1 {
		t.Fatalf("picking fabric is set loader.type fabric: manifest %+v, lock %+v, mods %v", m.Loader, l.Loader, m.Mods())
	}
}

func TestEscapingTheLoaderSelectKeepsLoaderRequired(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")

	var stderr bytes.Buffer
	a := h.newApp(io.Discard, &stderr)
	a.tty = func() bool { return true }
	a.asker = offering(func([]out.Choice) {})
	code := a.run(context.Background(), []string{"add", "sodium"})

	if code == 0 || !strings.Contains(stderr.String(), "shulker set loader.type") {
		t.Fatalf("an unanswered loader select ends on loader-required: exit %d\n%s", code, stderr.String())
	}
}
