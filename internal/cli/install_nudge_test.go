package cli

import (
	"strings"
	"testing"
)

const launcherNudge = "Play it in a launcher"

func TestInstallNudgesAtALauncherUntilLinked(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--name", "pack")
	if stdout := h.mustRun(t, "install"); !strings.Contains(stdout, launcherNudge) || !strings.Contains(stdout, "$ shulker link <launcher>") {
		t.Fatalf("unlinked install output: %s", stdout)
	}
	if stdout := h.mustRun(t, "install", "--json"); strings.Contains(stdout, "launcher") {
		t.Fatalf("--json carries the nudge: %s", stdout)
	}

	h.mustRun(t, "link", "prism", "--launcher-dir", t.TempDir())
	if stdout := h.mustRun(t, "install"); strings.Contains(stdout, launcherNudge) {
		t.Fatalf("linked install output: %s", stdout)
	}
}

func TestInstallSkipsTheLauncherNudge(t *testing.T) {
	server := newHarness(t)
	server.mustRun(t, "create", "--name", "pack", "--side", "server")
	if stdout := server.mustRun(t, "install"); strings.Contains(stdout, launcherNudge) {
		t.Fatalf("server-only install output: %s", stdout)
	}

	inPlace := newInPlace(t)
	if stdout := inPlace.mustRun(t, "install"); strings.Contains(stdout, launcherNudge) {
		t.Fatalf("in-place install output: %s", stdout)
	}
}
