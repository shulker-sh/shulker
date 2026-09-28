package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/selfupdate"
)

// uninstallHarness is a project with a Prism instance, an official launcher profile and a
// detached build, plus a stand-in for the running binary.
func uninstallHarness(t *testing.T) (h *harness, prismDir, mojangDir, into string) {
	t.Helper()
	h = newHarness(t)
	h.exe = filepath.Join(t.TempDir(), "shulker")
	if err := os.WriteFile(h.exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	prismDir, mojangDir = t.TempDir(), writeMojangLauncher(t)
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir)
	h.mustRun(t, "link", "mojang", "--launcher-dir", mojangDir)
	into = filepath.Join(t.TempDir(), "plain")
	h.mustRun(t, "sync", h.dir, "--into", into)
	return h, prismDir, mojangDir, into
}

func TestSelfUninstallClearsEveryInstanceAndLeavesTheRegistry(t *testing.T) {
	h, prismDir, mojangDir, into := uninstallHarness(t)
	gameDir := mojangGameDir(mojangDir, "pack")
	// A folder that moved away is still unhooked, from what the registry records about it.
	moved := filepath.Join(prismDir, "instances", "shulker-pack", "minecraft")
	prismCfg := filepath.Join(prismDir, "instances", "shulker-pack", launcher.PrismInstanceFile)
	// A hook some other shulker binary wrote is released all the same.
	f := readIntent(t, moved)
	f.Settings.Shulker = "/elsewhere/shulker"
	if err := f.Save(moved); err != nil {
		t.Fatal(err)
	}

	stdout := h.mustRun(t, "self", "uninstall")
	if !strings.Contains(stdout, "Unhooked 2 instances") || !strings.Contains(stdout, "Removed "+h.exe) {
		t.Fatalf("uninstall output: %s", stdout)
	}
	if !strings.Contains(stdout, "• pack (Prism Launcher)") || !strings.Contains(stdout, "• pack-2 (Minecraft Launcher: pack)") {
		t.Fatalf("every instance is named: %s", stdout)
	}
	if !strings.Contains(stdout, "The registry and every instance folder are untouched") || !strings.Contains(stdout, "$ shulker instances repair") {
		t.Fatalf("uninstall says what it left: %s", stdout)
	}
	if _, err := os.Stat(h.exe); !os.IsNotExist(err) {
		t.Fatalf("the binary should be gone: %v", err)
	}
	if cfg := readINIFile(t, prismCfg); cfg["PreLaunchCommand"] != "" || cfg["PostExitCommand"] != "" {
		t.Fatalf("the launcher's slots are cleared: %+v", cfg)
	}
	if java := profileJavaDir(t, mojangDir, "shulker-pack"); java != "" {
		t.Fatalf("javaDir = %q, want the profile's own Java back", java)
	}
	for _, dir := range []string{moved, gameDir} {
		for _, path := range hookFiles(dir) {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("%s should be gone: %v", path, err)
			}
		}
	}
	if instances := readInstances(t, h); len(instances) != 2 {
		t.Fatalf("the registry survives an uninstall: %+v", instances)
	}
	if _, err := os.Stat(prismCfg); err != nil {
		t.Fatalf("the launcher's own instance stays: %v", err)
	}
	if _, err := os.Stat(filepath.Join(into, "mods")); err != nil {
		t.Fatalf("a synced directory keeps its build: %v", err)
	}

	// Reinstalling and repairing puts every hook back, which is what keeping the registry buys.
	if err := os.WriteFile(h.exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "instances", "repair")
	if !strings.Contains(stdout, "Rehooked 2 instances") || !strings.Contains(stdout, "• pack (Prism Launcher)") || strings.Contains(stdout, "Every instance is registered") {
		t.Fatalf("repair reports what it hooked again: %s", stdout)
	}
	if cfg := readINIFile(t, prismCfg); cfg["PreLaunchCommand"] == "" {
		t.Fatalf("a repair after reinstalling hooks the instance again: %+v", cfg)
	}
	if java := profileJavaDir(t, mojangDir, "shulker-pack"); java != launcher.ShimPath(gameDir) {
		t.Fatalf("javaDir = %q, want the shim back", java)
	}
}

func TestSelfUninstallPurgeForgetsTheRegistry(t *testing.T) {
	h, _, _, into := uninstallHarness(t)
	stdout := h.mustRun(t, "self", "uninstall", "--purge")
	if !strings.Contains(stdout, "Forgot the registry") || strings.Contains(stdout, "$ shulker instances repair") {
		t.Fatalf("purge output: %s", stdout)
	}
	// Every row is a launcher's, so a repair finds them all again; the detached build was
	// never on the registry to lose.
	if strings.Contains(stdout, "can't be found again") || strings.Contains(stdout, into) {
		t.Fatalf("purge should have nothing to name: %s", stdout)
	}
	if _, err := os.Stat(registryPath(h)); !os.IsNotExist(err) {
		t.Fatalf("the registry should be gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(into, "mods")); err != nil {
		t.Fatalf("--purge still keeps the folders: %v", err)
	}
}

func TestSelfUninstallWarnsAndCarriesOn(t *testing.T) {
	h := newHarness(t)
	h.exe = filepath.Join(t.TempDir(), "shulker")
	if err := os.WriteFile(h.exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	launcherDir := t.TempDir()
	h.mustRun(t, "link", "atlauncher", "--launcher-dir", launcherDir)
	instanceFile := filepath.Join(launcherDir, "instances", "pack", launcher.ATLauncherInstanceFile)
	if err := os.WriteFile(instanceFile, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := h.mustRunStderr(t, "self", "uninstall")
	if !strings.Contains(stderr, "keeps shulker's hooks") {
		t.Fatalf("an instance that can't be unhooked warns: %s", stderr)
	}
	if strings.Contains(stdout, "Unhooked") || !strings.Contains(stdout, "Removed "+h.exe) {
		t.Fatalf("the binary still goes: %s", stdout)
	}
	if _, err := os.Stat(h.exe); !os.IsNotExist(err) {
		t.Fatalf("the binary should be gone: %v", err)
	}
}

func TestSelfUninstallWithNothingLinked(t *testing.T) {
	h := newHarness(t)
	h.exe = filepath.Join(t.TempDir(), "shulker")
	if err := os.WriteFile(h.exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout := h.mustRun(t, "self", "uninstall")
	if strings.Contains(stdout, "unhooked") {
		t.Fatalf("nothing was linked, so nothing is named: %s", stdout)
	}
	if !strings.Contains(stdout, "Removed "+h.exe) || !strings.Contains(stdout, "untouched") {
		t.Fatalf("uninstall output: %s", stdout)
	}
	code, stdout, _ := h.run(t, "self", "uninstall", "--json")
	if code == 0 || failureCode(t, stdout).Code != "self-uninstall" {
		t.Fatalf("a binary that can't be removed: exit %d %s", code, stdout)
	}
}

func TestSelfUninstallPurgeLeavesShulkersOwnInstancesFindable(t *testing.T) {
	h, _, _, _ := uninstallHarness(t)
	shulkerInstances(t, h)
	h.mustRun(t, "link", "shulker", "--as", "smp")

	stdout := h.mustRun(t, "self", "uninstall", "--purge")
	if !strings.Contains(stdout, "smp (Shulker: pack)") || strings.Contains(stdout, "can't be found again") {
		t.Fatalf("a shulker instance is unhooked with the rest and found again by a repair of the instances root: %s", stdout)
	}
}

func TestSelfUninstallHandsTheBinaryToItsPackageManager(t *testing.T) {
	h, prismDir, _, _ := uninstallHarness(t)
	h.build = &selfupdate.Build{Version: "0.0.1", Route: selfupdate.Homebrew}
	prismCfg := filepath.Join(prismDir, "instances", "shulker-pack", launcher.PrismInstanceFile)

	stdout := h.mustRun(t, "self", "uninstall")
	if !strings.Contains(stdout, "Unhooked 2 instances") || strings.Contains(stdout, "Removed ") {
		t.Fatalf("the hooks go and the binary stays: %s", stdout)
	}
	if !strings.Contains(stdout, "i The binary is Homebrew's to remove") || !strings.HasSuffix(stdout, "\n  Remove it with:\n    $ brew uninstall shulker\n") {
		t.Fatalf("the handoff closes the output: %s", stdout)
	}
	if !strings.Contains(stdout, "$ shulker instances repair") {
		t.Fatalf("the registry is kept as ever: %s", stdout)
	}
	if _, err := os.Stat(h.exe); err != nil {
		t.Fatalf("the binary is the package manager's: %v", err)
	}
	if cfg := readINIFile(t, prismCfg); cfg["PreLaunchCommand"] != "" {
		t.Fatalf("the launcher's slots are cleared: %+v", cfg)
	}

	env := h.runSetting(t, 0, "self", "uninstall")
	var res selfUninstallResult
	if err := json.Unmarshal(env.Data, &res); err != nil {
		t.Fatal(err)
	}
	if res.Removed != "" || res.Install != "homebrew" || !strings.Contains(string(env.Data), `"removed": ""`) {
		t.Fatalf("removed is empty beside the route: %s", env.Data)
	}
}
