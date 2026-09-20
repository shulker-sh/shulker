package modrinth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/provider"
)

func TestVersionsAsksForEveryLoader(t *testing.T) {
	var loaders string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loaders = r.URL.Query().Get("loaders")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	m := New(fetch.New("test"))
	m.BaseURL = srv.URL
	if _, err := m.Versions(context.Background(), "AANobbMI", "26.2", []string{"quilt", "fabric"}); err != nil {
		t.Fatal(err)
	}
	if loaders != `["quilt","fabric"]` {
		t.Errorf("loaders = %s", loaders)
	}
}

func TestSearchAsksForTheTypeAsAFacet(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`{"hits":[{"project_id":"AANobbMI","slug":"sodium","title":"Sodium","project_type":"mod","downloads":228124617,"client_side":"required","server_side":"unsupported"}]}`))
	}))
	defer srv.Close()
	m := New(fetch.New("test"))
	m.BaseURL = srv.URL
	projects, err := m.Search(context.Background(), "sodium", "mod", 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("query") != "sodium" || got.Get("limit") != "5" || got.Get("facets") != `[["project_type:mod"]]` {
		t.Errorf("search asked for %v", got)
	}
	want := provider.Project{ID: "AANobbMI", Slug: "sodium", Title: "Sodium", Side: "client", Type: "mod", Downloads: 228124617}
	if len(projects) != 1 || projects[0] != want {
		t.Errorf("projects %+v, want %+v", projects, want)
	}
	if _, err := m.Search(context.Background(), "sodium", "", 200); err != nil {
		t.Fatal(err)
	}
	if got.Has("facets") || got.Get("limit") != "100" {
		t.Errorf("search without a type asked for %v", got)
	}
}
