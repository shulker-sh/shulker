//go:build !windows

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
)

func enableLoader(t *testing.T, name string) {
	t.Helper()
	for i := range loader.All {
		if loader.All[i].Name == name && !loader.All[i].Supported {
			loader.All[i].Supported = true
			t.Cleanup(func() { loader.All[i].Supported = false })
		}
	}
}

func TestNeoForgeServer(t *testing.T) {
	enableLoader(t, "neoforge")
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "neoforge", "--target", "server")
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"eula": true}
	})
	var l lock.Lock
	h.readJSON(t, "shulker.lock", &l)
	if l.Loader.Type != "neoforge" || l.Loader.Version != "26.2.0.87" || l.Loader.Server != nil {
		t.Fatalf("lock loader after init: %+v", l.Loader)
	}

	stdout := h.mustRun(t, "install")
	if !strings.Contains(stdout, "installed neoforge 26.2.0.87") {
		t.Fatalf("install output:\n%s", stdout)
	}
	h.readJSON(t, "shulker.lock", &l)
	if s := l.Loader.Server; s == nil || s.Sha512 != h.neoInstaller.sha512 || s.Installer != "" || s.Minecraft != "" {
		t.Fatalf("lock server: %+v", s)
	}
	buildDir := filepath.Join(h.dir, "build", "server")
	if len(h.installs) != 1 || strings.Join(h.installs[0], " ") != "--install-server "+buildDir {
		t.Fatalf("installer runs: %v", h.installs)
	}
	if got := build.LoadState(buildDir).Loader; got == nil || *got != (build.InstalledLoader{Type: "neoforge", Version: "26.2.0.87"}) {
		t.Fatalf("state loader: %+v", got)
	}
	for _, name := range []string{"fabric-server-launch.jar", "neoforge-26.2.0.87-installer.jar", "installer.jar.log"} {
		if _, err := os.Stat(filepath.Join(buildDir, name)); err == nil {
			t.Fatalf("%s should not be in the server dir", name)
		}
	}

	h.mustRun(t, "install")
	h.mustRun(t, "build")
	if len(h.installs) != 1 || h.neoHits != 1 {
		t.Fatalf("an installed loader ran the installer again (%d runs, %d downloads)", len(h.installs), h.neoHits)
	}
	if build.LoadState(buildDir).Loader == nil {
		t.Fatal("a rebuild dropped the installed loader from build state")
	}

	h.stdin = strings.NewReader("stop\n")
	h.mustRun(t, "serve")
	args := readFile(t, filepath.Join(buildDir, "args.txt"))
	if !strings.HasSuffix(args, "\n@libraries/net/neoforged/neoforge/26.2.0.87/unix_args.txt\n--nogui\n") {
		t.Fatalf("serve args:\n%s", args)
	}

	if err := os.RemoveAll(filepath.Join(buildDir, "libraries")); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "build")
	if len(h.installs) != 2 {
		t.Fatalf("a missing args file should reinstall (%d runs)", len(h.installs))
	}

	if err := os.RemoveAll(filepath.Join(buildDir, "libraries")); err != nil {
		t.Fatal(err)
	}
	h.installErr = out.Errorf("installer-failed", "the loader installer failed")
	code, stdout, _ := h.run(t, "--json", "build")
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || code != 1 || env.Error.Code != "installer-failed" {
		t.Fatalf("failed installer: %d %s", code, stdout)
	}
	h.installErr = nil

	if err := build.RecordLoader(buildDir, build.InstalledLoader{Type: "neoforge", Version: "26.2.0.56-beta"}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(h.cache); err != nil {
		t.Fatal(err)
	}
	h.server.Close()
	code, _, stderr := h.run(t, "build")
	if code != 0 || !strings.Contains(stderr, "offline, keeping neoforge 26.2.0.56-beta installed in") {
		t.Fatalf("offline build: %d %s", code, stderr)
	}
}
