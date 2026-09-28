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

	os.WriteFile(path, []byte(`{"$schema":"https://shulker.sh/schema/v1/config.json","curseforge":{"key":"abc"},"registry":"instances.json","links":[{"dir":"/old"}]}`), 0o600)
	cfg, err = Load()
	if err != nil || cfg.CurseForge.Key != "abc" || cfg.Registry != "instances.json" {
		t.Fatalf("load: %+v %v", cfg, err)
	}

	os.WriteFile(path, []byte(`{`), 0o600)
	if _, err := Load(); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestConfigMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, c := range []struct{ name, data, code string }{
		{"current", `{"$schema":"https://shulker.sh/schema/v1/config.json","unknown":1}`, ""},
		{"newer", `{"$schema":"https://shulker.sh/schema/v2/config.json"}`, "schema-newer"},
		{"foreign", `{"$schema":"https://example.com/config.json"}`, "config-invalid"},
		{"no marker", `{"curseforge":{"key":"abc"}}`, "config-invalid"},
	} {
		os.WriteFile(path, []byte(c.data), 0o600)
		_, err := LoadFile(path)
		_, docErr := LoadDocument(path)
		if out.CodeOf(err) != c.code || out.CodeOf(docErr) != c.code {
			t.Errorf("%s: LoadFile %v, LoadDocument %v, want %q", c.name, err, docErr, c.code)
		}
		if c.code == "config-invalid" && out.AsError(err).Help != "run `shulker config set <key> <value>` to start a new one" {
			t.Errorf("%s: help %q", c.name, out.AsError(err).Help)
		}
	}
}

func TestSaveDocumentMarks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveDocument(path, map[string]any{"store": "x"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "{\n  \"$schema\": \"https://shulker.sh/schema/v1/config.json\"") {
		t.Errorf("saved %s", data)
	}
}

func TestReplaceDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"curseforge":`), 0o600)
	kept, err := ReplaceDocument(path, map[string]any{"store": "x"})
	if err != nil || kept != path+".replaced" {
		t.Fatalf("kept %q, %v", kept, err)
	}
	if data, _ := os.ReadFile(kept); string(data) != `{"curseforge":` {
		t.Errorf("replaced = %q", data)
	}
	cfg, err := LoadFile(path)
	if err != nil || cfg.Store != "x" {
		t.Errorf("new config: %+v %v", cfg, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
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

func TestRedactMasksEachSecretKeyAndLeavesTheRestAlone(t *testing.T) {
	doc := map[string]any{
		"curseforge": map[string]any{"key": "abcdef1234", "other": "kept"},
		"instances":  "/data/instances",
	}
	got := Redact(doc, func(s string) string { return "masked" })
	cf := got["curseforge"].(map[string]any)
	if cf["key"] != "masked" || cf["other"] != "kept" || got["instances"] != "/data/instances" {
		t.Fatalf("redacted = %v", got)
	}
	if doc["curseforge"].(map[string]any)["key"] != "abcdef1234" {
		t.Fatal("the document given is not written to")
	}
	if !IsSecret("curseforge.key") || IsSecret("instances") {
		t.Fatal("curseforge.key is the secret")
	}
	bare := map[string]any{"curseforge": map[string]any{"key": 5}, "registry": "r"}
	if got := Redact(bare, func(string) string { return "masked" }); got["curseforge"].(map[string]any)["key"] != 5 {
		t.Fatalf("a secret that isn't text is left as it is: %v", got)
	}
}

func TestCheckDocumentIsTheSchemasVerdict(t *testing.T) {
	if err := CheckDocument(map[string]any{"instances": "/data/instances"}); err != nil {
		t.Fatalf("a valid document passes: %v", err)
	}
	if err := CheckDocument(map[string]any{"instances": 5}); err == nil {
		t.Fatal("a value of the wrong type fails")
	}
}

func TestRecordSyncKeepsTheLastGoodStampOnFailure(t *testing.T) {
	in := Instance{LastSync: "then"}
	in.RecordSync("now", "")
	if in.LastSync != "now" || in.LastError != "" {
		t.Fatalf("ok = %+v", in)
	}
	in.RecordSync("later", "boom")
	if in.LastSync != "now" || in.LastError != "boom" {
		t.Fatalf("failed = %+v", in)
	}
	in.RecordSync("latest", "")
	if in.LastSync != "latest" || in.LastError != "" {
		t.Fatalf("recovered = %+v", in)
	}
}

func TestDownloadsWatchedReadsTildeAsHome(t *testing.T) {
	home := filepath.FromSlash("/home/steve")
	if got := (Downloads{}).Watched(home); !slices.Equal(got, []string{filepath.Join(home, "Downloads")}) {
		t.Fatalf("unset = %v", got)
	}
	if got := (Downloads{Watch: &[]string{}}).Watched(home); len(got) != 0 {
		t.Fatalf("[] watches nothing: %v", got)
	}
	list := []string{"~", "~/Mods", "/srv/drop", "~steve"}
	want := []string{home, home + "/Mods", "/srv/drop", "~steve"}
	if got := (Downloads{Watch: &list}).Watched(home); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
