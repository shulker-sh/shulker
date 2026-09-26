package build

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/provider"
)

func sha512Hex(data []byte) string { return envtest.Sha512Hex(data) }

func mod(id, slug string) provider.Project {
	return provider.Project{ID: id, Slug: slug, Type: manifest.TypeMod}
}

// zipOf is a zip of the entries in name order, so the same entries give the same bytes.
func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, entries[name])
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func modJar(t *testing.T, id, version string) []byte {
	t.Helper()
	return zipOf(t, map[string]string{"fabric.mod.json": fmt.Sprintf(`{"id":%q,"version":%q}`, id, version)})
}

func packZip(t *testing.T, description string) []byte {
	t.Helper()
	return zipOf(t, map[string]string{"pack.mcmeta": `{"pack":{"pack_format":34,"description":"` + description + `"}}`})
}

// rezip rewrites a zip's entries under another DOS modification time, which changes its bytes but
// not its size. Setting Modified instead would add an extended-timestamp field and change the size.
func rezip(t *testing.T, data []byte, edit func(name, content string) string) []byte {
	t.Helper()
	entries := unzip(t, data)
	for name, content := range entries {
		entries[name] = edit(name, content)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, ModifiedDate: 0x5d21, ModifiedTime: 0x6000})
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, entries[name])
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name] = string(content)
	}
	return entries
}

// project is a fabric 26.2 project in a temp dir with a client and a server side, its
// manifest and lock on disk and the lock's files in the cache, beside a fake Modrinth and a fake
// CurseForge on one cdn.
type testProject struct {
	t        *testing.T
	b        *Builder
	cdn      *envtest.CDN
	modrinth *envtest.Host
	cf       *envtest.Host
	log      []string
}

func newProject(t *testing.T) *testProject {
	t.Helper()
	p := &testProject{t: t, cdn: envtest.NewCDN(t)}
	p.modrinth = envtest.NewHost(p.cdn, "modrinth")
	p.cf = envtest.NewHost(p.cdn, "curseforge")
	p.cf.Label, p.cf.KeyedByID = "CurseForge", true
	dir := t.TempDir()
	p.b = &Builder{
		Dir: dir,
		Manifest: &manifest.Manifest{
			Name: "pack", Version: "1.0", Authors: []string{"Ann", "Bo"}, Minecraft: "26.2",
			Loader:    manifest.Loader{Type: "fabric"},
			Providers: []string{"modrinth", "curseforge"},
			Requires:  map[string]manifest.Require{},
			Client:    &manifest.Client{Name: "Demo Pack"},
			Server:    &manifest.Server{},
		},
		Lock:      lock.New(),
		LockPath:  filepath.Join(dir, lock.FileName),
		Cache:     &cache.Cache{Dir: t.TempDir()},
		Providers: provider.Providers{"modrinth": p.modrinth, "curseforge": p.cf},
		Fetch:     fetch.New("test"),
		Log:       func(format string, args ...any) { p.log = append(p.log, fmt.Sprintf(format, args...)) },
	}
	p.b.Lock.Minecraft = "26.2"
	p.b.Lock.Loader = lock.Loader{Type: "fabric", Version: "0.17.3"}
	p.b.Lock.Java = lock.Java{Major: 21, Component: "java-runtime-delta"}
	return p
}

// lockMod locks the version from h as key, with its file in the cache.
func (p *testProject) lockMod(key string, h *envtest.Host, v provider.Version) {
	p.t.Helper()
	p.b.Manifest.Requires[key] = manifest.Require{Provider: h.Name()}
	p.b.Lock.Mods[key] = lock.Mod{
		Provider: h.Name(), Project: v.ProjectID, Version: v.ID, VersionNumber: v.Number,
		Filename: v.File.Filename, URL: &v.File.URL,
		Sha512: v.File.Sha512, Size: v.File.Size, Side: "both", Channel: v.Channel, RequiredBy: []string{}, Aliases: lock.Aliases{},
	}
	p.cacheFile(v)
}

// lockPack locks the version from h as key of the given kind, placed under the key's name with
// the zip extension, the way an add names a pack.
func (p *testProject) lockPack(kind, key string, h *envtest.Host, v provider.Version, loaders ...string) {
	p.t.Helper()
	p.b.Manifest.Requires[key] = manifest.Require{Type: kind, Provider: h.Name()}
	p.b.Lock.Packs(kind)[key] = lock.Pack{
		Provider: h.Name(), Project: v.ProjectID, Version: v.ID, VersionNumber: v.Number,
		Filename: key + ".zip", ProviderFilename: v.File.Filename, URL: &v.File.URL,
		Sha512: v.File.Sha512, Sha1: v.File.Sha1, Size: v.File.Size, Channel: v.Channel, Loaders: loaders,
	}
	p.cacheFile(v)
}

func (p *testProject) cacheFile(v provider.Version) {
	p.t.Helper()
	data := p.cdn.Bytes(v)
	path := p.b.Cache.Object(v.File.Sha512)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		p.t.Fatal(err)
	}
}

// override writes a file into the project's shared overrides folder.
func (p *testProject) override(rel, content string) {
	p.t.Helper()
	path := filepath.Join(p.b.Dir, "overrides", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		p.t.Fatal(err)
	}
}

// exportCurseForge exports the project as a CurseForge pack to
// build/pack-1.0.zip.
func (p *testProject) exportCurseForge(bundle bool) (*ExportReport, error) {
	p.t.Helper()
	p.save()
	format, _ := packarchive.Lookup("curseforge")
	return p.b.Export(context.Background(), ExportOptions{Format: format, Version: "1.0", Output: p.archivePath(), Bundle: bundle})
}

func (p *testProject) archivePath() string {
	return filepath.Join(p.b.Dir, "build", "pack-1.0.zip")
}

// archive is the exported zip's entries by name.
func (p *testProject) archive() map[string]string {
	p.t.Helper()
	data, err := os.ReadFile(p.archivePath())
	if err != nil {
		p.t.Fatal(err)
	}
	return unzip(p.t, data)
}

// listed reads the archive back and returns its listed files.
func (p *testProject) listed() []packarchive.File {
	p.t.Helper()
	arc, err := packarchive.Read(p.archivePath())
	if err != nil {
		p.t.Fatal(err)
	}
	return arc.Files
}

func (p *testProject) logged(text string) bool {
	return slices.ContainsFunc(p.log, func(line string) bool { return strings.Contains(line, text) })
}

// listedIDs are the archive's files as project/version pairs, in listing order.
func listedIDs(files []packarchive.File) []string {
	ids := make([]string, len(files))
	for i, f := range files {
		ids[i] = f.Project + "/" + f.Version
	}
	return ids
}

// cacheBytes puts data in the cache under its sha512 and returns the digest.
func (p *testProject) cacheBytes(data []byte) string {
	p.t.Helper()
	sum := sha512Hex(data)
	path := p.b.Cache.Object(sum)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		p.t.Fatal(err)
	}
	return sum
}

// lockLocalMod locks data as the project's own jar files/filename under key.
func (p *testProject) lockLocalMod(key, filename string, data []byte) {
	p.t.Helper()
	p.b.Manifest.Requires[key] = manifest.Require{File: "files/" + filename}
	p.b.Lock.Mods[key] = lock.Mod{File: "files/" + filename, Filename: filename, Sha512: p.cacheBytes(data), Size: int64(len(data)), Side: "both", RequiredBy: []string{}, Aliases: lock.Aliases{}}
}

// lockLocalDatapack locks data as the project's own datapack files/filename under key.
func (p *testProject) lockLocalDatapack(key, filename string, data []byte) {
	p.t.Helper()
	p.b.Manifest.Requires[key] = manifest.Require{Type: manifest.TypeDatapack, File: "files/" + filename}
	p.b.Lock.Datapacks[key] = lock.Pack{File: "files/" + filename, Filename: filename, Sha512: p.cacheBytes(data), Size: int64(len(data)), Side: "both"}
}
