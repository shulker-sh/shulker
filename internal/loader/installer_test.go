package loader

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
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

// installerJar is a loader installer whose profiles list libs by maven name with their download
// under base, plus one library the installer ships itself.
func installerJar(t *testing.T, base string, libs map[string]*fakeFile) []byte {
	t.Helper()
	library := func(name string) map[string]any {
		path, _ := MavenPath(name)
		artifact := map[string]any{"path": path, "url": ""}
		if f, ok := libs[name]; ok {
			sum := sha1.Sum(f.data)
			artifact["url"], artifact["sha1"] = base+"/maven/"+path, hex.EncodeToString(sum[:])
		}
		return map[string]any{"name": name, "downloads": map[string]any{"artifact": artifact}}
	}
	profile, _ := json.Marshal(map[string]any{"minecraft": "26.2", "libraries": []any{library("org.ow2.asm:asm:9.10.1"), library("net.neoforged:bundled:1.0")}})
	version, _ := json.Marshal(map[string]any{
		"id": "neoforge-26.2.0.87", "inheritsFrom": "26.2", "mainClass": "cpw.mods.bootstraplauncher.BootstrapLauncher",
		"libraries": []any{library("net.neoforged:neoforge:26.2.0.87:universal"), library("org.ow2.asm:asm:9.10.1")},
	})
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string][]byte{"install_profile.json": profile, "version.json": version} {
		w, _ := zw.Create(name)
		w.Write(content)
	}
	zw.Close()
	return buf.Bytes()
}

const neoInstallerPath = "/releases/net/neoforged/neoforge/26.2.0.87/neoforge-26.2.0.87-installer.jar"

// neoforgeRemote serves the NeoForge installer and the libraries it lists, the installer built
// once the server has its URL.
func neoforgeRemote(t *testing.T) (r *Remote, installer, universal, asm *fakeFile) {
	t.Helper()
	universal, asm = &fakeFile{data: []byte("universal")}, &fakeFile{data: []byte("asm")}
	installer = &fakeFile{}
	r = fakeRemote(t, NeoForgeMavenURL, map[string]any{
		neoInstallerPath: func(base string) any {
			if installer.data == nil {
				installer.data = installerJar(t, base, map[string]*fakeFile{"net.neoforged:neoforge:26.2.0.87:universal": universal, "org.ow2.asm:asm:9.10.1": asm})
			}
			return installer
		},
		"/maven/net/neoforged/neoforge/26.2.0.87/neoforge-26.2.0.87-universal.jar": universal,
		"/maven/org/ow2/asm/asm/9.10.1/asm-9.10.1.jar":                             asm,
	})
	return r, installer, universal, asm
}

func TestInstallerServerLocksTheJarAndItsLibraries(t *testing.T) {
	r, installer, universal, asm := neoforgeRemote(t)
	ctx := context.Background()
	lk := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: "neoforge", Version: "26.2.0.87"}}

	res, err := neoforge.EnsureServer(ctx, r, lk)
	if err != nil || !res.ChangedLock || !res.WasFetched {
		t.Fatalf("first ensure: %+v, %v", res, err)
	}
	base := r.URLs[NeoForgeMavenURL]
	want := &lock.ServerJar{
		URL: base + neoInstallerPath, Sha512: installer.sha512(),
		Libraries: map[string]lock.Download{
			"net.neoforged:neoforge:26.2.0.87:universal": {URL: base + "/maven/net/neoforged/neoforge/26.2.0.87/neoforge-26.2.0.87-universal.jar", Sha512: universal.sha512()},
			"org.ow2.asm:asm:9.10.1":                     {URL: base + "/maven/org/ow2/asm/asm/9.10.1/asm-9.10.1.jar", Sha512: asm.sha512()},
		},
	}
	if !reflect.DeepEqual(lk.Loader.Server, want) {
		t.Fatalf("lock loader server:\n%+v\nwant\n%+v", lk.Loader.Server, want)
	}
	if !isServerCached(r.Cache, lk.Loader.Server) {
		t.Fatal("the installer and its libraries are not all in the cache")
	}
	downloads := func() int { return installer.hits + universal.hits + asm.hits }
	if res, err := neoforge.EnsureServer(ctx, r, lk); err != nil || res != (ServerResult{}) || downloads() != 3 {
		t.Fatalf("a locked, cached server ensures nothing: %+v, %v, %d downloads", res, err, downloads())
	}

	r.Cache = &cache.Cache{Dir: t.TempDir()}
	res, err = neoforge.EnsureServer(ctx, r, lk)
	if err != nil || res.ChangedLock || !res.WasFetched || !reflect.DeepEqual(lk.Loader.Server, want) || !isServerCached(r.Cache, lk.Loader.Server) {
		t.Fatalf("a fresh cache downloads the locked files: %+v, %v\n%+v", res, err, lk.Loader.Server)
	}
}

func TestInstallServerRunsTheLockedInstallerOffline(t *testing.T) {
	for name, flag := range map[string]string{"neoforge": "--install-server", "forge": "--installServer", "fabric": ""} {
		r := &Remote{Cache: &cache.Cache{Dir: t.TempDir()}}
		sha, err := r.Cache.Put(strings.NewReader("installer " + name))
		if err != nil {
			t.Fatal(err)
		}
		var runs [][]string
		r.RunInstaller = func(_ context.Context, java, jar string, args []string) error {
			runs = append(runs, append([]string{java, jar}, args...))
			return nil
		}
		lk := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: name, Version: "1", Server: &lock.ServerJar{Sha512: sha}}}
		if err := Running(lk).InstallServer(context.Background(), r, lk, "/srv", "/usr/bin/java"); err != nil {
			t.Fatal(err)
		}
		if flag == "" {
			if len(runs) != 0 {
				t.Fatalf("%s: a loader shulker assembles runs no installer: %v", name, runs)
			}
			continue
		}
		want := []string{"/usr/bin/java", r.Cache.Object(sha), flag, "/srv", "--offline"}
		if len(runs) != 1 || !slices.Equal(runs[0], want) {
			t.Fatalf("%s: installer runs %v, want %v", name, runs, want)
		}
	}
}

func TestInstallClientLocksTheInstallerOnce(t *testing.T) {
	r, installer, _, _ := neoforgeRemote(t)
	var runs [][]string
	r.RunInstaller = func(_ context.Context, java, jar string, args []string) error {
		runs = append(runs, append([]string{java, jar}, args...))
		return nil
	}
	ctx := context.Background()
	lk := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: "neoforge", Version: "26.2.0.87"}}

	changed, err := neoforge.InstallClient(ctx, r, lk, "/launcher", "/usr/bin/java")
	if err != nil || !changed {
		t.Fatalf("first install: changed %v, %v", changed, err)
	}
	want := &lock.Download{URL: r.URLs[NeoForgeMavenURL] + neoInstallerPath, Sha512: installer.sha512()}
	if !reflect.DeepEqual(lk.Loader.Client, want) {
		t.Fatalf("lock client %+v, want %+v", lk.Loader.Client, want)
	}
	if len(runs) != 1 || !slices.Equal(runs[0], []string{"/usr/bin/java", r.Cache.Object(want.Sha512), "--install-client", "/launcher"}) {
		t.Fatalf("installer runs %v", runs)
	}
	if changed, err := neoforge.InstallClient(ctx, r, lk, "/launcher", "/usr/bin/java"); err != nil || changed || installer.hits != 1 || len(runs) != 2 {
		t.Fatalf("a locked installer comes from the cache: changed %v, %v, %d downloads, %d runs", changed, err, installer.hits, len(runs))
	}
	if raw, changed, err := neoforge.InstallerVersion(ctx, r, lk); err != nil || changed || !strings.Contains(string(raw), `"id":"neoforge-26.2.0.87"`) {
		t.Fatalf("installer version: %s, changed %v, %v", raw, changed, err)
	}

	lk.Loader.Client, lk.Loader.Server = nil, &lock.ServerJar{URL: want.URL, Sha512: want.Sha512}
	if changed, err := neoforge.InstallClient(ctx, r, lk, "/launcher", "/usr/bin/java"); err != nil || !changed || installer.hits != 1 || !reflect.DeepEqual(lk.Loader.Client, want) {
		t.Fatalf("a server lock of the same jar is reused: changed %v, %v, %d downloads, %+v", changed, err, installer.hits, lk.Loader.Client)
	}
}
