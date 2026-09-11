package jarmeta

import (
	"archive/zip"
	"bytes"
	"maps"
	"testing"
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
	info, err := readZip(zr)
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
	if want := map[string]string{"shiny_api": "1.4.0", "old_shiny": "1.0", "inner": "2.0"}; !maps.Equal(info.Provides, want) {
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
