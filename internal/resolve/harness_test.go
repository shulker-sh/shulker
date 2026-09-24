package resolve

import (
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/fake"
)

// cdn serves the files the fake providers publish, by path. A path can be refused or cut short.
type cdn struct {
	srv       *httptest.Server
	mu        sync.Mutex
	files     map[string][]byte
	forbidden map[string]bool
	truncated map[string]bool
}

func newCDN(t *testing.T) *cdn {
	t.Helper()
	c := &cdn{files: map[string][]byte{}, forbidden: map[string]bool{}, truncated: map[string]bool{}}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		data, ok := c.files[r.URL.Path]
		switch {
		case !ok:
			http.NotFound(w, r)
		case c.forbidden[r.URL.Path]:
			w.WriteHeader(http.StatusForbidden)
		case c.truncated[r.URL.Path]:
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Write(data[:len(data)/2])
		default:
			w.Write(data)
		}
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *cdn) serve(path string, data []byte) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.files[path] = data
	return c.srv.URL + path
}

func (c *cdn) forbid(v provider.Version) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forbidden[pathOf(v)] = true
}

// truncate has the cdn cut v's bytes short, and restore serves them whole again.
func (c *cdn) truncate(v provider.Version) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.truncated[pathOf(v)] = true
}

func (c *cdn) restore(v provider.Version) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.truncated, pathOf(v))
}

// bytes is the file published at v.
func (c *cdn) bytes(v provider.Version) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.files[pathOf(v)]
}

func pathOf(v provider.Version) string {
	return "/" + v.ID + "/" + v.File.Filename
}

// host is a fake provider whose files sit on the test's cdn. sha1Only publishes files the way
// CurseForge does, with a sha1 and no sha512.
type host struct {
	*fake.Provider
	cdn      *cdn
	sha1Only bool
}

// likeCurseForge has the host publish sha1-only files and key projects by id.
func (h *host) likeCurseForge() *host {
	h.sha1Only, h.KeyedByID = true, true
	return h
}

func newHost(c *cdn, name string) *host {
	return &host{Provider: fake.New(name), cdn: c}
}

// publish adds v to proj with jar as its file, served by the cdn. A version with no ID takes the
// next free one; a project not yet known is added.
func (h *host) publish(proj provider.Project, v provider.Version, jar []byte) provider.Version {
	if proj.Type == "" {
		proj.Type = manifest.TypeMod
	}
	if !slices.ContainsFunc(h.Known, func(p provider.Project) bool { return p.ID == proj.ID }) {
		h.Known = append(h.Known, proj)
	}
	v.ProjectID = proj.ID
	if v.ID == "" {
		v.ID = fmt.Sprintf("%s-%d", proj.ID, len(h.Files)+1)
	}
	if v.Channel == "" {
		v.Channel = "release"
	}
	if v.Published.IsZero() {
		v.Published = day(1).Add(time.Duration(len(h.Files)) * time.Hour)
	}
	if v.GameVersions == nil {
		v.GameVersions = []string{"26.2"}
	}
	if v.Loaders == nil && proj.Type == manifest.TypeMod {
		v.Loaders = []string{"fabric"}
	}
	if v.Page == "" {
		v.Page = h.ProjectPage(proj.Type, proj.Slug) + "/files/" + v.ID
	}
	sum1 := sha1.Sum(jar)
	v.File.Sha1 = hex.EncodeToString(sum1[:])
	v.File.Size = int64(len(jar))
	if !h.sha1Only {
		v.File.Sha512 = sha512Hex(jar)
	}
	if v.File.URL == "" {
		v.File.URL = h.cdn.serve(pathOf(v), jar)
	}
	h.Files = append(h.Files, v)
	return v
}

// publishManual adds v to proj with no URL, as a file whose project opted out of distribution.
func (h *host) publishManual(proj provider.Project, v provider.Version, jar []byte) provider.Version {
	v = h.publish(proj, v, jar)
	v.File.URL = ""
	h.Files[len(h.Files)-1] = v
	return v
}

func sha512Hex(data []byte) string {
	sum := sha512.Sum512(data)
	return hex.EncodeToString(sum[:])
}

// day is a publication date in September 2026.
func day(n int) time.Time {
	return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC)
}

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
	cdn *cdn
	log []string
}

func newHarness(t *testing.T, providers ...*host) *harness {
	t.Helper()
	h := &harness{t: t}
	if len(providers) > 0 {
		h.cdn = providers[0].cdn
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
