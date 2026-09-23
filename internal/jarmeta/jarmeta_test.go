package jarmeta

import (
	"archive/zip"
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readBytes(t *testing.T, data []byte) *Info {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	info, err := readZip(zr, allFiles)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestReadFabricSkipsByteOrderMark(t *testing.T) {
	info := readBytes(t, buildZip(t, map[string]string{
		"fabric.mod.json": "\xef\xbb\xbf" + `{"schemaVersion":1,"id":"bommed","version":"1.0","environment":"client"}`,
	}))
	if info.ID != "bommed" || info.Side != "client" {
		t.Fatalf("parsed %+v", info)
	}
}

func TestReadAcceptsRawControlCharactersInStrings(t *testing.T) {
	fabric := readBytes(t, buildZip(t, map[string]string{
		"fabric.mod.json": "{\"id\":\"etf\",\"version\":\"7.0\",\"description\":\"Adds features\nSupports:\n\t- \\\"quoted\\\" textures\",\"environment\":\"client\"}",
	}))
	if fabric.ID != "etf" || fabric.Side != "client" {
		t.Fatalf("parsed %+v", fabric)
	}
	quilt := readBytes(t, buildZip(t, map[string]string{
		"quilt.mod.json": "{\"quilt_loader\":{\"id\":\"qetf\",\"version\":\"1.0\",\"metadata\":{\"description\":\"line one\nline two\"}}}",
	}))
	if quilt.ID != "qetf" {
		t.Fatalf("parsed %+v", quilt)
	}
}

func TestReadFabricAcceptsWhatStrictGsonAccepts(t *testing.T) {
	info := readBytes(t, buildZip(t, map[string]string{
		"fabric.mod.json": "{\"id\":\"lenient\",\"version\":\"1.0\",\"description\":\"it\\'s a \\\nline\",\"environment\":\"server\"}\n}trailing",
	}))
	if info.ID != "lenient" || info.Side != "server" {
		t.Fatalf("parsed %+v", info)
	}
}

func TestReadFabricRejectsWhatStrictGsonRejects(t *testing.T) {
	for name, body := range map[string]string{
		"line comment":   "{\"id\":\"x\", // note\n\"version\":\"1\"}",
		"block comment":  `{"id":"x", /* note */ "version":"1"}`,
		"trailing comma": `{"id":"x","version":"1",}`,
		"single quotes":  `{'id':'x'}`,
		"bad escape":     `{"id":"x","description":"\q"}`,
	} {
		data := buildZip(t, map[string]string{"fabric.mod.json": body})
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readZip(zr, allFiles); err == nil {
			t.Errorf("%s: read without error", name)
		}
	}
}

func TestReadQuilt(t *testing.T) {
	nested := buildZip(t, map[string]string{
		"fabric.mod.json": `{"id":"inner","version":"2.0"}`,
	})
	info := readBytes(t, buildZip(t, map[string]string{
		"fabric.mod.json":         `{"id":"ignored","version":"0"}`,
		"META-INF/jars/inner.jar": string(nested),
		"quilt.mod.json": `{
			"schema_version": 1,
			"quilt_loader": {
				"id": "shiny",
				"version": "1.4.0",
				"depends": [
					"quilt_loader",
					{"id": "minecraft", "versions": ">=26.2"},
					{"id": "org.quiltmc:qsl", "versions": ["^8.0", "^9.0"]},
					{"id": "fabric-api", "versions": {"all": [">=0.130", "<0.200"]}},
					{"id": "sodium", "versions": "^0.9", "optional": true},
					{"id": "iris", "unless": "oculus"},
					[{"id": "a"}, {"id": "b"}]
				],
				"breaks": [{"id": "optifabric", "versions": "*"}],
				"provides": ["shiny_api", {"id": "old_shiny", "version": "1.0"}],
				"jars": ["META-INF/jars/inner.jar"]
			},
			"minecraft": {"environment": "dedicated_server"}
		}`,
	}))
	if info.ID != "shiny" || info.Loader != "quilt" || info.Side != "server" {
		t.Fatalf("parsed %+v", info)
	}
	wantDepends := map[string]string{"quilt_loader": "*", "minecraft": ">=26.2", "qsl": "^8.0 || ^9.0", "fabric-api": ">=0.130 <0.200"}
	if !maps.Equal(info.Depends, wantDepends) {
		t.Errorf("depends %v, want %v", info.Depends, wantDepends)
	}
	if want := map[string]string{"sodium": "^0.9"}; !maps.Equal(info.Optional, want) {
		t.Errorf("optional %v, want %v", info.Optional, want)
	}
	if want := map[string]string{"optifabric": "*"}; !maps.Equal(info.Breaks, want) {
		t.Errorf("breaks %v, want %v", info.Breaks, want)
	}
	if want := map[string]string{"shiny_api": "1.4.0", "old_shiny": "1.0", "inner": "2.0"}; !maps.Equal(info.AllProvides(), want) {
		t.Errorf("provides %v, want %v", info.Provides, want)
	}
}

func TestQuiltRange(t *testing.T) {
	cases := []struct {
		versions any
		want     string
	}{
		{nil, "*"},
		{"=1.0", "=1.0"},
		{[]any{"1.0", "2.0"}, "1.0 || 2.0"},
		{map[string]any{"any": []any{"1.0", map[string]any{"all": []any{">=2", "<3"}}}}, "1.0 || >=2 <3"},
		{map[string]any{"all": []any{">=1", map[string]any{"any": []any{"1.5", "1.7"}}}}, "*"},
	}
	for _, c := range cases {
		if got := quiltRange(c.versions); got != c.want {
			t.Errorf("quiltRange(%v) = %q, want %q", c.versions, got, c.want)
		}
	}
}

func TestReadNeoForge(t *testing.T) {
	nested := buildZip(t, map[string]string{
		"META-INF/neoforge.mods.toml": "[[mods]]\nmodId=\"inner\"\nversion=\"3.1\"\n",
	})
	info := readBytes(t, buildZip(t, map[string]string{
		"META-INF/MANIFEST.MF":          "Manifest-Version: 1.0\r\nImplementation-Version: 0.8.1+mc26.2-build.12345678901234567890123456789012345678901234567890\r\n 123\r\n\r\n",
		"META-INF/jarjar/metadata.json": `{"jars":[{"identifier":{"group":"g","artifact":"inner"},"version":{"range":"[3,)","artifactVersion":"3.1"},"path":"META-INF/jarjar/inner.jar"},{"path":"META-INF/jarjar/missing.jar"}]}`,
		"META-INF/jarjar/inner.jar":     string(nested),
		"META-INF/neoforge.mods.toml": `
modLoader="javafml"
loaderVersion="[1,)"
license="MIT"

[[mods]]
modId="sodium"
version="${file.jarVersion}"

[[mods]]
modId="sodium_extra_api"
version="2.0"

[[dependencies.sodium]]
modId="neoforge"
type="required"
versionRange="[26.1.2.10-beta,)"
[[dependencies.sodium]]
modId="minecraft"
type="REQUIRED"
versionRange="[26.2,26.3)"
[[dependencies.sodium]]
modId="iris"
type="optional"
versionRange="[1.9,)"
[[dependencies.sodium]]
modId="optifine"
type="incompatible"
[[dependencies.sodium]]
modId="rubidium"
type="discouraged"
versionRange="*"
[[dependencies.sodium_extra_api]]
modId="sodium"
type="required"
[[dependencies.somebody_else]]
modId="ignored"
type="required"
`,
	}))
	if info.ID != "sodium" || info.Loader != "neoforge" || info.Side != "both" || !info.UsesMavenRanges {
		t.Fatalf("parsed %+v", info)
	}
	if want := "0.8.1+mc26.2-build.12345678901234567890123456789012345678901234567890123"; info.Version != want {
		t.Errorf("version %q, want %q", info.Version, want)
	}
	if want := map[string]string{"neoforge": "[26.1.2.10-beta,)", "minecraft": "[26.2,26.3)"}; !maps.Equal(info.Depends, want) {
		t.Errorf("depends %v, want %v", info.Depends, want)
	}
	if want := map[string]string{"iris": "[1.9,)"}; !maps.Equal(info.Optional, want) {
		t.Errorf("optional %v, want %v", info.Optional, want)
	}
	if want := map[string]string{"optifine": "*"}; !maps.Equal(info.Breaks, want) {
		t.Errorf("breaks %v, want %v", info.Breaks, want)
	}
	if want := map[string]string{"rubidium": "*"}; !maps.Equal(info.Conflicts, want) {
		t.Errorf("conflicts %v, want %v", info.Conflicts, want)
	}
	if want := map[string]string{"sodium_extra_api": "2.0", "inner": "3.1"}; !maps.Equal(info.AllProvides(), want) {
		t.Errorf("provides %v, want %v", info.Provides, want)
	}
}

func TestReadForgeMandatory(t *testing.T) {
	info := readBytes(t, buildZip(t, map[string]string{
		"META-INF/mods.toml": `
modLoader="javafml"
loaderVersion="65"
[[mods]]
modId="geckolib"
[[dependencies.geckolib]]
modId="minecraft"
mandatory=true
versionRange="[26.2,)"
[[dependencies.geckolib]]
modId="forge"
mandatory=false
versionRange="[65.1,)"
`,
	}))
	if info.ID != "geckolib" || info.Loader != "forge" || info.Version != "1" {
		t.Fatalf("parsed %+v", info)
	}
	if want := map[string]string{"minecraft": "[26.2,)"}; !maps.Equal(info.Depends, want) {
		t.Errorf("depends %v, want %v", info.Depends, want)
	}
	if want := map[string]string{"forge": "[65.1,)"}; !maps.Equal(info.Optional, want) {
		t.Errorf("optional %v, want %v", info.Optional, want)
	}
}

func TestReadPrefersTheProjectLoader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multi.jar")
	if err := os.WriteFile(path, buildZip(t, map[string]string{
		"fabric.mod.json":             `{"id":"multi_fabric","version":"1.0"}`,
		"META-INF/neoforge.mods.toml": "[[mods]]\nmodId=\"multi_neo\"\nversion=\"1.0\"\n",
	}), 0o644); err != nil {
		t.Fatal(err)
	}
	for loaderName, want := range map[string]string{"fabric": "multi_fabric", "quilt": "multi_fabric", "neoforge": "multi_neo", "": "multi_fabric"} {
		info, err := Read(path, "multi.jar", loaderName)
		if err != nil {
			t.Fatalf("%s: %v", loaderName, err)
		}
		if info.ID != want {
			t.Errorf("%s read %s, want %s", loaderName, info.ID, want)
		}
	}
	if _, err := Read(path, "multi.jar", "forge"); out.CodeOf(err) != "jar-metadata-missing" {
		t.Errorf("forge: %v, want no metadata", err)
	}
}

func TestReadErrorsNameTheJar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "5faae5cb")
	if err := os.WriteFile(path, buildZip(t, map[string]string{"fabric.mod.json": `{"id":`}), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Read(path, "better-end-4.0.11.jar", "fabric")
	if out.CodeOf(err) != "jar-metadata-invalid" || !strings.Contains(err.Error(), "better-end-4.0.11.jar") || strings.Contains(err.Error(), path) {
		t.Errorf("got %v, want jar-metadata-invalid naming better-end-4.0.11.jar and not %s", err, path)
	}
}
