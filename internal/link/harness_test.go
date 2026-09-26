package link

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/saves"
	"shulker.sh/shulker/internal/sync"
)

// harness is a link env on fakes: an instances root, a registry, and a fabric 26.2 client project
// "pack" on disk with sodium locked, plus lithium published on modrinth for a second mod.
type harness struct {
	t       *testing.T
	env     *envtest.Env
	e       *Env
	dir     string
	root    string
	sodium  provider.Version
	lithium provider.Version
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, env: envtest.New(t)}
	data := t.TempDir()
	h.root = filepath.Join(data, "instances")
	se := &sync.Env{Env: h.env.Env, Registry: filepath.Join(data, "registry.json"), Saves: saves.Roots{Saves: filepath.Join(data, "saves"), Backups: filepath.Join(data, "backups")}, SaveBackups: config.DefaultSaveBackups}
	h.e = &Env{Env: se, Instances: h.root}
	h.sodium = h.env.Modrinth.Publish(provider.Project{ID: "AANobbMI", Slug: "sodium", Title: "Sodium"}, provider.Version{ID: "QANobbMI", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, envtest.ModJar(t, "sodium", "0.9.2", "client"))
	h.lithium = h.env.Modrinth.Publish(provider.Project{ID: "gvQqBUqZ", Slug: "lithium", Title: "Lithium"}, provider.Version{ID: "QvQqBUqZ", Number: "0.14.0", File: provider.File{Filename: "lithium-fabric-0.14.0+mc26.2.jar"}}, envtest.ModJar(t, "lithium", "0.14.0", "*"))
	h.dir = h.newSource("pack", "sodium")
	return h
}

// newSource is a fabric 26.2 client project in a new directory with slug locked.
func (h *harness) newSource(name, slug string) string {
	h.t.Helper()
	dir := filepath.Join(h.t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	m := &manifest.Manifest{Name: name, Minecraft: "26.2", Loader: manifest.Loader{Type: "fabric"}, Providers: []string{"modrinth"}, Requires: map[string]manifest.Require{}, Client: &manifest.Client{}}
	if err := m.Save(filepath.Join(dir, manifest.FileName)); err != nil {
		h.t.Fatal(err)
	}
	l := lock.New()
	l.Minecraft, l.DataVersion = "26.2", 4600
	l.Loader = lock.Loader{Type: "fabric", Version: "0.17.3"}
	l.Java = lock.Java{Major: 25, Component: "java-runtime-epsilon"}
	if err := l.Save(filepath.Join(dir, lock.FileName)); err != nil {
		h.t.Fatal(err)
	}
	if slug != "" {
		h.add(dir, slug)
	}
	return dir
}

// add locks slug in the project at dir, the way `shulker add` does.
func (h *harness) add(dir, slug string) {
	h.t.Helper()
	p, err := project.Open(dir)
	if err != nil {
		h.t.Fatal(err)
	}
	r, err := resolve.New(context.Background(), h.e.Env.Env, p, resolve.PackMode{})
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

func (h *harness) editManifest(dir string, edit func(m *manifest.Manifest)) {
	h.t.Helper()
	m, err := manifest.Load(filepath.Join(dir, manifest.FileName))
	if err != nil {
		h.t.Fatal(err)
	}
	edit(m)
	if err := m.Save(filepath.Join(dir, manifest.FileName)); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) open(dir string) *sync.Source {
	h.t.Helper()
	src, err := sync.Open(context.Background(), h.e.Env, dir, modpack.At{})
	if err != nil {
		h.t.Fatal(err)
	}
	return src
}

func (h *harness) link(entry *launcher.Entry, dir string, req Request) (*Report, error) {
	h.t.Helper()
	if req.Reason == "" {
		req.Reason = "link"
	}
	return Into(context.Background(), h.e, entry, h.open(dir), req)
}

func (h *harness) mustLink(entry *launcher.Entry, dir string, req Request) *Report {
	h.t.Helper()
	rep, err := h.link(entry, dir, req)
	if err != nil {
		h.t.Fatalf("link %s: %v\nlog: %v\nwarnings: %v", entry.Name, err, h.env.Logged, h.env.Warnings)
	}
	return rep
}

func (h *harness) instances() []config.Instance {
	h.t.Helper()
	rows, err := config.LoadInstances(h.e.Registry)
	if err != nil {
		h.t.Fatal(err)
	}
	return rows
}

func (h *harness) manifest(dir string) *manifest.Manifest {
	h.t.Helper()
	m, err := manifest.Load(filepath.Join(dir, manifest.FileName))
	if err != nil {
		h.t.Fatal(err)
	}
	return m
}

func (h *harness) warned(text string) bool {
	return slices.ContainsFunc(h.env.Warnings, func(w string) bool { return strings.Contains(w, text) })
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readINI reads a Prism instance.cfg into a map, unquoting the values Prism quotes.
func readINI(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			value = strings.NewReplacer(`\\`, `\`, `\"`, `"`).Replace(value[1 : len(value)-1])
		}
		values[key] = value
	}
	return values
}
