package sync

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/saves"
)

// harness is a sync env on fakes with a fabric 26.2 client project on disk and sodium published
// on modrinth, ready to add.
type harness struct {
	t      *testing.T
	env    *envtest.Env
	e      *Env
	dir    string
	sodium provider.Version
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, env: envtest.New(t), dir: filepath.Join(t.TempDir(), "pack")}
	data := t.TempDir()
	h.e = &Env{Env: h.env.Env, Registry: filepath.Join(data, "registry.json"), Saves: saves.Roots{Saves: filepath.Join(data, "saves"), Backups: filepath.Join(data, "backups")}, SaveBackups: config.DefaultSaveBackups}
	h.sodium = h.env.Modrinth.Publish(provider.Project{ID: "AANobbMI", Slug: "sodium", Title: "Sodium"}, provider.Version{ID: "QANobbMI", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, envtest.ModJar(t, "sodium", "0.9.2", "client"))
	if err := os.MkdirAll(h.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Name: "pack", Minecraft: "26.2", Loader: manifest.Loader{Type: "fabric"}, Providers: []string{"modrinth"}, Requires: map[string]manifest.Require{}, Client: &manifest.Client{}}
	h.saveManifest(m)
	l := lock.New()
	l.Minecraft, l.DataVersion = "26.2", 4600
	l.Loader = lock.Loader{Type: "fabric", Version: "0.17.3"}
	l.Java = lock.Java{Major: 25, Component: "java-runtime-epsilon"}
	if err := l.Save(filepath.Join(h.dir, lock.FileName)); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) saveManifest(m *manifest.Manifest) {
	h.t.Helper()
	if err := m.Save(filepath.Join(h.dir, manifest.FileName)); err != nil {
		h.t.Fatal(err)
	}
}

// editManifest rewrites the project's manifest through edit.
func (h *harness) editManifest(edit func(m *manifest.Manifest)) {
	h.t.Helper()
	p := h.project()
	edit(p.Manifest)
	h.saveManifest(p.Manifest)
}

// editLock rewrites the project's lock through edit.
func (h *harness) editLock(edit func(l *lock.Lock)) {
	h.t.Helper()
	p := h.project()
	edit(p.Lock)
	if err := p.SaveLock(); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) project() *project.Project {
	h.t.Helper()
	p, err := project.Open(h.dir)
	if err != nil {
		h.t.Fatal(err)
	}
	return p
}

// add locks slug in the project, the way `shulker add` does.
func (h *harness) add(slug string) {
	h.t.Helper()
	p := h.project()
	r, err := resolve.New(context.Background(), h.e.Env, p, resolve.PackMode{})
	if err != nil {
		h.t.Fatal(err)
	}
	if err := r.Add(context.Background(), slug, resolve.AddOptions{}); err != nil {
		h.t.Fatalf("add %s: %v", slug, err)
	}
	if err := p.SaveManifest(); err != nil {
		h.t.Fatal(err)
	}
	if err := p.SaveLock(); err != nil {
		h.t.Fatal(err)
	}
}

// remove drops key from the manifest and lock, the way `shulker remove` does for a mod nothing
// else needs.
func (h *harness) remove(key string) {
	h.t.Helper()
	p := h.project()
	delete(p.Manifest.Requires, key)
	delete(p.Lock.Mods, key)
	if err := p.SaveManifest(); err != nil {
		h.t.Fatal(err)
	}
	if err := p.SaveLock(); err != nil {
		h.t.Fatal(err)
	}
}

// sync syncs the project into dir; an empty dir is the side's own build directory.
func (h *harness) sync(into string, req Request) (Result, error) {
	h.t.Helper()
	src, err := Open(context.Background(), h.e, h.dir, req.At)
	if err != nil {
		return Result{}, err
	}
	req.Into = into
	return Run(context.Background(), h.e, src, req)
}

func (h *harness) mustSync(into string, req Request) Result {
	h.t.Helper()
	res, err := h.sync(into, req)
	if err != nil {
		h.t.Fatalf("sync into %s: %v\nlog: %v\nwarnings: %v", into, err, h.env.Log, h.env.Warnings)
	}
	return res
}

func (h *harness) register(rows ...config.Instance) {
	h.t.Helper()
	_, err := config.UpdateInstances(h.e.Registry, func([]config.Instance) []config.Instance { return rows })
	if err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) instances() []config.Instance {
	h.t.Helper()
	rows, err := config.LoadInstances(h.e.Registry)
	if err != nil {
		h.t.Fatal(err)
	}
	return rows
}

func (h *harness) modPath(dir string) string {
	return filepath.Join(dir, "mods", h.sodium.File.Filename)
}

func (h *harness) warned(text string) bool {
	return slices.ContainsFunc(h.env.Warnings, func(w string) bool { return strings.Contains(w, text) })
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
