package resolve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
