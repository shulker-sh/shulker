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
	for name, sides := range map[string][]string{"client-server.json": {"client", "server"}, "minimal.json": {"client"}} {
		m, err := Load(filepath.Join("..", "..", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if got := m.Sides(); !slices.Equal(got, sides) {
			t.Errorf("%s declares %v, want %v", name, got, sides)
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
	m := &Manifest{Name: "x", Minecraft: "26.2", Loader: Loader{Type: "fabric", Version: "*"}, Requires: map[string]Require{}}
	if err := m.Save(filepath.Join(t.TempDir(), FileName)); err == nil {
		t.Fatal("expected schema error for a manifest with no side")
	}
	m.Client = &Client{Build: "build/client"}
	path := filepath.Join(t.TempDir(), FileName)
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestConditionsRoundTrip(t *testing.T) {
	m, err := Parse([]byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"features":{"fancy":{"default":true},"shaders":{}},"requires":{"aa":{"os":"macos"},"bb":{"feature":["fancy","!shaders"]}},"client":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Mods()["aa"].OS) != 1 || m.Mods()["aa"].OS[0] != "macos" || len(m.Mods()["bb"].Feature) != 2 || !m.Features["fancy"].Default {
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
		return []byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"requires":{` + entries + `},"client":{}}`)
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
	packs, err := Parse(doc(`"fresh":{"type":"resourcepack"},"complementary":{"type":"shader"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := packs.ResourcePacks()["fresh"]; !ok || len(packs.ResourcePacks()) != 1 {
		t.Errorf("resource packs %v", packs.ResourcePacks())
	}
	if _, ok := packs.Shaders()["complementary"]; !ok || len(packs.Shaders()) != 1 {
		t.Errorf("shaders %v", packs.Shaders())
	}
	local, err := Parse(doc(`"extras":{"file":"files/extras.jar"},"faithful":{"type":"resourcepack","file":"files/faithful.zip"},"bsl":{"type":"shader","file":"files/bsl.zip"}`))
	if err != nil {
		t.Fatal(err)
	}
	if local.Mods()["extras"].File != "files/extras.jar" || len(local.ResourcePacks()) != 1 || len(local.Shaders()) != 1 {
		t.Errorf("local files: mods %v, resource packs %v, shaders %v", local.Mods(), local.ResourcePacks(), local.Shaders())
	}
	if _, err := Parse(doc(`"extras":{"file":"files/extras.zip"}`)); out.CodeOf(err) != "manifest-invalid" {
		t.Errorf("a mod file that isn't a .jar should be invalid, got %v", err)
	}
	if _, err := Parse(doc(`"helper":{"type":"resourcepack","file":"Resource Packs/Mod Menu Helper"}`)); err != nil {
		t.Errorf("a resource pack may name a folder: %v", err)
	}
	archive, err := Parse(doc(`"cozy":{"type":"modpack","file":"files/cozy.mrpack"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.Mods()) != 0 || archive.Modpacks()["cozy"].File != "files/cozy.mrpack" {
		t.Errorf("an archive is a modpack: mods %v, modpacks %v", archive.Mods(), archive.Modpacks())
	}
	hosted, err := Parse(doc(`"cozy":{"type":"modpack","provider":"modrinth"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cozy, ok := hosted.Modpacks()["cozy"]; len(hosted.Mods()) != 0 || !ok || !cozy.IsHosted() || cozy.AutoUpdates() {
		t.Errorf("a hosted modpack is a modpack that follows nothing: mods %v, modpacks %v", hosted.Mods(), hosted.Modpacks())
	}
	for _, key := range []string{`"ref":"main"`, `"autoUpdate":true`, `"locked":true`, `"side":"client"`, `"os":"linux"`} {
		if _, err := Parse(doc(`"cozy":{"type":"modpack",` + key + `}`)); out.CodeOf(err) != "manifest-invalid" {
			t.Errorf("a hosted modpack with %s should be invalid, got %v", key, err)
		}
	}
}

func TestFeatureOverridesForms(t *testing.T) {
	doc := func(features string) []byte {
		return []byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"features":{` + features + `},"requires":{},"client":{}}`)
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
	m, err := Parse([]byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"requires":{},"variables":{"motd":"shared","port":25565},"client":{"name":"West Coast","build":".","variables":{"motd":"client"}},"server":{}}`))
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
	if !m.BuildsInPlace("client") || m.BuildsInPlace("server") {
		t.Error("BuildsInPlace")
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

	bare := &Manifest{Name: "p"}
	if len(bare.Sides()) != 0 {
		t.Errorf("sides without blocks: %v", bare.Sides())
	}
	if _, ok := bare.InPlaceSide(); ok {
		t.Error("InPlaceSide without blocks")
	}
	if bare.BuildDir("client") != "build/client" || bare.DisplayName("client") != "p" {
		t.Error("fallbacks without blocks")
	}
}

func TestParseRejects(t *testing.T) {
	doc := func(rest string) []byte {
		return []byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},` + rest + `}`)
	}
	for _, rest := range []string{
		`"requires":{},"features":{"client":{}},"client":{}`,
		`"requires":{},"features":{"server":{"default":true}},"client":{}`,
		`"requires":{},"client":{"build":"."},"server":{"build":"."}`,
		`"requires":{"iris":{"feature":"shaders"}},"client":{}`,
		`"features":{"shaders":{}},"requires":{"iris":{"feature":"!shadders"}},"client":{}`,
		`"requires":{}`,
		`"requires":{},"client":{},"icon":"../icon.png"`,
		`"requires":{},"client":{},"icon":"/tmp/icon.png"`,
		`"requires":{},"client":{},"icon":"icon.jpg"`,
	} {
		if _, err := Parse(doc(rest)); err == nil {
			t.Errorf("%s should be invalid", rest)
		} else if out.CodeOf(err) != "manifest-invalid" {
			t.Errorf("%s: code %q", rest, out.CodeOf(err))
		}
	}
	_, err := Parse(doc(`"requires":{}`))
	e, ok := err.(*out.Error)
	if !ok || e.Message != "shulker.json declares no side" || len(e.Rows) != 1 || e.Rows[0].Text != `add "client": {} or "server": {}` {
		t.Fatalf("no side: %v", err)
	}
	if _, err := Parse(doc(`"features":{"shaders":{}},"requires":{"iris":{"feature":"!shaders"}},"client":{"build":"."},"server":{"build":"build/server"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(doc(`"requires":{},"client":{},"icon":"assets/Icon.PNG"`)); err != nil {
		t.Fatal(err)
	}
}

func TestSkips(t *testing.T) {
	m := &Manifest{SkipFiles: []string{"*.bak", "config/*.log"}}
	for rel, want := range map[string]bool{
		".DS_Store":               true,
		"config/.DS_Store":        true,
		"config/._options.txt":    true,
		"THUMBS.DB":               true,
		"shaderpacks/Desktop.ini": true,
		"a.bak":                   true,
		"config/deep/a.bak":       true,
		"config/latest.log":       true,
		"config/deep/latest.log":  false,
		"latest.log":              false,
		"config/.ds_store":        false,
		"options.txt":             false,
		"config/.hidden.json":     false,
	} {
		if got := m.Skips(rel); got != want {
			t.Errorf("Skips(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestParseChecksTheMarker(t *testing.T) {
	doc := func(schema string) []byte {
		return []byte(`{"$schema":"` + schema + `","name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"requires":{},"client":{}}`)
	}
	newer := []byte(`{"$schema":"https://shulker.sh/schema/v2/manifest.json","name":"p","sides":["client"],"mods":{}}`)
	_, err := Parse(newer)
	e := out.AsError(err)
	if out.CodeOf(err) != "schema-newer" || len(e.Items) != 0 {
		t.Fatalf("newer marker: %v (items %v)", err, e.Items)
	}
	if want := "shulker.json was written by a newer shulker: its schema is v2, and this shulker knows v1"; e.Message != want {
		t.Errorf("newer message = %q, want %q", e.Message, want)
	}

	const foreign = "https://example.com/manifest.json"
	_, err = Parse(doc(foreign))
	if out.CodeOf(err) != "manifest-invalid" {
		t.Fatalf("foreign marker: code %q (%v)", out.CodeOf(err), err)
	}
	if want := "names the schema " + foreign + ", which this shulker doesn't know"; !strings.Contains(err.Error(), want) {
		t.Errorf("foreign message = %q, want it to contain %q", err.Error(), want)
	}

	if _, err := Parse(doc(SchemaURL)); err != nil {
		t.Errorf("current marker: %v", err)
	}
}

func TestSaveWritesTheMarker(t *testing.T) {
	m, err := Parse([]byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"requires":{},"client":{}}`))
	if err != nil {
		t.Fatalf("a manifest with no $schema should load: %v", err)
	}
	path := filepath.Join(t.TempDir(), FileName)
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var head struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		t.Fatal(err)
	}
	if head.Schema != SchemaURL {
		t.Errorf("saved $schema = %q, want %q", head.Schema, SchemaURL)
	}
}
