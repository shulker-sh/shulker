package cfpack

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func writeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "pack.zip")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(data))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return file
}

const packManifest = `{
  "minecraft": {"version": "1.20.1", "modLoaders": [{"id": "forge-47.2.0", "primary": false}, {"id": "neoforge-1.20.1-47.1.84", "primary": true}]},
  "manifestType": "minecraftModpack",
  "manifestVersion": 1,
  "name": "All the Things",
  "version": "1.2",
  "author": "someone",
  "files": [{"projectID": 238222, "fileID": 5000001, "required": true}, {"projectID": 306612, "fileID": 5000010, "required": false}],
  "overrides": "extras"
}`

func TestRead(t *testing.T) {
	file := writeZip(t, map[string]string{
		"manifest.json":             packManifest,
		"modlist.html":              "<ul></ul>",
		"extras/config/a.toml":      "a = 1\n",
		"extras/mods/bundled.jar":   "jar",
		`extras\config\windows.txt`: "w",
		"overrides/config/b.toml":   "not the named folder",
	})
	a, err := Read(file)
	if err != nil {
		t.Fatal(err)
	}
	m := a.Manifest
	if m.Name != "All the Things" || m.Version != "1.2" || m.Minecraft.Version != "1.20.1" || len(m.Files) != 2 || m.Files[1].ProjectID != 306612 || m.Files[1].FileID != 5000010 || m.Files[1].Required {
		t.Fatalf("manifest: %+v", m)
	}
	name, version, err := a.Loader()
	if err != nil || name != "neoforge" || version != "47.1.84" {
		t.Fatalf("loader: %s %s %v", name, version, err)
	}
	var paths []string
	for _, o := range a.Overrides {
		if o.Layer != "overrides" {
			t.Fatalf("layer: %+v", o)
		}
		paths = append(paths, o.Path)
	}
	if len(paths) != 3 || paths[0] != "config/a.toml" || paths[1] != "config/windows.txt" || paths[2] != "mods/bundled.jar" {
		t.Fatalf("overrides: %v", paths)
	}
}

func TestReadDefaultsTheOverridesFolder(t *testing.T) {
	file := writeZip(t, map[string]string{
		"manifest.json":           `{"minecraft": {"version": "26.2", "modLoaders": []}, "manifestType": "minecraftModpack", "manifestVersion": 1, "name": "x", "files": []}`,
		"overrides/config/b.toml": "b",
	})
	a, err := Read(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Overrides) != 1 || a.Overrides[0].Path != "config/b.toml" {
		t.Fatalf("overrides: %+v", a.Overrides)
	}
	if name, _, err := a.Loader(); name != "" || err != nil {
		t.Fatalf("a pack with no mod loaders has none: %s %v", name, err)
	}
	a.Manifest.Minecraft.ModLoaders = []ModLoader{{ID: "liteloader-1.12"}}
	if _, _, err := a.Loader(); out.CodeOf(err) != "unsupported-loader" {
		t.Fatalf("an unknown loader: %v", err)
	}
}

func TestReadRefuses(t *testing.T) {
	notZip := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(notZip, []byte("hello"), 0o644)
	cases := []struct {
		name string
		file string
		code string
	}{
		{"not a zip", notZip, "archive-not-modpack"},
		{"a resource pack", writeZip(t, map[string]string{"pack.mcmeta": "{}"}), "archive-not-modpack"},
		{"an mrpack", writeZip(t, map[string]string{"modrinth.index.json": "{}"}), "archive-not-modpack"},
		{"another manifest.json", writeZip(t, map[string]string{"manifest.json": `{"name": "npm"}`}), "archive-not-modpack"},
		{"broken json", writeZip(t, map[string]string{"manifest.json": `{"manifestType": `}), "curseforge-invalid"},
		{"no minecraft version", writeZip(t, map[string]string{"manifest.json": `{"manifestType": "minecraftModpack", "manifestVersion": 1, "minecraft": {}}`}), "curseforge-invalid"},
		{"newer format", writeZip(t, map[string]string{"manifest.json": `{"manifestType": "minecraftModpack", "manifestVersion": 2, "minecraft": {"version": "26.2"}}`}), "curseforge-invalid"},
		{"unsafe entry", writeZip(t, map[string]string{"manifest.json": `{"manifestType": "minecraftModpack", "manifestVersion": 1, "minecraft": {"version": "26.2"}}`, "overrides/../../evil": "x"}), "curseforge-invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Read(c.file)
			if got := out.CodeOf(err); got != c.code {
				t.Fatalf("code %q, want %q: %v", got, c.code, err)
			}
		})
	}
}

func TestReadNamesAnMrpack(t *testing.T) {
	file := writeZip(t, map[string]string{"modrinth.index.json": "{}"})
	_, err := Read(file)
	if e := out.AsError(err); e.Message != file+" is a Modrinth modpack, not a CurseForge one" {
		t.Fatalf("message: %+v", e)
	}
}
