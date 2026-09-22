package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
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
