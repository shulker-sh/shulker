package cli

import (
	"reflect"
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
