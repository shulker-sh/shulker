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

	"shulker.sh/shulker/internal/out"
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
	m := &Manifest{Name: "x", Minecraft: "26.2", Loader: Loader{Type: "fabric", Version: "*"}, Targets: map[string]Target{}, Requires: map[string]Require{}}
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
	m, err := Parse([]byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"targets":{"client":{"side":"client","overrides":["overrides"],"features":["fancy"]}},"requires":{"aa":{"os":"macos"},"bb":{"feature":["fancy","!shaders"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Mods()["aa"].OS) != 1 || m.Mods()["aa"].OS[0] != "macos" || len(m.Mods()["bb"].Feature) != 2 || m.Targets["client"].Features[0] != "fancy" {
		t.Fatalf("parsed conditions: %+v", m.Mods())
	}
	data, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, `"os": "macos"`) || !strings.Contains(s, "\"feature\": [\n") {
		t.Fatalf("encoded conditions: %s", s)
	}
}

func TestRequiresEntryKinds(t *testing.T) {
	doc := func(entries string) []byte {
		return []byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"targets":{"client":{"side":"client","overrides":["overrides"]}},"requires":{` + entries + `}}`)
	}
	for _, entries := range []string{
		`"base":{"source":"../base","pin":"AANobbMI"}`,
		`"base":{"source":"../base","type":"mod"}`,
		`"sodium":{"ref":"main"}`,
		`"extras":{"file":"extras.zip","provider":"modrinth"}`,
		`"Sodium":{}`,
	} {
		if _, err := Parse(doc(entries)); err == nil {
			t.Errorf("%s should be invalid", entries)
		}
	}
	m, err := Parse(doc(`"base":{"source":"../base","ref":"main","autoUpdate":false},"sodium":{"channel":"beta"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Modpacks()["base"]; !ok || len(m.Modpacks()) != 1 || len(m.Mods()) != 1 || m.Mods()["sodium"].Channel != "beta" {
		t.Fatalf("mods %v, modpacks %v", m.Mods(), m.Modpacks())
	}
	if err := m.CheckSupported(); err != nil {
		t.Fatal(err)
	}
	packs, err := Parse(doc(`"fresh":{"type":"resourcepack"},"complementary":{"type":"shader"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := packs.CheckSupported(); err != nil {
		t.Fatal(err)
	}
	if _, ok := packs.ResourcePacks()["fresh"]; !ok || len(packs.ResourcePacks()) != 1 {
		t.Errorf("resource packs %v", packs.ResourcePacks())
	}
	if _, ok := packs.Shaders()["complementary"]; !ok || len(packs.Shaders()) != 1 {
		t.Errorf("shaders %v", packs.Shaders())
	}
	for _, entries := range []string{
		`"extras":{"type":"mod","file":"mods/extras.jar"}`,
		`"cozy":{"type":"modpack","provider":"modrinth"}`,
	} {
		m, err := Parse(doc(entries))
		if err != nil {
			t.Fatalf("%s: %v", entries, err)
		}
		if err := m.CheckSupported(); err == nil || !strings.Contains(err.Error(), "aren't supported yet") {
			t.Errorf("%s: CheckSupported = %v", entries, err)
		}
		if len(m.Mods()) != 0 || len(m.Modpacks()) != 0 {
			t.Errorf("%s: unsupported entry counted as mod or modpack", entries)
		}
	}
}

func TestFeatureOverridesForms(t *testing.T) {
	doc := func(features string) []byte {
		return []byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"features":{` + features + `},"targets":{"client":{"side":"client","overrides":["overrides"]}},"requires":{}}`)
	}
	m, err := Parse(doc(`"shaders":{"default":true,"overrides":"extras/shaders"},"voice":{"overrides":{"client":"voice-client"}},"minimap":{}`))
	if err != nil {
		t.Fatal(err)
	}
	if f := m.Features["shaders"]; !f.Default || f.Overrides.Both != "extras/shaders" {
		t.Errorf("string form: %+v", f)
	}
	if f := m.Features["voice"]; f.Overrides.Client != "voice-client" || f.Overrides.Server != "" || f.Overrides.Both != "" {
		t.Errorf("object form: %+v", f)
	}
	if f := m.Features["minimap"]; f.Overrides != (FeatureOverrides{}) {
		t.Errorf("no folder declared: %+v", f)
	}
	data, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, `"overrides": "extras/shaders"`) || !strings.Contains(s, `"client": "voice-client"`) || strings.Contains(s, `"minimap": {"overrides"`) {
		t.Fatalf("encoded features: %s", s)
	}
	if _, err := Parse(data); err != nil {
		t.Fatal(err)
	}
	for _, features := range []string{`"shaders":{"overrides":{}}`, `"shaders":{"overrides":["a"]}`, `"shaders":{"overrides":{"both":"a"}}`} {
		if _, err := Parse(doc(features)); err == nil {
			t.Errorf("%s should be invalid", features)
		}
	}
}

func TestSideHelpers(t *testing.T) {
	m, err := Parse([]byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"targets":{"client":{"side":"client","overrides":["overrides"]}},"requires":{},"variables":{"motd":"shared","port":25565},"client":{"name":"West Coast","build":".","variables":{"motd":"client"}},"server":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Sides(); !slices.Equal(got, []string{"client", "server"}) {
		t.Errorf("sides %v", got)
	}
	if !m.HasSide("client") || !m.HasSide("server") || m.HasSide("both") {
		t.Error("HasSide")
	}
	if m.DisplayName("client") != "West Coast" || m.DisplayName("server") != "p" {
		t.Errorf("display names %q %q", m.DisplayName("client"), m.DisplayName("server"))
	}
	if m.BuildDir("client") != "." || m.BuildDir("server") != "build/server" {
		t.Errorf("build dirs %q %q", m.BuildDir("client"), m.BuildDir("server"))
	}
	if !m.InPlace("client") || m.InPlace("server") {
		t.Error("InPlace")
	}
	if side, ok := m.InPlaceSide(); !ok || side != "client" {
		t.Errorf("InPlaceSide = %q %v", side, ok)
	}
	if vars := m.SideVariables("client"); vars["motd"] != "client" || vars["port"] == nil {
		t.Errorf("client variables %v", vars)
	}
	if vars := m.SideVariables("server"); vars["motd"] != "shared" {
		t.Errorf("server variables %v", vars)
	}
	if m.Variables["motd"] != "shared" {
		t.Error("SideVariables wrote through to the project variables")
	}

	bare, err := Parse([]byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"targets":{"client":{"side":"client","overrides":["overrides"]}},"requires":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(bare.Sides()) != 0 {
		t.Errorf("sides without blocks: %v", bare.Sides())
	}
	if _, ok := bare.InPlaceSide(); ok {
		t.Error("InPlaceSide without blocks")
	}
}

func TestParseRejects(t *testing.T) {
	doc := func(rest string) []byte {
		return []byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"targets":{"client":{"side":"client","overrides":["overrides"]}},"requires":{},` + rest + `}`)
	}
	for _, rest := range []string{
		`"features":{"client":{}}`,
		`"features":{"server":{"default":true}}`,
		`"client":{"build":"."},"server":{"build":"."}`,
	} {
		if _, err := Parse(doc(rest)); err == nil {
			t.Errorf("%s should be invalid", rest)
		} else if out.CodeOf(err) != "manifest-invalid" {
			t.Errorf("%s: code %q", rest, out.CodeOf(err))
		}
	}
	if _, err := Parse(doc(`"client":{"build":"."},"server":{"build":"build/server"}`)); err != nil {
		t.Fatal(err)
	}
}
