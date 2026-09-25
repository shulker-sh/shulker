package build

import (
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
)

func adoptManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Minecraft: "26.2",
		Client:    &manifest.Client{},
		Requires: map[string]manifest.Require{
			"fresh":         {Type: manifest.TypeResourcePack, Filename: "FreshAnimations.zip"},
			"faithful":      {Type: manifest.TypeResourcePack},
			"bsl":           {Type: manifest.TypeShader, Filename: "BSL_v10.zip"},
			"complementary": {Type: manifest.TypeShader},
		},
	}
}

func TestAdoptPackChoicesMovesTheShippedChoicesIntoTheManifest(t *testing.T) {
	m := adoptManifest()
	overrides := []packarchive.Override{
		{Layer: "overrides", Path: "options.txt", Data: []byte("fov:0.5\nresourcePacks:[\"vanilla\",\"mod_resources\",\"file/faithful.zip\",\"programmer_art\",\"file/FreshAnimations.zip\",\"file/Gone.zip\"]\nlang:en_us\n")},
		{Layer: "overrides", Path: "config/iris.properties", Data: []byte("enableShaders=true\nshaderPack=BSL_v10.zip\n")},
	}
	kept, warnings := AdoptPackChoices(m, &lock.Lock{Minecraft: "26.2"}, overrides)
	if got := *m.Client.ResourcePacks; !slices.Equal(got, []string{"fresh", "programmer_art", "faithful"}) {
		t.Fatalf("resourcePacks %q", got)
	}
	if m.Client.Shader == nil || *m.Client.Shader != "bsl" {
		t.Fatalf("shader %v", m.Client.Shader)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Gone.zip") {
		t.Fatalf("warnings %q", warnings)
	}
	if got := string(kept[0].Data); got != "fov:0.5\nlang:en_us\n" {
		t.Fatalf("options.txt kept %q", got)
	}
	if got := string(kept[1].Data); got != "enableShaders=true\n" {
		t.Fatalf("iris.properties kept %q", got)
	}
}

func TestAdoptPackChoicesTakesAnEmptyShaderAsNone(t *testing.T) {
	m := adoptManifest()
	AdoptPackChoices(m, &lock.Lock{Minecraft: "26.2"}, []packarchive.Override{{Layer: "overrides", Path: "config/iris.properties", Data: []byte("enableShaders=true\nshaderPack=\n")}})
	if m.Client.Shader == nil || *m.Client.Shader != "" || m.Client.ResourcePacks != nil {
		t.Fatalf("shader %v, resource packs %v", m.Client.Shader, m.Client.ResourcePacks)
	}
}

func TestAdoptPackChoicesLeavesAListNamingAModsOwnPack(t *testing.T) {
	m := adoptManifest()
	options := "resourcePacks:[\"vanilla\",\"moonlight:merged\",\"file/faithful.zip\"]\n"
	kept, _ := AdoptPackChoices(m, &lock.Lock{Minecraft: "26.2"}, []packarchive.Override{{Layer: "overrides", Path: "options.txt", Data: []byte(options)}})
	if m.Client.ResourcePacks != nil || string(kept[0].Data) != options {
		t.Fatalf("resource packs %v, options.txt %q", m.Client.ResourcePacks, kept[0].Data)
	}
}
