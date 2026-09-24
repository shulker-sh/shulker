//go:build !windows

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/server"
)

func TestNeoForgeServer(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "neoforge", "--side", "server")
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"eula": true}
	})
	var l lock.Lock
	h.readJSON(t, "shulker.lock", &l)
	if l.Loader.Type != "neoforge" || l.Loader.Version != "26.2.0.87" || l.Loader.Server != nil {
		t.Fatalf("lock loader after init: %+v", l.Loader)
	}

	stdout := h.mustRun(t, "install")
	if !strings.Contains(stdout, "installed: neoforge 26.2.0.87") {
		t.Fatalf("install output:\n%s", stdout)
	}
	h.readJSON(t, "shulker.lock", &l)
	if l.Loader.Server == nil || l.Loader.Server.Sha512 != h.neoInstaller.sha512 {
		t.Fatalf("lock loader server: %+v", l.Loader.Server)
	}
	if l.Server == nil || *l.Server != (lock.Download{URL: h.server.URL + "/piston-data/server.jar", Sha512: h.vanilla.sha512}) {
		t.Fatalf("lock server: %+v", l.Server)
	}
	buildDir := filepath.Join(h.dir, "build", "server")
	if len(h.installs) != 1 || strings.Join(h.installs[0], " ") != "--install-server "+buildDir+" --offline" {
		t.Fatalf("installer runs: %v", h.installs)
	}
	state := build.LoadState(buildDir)
	if got := state.InstalledLoader; got == nil || *got != (build.InstalledLoader{Type: "neoforge", Version: "26.2.0.87"}) {
		t.Fatalf("state loader: %+v", got)
	}
	for _, rel := range []string{"libraries/org/ow2/asm/asm/9.10.1/asm-9.10.1.jar", "libraries/net/minecraft/server/26.2/server-26.2.jar"} {
		if _, ok := state.Files[rel]; !ok {
			t.Fatalf("%s is not tracked by build state", rel)
		}
	}
	for _, name := range []string{"fabric-server-launch.jar", "server.jar", "neoforge-26.2.0.87-installer.jar", "installer.jar.log"} {
		if _, err := os.Stat(filepath.Join(buildDir, name)); err == nil {
			t.Fatalf("%s should not be in the server dir", name)
		}
	}

	hits := h.cdnHits
	h.mustRun(t, "install")
	stdout = h.mustRun(t, "build")
	if len(h.installs) != 1 || h.cdnHits != hits {
		t.Fatalf("an installed loader ran the installer again (%d runs, %d downloads)", len(h.installs), h.cdnHits-hits)
	}
	if !strings.Contains(stdout, "(5 unchanged)") || build.LoadState(buildDir).InstalledLoader == nil {
		t.Fatalf("rebuild: %s", stdout)
	}

	h.stdin = strings.NewReader("stop\n")
	h.mustRun(t, "serve")
	args := readFile(t, filepath.Join(buildDir, "args.txt"))
	if !strings.HasSuffix(args, "\n@libraries/net/neoforged/neoforge/26.2.0.87/unix_args.txt\n--nogui\n") {
		t.Fatalf("serve args:\n%s", args)
	}

	if err := os.Remove(filepath.Join(buildDir, "libraries/net/neoforged/neoforge/26.2.0.87/unix_args.txt")); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "build")
	if len(h.installs) != 2 {
		t.Fatalf("a missing args file should reinstall (%d runs)", len(h.installs))
	}

	if err := os.Remove(filepath.Join(buildDir, "libraries/net/neoforged/neoforge/26.2.0.87/unix_args.txt")); err != nil {
		t.Fatal(err)
	}
	h.installErr = &server.InstallerFailure{Err: out.Errorf("installer-failed", "the loader installer failed"), Output: "every line the installer printed\n"}
	code, stdout, _ := h.run(t, "--json", "build")
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || code != 1 || env.Error.Code != "installer-failed" {
		t.Fatalf("failed installer: %d %s", code, stdout)
	}
	_, logPath, found := strings.Cut(env.Error.Message, "\nFull output: ")
	if data, err := os.ReadFile(logPath); !found || err != nil || string(data) != "every line the installer printed\n" {
		t.Fatalf("installer output not saved: %q (%v)", env.Error.Message, err)
	}
	h.installErr = nil

	if err := os.RemoveAll(buildDir); err != nil {
		t.Fatal(err)
	}
	h.server.Close()
	h.mustRun(t, "build")
	if len(h.installs) != 4 {
		t.Fatalf("a new server dir should install offline from the cache (%d runs)", len(h.installs))
	}
}

func TestNeoForgeLinkMojang(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "neoforge")

	launcherDir := t.TempDir()
	writeProfiles(t, launcherDir, launcherProfiles{
		Profiles: map[string]map[string]any{
			"5433c688": {"name": "", "type": "latest-release", "lastVersionId": "latest-release"},
			"NeoForge": {"name": "my own", "lastVersionId": "neoforge-26.1.2.40"},
		},
		Version: 3,
	})

	stdout := h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	if !strings.Contains(stdout, "installed neoforge-26.2.0.87 »") {
		t.Fatalf("link output:\n%s", stdout)
	}
	if len(h.installs) != 1 || strings.Join(h.installs[0], " ") != "--install-client "+launcherDir {
		t.Fatalf("installer runs: %v", h.installs)
	}

	var l lock.Lock
	h.readJSON(t, "shulker.lock", &l)
	if l.Loader.Client == nil || l.Loader.Client.Sha512 != h.neoInstaller.sha512 {
		t.Fatalf("lock client: %+v", l.Loader.Client)
	}

	profiles := readProfiles(t, launcherDir)
	if own := profiles.Profiles["NeoForge"]; own["name"] != "my own" || own["lastVersionId"] != "neoforge-26.1.2.40" {
		t.Fatalf("the installer's profile was not put back: %v", own)
	}
	linked := profiles.Profiles["shulker-pack"]
	wantGameDir := mojangGameDir(launcherDir, "pack")
	if linked == nil || linked["lastVersionId"] != "neoforge-26.2.0.87" || linked["gameDir"] != wantGameDir {
		t.Fatalf("linked profile: %v", linked)
	}
}

// A launcher that has never run has no launcher_profiles.json, which the installers refuse.
func TestNeoForgeLinkMojangFreshLauncher(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "neoforge")

	launcherDir := t.TempDir()
	h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	profiles := readProfiles(t, launcherDir)
	if _, ok := profiles.Profiles["NeoForge"]; ok {
		t.Fatalf("the installer's profile was not removed: %v", profiles.Profiles)
	}
	if linked := profiles.Profiles["shulker-pack"]; linked["lastVersionId"] != "neoforge-26.2.0.87" {
		t.Fatalf("linked profile: %v", linked)
	}
}
