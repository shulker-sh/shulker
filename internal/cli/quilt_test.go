package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
)

func TestQuiltServer(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--name", "pack", "--loader", "quilt", "--side", "server")
	h.mustRun(t, "add", "fabric-api")
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{}
	})
	h.mustRun(t, "config", "set", "eula", "true")

	h.mustRun(t, "install")
	var l lock.Lock
	h.readJSON(t, "shulker.lock", &l)
	if l.Loader.Type != "quilt" || l.Loader.Version != "0.30.1" || l.Loader.Provides["fabricloader"] != "0.19.5" {
		t.Fatalf("lock loader: %+v", l.Loader)
	}
	if s := l.Loader.Server; s == nil || s.Sha512 != h.quiltLaunch.sha512 || len(s.Libraries) != 2 {
		t.Fatalf("lock loader server: %+v", s)
	}
	if l.Server == nil || *l.Server != (lock.Download{URL: h.server.URL + "/piston-data/server.jar", Sha512: h.vanilla.sha512}) {
		t.Fatalf("lock server: %+v", l.Server)
	}

	buildDir := filepath.Join(h.dir, "build", "server")
	if got := readFile(t, filepath.Join(buildDir, "server.jar")); got != string(h.vanilla.data) {
		t.Fatal("server.jar is not the vanilla jar")
	}
	for _, rel := range []string{"quilt-server-launch.jar", "libraries/org/quiltmc/quilt-loader/0.30.1/quilt-loader-0.30.1.jar", "libraries/net/fabricmc/sponge-mixin/0.17.3/sponge-mixin-0.17.3.jar", "mods/fabric-api-0.130.0+26.2.jar"} {
		if _, err := os.Stat(filepath.Join(buildDir, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}

	h.stdin = strings.NewReader("stop\n")
	h.mustRun(t, "serve")
	args := readFile(t, filepath.Join(buildDir, "args.txt"))
	if !strings.HasSuffix(args, "\n-jar\nquilt-server-launch.jar\n--nogui\n") {
		t.Fatalf("serve args:\n%s", args)
	}
}

func TestQuiltLinkMojang(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--name", "pack", "--loader", "quilt")
	launcherDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(launcherDir, "launcher_profiles.json"), []byte(`{"profiles":{},"version":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	if !strings.Contains(stdout, "installed quilt-loader-0.30.1-26.2 »") {
		t.Fatalf("link output: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(launcherDir, "versions", "quilt-loader-0.30.1-26.2", "quilt-loader-0.30.1-26.2.json")); err != nil {
		t.Fatal(err)
	}
}
