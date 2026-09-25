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
	"shulker.sh/shulker/internal/resolve"
)

const datapackMcmeta = `{"pack":{"pack_format":48,"description":"loot"}}`

func TestAddLocalDatapacks(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
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
	h.mustRun(t, "create", "--loader", "fabric")
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
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "terralith")

	if got := h.readManifest(t).Requires["terralith"]; !reflect.DeepEqual(got, manifest.Require{Type: manifest.TypeDatapack, Filename: "Terralith_26.2_v2.6.4.zip"}) {
		t.Fatalf("a datapack-only Modrinth project adds as a datapack: %+v", got)
	}
	p := h.readLock(t).Datapacks["terralith"]
	if p.Provider != "modrinth" || p.VersionNumber != "2.6.4" || p.Filename != "Terralith_26.2_v2.6.4.zip" || p.Side != "both" || p.Sha1 == "" {
		t.Fatalf("locked like a resource pack, on both sides: %+v", p)
	}
	h.mustRun(t, "datapack", "remove", "terralith")
	if _, ok := h.readLock(t).Datapacks["terralith"]; ok {
		t.Fatal("remove drops it from the lock")
	}
}

func TestExportMrpackCarriesDatapacksBySide(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
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
	if env := paths["datapacks/Terralith_26.2_v2.6.4.zip"]; env["client"] != "required" || env["server"] != "unsupported" {
		t.Fatalf("the client's copy goes to datapacks/: %+v", index.Files)
	}
	if env := paths["world/datapacks/Terralith_26.2_v2.6.4.zip"]; env["server"] != "required" || env["client"] != "unsupported" {
		t.Fatalf("the server's copy goes to its world: %+v", index.Files)
	}

	paxi := makeJar(t, "paxi", "Paxi-26.2-Fabric-5.1.jar", "*")
	h.mustRun(t, "add", writeOutside(t, paxi.filename, paxi.data))
	h.mustRun(t, "export", "mrpack", "--version", "1.1", "--bundle")
	index, _ = readMrpack(t, filepath.Join(h.dir, "build", "pack-1.1.mrpack"))
	var datapacks []string
	for _, f := range index.Files {
		if strings.Contains(f.Path, "Terralith") {
			datapacks = append(datapacks, f.Path)
			if f.Env["client"] != "required" || f.Env["server"] != "required" {
				t.Fatalf("one copy serves both sides: %+v", f)
			}
		}
	}
	if !reflect.DeepEqual(datapacks, []string{"config/paxi/datapacks/Terralith_26.2_v2.6.4.zip"}) {
		t.Fatalf("with Paxi on both sides the datapack goes in once: %v", datapacks)
	}
}

func TestImportMrpackLocksDatapacks(t *testing.T) {
	h := newHarness(t)
	terralith, autoslabs := h.jars["terralith"], h.jars["autoslabs"]
	file := func(rel string, jar fakeJar) mrpackIndexFile {
		return mrpackIndexFile{Path: rel, Hashes: map[string]string{"sha1": jar.sha1, "sha512": jar.sha512}, Env: mrpackEnv("both"), Downloads: []string{h.server.URL + "/cdn/" + jar.filename}, FileSize: int64(len(jar.data))}
	}
	unknown := makeJarFiles(t, "inmis", "inmis_recipe_fix.zip", map[string]string{"pack.mcmeta": datapackMcmeta, "data/inmis/recipe/fix.json": "{}"})
	index := mrpackIndex{
		FormatVersion: 1, Game: "minecraft", VersionID: "40", Name: "Better",
		Files: []mrpackIndexFile{
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
	stdout := h.mustRun(t, "import", archive, "--dir", dir, "--json")
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
	text := h.mustRun(t, "import", archive, "--dir", filepath.Join(t.TempDir(), "text"))
	for _, rel := range res.Duplicates {
		if !regexp.MustCompile(`(?m)^\s+(├─|╰─) ` + regexp.QuoteMeta(rel) + `$`).MatchString(text) {
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
	h.mustRun(t, "import", stray, "--dir", dir)
	if _, l := readProject(t, dir); l.Datapacks["terralith"].Filename != "Terralith.zip" {
		t.Fatalf("a datapack under resourcepacks/ with no loaded copy is locked as a datapack: %+v", l.Datapacks)
	}
}

func TestPullAdoptsADroppedDatapack(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
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
	h.mustRun(t, "create", "--loader", "fabric")
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

func TestHybridDatapackFlagLocks(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	hybrid := makeJarFiles(t, "hybrid", "autoslabs.zip", map[string]string{"pack.mcmeta": datapackMcmeta, "data/a/tags/x.json": "{}", "assets/a/lang/en_us.json": "{}"})
	h.mustRun(t, "add", writeOutside(t, hybrid.filename, hybrid.data), "--type", "datapack")
	h.mustRun(t, "set", "requires.autoslabs.resourcepack", "true")

	_, stderr := h.mustRunStderr(t, "build")
	if !strings.Contains(stderr, "autoslabs: resourcepack false -> true") {
		t.Fatalf("the flag is part of what the lock records: %s", stderr)
	}
	h.mustRun(t, "lock")
	if p := h.readLock(t).Datapacks["autoslabs"]; !p.ResourcePack {
		t.Fatalf("the lock mirrors the flag: %+v", p)
	}
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["pack"] = map[string]any{"type": "resourcepack", "resourcepack": true}
	})
	if code, stdout, _ := h.run(t, "--json", "list"); code == 0 || failureCode(t, stdout).Code != "manifest-invalid" {
		t.Fatalf("resourcepack is refused on anything but a datapack: %s", stdout)
	}
}

func TestExportsShipAHybridDatapacksResourcePackCopy(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "terralith")
	h.mustRun(t, "set", "requires.terralith.resourcepack", "true")
	h.mustRun(t, "lock")
	h.mustRun(t, "install")
	h.allowMrpackHost(t)

	h.mustRun(t, "export", "mrpack", "--version", "1.0")
	index, _ := readMrpack(t, filepath.Join(h.dir, "build", "pack-1.0.mrpack"))
	paths := map[string]map[string]string{}
	for _, f := range index.Files {
		paths[f.Path] = f.Env
	}
	if env := paths["resourcepacks/Terralith_26.2_v2.6.4.zip"]; env["client"] != "required" || env["server"] != "unsupported" {
		t.Fatalf("the resource pack copy is a client file of its own: %+v", index.Files)
	}
	if _, ok := paths["datapacks/Terralith_26.2_v2.6.4.zip"]; !ok {
		t.Fatalf("the datapack copy still ships: %+v", index.Files)
	}
}

func TestAddAHybridDatapack(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	hybrid := makeJarFiles(t, "hybrid", "autoslabs.zip", map[string]string{"pack.mcmeta": datapackMcmeta, "data/a/tags/x.json": "{}", "assets/a/lang/en_us.json": "{}"})
	path := writeOutside(t, hybrid.filename, hybrid.data)
	code, stdout, _ := h.run(t, "--json", "add", path)
	if e := failureCode(t, stdout); code == 0 || !strings.Contains(e.Help, "--resourcepack") {
		t.Fatalf("the ambiguity names the hybrid flag: %+v", e)
	}
	if code, _, stderr := h.run(t, "add", path, "--type", "resourcepack", "--resourcepack"); code == 0 || !strings.Contains(stderr, "--resourcepack doesn't apply to a resourcepack") {
		t.Fatalf("the flag is a datapack's: %s", stderr)
	}

	h.mustRun(t, "add", path, "--resourcepack")
	h.mustRun(t, "datapack", "add", "terralith", "--resourcepack")
	m, l := h.readManifest(t), h.readLock(t)
	for _, key := range []string{"autoslabs", "terralith"} {
		if !m.Requires[key].ResourcePack || m.Requires[key].Kind() != manifest.TypeDatapack || !l.Datapacks[key].ResourcePack {
			t.Fatalf("%s is a datapack placed as a resource pack too: %+v %+v", key, m.Requires[key], l.Datapacks[key])
		}
	}
}

func TestImportMrpackKeepsAHybridDatapackAsBoth(t *testing.T) {
	h := newHarness(t)
	autoslabs := h.jars["autoslabs"]
	unknown := makeJarFiles(t, "tweaks", "Tweaks.zip", map[string]string{"pack.mcmeta": datapackMcmeta, "data/tweaks/tags/x.json": "{}", "assets/tweaks/lang/en_us.json": "{}"})
	index := mrpackIndex{
		FormatVersion: 1, Game: "minecraft", VersionID: "40", Name: "Better",
		Files: []mrpackIndexFile{{
			Path: "resourcepacks/AutoslabsCompat.zip", Hashes: map[string]string{"sha1": autoslabs.sha1, "sha512": autoslabs.sha512}, Env: mrpackEnv("both"),
			Downloads: []string{h.server.URL + "/cdn/" + autoslabs.filename}, FileSize: int64(len(autoslabs.data)),
		}},
		Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"},
	}
	archive := filepath.Join(t.TempDir(), "better.mrpack")
	writeMrpack(t, archive, index, map[string][]byte{
		"overrides/config/paxi/datapacks/AutoslabsCompat.zip": autoslabs.data,
		"overrides/config/paxi/datapacks/Tweaks.zip":          unknown.data,
		"overrides/resourcepacks/Tweaks.zip":                  unknown.data,
	})

	dir := filepath.Join(t.TempDir(), "better")
	var env struct {
		Data importResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "import", archive, "--dir", dir, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	res := env.Data.Mods
	if want := []resolve.LockedFile{{ID: "autoslabs-compat-bmc", Type: "datapack", Provider: "modrinth"}}; !slices.Equal(res.Locked, want) {
		t.Fatalf("a hybrid in both places locks once, as a datapack: %+v", res.Locked)
	}
	if len(res.Duplicates) != 0 {
		t.Fatalf("a hybrid's resource pack copy isn't a leftover: %+v", res.Duplicates)
	}
	if strings.Join(res.Unmanaged, ",") != "overrides/config/paxi/datapacks/Tweaks.zip,overrides/resourcepacks/Tweaks.zip" {
		t.Fatalf("a hybrid no provider has keeps both copies as overrides: %+v", res.Unmanaged)
	}
	m, l := readProject(t, dir)
	if got := m.Requires["autoslabs-compat-bmc"]; got.Type != manifest.TypeDatapack || !got.ResourcePack || got.Filename != "AutoslabsCompat.zip" {
		t.Fatalf("the entry places it as both: %+v", got)
	}
	if p := l.Datapacks["autoslabs-compat-bmc"]; !p.ResourcePack {
		t.Fatalf("the lock places it as both: %+v", p)
	}
	if len(l.ResourcePacks) != 0 {
		t.Fatalf("no second entry: %+v", l.ResourcePacks)
	}
}
