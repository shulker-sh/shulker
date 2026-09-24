package loader

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"maps"
	"reflect"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
)

func TestQuiltVersionsCountPrereleasesUnstable(t *testing.T) {
	r := fakeRemote(t, QuiltMetaURL, map[string]any{
		"/versions/loader/26.2": []map[string]any{
			{"loader": map[string]any{"version": "0.31.0-beta.4"}},
			{"loader": map[string]any{"version": "0.30.1"}},
		},
	})
	got, err := quilt.Versions(context.Background(), r, "26.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Version{"0.31.0-beta.4", false}) || got[1] != (Version{"0.30.1", true}) {
		t.Fatalf("versions %v", got)
	}
}

func TestQuiltProvidesJarIsOnItsMaven(t *testing.T) {
	r := fakeRemote(t, QuiltMetaURL, map[string]any{
		"/versions/loader/26.2/0.30.1": map[string]any{"loader": map[string]any{"maven": "org.quiltmc:quilt-loader:0.30.1"}},
		"/versions/loader/26.2/0.0.0":  map[string]any{"loader": map[string]any{"maven": "nope"}},
	})
	r.URLs[QuiltMavenURL] = "https://maven.example/release"
	url, ok, err := quilt.ProvidesJar(context.Background(), r, "26.2", "0.30.1")
	if err != nil || !ok || url != "https://maven.example/release/org/quiltmc/quilt-loader/0.30.1/quilt-loader-0.30.1.jar" {
		t.Fatalf("url %q, %v, %v", url, ok, err)
	}
	if _, _, err := quilt.ProvidesJar(context.Background(), r, "26.2", "0.0.0"); out.CodeOf(err) != "meta-invalid" {
		t.Fatalf("bad coordinate: %v", err)
	}
}

func TestQuiltServerProfileMustBeComplete(t *testing.T) {
	r := fakeRemote(t, QuiltMetaURL, map[string]any{
		"/versions/loader/26.2/0.30.1/server/json": map[string]any{"mainClass": "a.Main"},
	})
	if _, err := newQuiltMeta(r).serverProfile(context.Background(), "26.2", "0.30.1"); out.CodeOf(err) != "meta-invalid" {
		t.Fatalf("incomplete profile: %v", err)
	}
}

func TestQuiltLaunchJarIsDeterministic(t *testing.T) {
	libs := []string{"org.quiltmc:quilt-loader:0.30.1", "net.fabricmc:sponge-mixin:0.17.3"}
	a, err := quiltLaunchJar("a.Launcher", "a.Main", libs)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := quiltLaunchJar("a.Launcher", "a.Main", []string{libs[1], libs[0]})
	if !bytes.Equal(a, b) {
		t.Fatal("library order changed the jar")
	}
	zr, err := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	if err != nil {
		t.Fatal(err)
	}
	var manifest string
	for _, f := range zr.File {
		if f.Name == "META-INF/MANIFEST.MF" {
			rc, _ := f.Open()
			var buf bytes.Buffer
			buf.ReadFrom(rc)
			manifest = strings.ReplaceAll(buf.String(), "\r\n ", "")
		}
	}
	if !strings.Contains(manifest, "Main-Class: a.Launcher") || !strings.Contains(manifest, "libraries/net/fabricmc/sponge-mixin/0.17.3/sponge-mixin-0.17.3.jar") {
		t.Fatalf("manifest:\n%s", manifest)
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

func TestQuiltServerIsAssembledFromItsProfile(t *testing.T) {
	loaderJar, mixin := &fakeFile{data: []byte("quilt loader")}, &fakeFile{data: []byte("mixin")}
	r := fakeRemote(t, QuiltMetaURL, map[string]any{
		"/versions/loader/26.2/0.30.1/server/json": func(base string) any {
			return map[string]any{
				"id": "quilt-loader-0.30.1-26.2", "mainClass": "org.quiltmc.loader.impl.launch.knot.KnotServer",
				"launcherMainClass": "org.quiltmc.loader.impl.launch.server.QuiltServerLauncher",
				"libraries": []map[string]string{
					{"name": "net.fabricmc:sponge-mixin:0.17.3", "url": base + "/maven/"},
					{"name": "org.quiltmc:quilt-loader:0.30.1", "url": base + "/maven/"},
				},
			}
		},
		"/maven/org/quiltmc/quilt-loader/0.30.1/quilt-loader-0.30.1.jar":  loaderJar,
		"/maven/net/fabricmc/sponge-mixin/0.17.3/sponge-mixin-0.17.3.jar": mixin,
	})
	ctx := context.Background()
	lk := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: "quilt", Version: "0.30.1"}}

	res, err := quilt.EnsureServer(ctx, r, lk)
	if err != nil || !res.ChangedLock || !res.WasFetched {
		t.Fatalf("first ensure: %+v, %v", res, err)
	}
	base := r.URLs[QuiltMetaURL]
	wantLibs := map[string]lock.Download{
		"org.quiltmc:quilt-loader:0.30.1":  {URL: base + "/maven/org/quiltmc/quilt-loader/0.30.1/quilt-loader-0.30.1.jar", Sha512: loaderJar.sha512()},
		"net.fabricmc:sponge-mixin:0.17.3": {URL: base + "/maven/net/fabricmc/sponge-mixin/0.17.3/sponge-mixin-0.17.3.jar", Sha512: mixin.sha512()},
	}
	s := lk.Loader.Server
	if s == nil || s.Installer != "" || s.URL != "" || !maps.Equal(s.Libraries, wantLibs) {
		t.Fatalf("lock loader server: %+v", s)
	}
	for _, dl := range s.Libraries {
		if !r.Cache.Has(dl.Sha512) {
			t.Fatalf("%s is not in the cache", dl.URL)
		}
	}
	entries := zipEntries(t, r.Cache.Object(s.Sha512))
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

	if res, err := quilt.EnsureServer(ctx, r, lk); err != nil || res != (ServerResult{}) || loaderJar.hits+mixin.hits != 2 {
		t.Fatalf("a locked, cached server ensures nothing: %+v, %v, %d downloads", res, err, loaderJar.hits+mixin.hits)
	}

	before := *s
	before.Libraries = maps.Clone(s.Libraries)
	r.Cache = &cache.Cache{Dir: t.TempDir()}
	res, err = quilt.EnsureServer(ctx, r, lk)
	if err != nil || res.ChangedLock || !res.WasFetched || !reflect.DeepEqual(lk.Loader.Server, &before) {
		t.Fatalf("a fresh cache changed the lock: %+v, %v\n%+v", res, err, lk.Loader.Server)
	}
	if !r.Cache.Has(before.Sha512) {
		t.Fatal("a fresh cache did not regenerate the launch jar")
	}
}
