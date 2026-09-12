package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestUpdateLinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shulker", RegistryFileName)
	put := func(l Link) func([]Link) []Link {
		return func(links []Link) []Link {
			if i, ok := FindLink(links, l.Dir); ok {
				links[i] = l
				return links
			}
			return append(links, l)
		}
	}
	a := Link{Launcher: "prism", LauncherDir: "/l", Side: "client", Name: "My Pack", Dir: "/i/minecraft", Source: "/p", Target: "client"}

	if changed, err := UpdateLinks(path, func(l []Link) []Link { return l }); err != nil || changed {
		t.Fatalf("no-op on a missing file: %v %v", changed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a no-op must not create the file: %v", err)
	}
	if changed, err := UpdateLinks(path, put(a)); err != nil || !changed {
		t.Fatalf("add: %v %v", changed, err)
	}
	if links, err := LoadLinks(path); err != nil || len(links) != 1 || links[0] != a {
		t.Fatalf("after add: %+v %v", links, err)
	}

	os.WriteFile(path, []byte(`{"future":[1,2],"links":[]}`), 0o644)
	b := Link{Side: "server", Name: "Server", Dir: "/srv", Source: "https://example.com/p.git", Target: "server", Ref: "main"}
	if changed, err := UpdateLinks(path, put(b)); err != nil || !changed {
		t.Fatalf("add beside unknown keys: %v %v", changed, err)
	}
	if changed, err := UpdateLinks(path, put(b)); err != nil || changed {
		t.Fatalf("an unchanged entry must not rewrite the file: %v %v", changed, err)
	}
	if i, ok := FindLink([]Link{a, b}, "/srv/"); !ok || i != 1 {
		t.Fatalf("FindLink cleans the dir: %d %v", i, ok)
	}
	var top map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &top); err != nil || top["future"] == nil {
		t.Fatalf("unknown keys lost: %s", data)
	}

	if changed, err := UpdateLinks(path, func([]Link) []Link { return nil }); err != nil || !changed {
		t.Fatalf("remove all: %v %v", changed, err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "links") {
		t.Fatalf("an empty registry drops the key: %s", data)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}

	os.WriteFile(path, []byte(`{`), 0o644)
	if _, err := LoadLinks(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("a broken registry names its file: %v", err)
	}
}
