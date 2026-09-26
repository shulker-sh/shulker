package resolve

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/provider"
)

func day(n int) time.Time { return envtest.Day(n) }

func sha512Hex(data []byte) string { return envtest.Sha512Hex(data) }

func mod(id, slug string) provider.Project {
	return provider.Project{ID: id, Slug: slug, Title: slug, Type: manifest.TypeMod}
}

func modJar(t *testing.T, id, version, env string) []byte {
	t.Helper()
	return fabricJar(t, fmt.Sprintf(`{"id":%q,"version":%q,"environment":%q,"depends":{"fabricloader":">=0.17"}}`, id, version, env), nil)
}

func dependsOn(projectID string) provider.Dependency {
	return provider.Dependency{ProjectID: projectID, Type: "required"}
}

// harness is a Resolver on a fabric 26.2 project in a temp dir, with the manifest's providers in
// the order given and every log line kept.
type harness struct {
	t   *testing.T
	r   *Resolver
	cdn *envtest.CDN
	log []string
}

func newHarness(t *testing.T, providers ...*envtest.Host) *harness {
	t.Helper()
	h := &harness{t: t}
	if len(providers) > 0 {
		h.cdn = providers[0].CDN
	}
	h.r = &Resolver{
		Dir:       t.TempDir(),
		Manifest:  &manifest.Manifest{Name: "pack", Minecraft: "26.2", Loader: manifest.Loader{Type: "fabric"}, Requires: map[string]manifest.Require{}},
		Lock:      lock.New(),
		Providers: provider.Providers{},
		Cache:     &cache.Cache{Dir: t.TempDir()},
		Fetch:     fetch.New("test"),
		Log:       func(format string, args ...any) { h.log = append(h.log, fmt.Sprintf(format, args...)) },
	}
	h.r.Lock.Minecraft, h.r.Lock.DataVersion = "26.2", 4600
	h.r.Lock.Loader = lock.Loader{Type: "fabric", Version: "0.17.3"}
	for _, p := range providers {
		h.r.Providers[p.Name()] = p
		h.r.Manifest.Providers = append(h.r.Manifest.Providers, p.Name())
	}
	return h
}

func (h *harness) add(slug string, opts AddOptions) error {
	return h.r.Add(context.Background(), slug, opts)
}

func (h *harness) mustAdd(slug string, opts AddOptions) {
	h.t.Helper()
	if err := h.add(slug, opts); err != nil {
		h.t.Fatalf("add %s: %v", slug, err)
	}
}

func (h *harness) reconcile() ([]string, error) {
	return h.r.Reconcile(context.Background())
}

func (h *harness) mustReconcile() {
	h.t.Helper()
	if _, err := h.reconcile(); err != nil {
		h.t.Fatalf("reconcile: %v", err)
	}
}

func (h *harness) mod(id string) lock.Mod {
	h.t.Helper()
	m, ok := h.r.Lock.Mods[id]
	if !ok {
		h.t.Fatalf("%s is not locked: %v", id, slices.Sorted(maps.Keys(h.r.Lock.Mods)))
	}
	return m
}

func (h *harness) install() ([]string, []string, error) {
	return h.r.Install(context.Background())
}

// drop puts a file in the project's downloads folder, as a user does for a manual download.
func (h *harness) drop(name string, data []byte) {
	h.t.Helper()
	dir := filepath.Join(h.r.Dir, DownloadsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) logged(text string) bool {
	return slices.ContainsFunc(h.log, func(line string) bool { return strings.Contains(line, text) })
}
