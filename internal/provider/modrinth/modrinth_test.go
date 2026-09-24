package modrinth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
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
	want := provider.Project{ID: "AANobbMI", Slug: "sodium", Title: "Sodium", Side: "client", Type: "mod", Downloads: 228124617, Page: "https://modrinth.com/mod/sodium"}
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

func TestWaitsOutARateLimitOnce(t *testing.T) {
	for _, tc := range []struct {
		name, reset string
		limited     int
		wantWait    time.Duration
		wantErr     bool
	}{
		{"retries after the reset", "2", 1, 2 * time.Second, false},
		{"fails when limited again", "2", 2, 2 * time.Second, true},
		{"fails when the reset is too far", "120", 1, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls <= tc.limited {
					w.Header().Set("X-Ratelimit-Reset", tc.reset)
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				w.Write([]byte(`{"id":"AANobbMI","slug":"sodium","project_type":"mod"}`))
			}))
			defer srv.Close()
			m := New(fetch.New("test"))
			m.BaseURL = srv.URL
			var waited time.Duration
			var logged string
			m.sleep = func(_ context.Context, d time.Duration) error { waited += d; return nil }
			m.Log = func(format string, args ...any) { logged = fmt.Sprintf(format, args...) }
			_, err := m.Project(context.Background(), "sodium", "")
			if (err != nil) != tc.wantErr || waited != tc.wantWait {
				t.Fatalf("err %v, waited %s", err, waited)
			}
			if tc.wantErr && out.CodeOf(err) != "rate-limited" {
				t.Fatalf("code: %v", err)
			}
			if tc.wantWait > 0 && logged != "waiting 2s for Modrinth's rate limit" {
				t.Fatalf("logged %q", logged)
			}
		})
	}
}

func TestIdentifyAsksOnceForVersionsAndOnceForProjects(t *testing.T) {
	var paths []string
	var body struct {
		Hashes    []string `json:"hashes"`
		Algorithm string   `json:"algorithm"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/version_files":
			json.NewDecoder(r.Body).Decode(&body)
			w.Write([]byte(`{"86f7e437faa5a7fce15d1ddcb9eaeaea377667b8":{"id":"v1","project_id":"p1","version_number":"1.0","files":[{"url":"u","filename":"a.jar","primary":true,"hashes":{"sha1":"86f7e437faa5a7fce15d1ddcb9eaeaea377667b8","sha512":"ccc"}}]}}`))
		case "/projects":
			w.Write([]byte(`[{"id":"p1","slug":"one","project_type":"mod"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	m := New(fetch.New("test"))
	m.BaseURL = srv.URL
	found, err := m.Identify(context.Background(), map[string][]byte{"mods/a.jar": []byte("a"), "mods/b.jar": []byte("b")})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "POST /version_files" || paths[1] != "GET /projects" {
		t.Fatalf("requests: %v", paths)
	}
	if body.Algorithm != "sha1" || len(body.Hashes) != 2 {
		t.Errorf("body %+v", body)
	}
	if len(found) != 1 || found["mods/a.jar"].Project.Slug != "one" || found["mods/a.jar"].Version.ID != "v1" {
		t.Errorf("found %+v", found)
	}
}

func TestProjectsAsksOnce(t *testing.T) {
	calls := 0
	var ids string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		ids = r.URL.Query().Get("ids")
		w.Write([]byte(`[{"id":"p1","slug":"one","project_type":"mod"}]`))
	}))
	defer srv.Close()
	m := New(fetch.New("test"))
	m.BaseURL = srv.URL
	found, err := m.Projects(context.Background(), []string{"p1", "p2"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || ids != `["p1","p2"]` || len(found) != 1 || found["p1"].Slug != "one" {
		t.Fatalf("calls %d, ids %s, found %+v", calls, ids, found)
	}
}

func TestProjectTellsDatapacksApart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/project/terralith":
			w.Write([]byte(`{"id":"8oi3bsk5","slug":"terralith","project_type":"mod","loaders":["datapack","fabric","neoforge"]}`))
		case "/project/witch-huts-compat":
			w.Write([]byte(`{"id":"p2","slug":"witch-huts-compat","project_type":"mod","loaders":["datapack"]}`))
		}
	}))
	defer srv.Close()
	m := New(fetch.New("test"))
	m.BaseURL = srv.URL
	both, err := m.Project(context.Background(), "terralith", "")
	if err != nil {
		t.Fatal(err)
	}
	if both.Type != "mod" || !both.Datapack {
		t.Errorf("a mod that also ships a datapack: %+v", both)
	}
	only, err := m.Project(context.Background(), "witch-huts-compat", "")
	if err != nil {
		t.Fatal(err)
	}
	if only.Type != "datapack" || !only.Datapack {
		t.Errorf("a datapack-only project: %+v", only)
	}
}
