package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/launcher"
)

type localView struct {
	Features   map[string]bool `json:"features"`
	DetectedOS string          `json:"detectedOs"`
}

func TestFeatureChoicesAndOneOffFlags(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})
	localPath := filepath.Join(h.dir, "shulker.local.json")

	h.mustRun(t, "build")
	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Fatalf("a plain build must not create the local file: %v", err)
	}
	if stdout := h.mustRun(t, "feature", "list"); stdout != "fancy  off  gates: sodium\n" {
		t.Fatalf("list: %q", stdout)
	}
	code, stdout, _ := h.run(t, "feature", "on", "fanci", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "feature-not-found" || strings.Join(e.Candidates, ",") != "fancy" {
		t.Fatalf("unknown feature: exit %d %s", code, stdout)
	}

	if err := os.WriteFile(filepath.Join(h.dir, ".gitignore"), []byte("/build/"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr := h.mustRunStderr(t, "feature", "on", "fancy"); !strings.Contains(stderr, "added /shulker.local.json to .gitignore") {
		t.Fatalf("feature on stderr: %s", stderr)
	}
	if data, _ := os.ReadFile(filepath.Join(h.dir, ".gitignore")); string(data) != "/build/\n/shulker.local.json\n" {
		t.Fatalf(".gitignore: %q", data)
	}
	var lf localView
	h.readJSON(t, "shulker.local.json", &lf)
	if !lf.Features["fancy"] || lf.DetectedOS != build.DetectOS() {
		t.Fatalf("local file: %+v", lf)
	}
	if stdout := h.mustRun(t, "feature", "list"); stdout != "fancy  on (your choice)  gates: sodium\n" {
		t.Fatalf("list after on: %q", stdout)
	}
	h.mustRun(t, "build")
	if jars := modsDir(t, h); len(jars) != 2 {
		t.Fatalf("fancy on should ship sodium and fabric-api, got %v", jars)
	}

	if stdout := h.mustRun(t, "build", "--without", "fancy"); !strings.Contains(stdout, "excluded sodium (needs feature fancy)") {
		t.Fatalf("--without: %s", stdout)
	}
	h.readJSON(t, "shulker.local.json", &lf)
	if !lf.Features["fancy"] {
		t.Fatal("--without must not change the saved choice")
	}

	h.mustRun(t, "feature", "off", "fancy")
	h.mustRun(t, "build")
	if jars := modsDir(t, h); len(jars) != 0 {
		t.Fatalf("fancy off should ship nothing, got %v", jars)
	}
	h.mustRun(t, "build", "--with", "fancy")
	if jars := modsDir(t, h); len(jars) != 2 {
		t.Fatalf("--with fancy should ship sodium, got %v", jars)
	}

	if stdout := h.mustRun(t, "feature", "reset", "fancy"); !strings.Contains(stdout, "follows the target defaults again") {
		t.Fatalf("reset: %s", stdout)
	}
	if stdout := h.mustRun(t, "feature", "reset", "fancy"); !strings.Contains(stdout, "had no choice to reset") {
		t.Fatalf("second reset: %s", stdout)
	}
	setFeatures(t, h, []string{"fancy"})
	if stdout := h.mustRun(t, "feature", "list"); stdout != "fancy  on (target default)  gates: sodium\n" {
		t.Fatalf("list with a target default: %q", stdout)
	}

	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"build", "--with", "nope"}, "feature-not-found"},
		{[]string{"build", "--with", "fancy", "--without", "fancy"}, "usage"},
		{[]string{"build", "--os", "beos"}, "usage"},
		{[]string{"sync", h.dir, "--os", "beos"}, "usage"},
		{[]string{"export", "mrpack", "--version", "1", "--os", "beos"}, "usage"},
		{[]string{"export", "mrpack", "--version", "1", "--without", "nope"}, "feature-not-found"},
	} {
		code, stdout, _ := h.run(t, append(tc.args, "--json")...)
		if e := failureCode(t, stdout); code == 0 || e.Code != tc.code {
			t.Fatalf("%v: exit %d %s", tc.args, code, stdout)
		}
	}
}

func TestBuildForAnotherOS(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"os": "windows"})
	h.mustRun(t, "build", "--os", "windows")
	if jars := modsDir(t, h); len(jars) != 2 {
		t.Fatalf("--os windows should ship sodium, got %v", jars)
	}
	if stdout := h.mustRun(t, "build", "--os", "linux"); !strings.Contains(stdout, "excluded sodium (needs os windows)") {
		t.Fatalf("--os linux: %s", stdout)
	}
}

func TestLinkPrismKeepsFeatureFlags(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "my-pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})

	launcherDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", launcherDir, "--with", "fancy")
	cfg := readINIFile(t, filepath.Join(launcherDir, "instances", "shulker-my-pack", launcher.InstanceConfigFile))
	if !strings.HasSuffix(cfg["PreLaunchCommand"], ` --into "$INST_MC_DIR" --with fancy`) {
		t.Fatalf("PreLaunchCommand = %q", cfg["PreLaunchCommand"])
	}
	code, stdout, _ := h.run(t, "link", "prism", "--launcher-dir", launcherDir, "--mode", "symlink", "--with", "fancy", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" {
		t.Fatalf("symlink with --with: exit %d %s", code, stdout)
	}
}
