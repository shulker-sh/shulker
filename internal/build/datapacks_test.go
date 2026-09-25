package build

import (
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/provider"
)

const datapackMcmeta = `{"pack":{"pack_format":48,"description":"a datapack"}}`

// lockDatapack locks a datapack zip of entries from Modrinth as key on side, and returns its bytes.
func (p *testProject) lockDatapack(key, side string, entries map[string]string) string {
	p.t.Helper()
	data := zipOf(p.t, entries)
	p.lockPack(manifest.TypeDatapack, key, p.modrinth, p.modrinth.publish(mod("dp-"+key, key), provider.Version{ID: "m-" + key + "-1", Number: "1.0", File: provider.File{Filename: key + "-1.0.zip"}}, data))
	dp := p.b.Lock.Datapacks[key]
	dp.Side = side
	p.b.Lock.Datapacks[key] = dp
	return string(data)
}

func TestClientBuildPlacesDatapacks(t *testing.T) {
	p := newProject(t)
	data := p.lockDatapack("terralith", "both", map[string]string{"pack.mcmeta": datapackMcmeta, "data/terralith/worldgen/x.json": "{}"})

	report := p.mustBuild("client", Options{})
	if !contains(report.Warnings, "terralith: placed in datapacks/, which only some global datapack mods read; add one, such as paxi, to load it in every world") {
		t.Fatalf("no warning without a global datapack mod: %q", report.Warnings)
	}
	if p.built("client", "datapacks/terralith.zip") != data {
		t.Fatal("the datapack goes to datapacks/ without a global datapack mod")
	}

	p.lockMod("paxi", p.modrinth, p.modrinth.publish(mod("paxi-id", "paxi"), provider.Version{ID: "m-paxi-1", Number: "5.1", File: provider.File{Filename: "Paxi-26.2-Fabric-5.1.jar"}}, modJar(t, "paxi", "5.1")))
	report = p.mustBuild("client", Options{})
	if contains(report.Warnings, "global datapack mods") {
		t.Fatalf("Paxi loads it, so nothing to warn about: %q", report.Warnings)
	}
	if p.built("client", "config/paxi/datapacks/terralith.zip") != data {
		t.Fatal("with Paxi placed, the datapack goes to its folder")
	}
	if p.hasBuilt("client", "datapacks/terralith.zip") {
		t.Fatal("the old placement is removed")
	}
}

func TestServerBuildPlacesDatapacksInItsWorld(t *testing.T) {
	p := newProject(t)
	data := p.lockDatapack("terralith", "both", map[string]string{"pack.mcmeta": datapackMcmeta, "data/terralith/worldgen/x.json": "{}"})
	p.lockDatapack("loot", "client", map[string]string{"pack.mcmeta": datapackMcmeta, "data/loot/loot_table/chest.json": "{}"})
	p.b.Manifest.Server.Properties = map[string]any{"level-name": "adventure"}

	report := p.mustBuild("server", Options{NoLauncher: true})
	if contains(report.Warnings, "global datapack mods") {
		t.Fatalf("the server's world loads its datapacks without a mod: %q", report.Warnings)
	}
	if got := p.project("data/server/adventure/datapacks/terralith.zip"); got != data {
		t.Fatal("the datapack is written through the world's link")
	}
	if p.project("data/server/adventure/datapacks/loot.zip") != "" {
		t.Fatal("a client-side datapack stays off the server")
	}

	delete(p.b.Lock.Datapacks, "terralith")
	delete(p.b.Manifest.Requires, "terralith")
	p.mustBuild("server", Options{NoLauncher: true})
	if p.project("data/server/adventure/datapacks/terralith.zip") != "" {
		t.Fatal("a dropped datapack leaves the world")
	}
}

func TestInPlaceServerBuildWritesItsWorldsDatapacks(t *testing.T) {
	p := newProject(t)
	p.b.Manifest.Server.Build = "."
	data := p.lockDatapack("terralith", "both", map[string]string{"pack.mcmeta": datapackMcmeta, "data/terralith/worldgen/x.json": "{}"})
	p.mustBuild("server", Options{NoLauncher: true})
	if p.project("world/datapacks/terralith.zip") != data {
		t.Fatal("an in-place server writes its world's datapacks")
	}
}

func TestBuildPlacesAHybridDatapackAsAResourcePack(t *testing.T) {
	p := newProject(t)
	data := p.lockDatapack("autoslabs", "both", map[string]string{"pack.mcmeta": datapackMcmeta, "data/a/tags/x.json": "{}", "assets/a/lang/en_us.json": "{}"})
	p.b.Manifest.Requires["autoslabs"] = manifest.Require{Type: manifest.TypeDatapack, Provider: "modrinth", ResourcePack: true}
	hybrid := p.b.Lock.Datapacks["autoslabs"]
	hybrid.ResourcePack = true
	p.b.Lock.Datapacks["autoslabs"] = hybrid

	p.mustBuild("client", Options{})
	if p.built("client", "datapacks/autoslabs.zip") != data || p.built("client", "resourcepacks/autoslabs.zip") != data {
		t.Fatal("a hybrid is placed as a datapack and as a resource pack")
	}
	if !strings.Contains(p.built("client", "options.txt"), `"file/autoslabs.zip"`) {
		t.Fatal("its resource pack copy is enabled like any other")
	}

	hybrid.Side = "server"
	p.b.Lock.Datapacks["autoslabs"] = hybrid
	p.mustBuild("client", Options{})
	if p.hasBuilt("client", "datapacks/autoslabs.zip") {
		t.Fatal("side governs the datapack copy")
	}
	if p.built("client", "resourcepacks/autoslabs.zip") != data {
		t.Fatal("the resource pack copy stays on the client whatever the side")
	}
}
