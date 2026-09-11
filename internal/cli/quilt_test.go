package cli

import (
	"archive/zip"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
)

func TestQuiltServer(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "quilt", "--target", "server")
	h.mustRun(t, "add", "fabric-api")
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"eula": true}
	})

	h.mustRun(t, "install")
	var l lock.Lock
	h.readJSON(t, "shulker.lock", &l)
	if l.Loader.Type != "quilt" || l.Loader.Version != "0.30.1" || l.Loader.Provides["fabricloader"] != "0.19.5" {
		t.Fatalf("lock loader: %+v", l.Loader)
	}
	s := l.Loader.Server
	base := h.server.URL
	wantLibs := map[string]lock.Download{
		"org.quiltmc:quilt-loader:0.30.1":  {URL: base + "/qmaven/org/quiltmc/quilt-loader/0.30.1/quilt-loader-0.30.1.jar", Sha512: h.quiltLoader.sha512},
		"net.fabricmc:sponge-mixin:0.17.3": {URL: base + "/fmaven/net/fabricmc/sponge-mixin/0.17.3/sponge-mixin-0.17.3.jar", Sha512: h.mixin.sha512},
	}
	if s == nil || s.Installer != "" || s.URL != "" || *s.Minecraft != (lock.Download{URL: base + "/piston-data/server.jar", Sha512: h.vanilla.sha512}) || !maps.Equal(s.Libraries, wantLibs) {
		t.Fatalf("lock server: %+v", s)
	}

	buildDir := filepath.Join(h.dir, "build", "server")
	if got := readFile(t, filepath.Join(buildDir, "server.jar")); got != string(h.vanilla.data) {
		t.Fatal("server.jar is not the vanilla jar")
	}
	for _, rel := range []string{"libraries/org/quiltmc/quilt-loader/0.30.1/quilt-loader-0.30.1.jar", "libraries/net/fabricmc/sponge-mixin/0.17.3/sponge-mixin-0.17.3.jar", "mods/fabric-api-0.130.0+26.2.jar"} {
		if _, err := os.Stat(filepath.Join(buildDir, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	entries := zipEntries(t, filepath.Join(buildDir, "quilt-server-launch.jar"))
	manifest := strings.ReplaceAll(entries["META-INF/MANIFEST.MF"], "\r\n ", "")
	want := "Main-Class: org.quiltmc.loader.impl.launch.server.QuiltServerLauncher\r\n" +
		"Class-Path: libraries/net/fabricmc/sponge-mixin/0.17.3/sponge-mixin-0.17.3.jar libraries/org/quiltmc/quilt-loader/0.30.1/quilt-loader-0.30.1.jar\r\n"
	if !strings.Contains(manifest, want) {
		t.Fatalf("manifest:\n%s", entries["META-INF/MANIFEST.MF"])
	}
	for _, line := range strings.Split(entries["META-INF/MANIFEST.MF"], "\r\n") {
		if len(line) > 72 {
			t.Fatalf("manifest line over 72 bytes: %q", line)
		}
	}
	if entries["quilt-server-launch.properties"] != "launch.mainClass=org.quiltmc.loader.impl.launch.knot.KnotServer\n" {
		t.Fatalf("launch properties: %q", entries["quilt-server-launch.properties"])
	}

	hits := h.quiltHits
	h.mustRun(t, "install")
	if h.quiltHits != hits {
		t.Fatalf("second install downloaded %d more files", h.quiltHits-hits)
	}

	if err := os.RemoveAll(h.cache); err != nil {
		t.Fatal(err)
	}
	lockBefore := readFile(t, filepath.Join(h.dir, "shulker.lock"))
	h.mustRun(t, "install")
	if got := readFile(t, filepath.Join(h.dir, "shulker.lock")); got != lockBefore {
		t.Fatalf("a fresh cache changed the lock:\n%s", got)
	}
}

func TestQuiltLinkMojang(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "quilt")
	launcherDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(launcherDir, "launcher_profiles.json"), []byte(`{"profiles":{},"version":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	if !strings.Contains(stdout, "Installed quilt-loader-0.30.1-26.2 into") {
		t.Fatalf("link output: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(launcherDir, "versions", "quilt-loader-0.30.1-26.2", "quilt-loader-0.30.1-26.2.json")); err != nil {
		t.Fatal(err)
	}
}

func zipEntries(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	entries := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name] = string(data)
	}
	return entries
}
