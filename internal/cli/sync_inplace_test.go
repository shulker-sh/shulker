package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/modpack"
)

func gitPack(t *testing.T, name, mods, file string) (repo, source, first string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), name)
	writePrismPack(t, repo, "^26.1", mods, map[string]string{"config/" + file: "v1\n"})
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "one")
	return repo, "file://" + repo, gitRun(t, repo, "rev-parse", "HEAD")
}

func bumpPack(t *testing.T, repo, file string) string {
	t.Helper()
	writeFile(t, filepath.Join(repo, "overrides", "config", file), "v2\n")
	gitRun(t, repo, "commit", "-q", "-am", "two")
	return gitRun(t, repo, "rev-parse", "HEAD")
}

func readInPlace(t *testing.T, h *harness, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func historyCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := build.History(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestSyncInPlaceFollowsAutoUpdateModpacks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newInPlace(t)
	followRepo, follow, followFirst := gitPack(t, "follow", `"sodium": {}`, "follow.txt")
	heldRepo, held, heldFirst := gitPack(t, "held", "", "held.txt")
	h.mustRun(t, "modpack", "add", follow)
	h.mustRun(t, "modpack", "add", held, "--no-auto-update")
	if m := h.readManifest(t); m.Requires["held"].AutoUpdate == nil || *m.Requires["held"].AutoUpdate || m.Requires["follow"].AutoUpdate != nil {
		t.Fatalf("--no-auto-update should write autoUpdate false on that modpack only: %+v", m.Requires)
	}
	h.mustRun(t, "sync")
	if got := readInPlace(t, h, "config/follow.txt"); got != "v1\n" {
		t.Fatalf("follow.txt after the first sync: %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("sync should build in place: %v", err)
	}

	before := historyCount(t, h.dir)
	h.mustRun(t, "sync")
	if after := historyCount(t, h.dir); after != before {
		t.Fatalf("a sync that changes nothing must not take history: %d -> %d", before, after)
	}

	followSecond := bumpPack(t, followRepo, "follow.txt")
	heldSecond := bumpPack(t, heldRepo, "held.txt")
	stdout := h.mustRun(t, "sync")
	if !strings.Contains(stdout, "~ follow "+followFirst[:12]+" ⟶ "+followSecond[:12]+" (modpack)") || strings.Contains(stdout, "held") {
		t.Fatalf("sync should move only the modpack that follows its source: %s", stdout)
	}
	if !strings.Contains(stdout, "Synced client") {
		t.Fatalf("sync output: %s", stdout)
	}
	if historyCount(t, h.dir) != before+1 {
		t.Fatal("a sync that moves the lock should take history")
	}
	if got := readInPlace(t, h, "config/follow.txt"); got != "v2\n" {
		t.Fatalf("follow.txt after sync: %q", got)
	}
	if got := readInPlace(t, h, "config/held.txt"); got != "v1\n" {
		t.Fatalf("held.txt after sync: %q", got)
	}
	if l := readLock(t, h); l.Packs["held"]["commit"] != heldFirst {
		t.Fatalf("held modpack moved on sync: %v", l.Packs["held"])
	}

	stdout = h.mustRun(t, "update")
	if !strings.Contains(stdout, "~ held "+heldFirst[:12]+" ⟶ "+heldSecond[:12]+" (modpack)") || !strings.Contains(stdout, "Synced client") || strings.Contains(stdout, "shulker install") {
		t.Fatalf("update in an instance should move every modpack and build: %s", stdout)
	}
	if got := readInPlace(t, h, "config/held.txt"); got != "v2\n" {
		t.Fatalf("held.txt after update: %q", got)
	}
}

func TestSyncInPlaceThenItsChildren(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "install")
	child := filepath.Join(t.TempDir(), "child")
	h.mustRun(t, "sync", h.dir, "--into", child)
	h.mustRun(t, "add", "sodium")

	stdout := h.mustRun(t, "sync")
	if !strings.Contains(stdout, "Synced client") || !strings.Contains(stdout, "Child") {
		t.Fatalf("sync output: %s", stdout)
	}
	for _, dir := range []string{h.dir, child} {
		if _, err := os.Stat(filepath.Join(dir, "mods", h.jars["sodium"].filename)); err != nil {
			t.Fatalf("sync should build %s: %v", dir, err)
		}
	}

	if stdout := h.mustRun(t, "sync", "-i", "child", "--json"); !strings.Contains(stdout, `"dir": "`+child+`"`) {
		t.Fatalf("-i reaches the project's detached build by its folder's id: %s", stdout)
	}
	if stdout := h.mustRun(t, "sync", "--into", child); strings.Contains(stdout, "sodium") {
		t.Fatalf("a child sync has no lock of its own to change: %s", stdout)
	}
}

func TestUpdateInPlaceNudgesAtItsChildren(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "install")
	child := filepath.Join(t.TempDir(), "child")
	h.mustRun(t, "sync", h.dir, "--into", child)
	h.mustRun(t, "add", "sodium")

	stdout := h.mustRun(t, "update")
	synced, nudge := strings.Index(stdout, "Synced client"), strings.Index(stdout, "Build the instances synced from here")
	if synced < 0 || nudge < synced || !strings.Contains(stdout[nudge:], "shulker sync") {
		t.Fatalf("update should nudge at its children after the synced line: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(child, "mods", h.jars["sodium"].filename)); !os.IsNotExist(err) {
		t.Fatalf("update must not build a child: %v", err)
	}
	if stdout := h.mustRun(t, "update", "--json"); strings.Contains(stdout, "shulker sync") {
		t.Fatalf("the nudge is human-only: %s", stdout)
	}
	if stdout := h.mustRun(t, "sync"); strings.Contains(stdout, "Build the instances synced from here") {
		t.Fatalf("sync builds its children rather than nudging: %s", stdout)
	}
}

func TestUpdateInPlaceSucceedsWhenItCantReadTheRegistry(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "install")
	h.mustRun(t, "add", "sodium")
	writeFile(t, registryPath(h), "{")
	if code, stdout, stderr := h.run(t, "update"); code != 0 || !strings.Contains(stdout, "Synced client") {
		t.Fatalf("an unreadable registry only costs update its nudge (%d): %s%s", code, stdout, stderr)
	}
}

func TestUpdateWithoutChildrenHasNoSyncNudge(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "sodium")
	if stdout := h.mustRun(t, "update"); !strings.Contains(stdout, "Synced client") || strings.Contains(stdout, "shulker sync") {
		t.Fatalf("an instance with no children has nothing to nudge at: %s", stdout)
	}

	h = newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	if stdout := h.mustRun(t, "update"); strings.Contains(stdout, "shulker sync") {
		t.Fatalf("a project that doesn't build in place has no children to nudge at: %s", stdout)
	}
}

func TestCachePruneKeepsARemoteSourcedInstance(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "one")
	h.mustRun(t, "link", "prism", "file://"+h.dir, "--launcher-dir", t.TempDir(), "--name", "remote")

	sodium := (&cache.Cache{Dir: h.cache}).Object(h.jars["sodium"].sha512)
	h.mustRun(t, "--dir", t.TempDir(), "cache", "prune")
	if _, err := os.Stat(sodium); err != nil {
		t.Fatalf("an instance synced from git still needs the mods its checkout locks: %v", err)
	}
}

func TestPreLaunchInPlaceFallsBackToTheLock(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newInPlace(t)
	repo, source, _ := gitPack(t, "follow", "", "follow.txt")
	h.mustRun(t, "modpack", "add", source)
	h.mustRun(t, "install")
	if err := instance.SaveIntent(h.dir, true, h.dir, modpack.At{}, "client", false); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "add", "sodium")
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := h.run(t, "hook", "pre-launch")
	if code != 0 || !strings.Contains(stderr, "building what the lock already has") || !strings.Contains(stdout, "Synced client") {
		t.Fatalf("pre-launch must fall back, report the build and exit 0: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the fallback should build the lock in place: %v", err)
	}
}
