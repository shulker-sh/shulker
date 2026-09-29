package packarchive

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

func TestKindAtReadsTheFolder(t *testing.T) {
	for p, want := range map[string]string{
		"mods/a.jar":                       manifest.TypeMod,
		"config/a.jar":                     manifest.TypeMod,
		"resourcepacks/a.zip":              manifest.TypeResourcePack,
		"resourcepacks/nested/a.zip":       manifest.TypeMod,
		"shaderpacks/a.zip":                manifest.TypeShader,
		lock.DatapackFolders[0] + "/a.zip": manifest.TypeDatapack,
	} {
		if got := KindAt(p); got != want {
			t.Errorf("KindAt(%q) = %q, want %q", p, got, want)
		}
	}
}

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

func markerJar(t *testing.T, manifestJSON, lockJSON string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range map[string]string{"shulker.json": manifestJSON, "shulker.lock": lockJSON} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(data))
	}
	zw.Close()
	return buf.String()
}

const cfManifestFixture = `{
  "minecraft": {"version": "1.20.1", "modLoaders": [{"id": "forge-47.2.0", "primary": false}, {"id": "neoforge-1.20.1-47.1.84", "primary": true}], "recommendedRam": 8196},
  "manifestType": "minecraftModpack",
  "manifestVersion": 1,
  "name": "All the Things",
  "version": "1.2",
  "author": "someone",
  "files": [{"projectID": 238222, "fileID": 5000001, "required": true}, {"projectID": 306612, "fileID": 5000010, "required": false}],
  "overrides": "extras"
}`

const mrpackIndexFixture = `{
  "formatVersion": 1, "game": "minecraft", "versionId": "2.0", "name": "Cozy", "summary": "A summary",
  "files": [
    {"path": "mods/sodium.jar", "hashes": {"sha1": "a1", "sha512": "a512"}, "env": {"client": "required", "server": "unsupported"}, "downloads": ["https://cdn.modrinth.com/sodium.jar"], "fileSize": 3},
    {"path": "mods/lithium.jar", "hashes": {"sha1": "b1", "sha512": "b512"}, "downloads": ["https://cdn.modrinth.com/lithium.jar"], "fileSize": 4}
  ],
  "dependencies": {"minecraft": "26.2", "fabric-loader": "0.17.3"}
}`

func TestReadCurseForge(t *testing.T) {
	file := writeZip(t, map[string]string{
		"manifest.json":             cfManifestFixture,
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
	if a.Format.Name() != "curseforge" || a.Name != "All the Things" || a.Version != "1.2" || a.Minecraft != "1.20.1" || len(a.Authors) != 1 || a.Authors[0] != "someone" {
		t.Fatalf("archive: %+v", a)
	}
	if a.Loader != (Loader{Type: "neoforge", Version: "47.1.84"}) {
		t.Fatalf("loader: %+v", a.Loader)
	}
	if len(a.Files) != 2 || a.Files[0].Provider != "curseforge" || a.Files[0].Project != "238222" || a.Files[0].Version != "5000001" || a.Files[0].Optional || !a.Files[1].Optional || a.Files[1].Version != "5000010" {
		t.Fatalf("files: %+v", a.Files)
	}
	var paths []string
	for _, o := range a.Overrides {
		if o.Layer != "overrides" {
			t.Fatalf("layer: %+v", o)
		}
		paths = append(paths, o.Path)
	}
	if strings.Join(paths, " ") != "config/a.toml config/windows.txt mods/bundled.jar" {
		t.Fatalf("overrides: %v", paths)
	}
	if a.NeedsServer() {
		t.Fatal("one overrides folder says nothing about a server")
	}
}

func TestReadCurseForgeDefaultsTheOverridesFolder(t *testing.T) {
	file := writeZip(t, map[string]string{
		"manifest.json":           `{"minecraft": {"version": "26.2", "modLoaders": []}, "manifestType": "minecraftModpack", "manifestVersion": 1, "name": "x", "files": []}`,
		"overrides/config/b.toml": "b",
	})
	a, err := Read(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Overrides) != 1 || a.Overrides[0].Path != "config/b.toml" || a.Loader != (Loader{}) {
		t.Fatalf("archive: %+v", a)
	}
	unknown := writeZip(t, map[string]string{
		"manifest.json": `{"minecraft": {"version": "1.12", "modLoaders": [{"id": "liteloader-1.12"}]}, "manifestType": "minecraftModpack", "manifestVersion": 1, "name": "x", "files": []}`,
	})
	if _, err := Read(unknown); out.CodeOf(err) != "unsupported-loader" {
		t.Fatalf("an unknown loader: %v", err)
	}
}

func TestReadMrpack(t *testing.T) {
	file := writeZip(t, map[string]string{
		"modrinth.index.json":                mrpackIndexFixture,
		"icon.png":                           "png",
		"overrides/config/a.toml":            "a",
		"server-overrides/server.properties": "motd=hi",
		"client-overrides/options.txt":       "lang:en",
	})
	a, err := Read(file)
	if err != nil {
		t.Fatal(err)
	}
	if a.Format.Name() != "mrpack" || a.Name != "Cozy" || a.Version != "2.0" || a.Summary != "A summary" || a.Minecraft != "26.2" || a.Loader != (Loader{Type: "fabric", Version: "0.17.3"}) || string(a.Icon) != "png" {
		t.Fatalf("archive: %+v", a)
	}
	if len(a.Files) != 2 || a.Files[0].Side != "client" || a.Files[0].Path != "mods/sodium.jar" || a.Files[0].Size != 3 || a.Files[1].Side != "" {
		t.Fatalf("files: %+v", a.Files)
	}
	var layers []string
	for _, o := range a.Overrides {
		layers = append(layers, o.Layer+"/"+o.Path)
	}
	if strings.Join(layers, " ") != "client-overrides/options.txt overrides/config/a.toml server-overrides/server.properties" {
		t.Fatalf("overrides: %v", layers)
	}
	if !a.NeedsServer() {
		t.Fatal("a server override needs a server")
	}
}

func TestReadRefuses(t *testing.T) {
	notZip := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(notZip, []byte("hello"), 0o644)
	cfManifest := `{"manifestType": "minecraftModpack", "manifestVersion": 1, "minecraft": {"version": "26.2"}}`
	cases := []struct {
		name string
		file string
		code string
	}{
		{"not a zip", notZip, "archive-not-modpack"},
		{"a resource pack", writeZip(t, map[string]string{"pack.mcmeta": "{}"}), "archive-not-modpack"},
		{"another manifest.json", writeZip(t, map[string]string{"manifest.json": `{"name": "npm"}`}), "archive-not-modpack"},
		{"broken curseforge json", writeZip(t, map[string]string{"manifest.json": `{"manifestType": `}), "curseforge-invalid"},
		{"no minecraft version", writeZip(t, map[string]string{"manifest.json": `{"manifestType": "minecraftModpack", "manifestVersion": 1, "minecraft": {}}`}), "curseforge-invalid"},
		{"newer curseforge format", writeZip(t, map[string]string{"manifest.json": `{"manifestType": "minecraftModpack", "manifestVersion": 2, "minecraft": {"version": "26.2"}}`}), "curseforge-invalid"},
		{"unsafe curseforge entry", writeZip(t, map[string]string{"manifest.json": cfManifest, "overrides/../../evil": "x"}), "curseforge-invalid"},
		{"broken index", writeZip(t, map[string]string{"modrinth.index.json": "{"}), "mrpack-invalid"},
		{"newer mrpack format", writeZip(t, map[string]string{"modrinth.index.json": `{"formatVersion": 2, "game": "minecraft", "dependencies": {"minecraft": "26.2"}}`}), "mrpack-unsupported"},
		{"no minecraft dependency", writeZip(t, map[string]string{"modrinth.index.json": `{"formatVersion": 1, "game": "minecraft", "dependencies": {}}`}), "mrpack-invalid"},
		{"index file without hashes", writeZip(t, map[string]string{"modrinth.index.json": `{"formatVersion": 1, "game": "minecraft", "files": [{"path": "mods/a.jar"}], "dependencies": {"minecraft": "26.2"}}`}), "mrpack-invalid"},
		{"unsafe mrpack entry", writeZip(t, map[string]string{"modrinth.index.json": `{"formatVersion": 1, "game": "minecraft", "dependencies": {"minecraft": "26.2"}}`, "../evil": "x"}), "mrpack-invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Read(c.file)
			if got := out.CodeOf(err); got != c.code {
				t.Fatalf("code %q, want %q: %v", got, c.code, err)
			}
		})
	}
	if IsArchive(notZip) || !IsArchive(writeZip(t, map[string]string{"manifest.json": cfManifest})) {
		t.Fatal("IsArchive tells an archive by its content")
	}
}

func TestReadRefusesAnIndexPathOutsideTheFolder(t *testing.T) {
	paths := []string{"../../ESCAPED.txt", "mods/../../x.jar", "/etc/x", "\\\\server\\x", "\\x", "C:/x.jar", "c:x.jar", "mods/CON", "mods/nul.jar", "COM1/x.jar", "mods/lpt9.txt", "mods/Aux .jar"}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			file := writeZip(t, map[string]string{"modrinth.index.json": mrpackIndexWith(t, p)})
			_, err := Read(file)
			if out.CodeOf(err) != "mrpack-invalid" || !strings.Contains(err.Error(), p) {
				t.Fatalf("want mrpack-invalid naming %q, got %v", p, err)
			}
		})
	}
	for _, p := range []string{"mods/a.jar", "config/console.txt", "mods/com10.jar", "mods/..a.jar"} {
		if _, err := Read(writeZip(t, map[string]string{"modrinth.index.json": mrpackIndexWith(t, p)})); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
}

func mrpackIndexWith(t *testing.T, path string) string {
	t.Helper()
	index := map[string]any{
		"formatVersion": 1,
		"game":          "minecraft",
		"dependencies":  map[string]string{"minecraft": "26.2"},
		"files": []map[string]any{{
			"path":      path,
			"hashes":    map[string]string{"sha1": "a", "sha512": "b"},
			"downloads": []string{"https://cdn.modrinth.com/data/x/a.jar"},
		}},
	}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestReadTakesTheMarker(t *testing.T) {
	m := `{"$schema": "https://shulker.sh/schema/v1/manifest.json", "name": "cozy", "minecraft": "26.2", "loader": {"type": "fabric", "version": "0.17.3"}, "requires": {}, "client": {}}`
	locked := lock.New()
	locked.Minecraft, locked.Loader, locked.Java = "26.2", lock.Loader{Type: "fabric", Version: "0.17.3"}, lock.Java{Major: 21, Component: "java-runtime-delta"}
	lockData, err := json.Marshal(locked)
	if err != nil {
		t.Fatal(err)
	}
	l := string(lockData)
	file := writeZip(t, map[string]string{
		"modrinth.index.json":               mrpackIndexFixture,
		"client-overrides/mods/shulker.jar": markerJar(t, m, l),
	})
	a, err := Read(file)
	if err != nil {
		t.Fatal(err)
	}
	if a.Marker == nil || a.Marker.Layer != "client-overrides" || a.Marker.Manifest.Name != "cozy" || len(a.Overrides) != 0 {
		t.Fatalf("the marker jar is read and left out of the overrides: %+v", a)
	}
	root := writeZip(t, map[string]string{"modrinth.index.json": mrpackIndexFixture, "shulker.json": m, "shulker.lock": l})
	if a, err = Read(root); err != nil || a.Marker == nil || a.Marker.Layer != "" {
		t.Fatalf("the root identity is the marker: %+v %v", a, err)
	}
	broken := writeZip(t, map[string]string{"manifest.json": `{"manifestType": "minecraftModpack", "manifestVersion": 1, "minecraft": {"version": "26.2"}}`, "shulker.json": "{", "shulker.lock": l})
	if _, err := Read(broken); out.CodeOf(err) != "mrpack-marker" {
		t.Fatalf("a marker that can't be read: %v", err)
	}
}

func TestManifestFromTheArchive(t *testing.T) {
	file := writeZip(t, map[string]string{"manifest.json": cfManifestFixture})
	a, err := Read(file)
	if err != nil {
		t.Fatal(err)
	}
	m, warnings := a.Manifest("things")
	if len(warnings) != 0 || m.Name != "things" || m.Version != "1.2" || m.Minecraft != "1.20.1" || m.Loader.Type != "neoforge" || m.Client == nil || m.Client.Memory != "8196M" || m.Server != nil || len(m.Authors) != 1 {
		t.Fatalf("manifest: %+v %v", m, warnings)
	}
}

func TestCurseForgeRecommendedRamIsAHeap(t *testing.T) {
	for ram, want := range map[int]string{0: "", -1: "", 8196: "8196M", 8192: "8G", 1024: "1G", 512: "512M"} {
		if got := cfHeap(ram); got != want {
			t.Errorf("cfHeap(%d) = %q, want %q", ram, got, want)
		}
	}
}

func TestWriteReadsBack(t *testing.T) {
	for _, f := range Formats {
		t.Run(f.Name(), func(t *testing.T) {
			x := &Export{
				Name: "Cozy", Version: "1.0", Summary: "hi", Authors: []string{"me"}, Minecraft: "26.2", Loader: Loader{Type: "fabric", Version: "0.17.3"},
				Files:     []File{{Path: "mods/sodium.jar", Hashes: map[string]string{"sha1": "a1", "sha512": "a512"}, Side: "client", Downloads: []string{"https://cdn.modrinth.com/sodium.jar"}, Size: 3, Provider: "curseforge", Project: "394468", Version: "5000001", Filename: "sodium.jar", Title: "Sodium", Author: "jellysquid", Page: "https://www.curseforge.com/minecraft/mc-mods/sodium"}},
				Overrides: []Override{{Layer: "overrides", Path: "config/a.toml", Data: []byte("a")}, {Layer: "client-overrides", Path: "options.txt", Data: []byte("o")}},
				Icon:      []byte("png"), IconName: "icon.png",
			}
			if !f.Lists(x.Files[0]) {
				t.Fatal("the file is listable in every format")
			}
			output := filepath.Join(t.TempDir(), "pack"+f.Extension())
			if err := Write(f, output, x); err != nil {
				t.Fatal(err)
			}
			a, err := Read(output)
			if err != nil {
				t.Fatal(err)
			}
			if a.Format.Name() != f.Name() || a.Name != "Cozy" || a.Version != "1.0" || a.Minecraft != "26.2" || a.Loader != x.Loader || string(a.Icon) != "png" || len(a.Files) != 1 {
				t.Fatalf("read back: %+v", a)
			}
			if f.Sided() && (a.Files[0].Side != "client" || a.Overrides[0].Layer != "client-overrides" || a.Summary != "hi") {
				t.Fatalf("a sided format keeps sides: %+v", a)
			}
			if !f.Sided() && (a.Files[0].Project != "394468" || len(a.Overrides) != 2 || a.Overrides[0].Layer != "overrides" || a.Authors[0] != "me") {
				t.Fatalf("an unsided format lists by id and flattens layers: %+v", a)
			}
		})
	}
}

func TestFormatFacts(t *testing.T) {
	cf, _ := Lookup("curseforge")
	mr, _ := Lookup("mrpack")
	if !cf.Places("datapack", "datapacks/x.zip") || cf.Places("datapack", "global_packs/required_data/x.zip") || !mr.Places("datapack", "global_packs/required_data/x.zip") {
		t.Fatal("only CurseForge minds where a datapack goes")
	}
	if mr.Lists(File{Downloads: []string{"https://example.com/a.jar"}}) || !mr.Lists(File{Downloads: []string{"https://github.com/a.jar"}}) || cf.Lists(File{Provider: "modrinth", Project: "1", Version: "2"}) || cf.Lists(File{Provider: "curseforge", Project: "AANobbMI", Version: "2"}) {
		t.Fatal("listing rules")
	}
	if cf.NotListed("1 mod", 1).Code != "curseforge-not-found" || mr.NotListed("2 mods", 2).Code != "mrpack-host-not-allowed" || cf.CantPlace("1 datapack").Code != "curseforge-cant-place" {
		t.Fatal("refusal codes")
	}
	if Titles() != "Modrinth or CurseForge" || strings.Join(Names(), ",") != "mrpack,curseforge" {
		t.Fatal(Titles())
	}
}
