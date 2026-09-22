package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
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

	if stdout := h.mustRun(t, "config", "set", "curseforge.key", "abcd1234wxyz"); stdout != "  ~ curseforge.key (unset) ⟶ \"••••wxyz\"\n" {
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
	if stdout := h.mustRun(t, "config", "unset", "curseforge.key"); stdout != "  ~ curseforge.key \"••••wxyz\" ⟶ (unset)\n" {
		t.Errorf("unset output = %q", stdout)
	}
	if stdout := h.mustRun(t, "config", "unset", "curseforge.key"); stdout != "  i curseforge.key was not set\n" {
		t.Errorf("second unset output = %q", stdout)
	}
	if doc := readConfigDoc(t, h.config); len(doc) != 2 || doc["extra"] != true || doc["$schema"] == nil {
		t.Errorf("config.json after unset = %v, want only the marker and the unknown key", doc)
	}

	if env := h.runSetting(t, 1, "config", "get", "curseforge.key"); env.Error == nil || env.Error.Code != "path-not-set" {
		t.Errorf("get of an unset key: %+v", env.Error)
	}
	if env := h.runSetting(t, 2, "config", "set", "curseforge.key", ""); env.Error == nil || env.Error.Code != "usage" {
		t.Errorf("set to an empty value: %+v", env.Error)
	}
	env := h.runSetting(t, 1, "config", "set", "curseforge.keys", "x")
	if env.Error == nil || env.Error.Code != "path-invalid" || !slices.Equal(env.Error.Candidates, config.Keys) {
		t.Errorf("set of an unknown key: %+v", env.Error)
	}
}

func TestConfigRegistry(t *testing.T) {
	h := newHarness(t)
	defaultRegistry := filepath.Join(filepath.Dir(h.config), "registry.json")
	friends := `{"$schema": "https://shulker.sh/schema/v1/registry.json", "instances": [{"id": "friends", "name": "Friends", "dir": "/instances/friends", "source": "/pack"}]}`
	if err := os.WriteFile(defaultRegistry, []byte(friends), 0o644); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(t.TempDir(), "shared", "registry.json")
	env := h.runSetting(t, 1, "config", "set", "registry", moved)
	if env.Error == nil || env.Error.Code != "registry-has-instances" || !strings.Contains(env.Error.Message, defaultRegistry) {
		t.Fatalf("moving away from a registry with instances: %+v", env.Error)
	}
	if _, err := os.Stat(moved); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refused set created %s", moved)
	}
	if _, err := os.Stat(h.config); !errors.Is(err, os.ErrNotExist) {
		t.Error("the refused set wrote config.json")
	}

	want := fmt.Sprintf("  ~ registry (unset) ⟶ %q\n  ✔ created %s\n", moved, moved)
	if stdout := h.mustRun(t, "config", "set", "registry", moved, "--force"); stdout != want {
		t.Errorf("set --force output = %q, want %q", stdout, want)
	}
	if data, _ := os.ReadFile(moved); !strings.Contains(string(data), "schema/v1/registry.json") {
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

func TestConfigUnsetRegistryCreatesDefault(t *testing.T) {
	h := newHarness(t)
	defaultRegistry := filepath.Join(filepath.Dir(h.config), "registry.json")
	moved := filepath.Join(t.TempDir(), "registry.json")
	h.mustRun(t, "config", "set", "registry", moved)

	want := fmt.Sprintf("  ~ registry %q ⟶ (unset)\n  ✔ created %s\n", moved, defaultRegistry)
	if stdout := h.mustRun(t, "config", "unset", "registry"); stdout != want {
		t.Errorf("unset output = %q, want %q", stdout, want)
	}
	if data, _ := os.ReadFile(defaultRegistry); !strings.Contains(string(data), "schema/v1/registry.json") {
		t.Errorf("default registry = %q", data)
	}
}

// The three roots are config keys that report the directory behind them, set or not, so a player
// can see where shulker's own instances, their saves and the launch store will land.
func TestConfigRoots(t *testing.T) {
	h := newHarness(t)
	data := t.TempDir()
	t.Setenv("SHULKER_DATA", data)

	for key, want := range map[string]string{
		"instances": filepath.Join(data, "instances"),
		"saves":     filepath.Join(data, "saves"),
		"store":     filepath.Join(h.cache, "game"),
	} {
		if stdout := h.mustRun(t, "config", "get", key); stdout != want+"\n" {
			t.Errorf("unset %s = %q, want %s", key, stdout, want)
		}
	}

	elsewhere := t.TempDir()
	h.mustRun(t, "config", "set", "instances", elsewhere)
	if stdout := h.mustRun(t, "config", "get", "instances"); stdout != elsewhere+"\n" {
		t.Errorf("set instances = %q, want %s", stdout, elsewhere)
	}
	// A relative value resolves against config.json's own directory, the rule `registry` follows.
	h.mustRun(t, "config", "set", "saves", "worlds")
	want := filepath.Join(filepath.Dir(h.config), "worlds")
	if stdout := h.mustRun(t, "config", "get", "saves"); stdout != want+"\n" {
		t.Errorf("relative saves = %q, want %s", stdout, want)
	}

	var all struct {
		Instances string `json:"instances"`
		Saves     string `json:"saves"`
		Store     string `json:"store"`
	}
	if err := json.Unmarshal(h.runSetting(t, 0, "config", "get").Data, &all); err != nil {
		t.Fatal(err)
	}
	if all.Instances != elsewhere || all.Saves != want || all.Store != filepath.Join(h.cache, "game") {
		t.Errorf("get with no key = %+v", all)
	}

	h.mustRun(t, "config", "unset", "instances")
	if stdout := h.mustRun(t, "config", "get", "instances"); stdout != filepath.Join(data, "instances")+"\n" {
		t.Errorf("unset instances = %q, want the default back", stdout)
	}

	// `instances` held the registry itself before the split, as an array. It is a path now, and
	// shulker keeps no reader for the shapes it wrote before release.
	if err := os.WriteFile(h.config, []byte(`{"instances":[{"dir":"/old"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if env := h.runSetting(t, 1, "config", "get", "instances"); env.Error == nil || env.Error.Code != "config-invalid" {
		t.Errorf("an instances array: %+v", env.Error)
	}
}

func TestConfigItCantRead(t *testing.T) {
	for _, tc := range []struct{ name, config, code string }{
		{"corrupt", `{"curseforge":`, "config-invalid"},
		{"foreign", `{"$schema":"https://example.com/config.json"}`, "config-invalid"},
		{"no marker", `{"curseforge":{"key":"abc"}}`, "config-invalid"},
		{"newer", `{"$schema":"https://shulker.sh/schema/v2/config.json"}`, "schema-newer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if err := os.WriteFile(h.config, []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}

			var stderr bytes.Buffer
			a := h.newApp(io.Discard, &stderr)
			a.d = nil
			if _, err := a.deps(); err != nil {
				t.Fatalf("deps over the config: %v", err)
			}
			want := "; ignoring it\n"
			if tc.code == "schema-newer" {
				want = "; ignoring it. Run shulker self update to read it\n"
			}
			if !strings.HasSuffix(stderr.String(), want) {
				t.Fatalf("deps warned %q", stderr.String())
			}

			env := h.runEnvelope(t, 1, "config", "get", "store")
			if env.Error == nil || env.Error.Code != tc.code {
				t.Fatalf("config get: %+v", env.Error)
			}

			env = h.runEnvelope(t, 0, "config", "set", "store", "elsewhere")
			if len(env.Warnings) != 1 || !strings.HasSuffix(env.Warnings[0], "; replaced it and kept the old one as "+h.config+".replaced") {
				t.Fatalf("config set warnings: %q", env.Warnings)
			}
			if data, _ := os.ReadFile(h.config + ".replaced"); string(data) != tc.config {
				t.Fatalf("replaced = %q", data)
			}
			doc := readConfigDoc(t, h.config)
			if len(doc) != 2 || doc["store"] != "elsewhere" || doc["$schema"] != "https://shulker.sh/schema/v1/config.json" {
				t.Fatalf("new config: %v", doc)
			}

			h.mustRun(t, "config", "set", "store", "again")
			if data, _ := os.ReadFile(h.config + ".replaced"); string(data) != tc.config {
				t.Fatalf("a readable config was replaced again: %q", data)
			}
		})
	}
}

func TestConfigGetHidesTheMarker(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "config", "set", "store", "elsewhere")
	var all map[string]any
	if err := json.Unmarshal(h.runSetting(t, 0, "config", "get").Data, &all); err != nil {
		t.Fatal(err)
	}
	if _, ok := all["$schema"]; ok {
		t.Errorf("config get: %v", all)
	}
}

func (h *harness) runEnvelope(t *testing.T, wantExit int, args ...string) out.Envelope {
	t.Helper()
	code, stdout, stderr := h.run(t, append(args, "--json")...)
	if code != wantExit {
		t.Fatalf("%v exited %d, want %d\nstdout: %s\nstderr: %s", args, code, wantExit, stdout, stderr)
	}
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, stdout)
	}
	return env
}
