package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/meta"
	"shulker.sh/shulker/internal/out"
)

func runtimeIndex(t *testing.T, index map[string]map[string]string) *meta.Runtimes {
	t.Helper()
	body := map[string]map[string][]map[string]any{}
	for platform, components := range index {
		body[platform] = map[string][]map[string]any{}
		for component, version := range components {
			body[platform][component] = []map[string]any{{
				"manifest": map[string]any{"sha1": platform, "url": "https://example.com/" + platform + ".json"},
				"version":  map[string]any{"name": version},
			}}
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return &meta.Runtimes{Client: fetch.New("test"), IndexURL: srv.URL}
}

func TestFindReleaseFallsBackToIntelOnAppleSilicon(t *testing.T) {
	runtimes := runtimeIndex(t, map[string]map[string]string{
		"mac-os":       {"jre-legacy": "8u74", "java-runtime-delta": "21.0.3"},
		"mac-os-arm64": {"java-runtime-delta": "21.0.3"},
		"linux":        {"java-runtime-delta": "21.0.3"},
	})
	ctx := context.Background()
	yes, no := func() bool { return true }, func() bool { return false }

	r, err := findRelease(ctx, runtimes, "mac-os-arm64", "java-runtime-delta", no)
	if err != nil || r.ManifestSha1 != "mac-os-arm64" {
		t.Errorf("native: %+v, %v", r, err)
	}
	r, err = findRelease(ctx, runtimes, "mac-os-arm64", "jre-legacy", yes)
	if err != nil || r.ManifestSha1 != "mac-os" || r.Version != "8u74" {
		t.Errorf("intel fallback: %+v, %v", r, err)
	}
	if _, err := findRelease(ctx, runtimes, "mac-os-arm64", "jre-legacy", no); out.CodeOf(err) != "rosetta-required" {
		t.Errorf("without rosetta: %v", err)
	}
	if _, err := findRelease(ctx, runtimes, "linux", "jre-legacy", yes); out.CodeOf(err) != "runtime-unavailable" {
		t.Errorf("linux: %v", err)
	}
	if _, err := findRelease(ctx, runtimes, "mac-os-arm64", "java-runtime-gamma", yes); out.CodeOf(err) != "runtime-unavailable" {
		t.Errorf("unknown component: %v", err)
	}
}
