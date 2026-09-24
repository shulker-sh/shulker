package pack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
)

func TestFetchArchiveLeavesNonArchivesToTheSourceReaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readme", "/pack.mrpack":
			w.Write([]byte("not a zip"))
		case "/broken":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	var log []string
	s := &Store{Cache: &cache.Cache{Dir: t.TempDir()}, Fetch: fetch.New("test"), Log: func(format string, args ...any) { log = append(log, format) }}
	cases := []struct {
		path, code string
	}{
		{"/missing", ""},
		{"/readme", ""},
		{"/missing.mrpack", "modpack-fetch"},
		{"/pack.mrpack", "archive-not-modpack"},
		{"/broken", "modpack-fetch"},
	}
	for _, c := range cases {
		path, err := s.FetchArchive(context.Background(), srv.URL+c.path)
		if path != "" || out.CodeOf(err) != c.code {
			t.Errorf("%s: path %q, err %v, want code %q", c.path, path, err, c.code)
		}
	}
	if len(log) != len(cases) || log[0] != "fetching %s" {
		t.Fatalf("logged %v", log)
	}
}
