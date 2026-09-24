//go:build !windows

package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/lock"
)

func TestForgeServer(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "forge", "--side", "server")
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"eula": true}
	})

	h.mustRun(t, "install")
	var l lock.Lock
	h.readJSON(t, "shulker.lock", &l)
	if l.Loader.Type != "forge" || l.Loader.Version != "65.1.3" || l.Loader.Server == nil || l.Loader.Server.Sha512 != h.forgeInstaller.sha512 {
		t.Fatalf("lock loader: %+v", l.Loader)
	}

	buildDir := filepath.Join(h.dir, "build", "server")
	if len(h.installs) != 1 || strings.Join(h.installs[0], " ") != "--installServer "+buildDir+" --offline" {
		t.Fatalf("installer runs: %v", h.installs)
	}
	if got := build.LoadState(buildDir).InstalledLoader; got == nil || *got != (build.InstalledLoader{Type: "forge", Version: "65.1.3"}) {
		t.Fatalf("state loader: %+v", got)
	}

	h.stdin = strings.NewReader("stop\n")
	h.mustRun(t, "serve")
	args := readFile(t, filepath.Join(buildDir, "args.txt"))
	if !strings.HasSuffix(args, "\n@libraries/net/minecraftforge/forge/26.2-65.1.3/unix_args.txt\n--nogui\n") {
		t.Fatalf("serve args:\n%s", args)
	}
}

func TestForgeLinkMojangAndMarker(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "forge")

	launcherDir := t.TempDir()
	h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	if len(h.installs) != 1 || strings.Join(h.installs[0], " ") != "--installClient "+launcherDir {
		t.Fatalf("installer runs: %v", h.installs)
	}
	profiles := readProfiles(t, launcherDir)
	if _, ok := profiles.Profiles["forge"]; ok {
		t.Fatalf("the installer's profile was not removed: %v", profiles.Profiles)
	}
	if linked := profiles.Profiles["shulker-pack"]; linked["lastVersionId"] != "26.2-forge-65.1.3" {
		t.Fatalf("linked profile: %v", linked)
	}

	h.mustRun(t, "install")
	entries := readZip(t, []byte(readFile(t, filepath.Join(h.dir, "build", "client", "mods", "shulker-pack.jar"))))
	if _, ok := entries["META-INF/mods.toml"]; !ok {
		t.Fatalf("forge marker should declare itself in mods.toml: %v", entries)
	}
	if _, ok := entries["META-INF/neoforge.mods.toml"]; ok {
		t.Fatal("forge marker should not carry neoforge.mods.toml")
	}
	toml := string(entries["META-INF/mods.toml"])
	if strings.Contains(toml, "iconFile") {
		t.Fatal("iconFile is a NeoForge key; Forge reads logoFile")
	}
	// Forge rejects a mod file that names no language loader, or names one without a version, and
	// both loaders reject one with a blank license, which a manifest need not fill in.
	if !strings.Contains(toml, `modLoader = "lowcodefml"`) || !strings.Contains(toml, `loaderVersion = "[1,)"`) {
		t.Fatalf("forge marker must name its language loader:\n%s", toml)
	}
	if !strings.Contains(toml, `license = "All rights reserved"`) {
		t.Fatalf("marker with no manifest license must fall back:\n%s", toml)
	}
}
