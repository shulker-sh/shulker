package resolve

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
)

func versions(ids ...string) []loader.Version {
	var out []loader.Version
	for _, id := range ids {
		out = append(out, loader.Version{Version: id, Stable: !bytes.Contains([]byte(id), []byte("-"))})
	}
	return out
}

func TestLoaderVersionRanges(t *testing.T) {
	quilt := loader.Fake{Name: "quilt", Versions: versions("0.20.0-beta.9", "0.30.1", "0.31.0-beta.4", "0.30.0")}.Row()
	neoforge := loader.Fake{Name: "neoforge", Versions: versions("26.2.0.0-beta", "26.2.0.56-beta", "26.2.0.57", "26.2.0.87")}.Row()
	// Forge's maven lists branch builds like 26.2-65.1.4-1.26.x as stable; they are skipped as unparseable.
	forge := loader.Fake{Name: "forge", Versions: append(versions("65.0.9", "65.1.3"), loader.Version{Version: "65.1.4-1.26.x", Stable: true})}.Row()
	cases := []struct {
		row       loader.Loader
		rng, want string
	}{
		{quilt, "*", "0.30.1"},
		{quilt, "^0.30", "0.30.1"},
		{quilt, "0.30.0", "0.30.0"},
		{quilt, ">=0.31.0-beta", "0.31.0-beta.4"},
		{quilt, "<0.30.1 || >=1", "0.30.0"},
		{quilt, "^0.31", ""},
		{neoforge, "*", "26.2.0.87"},
		{neoforge, "^26.2.0", "26.2.0.87"},
		{neoforge, "<26.2.0.60", "26.2.0.57"},
		{neoforge, ">=26.2.0.0-beta", "26.2.0.87"},
		{neoforge, "26.2.0.56-beta", "26.2.0.56-beta"},
		{neoforge, "~26.2.0 <26.2.0.57", ""},
		{forge, "*", "65.1.3"},
	}
	mt := &Meta{}
	for _, c := range cases {
		got, err := mt.loaderVersion(context.Background(), c.row, c.rng, "26.2")
		switch {
		case c.want == "" && err == nil:
			t.Errorf("%s %s resolved %s, want no match", c.row.Name, c.rng, got)
		case c.want != "" && err != nil:
			t.Errorf("%s %s: %v", c.row.Name, c.rng, err)
		case got != c.want:
			t.Errorf("%s %s resolved %s, want %s", c.row.Name, c.rng, got, c.want)
		}
	}
}

func TestNoLoaderReleaseSaysSo(t *testing.T) {
	neoforge := loader.Fake{Name: "neoforge", Versions: versions("26.3.0.1-beta")}.Row()
	_, err := (&Meta{}).loaderVersion(context.Background(), neoforge, "*", "26.3")
	if err == nil || err.Error() != "NeoForge has no release for Minecraft 26.3 yet" {
		t.Errorf("err = %v", err)
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

func TestLoaderProvidesComesFromTheJar(t *testing.T) {
	jar := zipBytes(t, "quilt.mod.json", `{"schema_version":1,"quilt_loader":{"id":"quilt_loader","version":"0.31.0-beta.4","provides":[{"id":"fabricloader","version":"0.19.5"}]}}`)
	sum := sha512.Sum512(jar)
	sha := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quilt-loader-0.31.0-beta.4.jar" {
			http.NotFound(w, r)
			return
		}
		w.Write(jar)
	}))
	defer srv.Close()
	c := &cache.Cache{Dir: t.TempDir()}
	mt := &Meta{Loaders: &loader.Remote{Fetch: fetch.New("test"), Cache: c}}
	quilt := loader.Fake{Name: "quilt", ProvidesJar: srv.URL + "/quilt-loader-0.31.0-beta.4.jar"}.Row()
	provides, err := mt.loaderProvides(context.Background(), quilt, "26.2", "0.31.0-beta.4")
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"fabricloader": "0.19.5"}; !maps.Equal(provides, want) {
		t.Fatalf("provides %v, want %v", provides, want)
	}
	if !c.Has(sha) {
		t.Fatal("the loader jar should be in the content cache")
	}
	fabric := loader.Fake{Name: "fabric"}.Row()
	if provides, err := mt.loaderProvides(context.Background(), fabric, "26.2", "0.17.3"); err != nil || provides != nil {
		t.Fatalf("fabric provides %v, %v", provides, err)
	}
}

func TestDataVersionIsNotLookedForBefore114(t *testing.T) {
	mt := &Meta{}
	for _, game := range []string{"1.7.10", "1.13.2", "1.13-pre1"} {
		l := &lock.Lock{Minecraft: game}
		if warning := mt.FillDataVersion(context.Background(), l); l.DataVersion != 0 || warning != "" {
			t.Errorf("%s: got %d, %q", game, l.DataVersion, warning)
		}
	}
}
