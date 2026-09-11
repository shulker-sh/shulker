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

	os.WriteFile(path, []byte(`{"curseforge":{"key":"abc"}}`), 0o600)
	cfg, err = Load()
	if err != nil || cfg.CurseForge.Key != "abc" {
		t.Fatalf("load: %+v %v", cfg, err)
	}

	os.WriteFile(path, []byte(`{`), 0o600)
	if _, err := Load(); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestUpdateLinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shulker", "config.json")
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
	if cfg, err := LoadFile(path); err != nil || len(cfg.Links) != 1 || cfg.Links[0] != a {
		t.Fatalf("after add: %+v %v", cfg, err)
	}

	os.WriteFile(path, []byte(`{"curseforge":{"key":"abc"},"future":[1,2],"links":[]}`), 0o600)
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
	if err := json.Unmarshal(data, &top); err != nil || top["future"] == nil || top["curseforge"] == nil {
		t.Fatalf("unknown keys lost: %s", data)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600 since the file can hold an API key", info.Mode().Perm())
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
}
