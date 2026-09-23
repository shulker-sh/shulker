package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/resolve"
)

const datapackMcmeta = `{"pack":{"pack_format":48,"description":"loot"}}`

func TestAddLocalDatapacks(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	loot := makeJarFiles(t, "loot", "loot-tweaks.zip", map[string]string{"pack.mcmeta": datapackMcmeta, "data/loot/loot_table/chest.json": "{}"})
	h.mustRun(t, "add", writeOutside(t, loot.filename, loot.data))
	writeProjectFile(t, h, "packs/recipes/pack.mcmeta", []byte(datapackMcmeta))
	writeProjectFile(t, h, "packs/recipes/data/recipes/recipe/stick.json", []byte("{}"))
	h.mustRun(t, "datapack", "add", "packs/recipes", "--side", "server")

	m := h.readManifest(t)
	if got := m.Requires["loot-tweaks"]; !reflect.DeepEqual(got, manifest.Require{Type: manifest.TypeDatapack, File: "files/loot-tweaks.zip"}) {
		t.Fatalf("a zip with data/ and no assets/ is a datapack: %+v", got)
	}
	if got := m.Requires["recipes"]; !reflect.DeepEqual(got, manifest.Require{Type: manifest.TypeDatapack, File: "packs/recipes", Side: "server"}) {
		t.Fatalf("a datapack folder keeps its side: %+v", got)
	}
	l := h.readLock(t)
	if p := l.Datapacks["loot-tweaks"]; p.Sha512 != loot.sha512 || p.Side != "both" || p.Filename != "loot-tweaks.zip" {
		t.Fatalf("a datapack is placed on both sides by default: %+v", p)
	}
	if p := l.Datapacks["recipes"]; p.Side != "server" || p.Sha512 == "" {
		t.Fatalf("a datapack folder locks as a zip: %+v", p)
	}
}

func TestAddLocalDatapackAmbiguity(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	hybrid := makeJarFiles(t, "hybrid", "autoslabs.zip", map[string]string{"pack.mcmeta": datapackMcmeta, "data/a/tags/x.json": "{}", "assets/a/lang/en_us.json": "{}"})
	path := writeOutside(t, hybrid.filename, hybrid.data)
	code, stdout, _ := h.run(t, "--json", "add", path)
	if e := failureCode(t, stdout); code == 0 || e.Code != "type-ambiguous" {
		t.Fatalf("a pack with both data/ and assets/ needs --type: code=%d %+v", code, e)
	}
	h.mustRun(t, "add", path, "--type", "resourcepack")
	if _, ok := h.readLock(t).ResourcePacks["autoslabs"]; !ok {
		t.Fatal("--type settles it")
	}
}

func TestAddHostedDatapack(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "terralith")

	if got := h.readManifest(t).Requires["terralith"]; !reflect.DeepEqual(got, manifest.Require{Type: manifest.TypeDatapack}) {
		t.Fatalf("a datapack-only Modrinth project adds as a datapack: %+v", got)
	}
	p := h.readLock(t).Datapacks["terralith"]
	if p.Provider != "modrinth" || p.VersionNumber != "2.6.4" || p.Filename != "terralith.zip" || p.Side != "both" || p.Sha1 == "" {
		t.Fatalf("locked like a resource pack, on both sides: %+v", p)
	}
	h.mustRun(t, "datapack", "remove", "terralith")
	if _, ok := h.readLock(t).Datapacks["terralith"]; ok {
		t.Fatal("remove drops it from the lock")
	}
}

func TestClientBuildPlacesDatapacks(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "terralith")

	_, _, stderr := h.run(t, "install")
	if !strings.Contains(stderr, "! terralith: placed in datapacks/, which only some global datapack mods read; add one, such as paxi, to load it in every world") {
		t.Fatalf("no warning without a global datapack mod: %s", stderr)
	}
	if readBuilt(t, h, "datapacks/terralith.zip") != string(h.jars["terralith"].data) {
		t.Fatal("the datapack goes to datapacks/ without a global datapack mod")
	}

	paxi := makeJar(t, "paxi", "Paxi-26.2-Fabric-5.1.jar", "*")
	h.mustRun(t, "add", writeOutside(t, paxi.filename, paxi.data))
	_, _, stderr = h.run(t, "build")
	if strings.Contains(stderr, "global datapack mods") {
		t.Fatalf("Paxi loads it, so nothing to warn about: %s", stderr)
	}
	if readBuilt(t, h, "config/paxi/datapacks/terralith.zip") == "" {
		t.Fatal("with Paxi placed, the datapack goes to its folder")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "datapacks", "terralith.zip")); !os.IsNotExist(err) {
		t.Fatalf("the old placement is removed: %v", err)
	}
}

func TestServerBuildPlacesDatapacksInItsWorld(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.mustRun(t, "add", "terralith")
	loot := makeJarFiles(t, "loot", "loot.zip", map[string]string{"pack.mcmeta": datapackMcmeta, "data/loot/loot_table/chest.json": "{}"})
	h.mustRun(t, "add", writeOutside(t, loot.filename, loot.data), "--side", "client")
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"properties": map[string]any{"level-name": "adventure"}}
	})
	h.mustRun(t, "lock")

	_, _, stderr := h.run(t, "install")
	if strings.Contains(stderr, "global datapack mods") {
		t.Fatalf("the server's world loads its datapacks without a mod: %s", stderr)
	}
	placed := filepath.Join(h.dir, "data", "server", "adventure", "datapacks", "terralith.zip")
	if data, err := os.ReadFile(placed); err != nil || string(data) != string(h.jars["terralith"].data) {
		t.Fatalf("the datapack is written through the world's link: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "data", "server", "adventure", "datapacks", "loot.zip")); !os.IsNotExist(err) {
		t.Fatalf("a client-side datapack stays off the server: %v", err)
	}

	h.mustRun(t, "remove", "terralith")
	h.mustRun(t, "build")
	if _, err := os.Stat(placed); !os.IsNotExist(err) {
		t.Fatalf("a dropped datapack leaves the world: %v", err)
	}
}

func TestInPlaceServerBuildWritesItsWorldsDatapacks(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"build": "."}
	})
	h.mustRun(t, "add", "terralith")
	h.mustRun(t, "install")
	if readFile(t, filepath.Join(h.dir, "world", "datapacks", "terralith.zip")) != string(h.jars["terralith"].data) {
		t.Fatal("an in-place server writes its world's datapacks")
	}
}

func TestExportMrpackCarriesDatapacksBySide(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{}
	})
	h.mustRun(t, "add", "terralith")
	h.mustRun(t, "install")
	h.allowMrpackHost(t)

	stdout := h.mustRun(t, "export", "mrpack", "--version", "1.0")
	if !strings.Contains(stdout, "1 datapack by download") {
		t.Fatalf("export output counts the datapack: %s", stdout)
	}
	index, _ := readMrpack(t, filepath.Join(h.dir, "build", "pack-1.0.mrpack"))
	paths := map[string]map[string]string{}
	for _, f := range index.Files {
		paths[f.Path] = f.Env
	}
	if env := paths["datapacks/terralith.zip"]; env["client"] != "required" || env["server"] != "unsupported" {
		t.Fatalf("the client's copy goes to datapacks/: %+v", index.Files)
	}
	if env := paths["world/datapacks/terralith.zip"]; env["server"] != "required" || env["client"] != "unsupported" {
		t.Fatalf("the server's copy goes to its world: %+v", index.Files)
	}

	paxi := makeJar(t, "paxi", "Paxi-26.2-Fabric-5.1.jar", "*")
	h.mustRun(t, "add", writeOutside(t, paxi.filename, paxi.data))
	h.mustRun(t, "export", "mrpack", "--version", "1.1", "--bundle")
	index, _ = readMrpack(t, filepath.Join(h.dir, "build", "pack-1.1.mrpack"))
	var datapacks []string
	for _, f := range index.Files {
		if strings.Contains(f.Path, "terralith") {
			datapacks = append(datapacks, f.Path)
			if f.Env["client"] != "required" || f.Env["server"] != "required" {
				t.Fatalf("one copy serves both sides: %+v", f)
			}
		}
	}
	if !reflect.DeepEqual(datapacks, []string{"config/paxi/datapacks/terralith.zip"}) {
		t.Fatalf("with Paxi on both sides the datapack goes in once: %v", datapacks)
	}
}

func TestExportCurseForgeBundlesADatapackOutsideDatapacks(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	loot := makeJarFiles(t, "loot", "loot.zip", map[string]string{"pack.mcmeta": datapackMcmeta, "data/loot/loot_table/chest.json": "{}"})
	h.mustRun(t, "add", writeOutside(t, loot.filename, loot.data))
	paxi := makeJar(t, "paxi", "Paxi-26.2-Fabric-5.1.jar", "*")
	h.mustRun(t, "add", writeOutside(t, paxi.filename, paxi.data))
	h.mustRun(t, "install")

	code, stdout, _ := h.run(t, "--json", "export", "curseforge", "--version", "1.0")
	if e := failureCode(t, stdout); code == 0 || e.Code != "curseforge-cant-place" {
		t.Fatalf("a datapack in Paxi's folder can't go by file ID: code=%d %+v", code, e)
	}
	stdout = h.mustRun(t, "export", "curseforge", "--version", "1.0", "--bundle")
	if !strings.Contains(stdout, "1 datapack bundled") {
		t.Fatalf("export output counts the bundled datapack: %s", stdout)
	}
	entries := readArchive(t, filepath.Join(h.dir, "build", "pack-1.0.zip"))
	if entries["overrides/config/paxi/datapacks/loot.zip"] != string(loot.data) {
		t.Fatal("the datapack is bundled where Paxi reads it")
	}
}

func TestImportMrpackLocksDatapacks(t *testing.T) {
	h := newHarness(t)
	terralith, autoslabs := h.jars["terralith"], h.jars["autoslabs"]
	file := func(rel string, jar fakeJar) mrpack.File {
		return mrpack.File{Path: rel, Hashes: map[string]string{"sha1": jar.sha1, "sha512": jar.sha512}, Env: mrpack.Env("both"), Downloads: []string{h.server.URL + "/cdn/" + jar.filename}, FileSize: int64(len(jar.data))}
	}
	unknown := makeJarFiles(t, "inmis", "inmis_recipe_fix.zip", map[string]string{"pack.mcmeta": datapackMcmeta, "data/inmis/recipe/fix.json": "{}"})
	index := mrpack.Index{
		FormatVersion: 1, Game: "minecraft", VersionID: "40", Name: "Better",
		Files: []mrpack.File{
			file("resourcepacks/Terralith.zip", terralith),
			file("datapacks/Terralith.zip", terralith),
			file("resourcepacks/AutoslabsCompat.zip", autoslabs),
		},
		Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"},
	}
	archive := filepath.Join(t.TempDir(), "better.mrpack")
	writeMrpack(t, archive, index, map[string][]byte{
		"overrides/config/paxi/datapacks/Terralith.zip":        terralith.data,
		"overrides/config/paxi/datapacks/inmis_recipe_fix.zip": unknown.data,
		"overrides/resourcepacks/Terralith Copy.zip":           terralith.data,
	})

	dir := filepath.Join(t.TempDir(), "better")
	h.dir = filepath.Dir(dir)
	var env struct {
		Data importResult `json:"data"`
	}
	stdout := h.mustRun(t, "import", "mrpack", archive, "--dir", dir, "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	res := env.Data.Mods
	wantLocked := []resolve.LockedFile{{ID: "autoslabs-compat-bmc", Type: "resourcepack", Provider: "modrinth"}, {ID: "terralith", Type: "datapack", Provider: "modrinth"}}
	if !slices.Equal(res.Locked, wantLocked) {
		t.Fatalf("locked: %+v", res.Locked)
	}
	if strings.Join(res.Duplicates, ",") != "datapacks/Terralith.zip,overrides/resourcepacks/Terralith Copy.zip,resourcepacks/Terralith.zip" {
		t.Fatalf("copies of a loaded datapack are dropped, named by their path in the archive: %+v", res.Duplicates)
	}
	text := h.mustRun(t, "import", "mrpack", archive, "--dir", filepath.Join(t.TempDir(), "text"))
	for _, rel := range res.Duplicates {
		if !regexp.MustCompile(`(?m)^\s*│?\s+` + regexp.QuoteMeta(rel) + `$`).MatchString(text) {
			t.Fatalf("each left-out copy gets a line of its own:\n%s", text)
		}
	}
	if strings.Join(res.Unmanaged, ",") != "overrides/config/paxi/datapacks/inmis_recipe_fix.zip" {
		t.Fatalf("a datapack no provider has stays an override: %+v", res.Unmanaged)
	}
	m, l := readProject(t, dir)
	if got := m.Requires["terralith"]; got.Type != manifest.TypeDatapack || got.Filename != "Terralith.zip" || got.Pin != "urbokcOc" {
		t.Fatalf("terralith entry: %+v", got)
	}
	if p := l.Datapacks["terralith"]; p.Side != "both" || p.Filename != "Terralith.zip" {
		t.Fatalf("terralith lock: %+v", p)
	}
	if got := m.Requires["autoslabs-compat-bmc"]; got.Type != manifest.TypeResourcePack || got.Filename != "AutoslabsCompat.zip" || got.Project != "AutoSlb1" {
		t.Fatalf("a datapack carrying assets/ is a resource pack, keyed by its slug made a key: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "overrides", "config", "paxi", "datapacks", "Terralith.zip")); !os.IsNotExist(err) {
		t.Fatalf("the locked datapack isn't kept as an override: %v", err)
	}

	stray := filepath.Join(t.TempDir(), "stray.mrpack")
	index.Files = index.Files[:1]
	writeMrpack(t, stray, index, nil)
	dir = filepath.Join(t.TempDir(), "stray")
	h.mustRun(t, "import", "mrpack", stray, "--dir", dir)
	if _, l := readProject(t, dir); l.Datapacks["terralith"].Filename != "Terralith.zip" {
		t.Fatalf("a datapack under resourcepacks/ with no loaded copy is locked as a datapack: %+v", l.Datapacks)
	}
}

func TestPullAdoptsADroppedDatapack(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "install")
	buildDir := filepath.Join(h.dir, "build", "client")
	loot := makeJarFiles(t, "loot", "Loot Tweaks.zip", map[string]string{"pack.mcmeta": datapackMcmeta, "data/loot/loot_table/chest.json": "{}"})
	writeOverride(t, buildDir, "datapacks/Loot Tweaks.zip", string(loot.data))

	rep := pullReport(t, h, "datapacks/Loot Tweaks.zip")
	if want := []string{"datapacks/Loot Tweaks.zip -> files/Loot Tweaks.zip"}; !reflect.DeepEqual(rep.Entries, want) {
		t.Fatalf("a zip in a datapack folder is adopted: %q", rep.Entries)
	}
	want := manifest.Require{Type: manifest.TypeDatapack, File: "files/Loot Tweaks.zip", Filename: "Loot Tweaks.zip"}
	if got := h.readManifest(t).Requires["loot-tweaks"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("an adopted datapack keeps its file name: %+v", got)
	}
	if p := h.readLock(t).Datapacks["loot-tweaks"]; p.Sha512 != loot.sha512 || p.Filename != "Loot Tweaks.zip" {
		t.Fatalf("the adopted datapack is locked: %+v", p)
	}
	h.mustRun(t, "install")
	if _, err := os.Stat(filepath.Join(buildDir, "datapacks", "Loot Tweaks.zip")); err != nil {
		t.Fatalf("the next build places it where it was: %v", err)
	}
}

func TestMatchLocksAnOverrideDatapack(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	terralith := h.jars["terralith"]
	writeFile(t, filepath.Join(h.dir, "overrides/config/paxi/datapacks/Terralith.zip"), string(terralith.data))

	var env struct {
		Data matchResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "match", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if want := []resolve.LockedFile{{ID: "terralith", Type: "datapack", Provider: "modrinth"}}; !slices.Equal(env.Data.Locked, want) {
		t.Fatalf("a zip in a loader's datapack folder is matched: %+v", env.Data)
	}
	if p := h.readLock(t).Datapacks["terralith"]; p.Filename != "Terralith.zip" {
		t.Fatalf("the matched datapack keeps its file name: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "overrides/config/paxi/datapacks/Terralith.zip")); !os.IsNotExist(err) {
		t.Fatalf("the matched override is removed: %v", err)
	}
}
