package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
)

// markerJarIn is the marker an instance's own builds carry. It is named after the instance's
// manifest, which a link makes unique across the registry, so two instances of one pack differ.
func markerJarIn(t *testing.T, gameDir string) string {
	t.Helper()
	name, _ := instanceManifest(t, gameDir)["name"].(string)
	return filepath.Join(gameDir, "mods", "shulker-"+name+".jar")
}

func TestLinkSeedsHooksFromTheManifestThenTheFlags(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.editManifest(t, func(m map[string]any) {
		m["marker"] = false
		m["client"].(map[string]any)["hooks"] = map[string]any{"postExit": false}
	})

	launcherDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", launcherDir)
	instDir := filepath.Join(launcherDir, "instances", "shulker-pack")
	gameDir := filepath.Join(instDir, "minecraft")
	f := readIntent(t, gameDir)
	if !f.Settings.PreLaunch() || f.Settings.PostExit() || f.Settings.Marker != nil {
		t.Fatalf("the hooks seed the instance and the marker is left to the manifest: %+v", f.Settings)
	}
	cfg := readINIFile(t, filepath.Join(instDir, launcher.PrismInstanceFile))
	if cfg["PreLaunchCommand"] == "" || cfg["PostExitCommand"] != "" {
		t.Fatalf("the switch that is off gets no slot: %+v", cfg)
	}
	markerJar := markerJarIn(t, gameDir)
	if _, err := os.Stat(markerJar); !os.IsNotExist(err) {
		t.Fatalf("a pack that leaves the marker out gives an instance that leaves it out: %v", err)
	}

	// An instance that says on keeps the jar over a manifest that leaves it out.
	f.Settings.Marker = instance.On()
	if err := f.Save(gameDir); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "-C", gameDir, "sync")
	if _, err := os.Stat(markerJar); err != nil {
		t.Fatalf("settings.marker on wins over the manifest: %v", err)
	}

	second := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", second,
		"--no-pre-launch", "--no-marker", "--java", filepath.Join(h.dir, "jdk", "bin", "java"), "--wrapper", "gamemoderun --dlsym")
	secondGame := filepath.Join(second, "instances", "shulker-pack", "minecraft")
	f = readIntent(t, secondGame)
	if f.Settings.PreLaunch() || f.Settings.PostExit() || f.Settings.Marker == nil || *f.Settings.Marker {
		t.Fatalf("--no-pre-launch over the manifest's own postExit, and --no-marker written out: %+v", f.Settings)
	}
	if f.Settings.Java != filepath.Join(h.dir, "jdk", "bin", "java") || !slices.Equal(f.Settings.Wrapper, []string{"gamemoderun", "--dlsym"}) {
		t.Fatalf("--java and --wrapper land in the settings: %+v", f.Settings)
	}
	cfg = readINIFile(t, filepath.Join(second, "instances", "shulker-pack", launcher.PrismInstanceFile))
	if cfg["WrapperCommand"] != "gamemoderun --dlsym" || cfg["OverrideCommands"] != "true" {
		t.Fatalf("the wrapper goes in the launcher's own slot, with the override it needs: %+v", cfg)
	}
	if cfg["PreLaunchCommand"] != "" || cfg["PostExitCommand"] != "" {
		t.Fatalf("both switches are off here, so neither command slot is filled: %+v", cfg)
	}

	// --with-marker puts the jar back over a manifest that leaves it out.
	third := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", third, "--with-marker")
	thirdGame := filepath.Join(third, "instances", "shulker-pack", "minecraft")
	if f := readIntent(t, thirdGame); f.Settings.Marker == nil || !*f.Settings.Marker {
		t.Fatalf("--with-marker writes the switch on: %+v", f.Settings)
	}
	if _, err := os.Stat(markerJarIn(t, thirdGame)); err != nil {
		t.Fatalf("--with-marker keeps the marker jar: %v", err)
	}

	// Once the instance exists the settings are the player's: a sync leaves a hand edit alone.
	f.Settings.Java = filepath.Join(h.dir, "other", "bin", "java")
	f.Settings.Hooks.PreLaunch = nil
	if err := f.Save(secondGame); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "-C", secondGame, "sync")
	f = readIntent(t, secondGame)
	if f.Settings.Java != filepath.Join(h.dir, "other", "bin", "java") || !f.Settings.PreLaunch() {
		t.Fatalf("a sync must not rewrite the settings block: %+v", f.Settings)
	}
	// An in-place instance builds itself, so nothing touches the launcher's slots until a repair.
	h.mustRun(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", second)
	cfg = readINIFile(t, filepath.Join(second, "instances", "shulker-pack", launcher.PrismInstanceFile))
	if cfg["PreLaunchCommand"] == "" {
		t.Fatalf("a hand-edited switch takes effect on the next repair: %+v", cfg)
	}
}

func TestLinkSettingsFlagsOnEveryLauncher(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")

	launcherDir := t.TempDir()
	code, stdout, _ := h.run(t, "link", "prism", "--launcher-dir", launcherDir, "--java", "jdk/bin/java", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--java must be absolute: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "link", "prism", "--launcher-dir", launcherDir, "--no-marker", "--with-marker", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--no-marker and --with-marker ask for opposite things: exit %d %s", code, stdout)
	}

	for _, name := range []string{"mojang", "prism", "multimc", "atlauncher", "gdlauncher"} {
		dir := t.TempDir()
		if name == "multimc" {
			if err := os.WriteFile(filepath.Join(dir, "multimc.cfg"), []byte("[General]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		h.mustRun(t, "link", name, "--launcher-dir", dir, "--no-hooks", "--no-marker", "--wrapper", "gamemoderun")
		gameDir := gameDirOfInstance(t, h, name)
		f := readIntent(t, gameDir)
		if f.Settings.PreLaunch() || f.Settings.PostExit() || f.Settings.Marker == nil || *f.Settings.Marker {
			t.Fatalf("%s: --no-hooks turns both switches off: %+v", name, f.Settings)
		}
		if !slices.Equal(f.Settings.Wrapper, []string{"gamemoderun"}) {
			t.Fatalf("%s: wrapper %q", name, f.Settings.Wrapper)
		}
	}
}

// gameDirOfInstance is the directory the launcher's own row points at, which is where link put the
// instance file.
func gameDirOfInstance(t *testing.T, h *harness, launcherName string) string {
	t.Helper()
	for _, in := range readInstances(t, h) {
		if in.Launcher == launcherName {
			return in.Dir
		}
	}
	t.Fatalf("no %s row in the registry", launcherName)
	return ""
}
