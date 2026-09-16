package curseforge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
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
	if out.CodeOf(err) != "curseforge-key-rejected" || !strings.Contains(err.Error(), "shulker.sh has no newer one") || !strings.Contains(err.Error(), "report it") {
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
