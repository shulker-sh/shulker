package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreLaunchReportsTheSync(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")

	code, stdout, stderr := h.run(t, "hook", "pre-launch", "-C", gameDir)
	if code != 0 || !strings.Contains(stdout, "synced client") {
		t.Fatalf("pre-launch should report its sync: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("pre-launch should sync the instance: %v", err)
	}
}
