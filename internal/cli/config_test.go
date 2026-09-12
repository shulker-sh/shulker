package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func readConfigDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}

func TestConfigCurseForgeKey(t *testing.T) {
	t.Setenv("SHULKER_CURSEFORGE_KEY", "")
	h := newHarness(t)

	if stdout := h.mustRun(t, "config", "set", "curseforge.key", "abcd1234wxyz"); stdout != "curseforge.key: (unset) -> \"••••wxyz\"\n" {
		t.Fatalf("set output = %q", stdout)
	}
	info, err := os.Stat(h.config)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("new config.json mode = %o, want 600", mode)
	}
	if stdout := h.mustRun(t, "config", "get", "curseforge.key"); stdout != "••••wxyz\n" {
		t.Errorf("get output = %q", stdout)
	}
	if stdout := h.mustRun(t, "config", "get", "curseforge.key", "--reveal"); stdout != "abcd1234wxyz\n" {
		t.Errorf("get --reveal output = %q", stdout)
	}
	if env := h.runSetting(t, 0, "config", "get", "curseforge.key"); string(env.Data) != `"••••wxyz"` {
		t.Errorf("get --json data = %s", env.Data)
	}

	var all struct {
		CurseForge struct {
			Key string `json:"key"`
		} `json:"curseforge"`
		Registry string `json:"registry"`
	}
	if err := json.Unmarshal(h.runSetting(t, 0, "config", "get").Data, &all); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(filepath.Dir(h.config), "registry.json"); all.CurseForge.Key != "••••wxyz" || all.Registry != want {
		t.Errorf("get with no key = %+v, want the masked key and registry %s", all, want)
	}

	doc := readConfigDoc(t, h.config)
	doc["extra"] = true
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(h.config, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if stdout := h.mustRun(t, "config", "unset", "curseforge.key"); stdout != "curseforge.key: \"••••wxyz\" -> (unset)\n" {
		t.Errorf("unset output = %q", stdout)
	}
	if stdout := h.mustRun(t, "config", "unset", "curseforge.key"); stdout != "curseforge.key was not set.\n" {
		t.Errorf("second unset output = %q", stdout)
	}
	if doc := readConfigDoc(t, h.config); len(doc) != 1 || doc["extra"] != true {
		t.Errorf("config.json after unset = %v, want only the unknown key", doc)
	}

	if env := h.runSetting(t, 1, "config", "get", "curseforge.key"); env.Error == nil || env.Error.Code != "path-not-set" {
		t.Errorf("get of an unset key: %+v", env.Error)
	}
	if env := h.runSetting(t, 2, "config", "set", "curseforge.key", ""); env.Error == nil || env.Error.Code != "usage" {
		t.Errorf("set to an empty value: %+v", env.Error)
	}
	env := h.runSetting(t, 1, "config", "set", "curseforge.keys", "x")
	if env.Error == nil || env.Error.Code != "path-invalid" || !slices.Equal(env.Error.Candidates, []string{"curseforge.key", "registry"}) {
		t.Errorf("set of an unknown key: %+v", env.Error)
	}
}

func TestConfigRegistry(t *testing.T) {
	h := newHarness(t)
	defaultRegistry := filepath.Join(filepath.Dir(h.config), "registry.json")
	friends := `{"links": [{"side": "client", "name": "Friends", "dir": "/instances/friends", "source": "/pack", "target": "client"}]}`
	if err := os.WriteFile(defaultRegistry, []byte(friends), 0o644); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(t.TempDir(), "shared", "registry.json")
	env := h.runSetting(t, 1, "config", "set", "registry", moved)
	if env.Error == nil || env.Error.Code != "registry-has-links" || !strings.Contains(env.Error.Message, defaultRegistry) {
		t.Fatalf("moving away from a registry with links: %+v", env.Error)
	}
	if _, err := os.Stat(moved); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refused set created %s", moved)
	}
	if _, err := os.Stat(h.config); !errors.Is(err, os.ErrNotExist) {
		t.Error("the refused set wrote config.json")
	}

	want := fmt.Sprintf("registry: (unset) -> %q\ncreated %s\n", moved, moved)
	if stdout := h.mustRun(t, "config", "set", "registry", moved, "--force"); stdout != want {
		t.Errorf("set --force output = %q, want %q", stdout, want)
	}
	if data, _ := os.ReadFile(moved); string(data) != "{}\n" {
		t.Errorf("created registry = %q", data)
	}
	if stdout := h.mustRun(t, "config", "get", "registry"); stdout != moved+"\n" {
		t.Errorf("get registry = %q", stdout)
	}

	h.mustRun(t, "config", "unset", "registry")

	empty := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if stdout := h.mustRun(t, "config", "set", "registry", empty, "--force"); strings.Contains(stdout, "created") {
		t.Errorf("an existing empty registry was reported as created: %q", stdout)
	}
	h.mustRun(t, "config", "unset", "registry")

	notes := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notes, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if env := h.runSetting(t, 1, "config", "set", "registry", notes, "--force"); env.Error == nil || env.Error.Code != "registry-invalid" {
		t.Errorf("pointing at a file that isn't a registry: %+v", env.Error)
	}

	copied := filepath.Join(t.TempDir(), "copy.json")
	if err := os.WriteFile(copied, []byte(friends), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "config", "set", "registry", copied)
}
