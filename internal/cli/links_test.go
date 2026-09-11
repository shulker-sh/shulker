package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
)

func readLinks(t *testing.T, h *harness) []config.Link {
	t.Helper()
	cfg, err := config.LoadFile(h.config)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Links
}

func TestSyncIntoRegisters(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	h.mustRun(t, "sync", h.dir)
	h.mustRun(t, "sync", h.dir, "--into", filepath.Join(h.dir, "build", "client"))
	if links := readLinks(t, h); len(links) != 0 {
		t.Fatalf("syncing into the build directory must not register: %+v", links)
	}
	code, stdout, _ := h.run(t, "sync", h.dir, "--name", "Mine", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--name without --into: exit %d %s", code, stdout)
	}

	into := filepath.Join(t.TempDir(), "instance")
	stdout = h.mustRun(t, "sync", h.dir, "--into", into)
	links := readLinks(t, h)
	want := config.Link{Side: "client", Name: links[0].Name, Dir: into, Source: h.dir, Target: "client"}
	if len(links) != 1 || links[0] != want || want.Name == "" {
		t.Fatalf("entry: %+v", links)
	}
	if !strings.Contains(stdout, `registered "`+want.Name+`" (client)`) {
		t.Fatalf("sync should say it registered the directory: %s", stdout)
	}
	if stdout := h.mustRun(t, "sync", h.dir, "--into", into); strings.Contains(stdout, "registered") {
		t.Fatalf("an unchanged entry is not registered again: %s", stdout)
	}

	if stdout := h.mustRun(t, "sync", h.dir, "--into", into, "--name", "Mine"); !strings.Contains(stdout, `registered "Mine" (client)`) {
		t.Fatalf("--name renames the entry: %s", stdout)
	}
	h.mustRun(t, "sync", h.dir, "--into", into)
	if links := readLinks(t, h); len(links) != 1 || links[0].Name != "Mine" {
		t.Fatalf("a later sync keeps the name: %+v", links)
	}
}

func TestLinkRegisters(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	prismDir := t.TempDir()
	stdout := h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	if strings.Contains(stdout, "registered") {
		t.Fatalf("the first sync matches the entry link just wrote: %s", stdout)
	}
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	prism := config.Link{Launcher: "prism", LauncherDir: prismDir, Side: "client", Name: "Friends", Dir: gameDir, Source: h.dir, Target: "client"}
	if links := readLinks(t, h); len(links) != 1 || links[0] != prism {
		t.Fatalf("prism entry: %+v", links)
	}
	if stdout := h.mustRun(t, "sync", h.dir, "--target", "client", "--into", gameDir); strings.Contains(stdout, "registered") {
		t.Fatalf("a pre-launch sync must not change the entry: %s", stdout)
	}

	mojangDir := t.TempDir()
	h.mustRun(t, "link", "mojang", "--launcher-dir", mojangDir)
	links := readLinks(t, h)
	if len(links) != 2 || links[0] != prism {
		t.Fatalf("entries after link mojang: %+v", links)
	}
	if m := links[1]; m.Launcher != "mojang" || m.LauncherDir != mojangDir || m.Dir != filepath.Join(h.dir, "build", "client") || m.Source != h.dir || m.Side != "client" {
		t.Fatalf("mojang entry: %+v", m)
	}

	multimcDir := t.TempDir()
	h.mustRun(t, "link", "multimc", "--launcher-dir", multimcDir)
	if links := readLinks(t, h); len(links) != 3 || links[2].Launcher != "multimc" {
		t.Fatalf("multimc entry: %+v", links)
	}
}

func TestSyncWarnsWhenConfigIsUnwritable(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h.config = filepath.Join(blocker, "config.json")
	stdout, stderr := h.mustRunStderr(t, "sync", h.dir, "--into", filepath.Join(t.TempDir(), "one"))
	if !strings.Contains(stderr, "warning: config.json not updated") || strings.Contains(stdout, "registered") {
		t.Fatalf("stdout: %s\nstderr: %s", stdout, stderr)
	}
}
