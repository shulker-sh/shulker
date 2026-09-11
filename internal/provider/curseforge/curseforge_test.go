package curseforge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/fetch"
)

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
