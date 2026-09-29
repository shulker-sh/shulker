// Package envtest is an env.Env on fakes: providers that publish to a test's own CDN, a Piston
// for Minecraft 26.2 with a Java runtime index, and loader rows that answer offline.
package envtest

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"shulker.sh/shulker/internal/fetch/fetchtest"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/fake"
)

// CDN serves the files the fake providers publish, by path. A path can be refused or cut short.
type CDN struct {
	srv       *httptest.Server
	mu        sync.Mutex
	files     map[string][]byte
	forbidden map[string]bool
	truncated map[string]bool
}

// NewCDN starts an empty CDN for the test.
func NewCDN(t *testing.T) *CDN {
	t.Helper()
	c := &CDN{files: map[string][]byte{}, forbidden: map[string]bool{}, truncated: map[string]bool{}}
	c.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
		}
	}))
	t.Cleanup(c.srv.Close)
	return c
}

// URL is where the CDN serves.
func (c *CDN) URL() string { return c.srv.URL }

// Serve publishes data at path and returns its URL.
func (c *CDN) Serve(path string, data []byte) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.files[path] = data
	return c.srv.URL + path
}

// ServeOnModrinth publishes data at path and returns the cdn.modrinth.com URL for it, which an
// mrpack may list and a client from Client reaches here.
func (c *CDN) ServeOnModrinth(path string, data []byte) string {
	c.Serve(path, data)
	return "https://cdn.modrinth.com" + path
}

// Client is an HTTP client that reaches the CDN for cdn.modrinth.com and every fake host's own
// .test domain as well as at its own URL.
func (c *CDN) Client() *http.Client {
	return fetchtest.Routed(c.srv, "cdn.modrinth.com", ".test")
}

// Forbid has the CDN refuse v.
func (c *CDN) Forbid(v provider.Version) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forbidden[PathOf(v)] = true
}

// Truncate has the CDN cut v's bytes short, and Restore serves them whole again.
func (c *CDN) Truncate(v provider.Version) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.truncated[PathOf(v)] = true
}

// Restore serves v whole again after a Truncate.
func (c *CDN) Restore(v provider.Version) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.truncated, PathOf(v))
}

// Bytes is the file published at v.
func (c *CDN) Bytes(v provider.Version) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.files[PathOf(v)]
}

// PathOf is the path a version's file is published under.
func PathOf(v provider.Version) string {
	return "/" + v.ID + "/" + v.File.Filename
}

// Host is a fake provider whose files sit on the test's CDN. Sha1Only publishes files the way
// CurseForge does, with a sha1 and no sha512.
type Host struct {
	*fake.Provider
	CDN      *CDN
	Sha1Only bool
}

// NewHost is a fake provider called name whose files sit on c.
func NewHost(c *CDN, name string) *Host {
	return &Host{Provider: fake.New(name), CDN: c}
}

// LikeCurseForge has the host publish sha1-only files, key projects by id and name its versions.
func (h *Host) LikeCurseForge() *Host {
	h.Sha1Only, h.KeyedByID, h.NamedVersions, h.ByContent = true, true, true, true
	return h
}

// Publish adds v to proj with data as its file, served by the CDN, and returns v as published. A
// version with no ID takes the next free one; a project not yet known is added.
func (h *Host) Publish(proj provider.Project, v provider.Version, data []byte) provider.Version {
	if proj.Type == "" {
		proj.Type = manifest.TypeMod
	}
	if proj.Title == "" {
		proj.Title = proj.Slug
	}
	if proj.Page == "" {
		proj.Page = h.ProjectPage(proj.Type, proj.Slug)
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
		v.Published = Day(1).Add(time.Duration(len(h.Files)) * time.Hour)
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
	v.File.Sha1 = Sha1Hex(data)
	v.File.Size = int64(len(data))
	if !h.Sha1Only {
		v.File.Sha512 = Sha512Hex(data)
	}
	if v.File.URL == "" {
		h.CDN.Serve(PathOf(v), data)
		v.File.URL = "https://" + h.Hosts()[0] + PathOf(v)
	}
	h.Files = append(h.Files, v)
	return v
}

// PublishManual adds v to proj with no URL, as a file whose project opted out of distribution.
func (h *Host) PublishManual(proj provider.Project, v provider.Version, data []byte) provider.Version {
	v = h.Publish(proj, v, data)
	v.File.URL = ""
	h.Files[len(h.Files)-1] = v
	return v
}

// Republish swaps the bytes behind the version with the given id, as a host does when the
// upload differs from what another host has.
func (h *Host) Republish(id string, data []byte) provider.Version {
	i := slices.IndexFunc(h.Files, func(v provider.Version) bool { return v.ID == id })
	v := h.Files[i]
	v.File.Sha1 = Sha1Hex(data)
	v.File.Sha512 = Sha512Hex(data)
	v.File.Size = int64(len(data))
	v.File.URL = h.CDN.Serve(PathOf(v), data)
	h.Files[i] = v
	return v
}

// Sha1Hex is the hex sha1 of data.
func Sha1Hex(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

// Sha512Hex is the hex sha512 of data.
func Sha512Hex(data []byte) string {
	sum := sha512.Sum512(data)
	return hex.EncodeToString(sum[:])
}

// Day is a publication date in September 2026.
func Day(n int) time.Time {
	return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC)
}
