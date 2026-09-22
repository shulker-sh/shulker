package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv(PathEnv, path)

	cfg, err := Load()
	if err != nil || cfg.CurseForge.Key != "" {
		t.Fatalf("missing file: %+v %v", cfg, err)
	}

	os.WriteFile(path, []byte(`{"curseforge":{"key":"abc"},"registry":"instances.json","links":[{"dir":"/old"}]}`), 0o600)
	cfg, err = Load()
	if err != nil || cfg.CurseForge.Key != "abc" || cfg.Registry != "instances.json" {
		t.Fatalf("load: %+v %v", cfg, err)
	}

	os.WriteFile(path, []byte(`{`), 0o600)
	if _, err := Load(); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestRegistryPath(t *testing.T) {
	configPath := filepath.Join(string(filepath.Separator)+"c", "shulker", "config.json")
	elsewhere := filepath.Join(string(filepath.Separator)+"elsewhere", "registry.json")
	for _, c := range []struct{ registry, want string }{
		{"", filepath.Join(filepath.Dir(configPath), "registry.json")},
		{"instances.json", filepath.Join(filepath.Dir(configPath), "instances.json")},
		{filepath.Join("..", "shared", "registry.json"), filepath.Join(string(filepath.Separator)+"c", "shared", "registry.json")},
		{elsewhere, elsewhere},
	} {
		if got := RegistryPath(configPath, Config{Registry: c.registry}); got != c.want {
			t.Errorf("registry %q: got %s, want %s", c.registry, got, c.want)
		}
	}
}

func TestUpdateInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shulker", RegistryFileName)
	put := func(in Instance) func([]Instance) []Instance {
		return func(instances []Instance) []Instance {
			if i, ok := FindInstance(instances, in.Dir); ok {
				instances[i] = in
				return instances
			}
			return append(instances, in)
		}
	}
	a := Instance{ID: "my-pack", Launcher: "prism", LauncherDir: "/l", Name: "My Pack", Dir: "/i/minecraft", Source: "/p"}

	if changed, err := UpdateInstances(path, func(in []Instance) []Instance { return in }); err != nil || changed {
		t.Fatalf("no-op on a missing file: %v %v", changed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a no-op must not create the file: %v", err)
	}
	if changed, err := UpdateInstances(path, put(a)); err != nil || !changed {
		t.Fatalf("add: %v %v", changed, err)
	}
	if instances, err := LoadInstances(path); err != nil || len(instances) != 1 || instances[0] != a {
		t.Fatalf("after add: %+v %v", instances, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), RegistrySchemaURL) {
		t.Fatalf("a registry shulker writes names its schema: %s", data)
	}

	os.WriteFile(path, []byte(`{"$schema":"`+RegistrySchemaURL+`","future":[1,2],"instances":[]}`), 0o644)
	b := Instance{ID: "server", Name: "Server", Dir: "/srv", Source: "https://example.com/p.git"}
	if changed, err := UpdateInstances(path, put(b)); err != nil || !changed {
		t.Fatalf("add beside unknown keys: %v %v", changed, err)
	}
	if changed, err := UpdateInstances(path, put(b)); err != nil || changed {
		t.Fatalf("an unchanged instance must not rewrite the file: %v %v", changed, err)
	}
	if i, ok := FindInstance([]Instance{a, b}, "/srv/"); !ok || i != 1 {
		t.Fatalf("FindInstance cleans the dir: %d %v", i, ok)
	}
	if i, ok := FindID([]Instance{a, b}, "server"); !ok || i != 1 {
		t.Fatalf("FindID: %d %v", i, ok)
	}
	var top map[string]any
	data, _ = os.ReadFile(path)
	if err := json.Unmarshal(data, &top); err != nil || top["future"] == nil {
		t.Fatalf("unknown keys lost: %s", data)
	}

	if changed, err := UpdateInstances(path, func([]Instance) []Instance { return nil }); err != nil || !changed {
		t.Fatalf("remove all: %v %v", changed, err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "instances") {
		t.Fatalf("an empty registry drops the key: %s", data)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}

	os.WriteFile(path, []byte(`{`), 0o644)
	if _, err := LoadInstances(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("a broken registry names its file: %v", err)
	}
}

// A registry from before instances had ids fails outright, since shulker keeps no compatibility
// code for the shapes it wrote before release; `instances repair` is the way back.
func TestOldRegistryFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), RegistryFileName)
	os.WriteFile(path, []byte(`{"links":[{"side":"client","name":"old","dir":"/old","source":"/old","target":"client"}]}`), 0o644)
	_, err := LoadInstances(path)
	if err == nil || !strings.Contains(out.AsError(err).Help, "instances repair") {
		t.Fatalf("an old registry points at repair: %v", err)
	}

	os.WriteFile(path, []byte(`{"$schema":"https://example.com/registry.json","instances":[]}`), 0o644)
	if _, err := LoadInstances(path); err == nil || out.AsError(err).Code != "registry-invalid" || !strings.Contains(err.Error(), "names the schema https://example.com/registry.json, which this shulker doesn't know") {
		t.Fatalf("a schema shulker doesn't know names it: %v", err)
	}

	// Repair would write a newer registry back in this shulker's shape, so it asks for an update instead.
	os.WriteFile(path, []byte(`{"$schema":"https://shulker.sh/schema/v2/registry.json","instances":[]}`), 0o644)
	if _, err := LoadInstances(path); err == nil || out.AsError(err).Code != "schema-newer" || out.AsError(err).Nudge.Command != "shulker self update" {
		t.Fatalf("a newer registry nudges self update: %v", err)
	}

	if _, err := WriteInstances(path, []Instance{{ID: "a", Dir: "/a", Source: "/p"}}); err != nil {
		t.Fatal(err)
	}
	if instances, err := LoadInstances(path); err != nil || len(instances) != 1 {
		t.Fatalf("repair replaces a registry it can't read: %+v %v", instances, err)
	}
}

func TestRoot(t *testing.T) {
	configPath := filepath.Join(string(filepath.Separator)+"c", "shulker", "config.json")
	fallback := filepath.Join(string(filepath.Separator)+"d", "shulker", "instances")
	elsewhere := filepath.Join(string(filepath.Separator)+"elsewhere", "instances")
	for _, c := range []struct{ value, want string }{
		{"", fallback},
		{"instances", filepath.Join(filepath.Dir(configPath), "instances")},
		{filepath.Join("..", "shared", "instances"), filepath.Join(string(filepath.Separator)+"c", "shared", "instances")},
		{elsewhere, elsewhere},
	} {
		if got := Root(configPath, c.value, fallback); got != c.want {
			t.Errorf("root %q: got %s, want %s", c.value, got, c.want)
		}
	}
}

func TestDataDir(t *testing.T) {
	t.Setenv(DataPathEnv, filepath.Join(string(filepath.Separator)+"scratch", "data"))
	dir, err := DataDir()
	if err != nil || dir != filepath.Join(string(filepath.Separator)+"scratch", "data") {
		t.Fatalf("the environment wins: %s %v", dir, err)
	}

	t.Setenv(DataPathEnv, "")
	dir, err = DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "shulker" || !filepath.IsAbs(dir) {
		t.Fatalf("a data dir is an absolute path ending in shulker: %s", dir)
	}
}

func TestRootsAreKeys(t *testing.T) {
	for _, key := range []string{"instances", "saves", "store"} {
		if !slices.Contains(Keys, key) {
			t.Errorf("%s is not a config key", key)
		}
	}
}
