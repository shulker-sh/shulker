package manifest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	for _, name := range []string{"two-target.json", "minimal.json"} {
		m, err := Load(filepath.Join("..", "..", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		data, err := m.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(data); err != nil {
			t.Fatalf("%s: re-parse: %v", name, err)
		}
	}
}

func TestSaveKeepsEverySchemaProperty(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "every-field.json"))
	if err != nil {
		t.Fatal(err)
	}
	rawSchema, err := os.ReadFile(filepath.Join("..", "..", "schema", "v1", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := decodeJSON(t, rawSchema).(map[string]any)
	doc := decodeJSON(t, data)

	want, have := map[string]bool{}, map[string]bool{}
	schemaCoverage(root, root, doc, "", want, have)
	var missing []string
	for p := range want {
		if !have[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		t.Fatalf("every-field.json doesn't set %s", strings.Join(missing, ", "))
	}

	m, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeJSON(t, encoded); !reflect.DeepEqual(got, doc) {
		t.Fatalf("a save changed the manifest:\n%s", encoded)
	}
}

func decodeJSON(t *testing.T, data []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// schemaCoverage records in want every property path the schema allows and in
// have the ones doc sets. Map-like objects (additionalProperties with a schema)
// only descend into their values, so their listed known keys aren't required.
func schemaCoverage(root, node map[string]any, doc any, path string, want, have map[string]bool) {
	if ref, ok := node["$ref"].(string); ok {
		node = root["$defs"].(map[string]any)[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		branches, _ := node[key].([]any)
		for _, branch := range branches {
			schemaCoverage(root, branch.(map[string]any), doc, path, want, have)
		}
	}
	obj, _ := doc.(map[string]any)
	if values, ok := node["additionalProperties"].(map[string]any); ok {
		for _, v := range obj {
			schemaCoverage(root, values, v, path+".*", want, have)
		}
		if len(obj) == 0 {
			schemaCoverage(root, values, nil, path+".*", want, have)
		}
		return
	}
	props, _ := node["properties"].(map[string]any)
	for key, prop := range props {
		want[path+"."+key] = true
		v, ok := obj[key]
		if ok {
			have[path+"."+key] = true
		}
		schemaCoverage(root, prop.(map[string]any), v, path+"."+key, want, have)
	}
	if items, ok := node["items"].(map[string]any); ok {
		elems, _ := doc.([]any)
		for _, e := range elems {
			schemaCoverage(root, items, e, path+"[]", want, have)
		}
		if len(elems) == 0 {
			schemaCoverage(root, items, nil, path+"[]", want, have)
		}
	}
}

func TestSaveRefusesInvalid(t *testing.T) {
	m := &Manifest{Name: "x", Minecraft: "26.2", Loader: Loader{Type: "fabric", Version: "*"}, Targets: map[string]Target{}, Mods: map[string]Mod{}}
	if err := m.Save(filepath.Join(t.TempDir(), FileName)); err == nil {
		t.Fatal("expected schema error for empty targets")
	}
	m.Targets["client"] = Target{Side: "client", Overrides: []string{"overrides"}, Build: "build/client"}
	path := filepath.Join(t.TempDir(), FileName)
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestConditionsRoundTrip(t *testing.T) {
	m, err := Parse([]byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"targets":{"client":{"side":"client","overrides":["overrides"],"features":["fancy"]}},"mods":{"aa":{"os":"macos"},"bb":{"feature":["fancy","!shaders"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Mods["aa"].OS) != 1 || m.Mods["aa"].OS[0] != "macos" || len(m.Mods["bb"].Feature) != 2 || m.Targets["client"].Features[0] != "fancy" {
		t.Fatalf("parsed conditions: %+v", m.Mods)
	}
	data, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, `"os": "macos"`) || !strings.Contains(s, "\"feature\": [\n") {
		t.Fatalf("encoded conditions: %s", s)
	}
}
