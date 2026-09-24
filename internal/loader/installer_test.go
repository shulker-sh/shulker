package loader

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
)

func TestServerJarsFollowTheForgeVersion(t *testing.T) {
	cases := []struct {
		minecraft, version, vanilla string
		launch                      []string
	}{
		{"1.12.2", "14.23.5.2860", "minecraft_server.1.12.2.jar", []string{"-jar", "forge-1.12.2-14.23.5.2860.jar"}},
		{"1.16.5", "36.2.39", "minecraft_server.1.16.5.jar", []string{"-jar", "forge-1.16.5-36.2.39.jar"}},
		{"26.2", "65.1.3", "libraries/net/minecraft/server/26.2/server-26.2-bundled.jar", []string{"@libraries/net/minecraftforge/forge/26.2-65.1.3/"}},
	}
	for _, c := range cases {
		lk := &lock.Lock{Minecraft: c.minecraft, Loader: lock.Loader{Type: "forge", Version: c.version}}
		l := Running(lk)
		if got := l.VanillaServerPath(c.minecraft); got != c.vanilla {
			t.Errorf("%s: vanilla jar at %s, want %s", c.minecraft, got, c.vanilla)
		}
		got := l.LaunchArgs(lk)
		if len(got) != len(c.launch) || !slices.Equal(got[:len(got)-1], c.launch[:len(c.launch)-1]) || !strings.HasPrefix(got[len(got)-1], c.launch[len(c.launch)-1]) {
			t.Errorf("%s: launch args %v, want %v", c.minecraft, got, c.launch)
		}
	}
}

func TestServerPlacementByLoader(t *testing.T) {
	cases := map[string]struct {
		vanilla string
		launch  []string
	}{
		"":         {"server.jar", []string{"-jar", "server.jar"}},
		"fabric":   {".fabric/server/26.2-server.jar", []string{"-jar", "fabric-server-launch.jar"}},
		"quilt":    {"server.jar", []string{"-jar", "quilt-server-launch.jar"}},
		"neoforge": {"libraries/net/minecraft/server/26.2/server-26.2.jar", []string{"@libraries/net/neoforged/neoforge/26.2.0.87/unix_args.txt"}},
	}
	for name, c := range cases {
		lk := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: name, Version: "26.2.0.87"}}
		l := Running(lk)
		if got := l.VanillaServerPath("26.2"); got != c.vanilla {
			t.Errorf("%q: vanilla jar at %s, want %s", name, got, c.vanilla)
		}
		if got := l.LaunchArgs(lk); !slices.Equal(got, c.launch) {
			t.Errorf("%q: launch args %v, want %v", name, got, c.launch)
		}
	}
}

func TestInstallerLibrariesSkipBundledOnes(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"install_profile.json": `{"libraries":[{"name":"a:b:1","downloads":{"artifact":{"url":"https://x/b.jar","sha1":"1"}}},{"name":"a:inside:1","downloads":{"artifact":{"url":""}}}]}`,
		"version.json":         `{"libraries":[{"name":"a:c:2","downloads":{"artifact":{"url":"https://x/c.jar","sha1":"2"}}}]}`,
	}
	for name, content := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(content))
	}
	zw.Close()
	path := filepath.Join(t.TempDir(), "installer.jar")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	libs, err := installerLibraries(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 2 || libs[0] != (installerLibrary{"a:b:1", "https://x/b.jar", "1"}) || libs[1] != (installerLibrary{"a:c:2", "https://x/c.jar", "2"}) {
		t.Fatalf("libraries %+v", libs)
	}
	if raw, err := installerVersionJSON(path); err != nil || string(raw) != files["version.json"] {
		t.Fatalf("version.json %s, %v", raw, err)
	}
}
