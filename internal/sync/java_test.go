package sync

import (
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

// A vanilla server: the fake loader rows install no server of their own.
func TestServerSyncKeepsTheInstalledRuntimeOffline(t *testing.T) {
	h := newHarness(t)
	h.editManifest(func(m *manifest.Manifest) { m.Client, m.Server, m.Loader = nil, &manifest.Server{}, manifest.Loader{} })
	h.editLock(func(l *lock.Lock) { l.Loader = lock.Loader{} })
	into := filepath.Join(t.TempDir(), "server")
	res := h.mustSync(into, Request{})
	if len(res.Fetched) == 0 || res.Fetched[len(res.Fetched)-1] != "java-runtime-epsilon 25.0.1" {
		t.Fatalf("a server sync fetches the lock's runtime: %v", res.Fetched)
	}

	h.e.Runtimes.IndexURL = "https://127.0.0.1:1/jrt/all.json"
	res = h.mustSync(into, Request{})
	if !h.warned("offline, keeping the installed Java runtime java-runtime-epsilon 25.0.1") {
		t.Fatalf("offline runtime refresh: %v", h.env.Warnings)
	}
	if len(res.Fetched) != 0 {
		t.Fatalf("nothing is fetched offline: %v", res.Fetched)
	}
}
