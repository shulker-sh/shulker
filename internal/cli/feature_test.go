package cli

import (
	"encoding/json"
	"os"
	"os/exec"
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

func readLocal(t *testing.T, dir string) localView {
	t.Helper()
	var lf localView
	data, err := os.ReadFile(filepath.Join(dir, "shulker.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &lf); err != nil {
		t.Fatal(err)
	}
	return lf
}

func TestFeatureIntoSyncedDir(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})
	into := filepath.Join(t.TempDir(), "instance")
	jar := filepath.Join(into, "mods", h.jars["sodium"].filename)
	h.mustRun(t, "sync", h.dir, "--into", into)

	stdout := h.mustRun(t, "feature", "on", "fancy", "--into", into)
	if stdout != "  ✔ fancy on » "+into+" (takes effect on the next sync; a linked Prism instance syncs on launch)\n" {
		t.Fatalf("feature on --into: %q", stdout)
	}
	if lf := readLocal(t, into); !lf.Features["fancy"] {
		t.Fatalf("instance local file: %+v", lf)
	}
	if lf := readLocal(t, h.dir); len(lf.Features) != 0 {
		t.Fatalf("the project local file must not change: %+v", lf)
	}
	if _, err := os.Stat(jar); !os.IsNotExist(err) {
		t.Fatalf("feature --into without --sync must not sync: %v", err)
	}
	if stdout := h.mustRun(t, "feature", "list", "--into", into); stdout != "  • fancy on (your choice, gates: sodium)\n" {
		t.Fatalf("list --into: %q", stdout)
	}

	if stdout := h.mustRun(t, "feature", "on", "fancy", "--into", into, "--sync"); !strings.HasPrefix(stdout, "  ✔ fancy on » "+into+"\n  ✔ synced client » ") {
		t.Fatalf("feature on --sync: %q", stdout)
	}
	if _, err := os.Stat(jar); err != nil {
		t.Fatalf("--sync should ship the gated mod: %v", err)
	}
	if stdout := h.mustRun(t, "feature", "reset", "fancy", "--into", into, "--sync"); !strings.Contains(stdout, "excluded: sodium") {
		t.Fatalf("feature reset --sync: %q", stdout)
	}
	if _, err := os.Stat(jar); !os.IsNotExist(err) {
		t.Fatalf("reset --sync should drop the gated mod: %v", err)
	}

	code, stdout, _ := h.run(t, "feature", "on", "fanci", "--into", into, "--json")
	if code == 0 || failureCode(t, stdout).Code != "feature-not-found" {
		t.Fatalf("unknown feature --into: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "feature", "on", "fancy", "--sync", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--sync without --into: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "feature", "on", "fancy", "--into", t.TempDir(), "--json")
	if code == 0 || failureCode(t, stdout).Code != "not-synced" {
		t.Fatalf("--into a dir never synced: exit %d %s", code, stdout)
	}
}

func TestFeatureIntoGitSyncedDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "one")
	source := "file://" + h.dir
	into := filepath.Join(t.TempDir(), "instance")
	h.mustRun(t, "sync", source, "--into", into)

	h.mustRun(t, "feature", "on", "fancy", "--into", into, "--sync")
	if _, err := os.Stat(filepath.Join(into, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("--sync from the recorded git source should ship the gated mod: %v", err)
	}
	if st := build.LoadState(into); st.Source != source {
		t.Fatalf("state origin: %+v", st.Origin)
	}
}

func TestFeatureChoicesAndOneOffFlags(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})
	localPath := filepath.Join(h.dir, "shulker.local.json")

	h.mustRun(t, "build")
	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Fatalf("a plain build must not create the local file: %v", err)
	}
	if stdout := h.mustRun(t, "feature", "list"); stdout != "  • fancy off (gates: sodium)\n" {
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
	if stdout := h.mustRun(t, "feature", "list"); stdout != "  • fancy on (your choice, gates: sodium)\n" {
		t.Fatalf("list after on: %q", stdout)
	}
	h.mustRun(t, "build")
	if jars := modsDir(t, h); len(jars) != 2 {
		t.Fatalf("fancy on should ship sodium and fabric-api, got %v", jars)
	}

	if stdout := h.mustRun(t, "build", "--without", "fancy"); !strings.Contains(stdout, "excluded: sodium (needs feature fancy)") {
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
	if stdout := h.mustRun(t, "feature", "list"); stdout != "  • fancy on (target default, gates: sodium)\n" {
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
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"os": "windows"})
	h.mustRun(t, "build", "--os", "windows")
	if jars := modsDir(t, h); len(jars) != 2 {
		t.Fatalf("--os windows should ship sodium, got %v", jars)
	}
	if stdout := h.mustRun(t, "build", "--os", "linux"); !strings.Contains(stdout, "excluded: sodium (needs os windows)") {
		t.Fatalf("--os linux: %s", stdout)
	}
}

func TestLinkPrismKeepsFeatureFlags(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "my-pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})

	launcherDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", launcherDir, "--with", "fancy")
	instDir := filepath.Join(launcherDir, "instances", "shulker-my-pack")
	cfg := readINIFile(t, filepath.Join(instDir, launcher.InstanceConfigFile))
	if !strings.HasSuffix(cfg["PreLaunchCommand"], `/.shulker/pre-launch"`) {
		t.Fatalf("PreLaunchCommand = %q", cfg["PreLaunchCommand"])
	}
	gameDir := filepath.Join(instDir, "minecraft")
	if lf := readLocal(t, gameDir); !lf.Features["fancy"] {
		t.Fatalf("instance local file: %+v", lf)
	}
	h.mustRun(t, "link", "prism", "--launcher-dir", launcherDir, "--without", "fancy")
	if lf := readLocal(t, gameDir); lf.Features["fancy"] {
		t.Fatalf("re-linking with --without should update the choice: %+v", lf)
	}
	h.mustRun(t, "link", "prism", "--launcher-dir", launcherDir)
	if lf := readLocal(t, gameDir); len(lf.Features) != 1 {
		t.Fatalf("re-linking without flags keeps the choices: %+v", lf)
	}
	code, stdout, _ := h.run(t, "link", "prism", "--launcher-dir", launcherDir, "--mode", "symlink", "--with", "fancy", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" {
		t.Fatalf("symlink with --with: exit %d %s", code, stdout)
	}
}
