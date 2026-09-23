package curseforge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

type keyServer struct {
	accepted   string
	served     string
	keyFetches int
	apiCalls   int
}

func (k *keyServer) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key" {
			k.keyFetches++
			if !strings.HasPrefix(r.UserAgent(), "shulker/") || r.Header.Get("X-Shulker-Client") != "cli" || r.Header.Get("X-Api-Key") != "" {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"key": k.served})
			return
		}
		k.apiCalls++
		if r.Header.Get("X-Api-Key") != k.accepted {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 10, "name": "Shiny", "slug": "shiny"}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRejectedSharedKeyIsReplacedFromShulker(t *testing.T) {
	k := &keyServer{accepted: "new", served: "new"}
	srv := k.start(t)
	dir := t.TempDir()
	c := NewShared(fetch.New("test"), "old", dir)
	c.BaseURL, c.KeyURL = srv.URL, srv.URL+"/key"
	if _, err := c.Project(context.Background(), "10", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Project(context.Background(), "10", ""); err != nil {
		t.Fatal(err)
	}
	if k.keyFetches != 1 || k.apiCalls != 3 {
		t.Errorf("key fetches %d, API calls %d; want 1 and 3", k.keyFetches, k.apiCalls)
	}
	if got := SharedKey(dir); got != "new" {
		t.Errorf("saved key %q, want new", got)
	}
}

func TestSharedKeyFailsWhenShulkerHasNoNewerKey(t *testing.T) {
	k := &keyServer{accepted: "other", served: "old"}
	srv := k.start(t)
	c := NewShared(fetch.New("test"), "old", t.TempDir())
	c.BaseURL, c.KeyURL = srv.URL, srv.URL+"/key"
	_, err := c.Project(context.Background(), "10", "")
	e := out.AsError(err)
	if e.Code != "curseforge-key-rejected" || !strings.Contains(e.Message, "shulker.sh has no newer one") || !strings.Contains(e.Help, "report it") {
		t.Fatalf("error %v", err)
	}
	if _, err := c.Project(context.Background(), "10", ""); err == nil || k.keyFetches != 1 {
		t.Errorf("second call: error %v, key fetches %d; want an error and 1 fetch", err, k.keyFetches)
	}
}

func TestOwnKeyIsNeverReplaced(t *testing.T) {
	k := &keyServer{accepted: "new", served: "new"}
	srv := k.start(t)
	c := New(fetch.New("test"), "mine")
	c.BaseURL, c.KeyURL = srv.URL, srv.URL+"/key"
	_, err := c.Project(context.Background(), "10", "")
	if out.CodeOf(err) != "curseforge-key-rejected" || !strings.Contains(err.Error(), "the API key was rejected") {
		t.Fatalf("error %v", err)
	}
	if k.keyFetches != 0 {
		t.Errorf("fetched a key %d times for the user's own key", k.keyFetches)
	}
}

func TestVersionsQueriesEachLoaderType(t *testing.T) {
	fileJSON := func(id int, loaders ...string) map[string]any {
		return map[string]any{
			"id": id, "modId": 10, "displayName": "Shiny 1.2.0", "fileName": "shiny.jar", "releaseType": 1,
			"fileDate": "2026-09-01T00:00:00Z", "downloadUrl": "https://example.test/shiny.jar", "isAvailable": true,
			"gameVersions": append([]string{"26.2"}, loaders...),
			"hashes":       []map[string]any{{"value": "abc", "algo": 1}},
		}
	}
	byType := map[string][]map[string]any{
		"5": {fileJSON(1, "Quilt"), fileJSON(2, "Fabric", "Quilt")},
		"4": {fileJSON(2, "Fabric", "Quilt"), fileJSON(3, "Fabric")},
	}
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mods/10" {
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 10, "name": "Shiny", "slug": "shiny"}})
			return
		}
		typ := r.URL.Query().Get("modLoaderType")
		asked = append(asked, typ)
		data := byType[typ]
		json.NewEncoder(w).Encode(map[string]any{"data": data, "pagination": map[string]int{"totalCount": len(data)}})
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL
	versions, err := c.Versions(context.Background(), "10", "26.2", []string{"quilt", "fabric"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asked, []string{"5", "4"}) {
		t.Errorf("asked for loader types %v", asked)
	}
	var ids []string
	for _, v := range versions {
		ids = append(ids, v.ID)
	}
	if !slices.Equal(ids, []string{"1", "2", "3"}) {
		t.Errorf("versions %v, want 1 2 3", ids)
	}
	if !slices.Equal(versions[1].Loaders, []string{"fabric", "quilt"}) {
		t.Errorf("loaders of the file tagged both = %v", versions[1].Loaders)
	}
}

func TestSearchSortsByPopularityAndKeepsClassesShulkerCanAdd(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"id": 394468, "name": "Sodium", "slug": "sodium", "classId": 6, "downloadCount": 151434981},
			{"id": 900000, "name": "Sodium World", "slug": "sodium-world", "classId": 17, "downloadCount": 12},
		}})
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL
	projects, err := c.Search(context.Background(), "sodium", "", 80)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("gameId") != "432" || got.Get("searchFilter") != "sodium" || got.Get("sortField") != "2" || got.Get("sortOrder") != "desc" || got.Get("pageSize") != "50" || got.Has("classId") {
		t.Errorf("search asked for %v", got)
	}
	want := provider.Project{ID: "394468", Slug: "sodium", Title: "Sodium", Type: "mod", Downloads: 151434981}
	if len(projects) != 1 || projects[0] != want {
		t.Errorf("projects %+v, want %+v", projects, want)
	}
	if _, err := c.Search(context.Background(), "sodium", "shader", 5); err != nil {
		t.Fatal(err)
	}
	if got.Get("classId") != classShader || got.Get("pageSize") != "5" {
		t.Errorf("search for shaders asked for %v", got)
	}
}

func TestModsThenFilesAskTwice(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/mods":
			w.Write([]byte(`{"data":[{"id":10,"slug":"jei","name":"JEI","classId":6},{"id":11,"slug":"pack","name":"Pack","classId":12}]}`))
		case "/mods/files":
			w.Write([]byte(`{"data":[{"id":100,"modId":10,"displayName":"1.0","fileName":"jei.jar","downloadUrl":"https://x/jei.jar","hashes":[{"algo":1,"value":"abc"}]},{"id":101,"modId":11,"displayName":"2.0","fileName":"pack.zip","downloadUrl":null,"hashes":[{"algo":1,"value":"def"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL
	ctx := context.Background()
	projects, err := c.Mods(ctx, []int{10, 11, 12})
	if err != nil {
		t.Fatal(err)
	}
	files, unusable, err := c.Files(ctx, []int{100, 101})
	if err != nil {
		t.Fatal(err)
	}
	if len(unusable) != 0 {
		t.Fatalf("unusable: %v", unusable)
	}
	if len(paths) != 2 || paths[0] != "POST /mods" || paths[1] != "POST /mods/files" {
		t.Fatalf("requests: %v", paths)
	}
	if projects[11].Type != "resourcepack" || len(projects) != 2 {
		t.Fatalf("projects: %+v", projects)
	}
	if files[100].File.URL != "https://x/jei.jar" || files[101].File.URL != "" || files[101].Page != "https://www.curseforge.com/minecraft/texture-packs/pack/files/101" {
		t.Fatalf("files: %+v", files)
	}
}

func TestRateLimitIsNamed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL
	if _, err := c.Mods(context.Background(), []int{10}); out.CodeOf(err) != "rate-limited" {
		t.Fatalf("got %v", err)
	}
}

func TestForbiddenAfterTheKeyWorkedIsALockout(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 10, "name": "Shiny", "slug": "shiny"}})
	}))
	defer srv.Close()
	c := NewShared(fetch.New("test"), "key", t.TempDir())
	c.BaseURL, c.KeyURL = srv.URL, srv.URL+"/key"
	if _, err := c.Project(context.Background(), "10", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mods(context.Background(), []int{10}); out.CodeOf(err) != "rate-limited" {
		t.Fatalf("got %v", err)
	}
	if calls != 2 {
		t.Errorf("%d calls, want 2: a lockout fetches no new key", calls)
	}
}

func TestFilesNamesAFileWithNothingToDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":100,"modId":10,"displayName":"1.0","fileName":"jei.jar","downloadUrl":"https://x/jei.jar","hashes":[]}]}`))
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL
	files, unusable, err := c.Files(context.Background(), []int{100})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 || out.CodeOf(unusable[100]) != "version-no-file" {
		t.Fatalf("files %v, unusable %v", files, unusable)
	}
}
