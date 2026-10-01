package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

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
		h.mustRun(t, "link", name, "--launcher-dir", dir, "--no-hooks", "--no-marker", "--wrapper", "gamemoderun", "--sandbox")
		gameDir := gameDirOfInstance(t, h, name)
		f := readIntent(t, gameDir)
		if f.Settings.PreLaunch() || f.Settings.PostExit() || f.Settings.Marker == nil || *f.Settings.Marker {
			t.Fatalf("%s: --no-hooks turns both switches off: %+v", name, f.Settings)
		}
		if !slices.Equal(f.Settings.Wrapper, []string{"gamemoderun"}) {
			t.Fatalf("%s: wrapper %q", name, f.Settings.Wrapper)
		}
		if !f.Settings.Sandboxed(false) {
			t.Fatalf("%s: --sandbox turns the instance's sandbox on: %+v", name, f.Settings)
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
