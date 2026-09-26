package play

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/saves"
	"shulker.sh/shulker/internal/sync"
)

const (
	notchID = "069a79f4-44e9-4726-a5be-fca90e38aaf5"
	steveID = "8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21"
	jebID   = "0e05d36c-9cbd-4b0a-ae4e-7b2e2b7eb1f4"
)

// harness is a play env on fakes with one registered shulker instance, "pack": an in-place client
// project on Minecraft 26.2, on the loader named or none.
type harness struct {
	t   *testing.T
	env *envtest.Env
	e   *Env
	in  config.Instance
}

func newHarness(t *testing.T, loaderType string) *harness {
	t.Helper()
	h := &harness{t: t, env: envtest.New(t)}
	data := t.TempDir()
	cfg := filepath.Join(data, "config.json")
	se := &sync.Env{Env: h.env.Env, Registry: filepath.Join(data, "registry.json"), Saves: saves.Roots{Saves: filepath.Join(data, "saves"), Backups: filepath.Join(data, "backups")}, SaveBackups: config.DefaultSaveBackups}
	h.e = &Env{
		Env:     se,
		Store:   game.Store{Root: filepath.Join(data, "store"), Resources: h.env.Piston.URL() + "/resources"},
		Config:  cfg,
		SignIn:  account.NewSignIn(h.env.Fetch),
		Version: "test",
	}
	dir := filepath.Join(data, "instances", "pack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Name: "pack", Minecraft: "26.2", Providers: []string{"modrinth"}, Requires: map[string]manifest.Require{}, Client: &manifest.Client{Build: "."}}
	l := lock.New()
	l.Minecraft, l.DataVersion = "26.2", 4600
	l.Java = lock.Java{Major: 25, Component: "java-runtime-epsilon"}
	if loaderType != "" {
		m.Loader = manifest.Loader{Type: loaderType}
		l.Loader = lock.Loader{Type: loaderType, Version: envtest.Versions("0.17.3")[0].Version}
	}
	if err := m.Save(filepath.Join(dir, manifest.FileName)); err != nil {
		t.Fatal(err)
	}
	if err := l.Save(filepath.Join(dir, lock.FileName)); err != nil {
		t.Fatal(err)
	}
	if err := instance.New().Save(dir); err != nil {
		t.Fatal(err)
	}
	h.in = config.Instance{ID: "pack", Name: "pack", Launcher: "shulker", Dir: dir, Source: dir}
	if _, err := config.UpdateInstances(se.Registry, func([]config.Instance) []config.Instance { return []config.Instance{h.in} }); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) assemble(req Request) (*Plan, error) {
	h.t.Helper()
	return Assemble(context.Background(), h.e, h.in, req)
}

func (h *harness) mustAssemble(req Request) *Plan {
	h.t.Helper()
	plan, err := h.assemble(req)
	if err != nil {
		h.t.Fatalf("assemble: %v\nlog: %v\nwarnings: %v", err, h.env.Logged, h.env.Warnings)
	}
	return plan
}

// setConfig writes one dotted key into config.json.
func (h *harness) setConfig(section, key string, value any) {
	h.t.Helper()
	doc, err := config.LoadDocument(h.e.Config)
	if err != nil {
		h.t.Fatal(err)
	}
	block, _ := doc[section].(map[string]any)
	if block == nil {
		block = map[string]any{}
	}
	block[key] = value
	doc[section] = block
	if err := config.SaveDocument(h.e.Config, doc); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) editInstance(edit func(f *instance.File)) {
	h.t.Helper()
	f, err := instance.Load(h.in.Dir)
	if err != nil {
		h.t.Fatal(err)
	}
	edit(f)
	if err := f.Save(h.in.Dir); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) editManifest(edit func(m *manifest.Manifest)) {
	h.t.Helper()
	m, err := manifest.Load(filepath.Join(h.in.Dir, manifest.FileName))
	if err != nil {
		h.t.Fatal(err)
	}
	edit(m)
	if err := m.Save(filepath.Join(h.in.Dir, manifest.FileName)); err != nil {
		h.t.Fatal(err)
	}
}

// accounts writes shulker's own store beside config.json.
func (h *harness) accounts(accounts ...account.Account) {
	h.t.Helper()
	if err := account.Save(account.Path(h.e.Config), account.Store{Accounts: accounts}); err != nil {
		h.t.Fatal(err)
	}
}

func ownAccount(name, id string) account.Account {
	return account.Account{Type: account.Microsoft, Profile: &account.Profile{ID: id, Name: name}, RefreshToken: "r-" + id}
}

func offlineAccount(name, id string) account.Account {
	return account.Account{Type: account.Offline, Profile: &account.Profile{ID: id, Name: name}}
}

func (h *harness) warned(text string) bool {
	return slices.ContainsFunc(h.env.Warnings, func(w string) bool { return strings.Contains(w, text) })
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
