package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
)

func (h *harness) sendSodiumElsewhere(t *testing.T) {
	t.Helper()
	h.editLock(t, func(l *lock.Lock) {
		m := l.Mods["sodium"]
		elsewhere := "https://evil.example/sodium.jar"
		m.URL = &elsewhere
		l.Mods["sodium"] = m
	})
}

func provenanceRefusal(t *testing.T, code int, stdout string) *out.Error {
	t.Helper()
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "provenance-mismatch" || e.Protection != "provenance" || !slices.Equal(e.Items, []string{"sodium"}) || e.Message != "sodium is locked from Modrinth but downloads from evil.example" {
		t.Fatalf("expected provenance-mismatch for sodium: exit %d %s", code, stdout)
	}
	return e
}

func TestBuildRefusesAnEntryOffItsProvidersHostsUntilLockLooksItUpAgain(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.sendSodiumElsewhere(t)

	code, stdout, _ := h.run(t, "build", "--json")
	if e := provenanceRefusal(t, code, stdout); e.Help != "run `shulker lock sodium` to look it up again from its provider" {
		t.Fatalf("help: %q", e.Help)
	}
	code, stdout, _ = h.run(t, "install", "--json")
	provenanceRefusal(t, code, stdout)

	h.mustRun(t, "lock", "sodium")
	if u := *h.readLock(t).Mods["sodium"].URL; !strings.HasPrefix(u, "https://cdn.modrinth.com/") {
		t.Fatalf("lock sodium left its URL at %s", u)
	}
	h.mustRun(t, "build")
}

func TestSyncOfAGitSourceSaysItsAuthorHasToFixAMismatch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.sendSodiumElsewhere(t)
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "one")
	source := "file://" + h.dir

	code, stdout, _ := h.run(t, "sync", source, "--into", filepath.Join(t.TempDir(), "minecraft"), "--json")
	if e := provenanceRefusal(t, code, stdout); e.Help != "this lock is "+source+"'s, so its author has to fix it" {
		t.Fatalf("help: %q", e.Help)
	}
}

func TestPreLaunchKeepsTheLastBuildWhenTheLockIsRefused(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	h.mustRun(t, "hook", "pre-launch", "-C", gameDir)
	h.sendSodiumElsewhere(t)

	code, stdout, stderr := h.run(t, "hook", "pre-launch", "-C", gameDir)
	if code != 0 || !strings.Contains(stderr, "sodium is locked from Modrinth but downloads from evil.example") {
		t.Fatalf("pre-launch should warn and let the game start: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the last good build should stay in place: %v", err)
	}

	code, stdout, _ = h.run(t, "sync", "-i", "friends", "--json")
	if e := provenanceRefusal(t, code, stdout); e.Help != "run `shulker lock sodium -C "+h.dir+"` to look it up again from its provider" {
		t.Fatalf("help: %q", e.Help)
	}
}
