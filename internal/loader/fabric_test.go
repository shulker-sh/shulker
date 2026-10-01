package loader

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
)

// fakeFile is a download a fake route serves, counting the times it was fetched.
type fakeFile struct {
	data []byte
	hits int
}

func (f *fakeFile) sha512() string {
	sum := sha512.Sum512(f.data)
	return hex.EncodeToString(sum[:])
}

// fakeRemote serves routes at base: a string as is, a *fakeFile counted, a func given the server's
// URL for a body that names it, and anything else as JSON. The remote gets a cache of its own.
func fakeRemote(t *testing.T, base string, routes map[string]any) *Remote {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if lazy, ok := body.(func(base string) any); ok {
			body = lazy(srv.URL)
		}
		switch body := body.(type) {
		case string:
			w.Write([]byte(body))
		case *fakeFile:
			body.hits++
			w.Write(body.data)
		default:
			json.NewEncoder(w).Encode(body)
		}
	}))
	t.Cleanup(srv.Close)
	return &Remote{Fetch: fetch.New("test"), Cache: &cache.Cache{Dir: t.TempDir()}, URLs: map[string]string{base: srv.URL}}
}

func TestFabricVersionsKeepTheStableFlag(t *testing.T) {
	r := fakeRemote(t, FabricMetaURL, map[string]any{
		"/versions/loader/26.2": []map[string]any{
			{"loader": map[string]any{"version": "0.17.3", "stable": true}},
			{"loader": map[string]any{"version": "0.18.0-beta.1", "stable": false}},
		},
	})
	got, err := fabric.Versions(context.Background(), r, "26.2")
	if err != nil {
		t.Fatal(err)
	}
	want := []Version{{"0.17.3", true}, {"0.18.0-beta.1", false}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("versions %v, want %v", got, want)
	}
}

func TestFabricInstallerPrefersStable(t *testing.T) {
	r := fakeRemote(t, FabricMetaURL, map[string]any{
		"/versions/installer": []map[string]any{{"version": "1.2.0-rc1", "stable": false}, {"version": "1.1.2", "stable": true}},
	})
	if got, err := newFabricMeta(r).installerVersion(context.Background()); err != nil || got != "1.1.2" {
		t.Fatalf("installer %q, %v", got, err)
	}
	r = fakeRemote(t, FabricMetaURL, map[string]any{
		"/versions/installer": []map[string]any{{"version": "1.2.0-rc1", "stable": false}},
	})
	if got, err := newFabricMeta(r).installerVersion(context.Background()); err != nil || got != "1.2.0-rc1" {
		t.Fatalf("installer %q, %v", got, err)
	}
	r = fakeRemote(t, FabricMetaURL, map[string]any{"/versions/installer": []map[string]any{}})
	if _, err := newFabricMeta(r).installerVersion(context.Background()); out.CodeOf(err) != "meta-invalid" {
		t.Fatalf("no installers: %v", err)
	}
}

func TestFetchFailureKeepsNetwork(t *testing.T) {
	c := fetch.New("test")
	c.Offline = true
	_, err := fabric.Versions(context.Background(), &Remote{Fetch: c}, "1.21.1")
	if out.CodeOf(err) != "meta-fetch" || !fetch.IsNetwork(err) {
		t.Fatalf("offline: code %q, network %v", out.CodeOf(err), fetch.IsNetwork(err))
	}
	if rows := out.AsError(err).Rows; len(rows) != 1 || rows[0].Label != "fabric" {
		t.Errorf("rows %+v", rows)
	}
}

func TestVanillaHasNoVersions(t *testing.T) {
	if _, err := (Loader{}).Versions(context.Background(), &Remote{}, "26.2"); out.CodeOf(err) != "unsupported-loader" {
		t.Fatalf("got %v", err)
	}
	if _, err := neoforge.Profile(context.Background(), &Remote{}, "26.2", "26.2.0.87"); out.CodeOf(err) != "unsupported-loader" {
		t.Fatalf("got %v", err)
	}
}

func TestFabricServerLauncherIsLockedOnce(t *testing.T) {
	launcher := &fakeFile{data: []byte("fabric server launcher")}
	r := fakeRemote(t, FabricMetaURL, map[string]any{
		"/versions/installer":                           []map[string]any{{"version": "1.2.0-beta.1", "stable": false}, {"version": "1.1.2", "stable": true}},
		"/versions/loader/26.2/0.17.3/1.1.2/server/jar": launcher,
	})
	ctx := context.Background()
	lk := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: "fabric", Version: "0.17.3"}}

	res, err := fabric.EnsureServer(ctx, r, lk)
	if err != nil || !res.ChangedLock || !res.WasFetched {
		t.Fatalf("first ensure: %+v, %v", res, err)
	}
	want := &lock.ServerJar{Installer: "1.1.2", URL: r.URLs[FabricMetaURL] + "/versions/loader/26.2/0.17.3/1.1.2/server/jar", Sha512: launcher.sha512()}
	if !reflect.DeepEqual(lk.Loader.Server, want) || !r.Cache.Has(want.Sha512) {
		t.Fatalf("lock loader server: %+v", lk.Loader.Server)
	}
	if res, err := fabric.EnsureServer(ctx, r, lk); err != nil || res != (ServerResult{}) || launcher.hits != 1 {
		t.Fatalf("a locked, cached launcher ensures nothing: %+v, %v, %d downloads", res, err, launcher.hits)
	}

	lk.Loader.Server.URL = ""
	if res, err := fabric.EnsureServer(ctx, r, lk); err != nil || !res.ChangedLock || res.WasFetched || !reflect.DeepEqual(lk.Loader.Server, want) {
		t.Fatalf("a blank url is written back from the locked installer: %+v, %v, %+v", res, err, lk.Loader.Server)
	}

	r.Cache = &cache.Cache{Dir: t.TempDir()}
	if res, err := fabric.EnsureServer(ctx, r, lk); err != nil || res.ChangedLock || !res.WasFetched || launcher.hits != 2 || !reflect.DeepEqual(lk.Loader.Server, want) {
		t.Fatalf("a fresh cache downloads the locked launcher: %+v, %v, %d downloads", res, err, launcher.hits)
	}
}

func TestFabricProvidesJarIsOnItsMaven(t *testing.T) {
	r := fakeRemote(t, FabricMetaURL, map[string]any{
		"/versions/loader/26.3/0.19.5": map[string]any{"loader": map[string]any{"maven": "net.fabricmc:fabric-loader:0.19.5"}},
		"/versions/loader/26.3/0.0.0":  map[string]any{"loader": map[string]any{"maven": "nope"}},
	})
	r.URLs[FabricMavenURL] = "https://maven.example"
	url, ok, err := fabric.ProvidesJar(context.Background(), r, "26.3", "0.19.5")
	if err != nil || !ok || url != "https://maven.example/net/fabricmc/fabric-loader/0.19.5/fabric-loader-0.19.5.jar" {
		t.Fatalf("url %q, %v, %v", url, ok, err)
	}
	if _, _, err := fabric.ProvidesJar(context.Background(), r, "26.3", "0.0.0"); out.CodeOf(err) != "meta-invalid" {
		t.Fatalf("bad coordinate: %v", err)
	}
}

func TestFabricServerLauncherFromAnotherAddressIsRefused(t *testing.T) {
	r := fakeRemote(t, FabricMetaURL, map[string]any{})
	sha, err := r.Cache.Put(strings.NewReader("not the launcher"))
	if err != nil {
		t.Fatal(err)
	}
	lk := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: "fabric", Version: "0.17.3", Server: &lock.ServerJar{Installer: "1.1.2", URL: "https://example.com/server.jar", Sha512: sha}}}

	if _, err := fabric.EnsureServer(context.Background(), r, lk); out.CodeOf(err) != "provenance-mismatch" {
		t.Fatalf("want provenance-mismatch, got %v", err)
	}
}
