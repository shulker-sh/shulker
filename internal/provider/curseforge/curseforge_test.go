package curseforge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"id": 394468, "name": "Sodium", "slug": "sodium", "summary": "The fastest rendering mod", "classId": 6, "downloadCount": 151434981},
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
	want := provider.Project{ID: "394468", Slug: "sodium", Title: "Sodium", Summary: "The fastest rendering mod", Type: "mod", Downloads: 151434981}
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	projects, err := c.Projects(ctx, []string{"10", "11", "12"})
	if err != nil {
		t.Fatal(err)
	}
	files, unusable, err := c.VersionsByID(ctx, []string{"100", "101"})
	if err != nil {
		t.Fatal(err)
	}
	if len(unusable) != 0 {
		t.Fatalf("unusable: %v", unusable)
	}
	if len(paths) != 2 || paths[0] != "POST /mods" || paths[1] != "POST /mods/files" {
		t.Fatalf("requests: %v", paths)
	}
	if projects["11"].Type != "resourcepack" || len(projects) != 2 {
		t.Fatalf("projects: %+v", projects)
	}
	if files["100"].File.URL != "https://x/jei.jar" || files["101"].File.URL != "" || files["101"].Page != "https://www.curseforge.com/minecraft/texture-packs/pack/files/101" {
		t.Fatalf("files: %+v", files)
	}
}

func TestRateLimitIsNamed(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL
	if _, err := c.Projects(context.Background(), []string{"10"}); out.CodeOf(err) != "rate-limited" {
		t.Fatalf("got %v", err)
	}
}

func TestForbiddenAfterTheKeyWorkedIsALockout(t *testing.T) {
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if _, err := c.Projects(context.Background(), []string{"10"}); out.CodeOf(err) != "rate-limited" {
		t.Fatalf("got %v", err)
	}
	if calls != 2 {
		t.Errorf("%d calls, want 2: a lockout fetches no new key", calls)
	}
}

func TestFilesNamesAFileWithNothingToDownload(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":100,"modId":10,"displayName":"1.0","fileName":"jei.jar","downloadUrl":"https://x/jei.jar","hashes":[]}]}`))
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL
	files, unusable, err := c.VersionsByID(context.Background(), []string{"100"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 || out.CodeOf(unusable["100"]) != "version-no-file" {
		t.Fatalf("files %v, unusable %v", files, unusable)
	}
}

func TestIdentifyFingerprintsThenAsksForProjectsAndFiles(t *testing.T) {
	var paths []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/fingerprints":
			var body struct {
				Fingerprints []uint32 `json:"fingerprints"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Fingerprints) != 2 || body.Fingerprints[0] != 3817166195 {
				t.Errorf("fingerprints asked: %v", body.Fingerprints)
			}
			w.Write([]byte(`{"data":{"exactMatches":[{"file":{"id":100,"modId":10,"fileFingerprint":3817166195}}]}}`))
		case "/mods":
			w.Write([]byte(`{"data":[{"id":10,"slug":"jei","name":"JEI","classId":6,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/mc-mods/jei"},"authors":[{"name":"mezz"}]}]}`))
		case "/mods/files":
			w.Write([]byte(`{"data":[{"id":100,"modId":10,"displayName":"1.0","fileName":"jei.jar","downloadUrl":"https://x/jei.jar","hashes":[{"algo":1,"value":"abc"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL
	found, err := c.Identify(context.Background(), map[string][]byte{"mods/jei.jar": []byte("shulker"), "mods/other.jar": []byte("other")})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(paths, []string{"POST /fingerprints", "POST /mods", "POST /mods/files"}) {
		t.Fatalf("requests: %v", paths)
	}
	h, ok := found["mods/jei.jar"]
	if len(found) != 1 || !ok || h.Project.Author != "mezz" || h.Project.Page != "https://www.curseforge.com/minecraft/mc-mods/jei" || h.Version.ID != "100" {
		t.Fatalf("found %+v", found)
	}
}

func TestURLsRoundTrip(t *testing.T) {
	c := New(fetch.New("test"), "key")
	for arg, want := range map[string]provider.Ref{
		"https://www.curseforge.com/minecraft/mc-mods/jei":                              {Project: "jei"},
		"https://curseforge.com/minecraft/texture-packs/fresh-animations/files/5000001": {Project: "fresh-animations", Version: "5000001"},
		"https://legacy.curseforge.com/minecraft/mc-mods/balm-fabric/download/5700001":  {Project: "balm-fabric", Version: "5700001"},
		"https://www.curseforge.com/projects/500525":                                    {Project: "500525"},
	} {
		u, _ := url.Parse(arg)
		if got, err := c.ParseURL(u); err != nil || got != want {
			t.Errorf("%s: got %+v err=%v", arg, got, err)
		}
	}
	for _, arg := range []string{"https://www.curseforge.com/minecraft/worlds/skyblock", "https://www.curseforge.com/minecraft/mc-mods/jei/files", "https://www.curseforge.com/projects/jei"} {
		u, _ := url.Parse(arg)
		if _, err := c.ParseURL(u); out.CodeOf(err) != "usage" {
			t.Errorf("%s: err=%v, want usage", arg, err)
		}
	}
	u, _ := url.Parse("https://modrinth.com/mod/sodium")
	if _, err := c.ParseURL(u); err != provider.ErrNotHosted {
		t.Errorf("another host: %v", err)
	}
	if got := c.ProjectPage("resourcepack", "fresh-animations"); got != "https://www.curseforge.com/minecraft/texture-packs/fresh-animations" {
		t.Errorf("project page %s", got)
	}
	if got := c.ProjectPage("", "500525"); got != "https://www.curseforge.com/projects/500525" {
		t.Errorf("project page by id %s", got)
	}
	if got := c.VersionsPage("mod", "jei"); got != "https://www.curseforge.com/minecraft/mc-mods/jei/files" {
		t.Errorf("versions page %s", got)
	}
	if c.Available() != nil || New(fetch.New("test"), "").Available() == nil {
		t.Error("availability follows the key")
	}
}

func TestAFileIDReachesAProjectTheSearchMisses(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mods/search":
			w.Write([]byte(`{"data":[]}`))
		case "/mods/files":
			w.Write([]byte(`{"data":[{"id":5700001,"modId":500525,"displayName":"7.3.9","fileName":"balm-fabric-7.3.9.jar","downloadUrl":"https://x/balm.jar","hashes":[{"algo":1,"value":"abc"}]}]}`))
		case "/mods/500525":
			w.Write([]byte(`{"data":{"id":500525,"slug":"balm-fabric","name":"Balm","classId":6}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL
	ctx := context.Background()
	if _, err := c.Project(ctx, "balm-fabric", "mod"); !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("an unlisted slug is not found: %v", err)
	}
	if !strings.Contains(c.NotFoundHelp(), "file URL") {
		t.Fatalf("the miss points at a file URL: %s", c.NotFoundHelp())
	}
	v, err := c.ProjectVersion(ctx, "balm-fabric", "5700001")
	if err != nil {
		t.Fatal(err)
	}
	if v.ProjectID != "500525" || v.ID != "5700001" || v.Page != "https://www.curseforge.com/minecraft/mc-mods/balm-fabric/files/5700001" {
		t.Fatalf("the file names its project: %+v", v)
	}
}

func TestAVersionNamesItsServerPack(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mods/10":
			w.Write([]byte(`{"data":{"id":10,"slug":"atm","name":"ATM","classId":4471,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/modpacks/atm"}}}`))
			return
		case "/mods":
			w.Write([]byte(`{"data":[{"id":10,"slug":"atm","name":"ATM","classId":4471,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/modpacks/atm"}}]}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":100,"modId":10,"displayName":"8.2","fileName":"pack.zip","downloadUrl":"https://x/pack.zip","hashes":[{"algo":1,"value":"abc"}],"serverPackFileId":101},{"id":102,"modId":10,"displayName":"8.1","fileName":"old.zip","downloadUrl":"https://x/old.zip","hashes":[{"algo":1,"value":"def"}]}]}`))
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL
	found, _, err := c.VersionsByID(context.Background(), []string{"100", "102"})
	if err != nil {
		t.Fatal(err)
	}
	if found["100"].ServerPack != "101" || found["102"].ServerPack != "" {
		t.Fatalf("server packs %q and %q", found["100"].ServerPack, found["102"].ServerPack)
	}
}

func TestConvertFileNamesShaderModsByIntegration(t *testing.T) {
	cases := []struct {
		gameVersions []string
		loaders      []string
		game         []string
	}{
		{gameVersions: []string{"26.2", "OptiFine"}, loaders: []string{"iris", "oculus"}, game: []string{"26.2", "OptiFine"}},
		{gameVersions: []string{"26.2", "Iris", "OptiFine"}, loaders: []string{"iris", "oculus"}, game: []string{"26.2", "Iris", "OptiFine"}},
		{gameVersions: []string{"26.2", "Fabric"}, loaders: []string{"fabric"}, game: []string{"26.2"}},
		{gameVersions: []string{"26.2"}, game: []string{"26.2"}},
	}
	for _, c := range cases {
		gameVersions, _ := json.Marshal(c.gameVersions)
		var f file
		raw := `{"id":1,"modId":2,"fileName":"a.zip","downloadUrl":"https://x/a.zip","hashes":[{"algo":1,"value":"abc"}],"gameVersions":` + string(gameVersions) + `}`
		if err := json.Unmarshal([]byte(raw), &f); err != nil {
			t.Fatal(err)
		}
		v, err := convertFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(v.Loaders, c.loaders) || !slices.Equal(v.GameVersions, c.game) {
			t.Errorf("%v became loaders %v and game versions %v", c.gameVersions, v.Loaders, v.GameVersions)
		}
	}
}

func TestAModpackFileTaggedWithNoLoaderIsListed(t *testing.T) {
	files, err := os.ReadFile(filepath.Join("testdata", "rlcraft-files.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mods/285109" {
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 285109, "name": "RLCraft", "slug": "rlcraft", "classId": 4471}})
			return
		}
		w.Write(files)
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL

	versions, err := c.Versions(context.Background(), "285109", "1.12.2", nil)
	if err != nil {
		t.Fatal(err)
	}

	newest, ok := provider.Newest(versions, "release", "")
	if !ok || newest.ID != "4612979" || len(newest.Loaders) != 0 {
		t.Fatalf("RLCraft 2.9.3 is tagged 1.12.2 alone and still the newest release: %+v", newest)
	}
}

func TestFiledFingerprintsTheCachedFilesInOneRequest(t *testing.T) {
	var paths []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Write([]byte(`{"data":{"exactMatches":[{"file":{"id":100,"modId":10,"fileFingerprint":3817166195}}]}}`))
	}))
	defer srv.Close()
	c := New(fetch.New("test"), "key")
	c.BaseURL = srv.URL
	dir := t.TempDir()
	jei, other := filepath.Join(dir, "jei"), filepath.Join(dir, "other")
	os.WriteFile(jei, []byte("shulker"), 0o644)
	os.WriteFile(other, []byte("other"), 0o644)
	found, unchecked, err := c.Filed(context.Background(), map[string]provider.LockedFile{
		"jei":      {Type: "mod", Path: jei},
		"other":    {Type: "mod", Path: other},
		"uncached": {Type: "mod"},
		"pack":     {Type: "modpack", Path: jei},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(paths, []string{"POST /fingerprints"}) {
		t.Fatalf("requests: %v", paths)
	}
	if len(found) != 1 || found["jei"] != (provider.Filing{Project: "10", Version: "100"}) || !slices.Equal(unchecked, []string{"pack", "uncached"}) {
		t.Fatalf("found %+v, unchecked %v", found, unchecked)
	}
}
