package resolve

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/meta"
)

func TestQuiltLoaderVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/versions/loader/26.2" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"loader": map[string]any{"version": "0.20.0-beta.9"}},
			{"loader": map[string]any{"version": "0.30.1"}},
			{"loader": map[string]any{"version": "0.31.0-beta.4"}},
			{"loader": map[string]any{"version": "0.30.0"}},
		})
	}))
	defer srv.Close()
	q := meta.NewQuilt(fetch.New("test"))
	q.BaseURL = srv.URL
	mt := &Meta{Quilt: q}
	cases := map[string]string{
		"*":              "0.30.1",
		"^0.30":          "0.30.1",
		"0.30.0":         "0.30.0",
		">=0.31.0-beta":  "0.31.0-beta.4",
		"<0.30.1 || >=1": "0.30.0",
	}
	for rng, want := range cases {
		got, err := mt.loaderVersion(context.Background(), manifest.Loader{Type: "quilt", Version: rng}, "26.2")
		if err != nil {
			t.Errorf("%s: %v", rng, err)
			continue
		}
		if got != want {
			t.Errorf("%s resolved %s, want %s", rng, got, want)
		}
	}
	if _, err := mt.loaderVersion(context.Background(), manifest.Loader{Type: "quilt", Version: "^0.31"}, "26.2"); err == nil {
		t.Error("^0.31 should not match a beta")
	}
}

func TestNeoForgeLoaderVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/maven/versions/releases/net/neoforged/neoforge" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"isSnapshot": false, "versions": []string{
			"21.1.200", "26.1.0.0-alpha.11+snapshot-7", "26.1.2.40", "26.2.0.0-beta", "26.2.0.56-beta", "26.2.0.57", "26.2.0.87",
		}})
	}))
	defer srv.Close()
	n := meta.NewNeoForge(fetch.New("test"))
	n.BaseURL = srv.URL
	mt := &Meta{NeoForge: n}
	cases := map[string]string{
		"*":                  "26.2.0.87",
		"^26.2.0":            "26.2.0.87",
		"<26.2.0.60":         "26.2.0.57",
		">=26.2.0.0-beta":    "26.2.0.87",
		"26.2.0.56-beta":     "26.2.0.56-beta",
		"~26.2.0 <26.2.0.57": "",
	}
	for rng, want := range cases {
		got, err := mt.loaderVersion(context.Background(), manifest.Loader{Type: "neoforge", Version: rng}, "26.2")
		if want == "" {
			if err == nil {
				t.Errorf("%s resolved %s, want no match", rng, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", rng, err)
			continue
		}
		if got != want {
			t.Errorf("%s resolved %s, want %s", rng, got, want)
		}
	}
	if got, err := mt.loaderVersion(context.Background(), manifest.Loader{Type: "neoforge", Version: "*"}, "1.21.1"); err != nil || got != "21.1.200" {
		t.Errorf("1.21.1 resolved %s, %v", got, err)
	}
}

func zipBytes(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestQuiltLoaderProvides(t *testing.T) {
	jar := zipBytes(t, "quilt.mod.json", `{"schema_version":1,"quilt_loader":{"id":"quilt_loader","version":"0.31.0-beta.4","provides":[{"id":"fabricloader","version":"0.19.5"}]}}`)
	sum := sha512.Sum512(jar)
	sha := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/versions/loader/26.2/0.31.0-beta.4":
			json.NewEncoder(w).Encode(map[string]any{"loader": map[string]any{
				"maven":  "org.quiltmc:quilt-loader:0.31.0-beta.4",
				"hashes": map[string]string{"sha512": strings.Repeat("0", 128)},
			}})
		case "/maven/org/quiltmc/quilt-loader/0.31.0-beta.4/quilt-loader-0.31.0-beta.4.jar":
			w.Write(jar)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	q := meta.NewQuilt(fetch.New("test"))
	q.BaseURL = srv.URL
	q.MavenURL = srv.URL + "/maven"
	c := &cache.Cache{Dir: t.TempDir()}
	mt := &Meta{Quilt: q, Cache: c}
	provides, err := mt.loaderProvides(context.Background(), "quilt", "26.2", "0.31.0-beta.4")
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"fabricloader": "0.19.5"}; !maps.Equal(provides, want) {
		t.Fatalf("provides %v, want %v", provides, want)
	}
	if !c.Has(sha) {
		t.Fatal("the loader jar should be in the content cache")
	}
	if provides, err := mt.loaderProvides(context.Background(), "fabric", "26.2", "0.17.3"); err != nil || provides != nil {
		t.Fatalf("fabric provides %v, %v", provides, err)
	}
}

func TestDataVersionIsNotLookedForBefore114(t *testing.T) {
	mt := &Meta{}
	for _, game := range []string{"1.7.10", "1.13.2", "1.13-pre1"} {
		if got, warning := mt.DataVersion(context.Background(), game); got != 0 || warning != "" {
			t.Errorf("%s: got %d, %q", game, got, warning)
		}
	}
}
