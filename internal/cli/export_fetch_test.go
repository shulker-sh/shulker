package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func (h *harness) recordRequests(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	inner := h.server.Config.Handler
	h.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		inner.ServeHTTP(w, r)
	})
	t.Cleanup(func() { h.server.Config.Handler = inner })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		taken := paths
		paths = nil
		return taken
	}
}

func (h *harness) emptyCache(t *testing.T) {
	t.Helper()
	if err := os.RemoveAll(h.cache); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(h.cache, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestExportFetchesMissingLockedFiles(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.editManifest(t, func(m map[string]any) {
		m["version"] = "1.0"
		m["server"] = map[string]any{"eula": true}
	})
	h.allowMrpackHost(t)
	requests := h.recordRequests(t)

	h.emptyCache(t)
	requests()
	_, stderr := h.mustRunStderr(t, "export", "mrpack", "--side", "server")
	fetched := strings.Join(requests(), "\n")
	if !strings.Contains(fetched, h.jars["fabric-api"].filename) || strings.Contains(fetched, h.jars["sodium"].filename) {
		t.Fatalf("a server export fetches fabric-api and not the client-only sodium:\n%s", fetched)
	}
	if !strings.Contains(stderr, "fetched 1 file") || strings.Contains(stderr, "sodium") {
		t.Fatalf("the fetch gets its own step, and a mod the export leaves out no warning: %s", stderr)
	}

	h.emptyCache(t)
	requests()
	stdout := h.mustRun(t, "export", "mrpack")
	if !strings.Contains(stdout, "2 mods by download") {
		t.Fatalf("export from an empty cache: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "pack-1.0.mrpack")); err != nil {
		t.Fatal(err)
	}

	requests()
	h.mustRun(t, "export", "mrpack")
	if warm := requests(); len(warm) != 0 {
		t.Fatalf("a warm cache exports offline: %v", warm)
	}

	h.emptyCache(t)
	requests()
	h.mustRun(t, "export", "curseforge", "--bundle")
	if fetched := strings.Join(requests(), "\n"); !strings.Contains(fetched, h.jars["sodium"].filename) {
		t.Fatalf("a curseforge export fetches the client's mods:\n%s", fetched)
	}
}
