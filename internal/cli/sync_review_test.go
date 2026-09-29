package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cmdlog"
)

// linkedWithNewMod is a fabric pack linked into a fake Prism as Friends, synced once, with irisshaders added
// to the project since, so the next sync of the instance brings it.
func linkedWithNewMod(t *testing.T, h *harness) (gameDir, id string) {
	t.Helper()
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Friends")
	gameDir = filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	h.mustRun(t, "add", "irisshaders")
	return gameDir, readInstances(t, h)[0].ID
}

func TestSyncAtATerminalAsksBeforeApplyingItsChanges(t *testing.T) {
	h := newHarness(t)
	gameDir, id := linkedWithNewMod(t, h)
	iris := filepath.Join(gameDir, "mods", h.jars["irisshaders"].filename)

	code, stdout, stderr, s := h.runAnswering(t, map[string]string{"Apply it?": "no"}, "sync", "-i", id)
	if code != 0 || !slices.Contains(s.asked, "Apply it?") {
		t.Fatalf("code %d, asked %q\nstdout: %s\nstderr: %s", code, s.asked, stdout, stderr)
	}
	if !strings.Contains(stderr, "adds iris from Modrinth") || !strings.Contains(stdout, "as it was") {
		t.Fatalf("stdout: %s\nstderr: %s", stdout, stderr)
	}
	if _, err := os.Stat(iris); err == nil {
		t.Fatal("a declined sync places nothing")
	}

	code, stdout, stderr, _ = h.runAnswering(t, map[string]string{"Apply it?": "yes"}, "sync", "-i", id)
	if _, err := os.Stat(iris); code != 0 || err != nil {
		t.Fatalf("an accepted sync applies the change: code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

func TestSyncWithNoInputAppliesItsChangesAndWarns(t *testing.T) {
	h := newHarness(t)
	gameDir, id := linkedWithNewMod(t, h)

	code, stdout, stderr := h.run(t, "sync", "-i", id, "--no-input")

	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["irisshaders"].filename)); code != 0 || err != nil {
		t.Fatalf("--no-input applies the change: code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "applied without asking") || !strings.Contains(stderr, "adds iris from Modrinth") {
		t.Fatalf("stderr: %s", stderr)
	}
}

func TestPreLaunchAppliesAChangeWarnsAndLogsIt(t *testing.T) {
	path := isolatedLog(t)
	h := newHarness(t)
	gameDir, _ := linkedWithNewMod(t, h)

	code, stdout, stderr := h.run(t, "hook", "pre-launch", "-C", gameDir)

	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["irisshaders"].filename)); code != 0 || err != nil {
		t.Fatalf("the hook applies the change: code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "applied without asking") {
		t.Fatalf("the hook warns: %s", stderr)
	}
	var warned []string
	for _, e := range logEntries(t, path) {
		if e.Level == cmdlog.LevelWarn {
			warned = append(warned, e.Msg)
		}
	}
	if !slices.ContainsFunc(warned, func(msg string) bool { return strings.Contains(msg, "adds iris from Modrinth") }) {
		t.Fatalf("the change is in the command log: %q", warned)
	}
}
