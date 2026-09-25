package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
)

func profileJavaDir(t *testing.T, launcherDir, key string) string {
	t.Helper()
	p := readProfiles(t, launcherDir).Profiles[key]
	if p == nil {
		t.Fatalf("no profile %s in %s", key, launcherDir)
	}
	java, _ := p["javaDir"].(string)
	return java
}

func hookFiles(dir string) []string {
	return []string{
		filepath.Join(dir, instance.Dir, "pre-launch"),
		filepath.Join(dir, instance.Dir, "post-exit"),
		launcher.ShimPath(dir),
	}
}

func TestLinkMojangPointsTheProfileAtTheShim(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	launcherDir := writeMojangLauncher(t)
	gameDir := mojangGameDir(launcherDir, "pack")

	h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	shim := launcher.ShimPath(gameDir)
	if got := profileJavaDir(t, launcherDir, "shulker-pack"); got != shim {
		t.Fatalf("javaDir = %q, want the shim %q", got, shim)
	}
	for _, path := range hookFiles(gameDir) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("a linked profile gets its scripts and its shim: %v", err)
		}
	}
	data, err := os.ReadFile(shim)
	if err != nil {
		t.Fatal(err)
	}
	f := readIntent(t, gameDir)
	body := string(data)
	if !strings.Contains(body, "'"+f.Settings.Shulker+"'") {
		t.Fatalf("the shim runs the recorded shulker:\n%s", body)
	}
	if java := resolvedJava(t, gameDir); java == "" || !strings.Contains(body, "'"+java+"'") {
		t.Fatalf("the shim falls back to the managed runtime %q:\n%s", java, body)
	}
	if f.Resolved.LauncherJava != "" {
		t.Fatalf("the profile had no Java of its own, so there is nothing to restore: %+v", f.Resolved)
	}
}

func TestMojangShimFollowsTheSwitchesAndGivesTheJavaBack(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	launcherDir := t.TempDir()
	gameDir := mojangGameDir(launcherDir, "pack")
	// A profile from an earlier link, with a Java the player chose for it.
	const playerJava = "/opt/jdk/bin/java"
	if err := os.WriteFile(filepath.Join(launcherDir, "launcher_profiles.json"), fmt.Appendf(nil,
		`{"profiles":{"shulker-pack":{"name":"pack","type":"custom","lastVersionId":"26.2","gameDir":%q,"javaDir":%q}},"version":3}`,
		gameDir, playerJava), 0o644); err != nil {
		t.Fatal(err)
	}

	h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	shim := launcher.ShimPath(gameDir)
	if got := profileJavaDir(t, launcherDir, "shulker-pack"); got != shim {
		t.Fatalf("javaDir = %q, want the shim %q", got, shim)
	}
	if got := readIntent(t, gameDir).Resolved.LauncherJava; got != playerJava {
		t.Fatalf("launcherJava = %q, want the Java the profile had", got)
	}

	h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	if got := readIntent(t, gameDir).Resolved.LauncherJava; got != playerJava {
		t.Fatalf("a relink finds its own shim there and must keep what it captured first, got %q", got)
	}

	// Both switches off by hand: the profile goes back to the Java it had.
	f := readIntent(t, gameDir)
	off := false
	f.Settings.Hooks.PreLaunch, f.Settings.Hooks.PostExit = &off, &off
	if err := f.Save(gameDir); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "instances", "repair", "--launcher", "mojang", "--launcher-dir", launcherDir)
	if got := profileJavaDir(t, launcherDir, "shulker-pack"); got != playerJava {
		t.Fatalf("javaDir = %q, want the player's Java back", got)
	}
	for _, path := range hookFiles(gameDir) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("the switches are off, so nothing generated should be left: %s (%v)", path, err)
		}
	}

	f = readIntent(t, gameDir)
	on := true
	f.Settings.Hooks.PreLaunch, f.Settings.Hooks.PostExit = &on, &on
	if err := f.Save(gameDir); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "instances", "repair", "--launcher", "mojang", "--launcher-dir", launcherDir)
	if got := profileJavaDir(t, launcherDir, "shulker-pack"); got != shim {
		t.Fatalf("switching a hook back on writes the shim again, got %q", got)
	}

	h.mustRun(t, "unlink", "pack")
	if profiles := readProfiles(t, launcherDir).Profiles; len(profiles) != 0 {
		t.Fatalf("unlink removes the profile: %+v", profiles)
	}
	for _, path := range hookFiles(gameDir) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unlink takes the generated files with it: %s (%v)", path, err)
		}
	}
}
