package build

import (
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// shaderProject has iris placed and the bsl and complementary shaders locked, and two resource
// packs, faithful and fresh.
func shaderProject(t *testing.T) *testProject {
	t.Helper()
	p := newProject(t)
	iris := provider.Project{ID: "iris-id", Slug: "iris", Title: "Iris", Type: manifest.TypeMod}
	p.lockMod("iris", p.modrinth, p.modrinth.publish(iris, provider.Version{ID: "v-iris", Number: "1.8", File: provider.File{Filename: "iris.jar"}}, modJar(t, "iris", "1.8")))
	for _, key := range []string{"bsl", "complementary"} {
		sp := provider.Project{ID: key + "-id", Slug: key, Title: key, Type: manifest.TypeShader}
		p.lockPack(manifest.TypeShader, key, p.modrinth, p.modrinth.publish(sp, provider.Version{ID: "v-" + key, Number: "1", Loaders: []string{}, File: provider.File{Filename: key + "-1.zip"}}, packZip(t, key)), "iris")
	}
	for _, key := range []string{"faithful", "fresh"} {
		rp := provider.Project{ID: key + "-id", Slug: key, Title: key, Type: manifest.TypeResourcePack}
		p.lockPack(manifest.TypeResourcePack, key, p.modrinth, p.modrinth.publish(rp, provider.Version{ID: "v-" + key, Number: "1", Loaders: []string{}, File: provider.File{Filename: key + "-1.zip"}}, packZip(t, key)))
	}
	return p
}

func TestNoShaderStartsOnUnlessTheManifestNamesOne(t *testing.T) {
	p := shaderProject(t)
	report := p.mustBuild("client", Options{})
	if p.hasBuilt("client", "config/iris.properties") {
		t.Fatalf("a shader was enabled: %q", p.built("client", "config/iris.properties"))
	}
	if hasWarning(report, "placed but not enabled") {
		t.Fatalf("warned about a shader the manifest leaves off: %v", report.Warnings)
	}

	shader := "complementary"
	p.b.Manifest.Client.Shader = &shader
	p.mustBuild("client", Options{})
	if got := p.built("client", "config/iris.properties"); !strings.Contains(got, "shaderPack=complementary.zip") || !strings.Contains(got, "enableShaders=true") {
		t.Fatalf("iris.properties: %q", got)
	}
}

func TestAnEmptyShaderClearsTheOneTheOverridesShip(t *testing.T) {
	p := shaderProject(t)
	p.file("overrides/config/iris.properties", "enableShaders=true\nshaderPack=bsl.zip\nmaxShadowRenderDistance=16\n")
	p.mustBuild("client", Options{})
	if got := p.built("client", "config/iris.properties"); !strings.Contains(got, "shaderPack=bsl.zip") {
		t.Fatalf("with no manifest choice the shipped shader stands: %q", got)
	}
	none := ""
	p.b.Manifest.Client.Shader = &none
	p.mustBuild("client", Options{})
	got := p.built("client", "config/iris.properties")
	if !strings.Contains(got, "shaderPack=\n") || !strings.Contains(got, "maxShadowRenderDistance=16") || !strings.Contains(got, "enableShaders=true") {
		t.Fatalf("iris.properties: %q", got)
	}
}

func TestAnUnknownShaderFailsTheBuild(t *testing.T) {
	p := shaderProject(t)
	shader := "nope"
	p.b.Manifest.Client.Shader = &shader
	if _, err := p.build("client", Options{}); out.CodeOf(err) != "pack-unknown" {
		t.Fatalf("err %v, want pack-unknown", err)
	}
}

func TestTheResourcePackListSetsWhatStartsOnAndItsOrder(t *testing.T) {
	p := shaderProject(t)
	p.b.Manifest.Client.ResourcePacks = &[]string{"fresh", "programmer_art", "faithful"}
	report := p.mustBuild("client", Options{})
	if got := p.built("client", "options.txt"); !strings.Contains(got, `resourcePacks:["vanilla","file/faithful.zip","programmer_art","file/fresh.zip"]`) {
		t.Fatalf("options.txt: %q", got)
	}
	p.b.Manifest.Client.ResourcePacks = &[]string{"fresh"}
	report = p.mustBuild("client", Options{})
	if got := p.built("client", "options.txt"); !strings.Contains(got, `resourcePacks:["vanilla","file/fresh.zip"]`) {
		t.Fatalf("an untouched list follows the manifest: %q", got)
	}
	if hasWarning(report, "placed but not enabled") {
		t.Fatalf("warned about a pack the manifest leaves off: %v", report.Warnings)
	}
}

func TestAListThePlayerChangedIsKeptUntilForce(t *testing.T) {
	p := shaderProject(t)
	p.b.Manifest.Client.ResourcePacks = &[]string{"fresh"}
	p.mustBuild("client", Options{})
	mine := `resourcePacks:["vanilla","file/faithful.zip"]`
	p.writeBuilt("client", "options.txt", mine+"\n")

	report := p.mustBuild("client", Options{})
	if got := p.built("client", "options.txt"); !strings.Contains(got, mine) || len(report.Warnings) != 0 {
		t.Fatalf("an unchanged manifest keeps the player's list: %q, %v", got, report.Warnings)
	}
	p.b.Manifest.Client.ResourcePacks = &[]string{"faithful", "fresh"}
	report = p.mustBuild("client", Options{})
	if got := p.built("client", "options.txt"); !strings.Contains(got, mine) {
		t.Fatalf("a manifest change doesn't overwrite the player's list: %q", got)
	}
	if !slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.Contains(w, "shulker build --force") }) {
		t.Fatalf("no note naming --force: %v", report.Warnings)
	}
	p.mustBuild("client", Options{Force: true})
	if got := p.built("client", "options.txt"); !strings.Contains(got, `resourcePacks:["vanilla","file/fresh.zip","file/faithful.zip"]`) {
		t.Fatalf("--force writes the manifest's list: %q", got)
	}
}

func TestARenamedPackInAListThePlayerChangedIsNamed(t *testing.T) {
	p := shaderProject(t)
	p.b.Manifest.Client.ResourcePacks = &[]string{"fresh", "faithful"}
	p.mustBuild("client", Options{})
	mine := `resourcePacks:["vanilla","file/fresh.zip"]`
	p.writeBuilt("client", "options.txt", mine+"\n")

	entry := p.b.Manifest.Requires["fresh"]
	entry.Filename = "Fresh Animations.zip"
	p.b.Manifest.Requires["fresh"] = entry
	locked := p.b.Lock.ResourcePacks["fresh"]
	locked.Filename = "Fresh Animations.zip"
	p.b.Lock.ResourcePacks["fresh"] = locked
	report := p.mustBuild("client", Options{})
	if got := p.built("client", "options.txt"); !strings.Contains(got, mine) {
		t.Fatalf("the player's list was rewritten: %q", got)
	}
	if hasWarning(report, "changed in shulker.json") || !hasWarning(report, "fresh.zip was renamed Fresh Animations.zip") {
		t.Fatalf("warnings: %v", report.Warnings)
	}
}
