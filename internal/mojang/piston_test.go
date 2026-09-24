package mojang

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"shulker.sh/shulker/internal/fetch"
)

func serverJar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fakePiston(t *testing.T, jar []byte) *Piston {
	t.Helper()
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"versions": []map[string]string{{"id": "26.2", "url": base + "/26.2.json"}}})
	})
	mux.HandleFunc("/26.2.json", func(w http.ResponseWriter, r *http.Request) {
		downloads := map[string]any{}
		if jar != nil {
			downloads["server"] = map[string]string{"url": base + "/server.jar", "sha1": "x"}
		}
		json.NewEncoder(w).Encode(map[string]any{"downloads": downloads})
	})
	mux.HandleFunc("/server.jar", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "server.jar", time.Time{}, bytes.NewReader(jar))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	p := NewPiston(fetch.New("test"))
	p.ManifestURL = base + "/manifest.json"
	return p
}

func TestDataVersionIsTheServerJarsWorldVersion(t *testing.T) {
	p := fakePiston(t, serverJar(t, map[string]string{"version.json": `{"id":"26.2","world_version":4903}`, "META-INF/MANIFEST.MF": ""}))
	got, err := p.DataVersion(context.Background(), "26.2")
	if err != nil || got != 4903 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestDataVersionIsUnknownWithoutOne(t *testing.T) {
	cases := map[string][]byte{
		"no server download": nil,
		"no version.json":    serverJar(t, map[string]string{"META-INF/MANIFEST.MF": ""}),
		"no world_version":   serverJar(t, map[string]string{"version.json": `{"id":"26.2"}`}),
	}
	for name, jar := range cases {
		got, err := fakePiston(t, jar).DataVersion(context.Background(), "26.2")
		if err != nil || got != 0 {
			t.Errorf("%s: got %d, %v", name, got, err)
		}
	}
}
