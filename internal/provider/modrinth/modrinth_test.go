package modrinth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

func TestVersionsAsksForEveryLoader(t *testing.T) {
	var loaders string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`{"hits":[{"project_id":"AANobbMI","slug":"sodium","title":"Sodium","description":"The fastest rendering mod","project_type":"mod","downloads":228124617,"client_side":"required","server_side":"unsupported"}]}`))
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
	want := provider.Project{ID: "AANobbMI", Slug: "sodium", Title: "Sodium", Summary: "The fastest rendering mod", Side: "client", Type: "mod", Downloads: 228124617, Page: "https://modrinth.com/mod/sodium"}
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
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestURLsRoundTrip(t *testing.T) {
	m := New(fetch.New("test"))
	for arg, want := range map[string]provider.Ref{
		"https://modrinth.com/mod/sodium":                                            {Project: "sodium"},
		"https://www.modrinth.com/shader/complementary-reimagined/":                  {Project: "complementary-reimagined"},
		"https://modrinth.com/project/AANobbMI?tab=versions#top":                     {Project: "AANobbMI"},
		"https://modrinth.com/mod/sodium/version/mc1.21.1-0.6.0-fabric":              {Project: "sodium", Version: "mc1.21.1-0.6.0-fabric"},
		"https://cdn.modrinth.com/data/AANobbMI/versions/b70slbHV/sodium-fabric.jar": {Project: "AANobbMI", Version: "b70slbHV"},
	} {
		u, _ := url.Parse(arg)
		if got, err := m.ParseURL(u); err != nil || got != want {
			t.Errorf("%s: got %+v err=%v", arg, got, err)
		}
	}
	for _, arg := range []string{"https://modrinth.com/user/jellysquid3", "https://modrinth.com/mod/sodium/versions", "https://cdn.modrinth.com/data/AANobbMI/icon.png"} {
		u, _ := url.Parse(arg)
		if _, err := m.ParseURL(u); out.CodeOf(err) != "usage" {
			t.Errorf("%s: err=%v, want usage", arg, err)
		}
	}
	u, _ := url.Parse("https://www.curseforge.com/minecraft/mc-mods/jei")
	if _, err := m.ParseURL(u); err != provider.ErrNotHosted {
		t.Errorf("another host: %v", err)
	}
	if got := m.ProjectPage("shader", "complementary-reimagined"); got != "https://modrinth.com/shader/complementary-reimagined" {
		t.Errorf("project page %s", got)
	}
	if got := m.ProjectPage("", "AANobbMI"); got != "https://modrinth.com/project/AANobbMI" {
		t.Errorf("project page by id %s", got)
	}
	if got := m.VersionsPage("mod", "sodium"); got != "https://modrinth.com/mod/sodium/versions" {
		t.Errorf("versions page %s", got)
	}
}

func TestConvertNamesShaderModsByIntegration(t *testing.T) {
	cases := []struct {
		loaders []string
		want    []string
	}{
		{loaders: []string{"optifine", "iris"}, want: []string{"iris", "oculus"}},
		{loaders: []string{"iris"}, want: []string{"iris"}},
		{loaders: []string{"vanilla", "canvas"}, want: []string{"canvas", "vanilla"}},
		{loaders: []string{"vanilla"}, want: []string{"vanilla"}},
		{loaders: []string{"quilt", "fabric"}, want: []string{"quilt", "fabric"}},
	}
	for _, c := range cases {
		loaders, _ := json.Marshal(c.loaders)
		var v version
		raw := `{"id":"v1","loaders":` + string(loaders) + `,"files":[{"url":"https://x/a.zip","filename":"a.zip","primary":true,"hashes":{"sha512":"abc"}}]}`
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatal(err)
		}
		got, err := convert(v)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Loaders, c.want) {
			t.Errorf("loaders %v became %v, want %v", c.loaders, got.Loaders, c.want)
		}
	}
}

func TestFiledAsksVersionFilesOnceBySha512(t *testing.T) {
	var paths []string
	var body struct {
		Hashes    []string `json:"hashes"`
		Algorithm string   `json:"algorithm"`
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"aaa":{"id":"v1","project_id":"p1","files":[]}}`))
	}))
	defer srv.Close()
	m := New(fetch.New("test"))
	m.BaseURL = srv.URL
	found, unchecked, err := m.Filed(context.Background(), map[string]provider.LockedFile{"sodium": {Sha512: "aaa"}, "gone": {Sha512: "bbb"}, "pending": {}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(paths, []string{"POST /version_files"}) || body.Algorithm != "sha512" || !slices.Equal(body.Hashes, []string{"aaa", "bbb"}) {
		t.Fatalf("requests %v, body %+v", paths, body)
	}
	if len(found) != 1 || found["sodium"] != (provider.Filing{Project: "p1", Version: "v1"}) || !slices.Equal(unchecked, []string{"pending"}) {
		t.Fatalf("found %+v, unchecked %v", found, unchecked)
	}
}
