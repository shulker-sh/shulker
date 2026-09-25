package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

type packLock struct {
	ResourcePacks map[string]struct {
		Filename         string `json:"filename"`
		ProviderFilename string `json:"providerFilename"`
		Channel          string `json:"channel"`
	} `json:"resourcepacks"`
	Shaders map[string]struct {
		Filename         string   `json:"filename"`
		ProviderFilename string   `json:"providerFilename"`
		Loaders          []string `json:"loaders"`
	} `json:"shaders"`
}

func TestResourcePacksAndShaders(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")

	// No --type: the provider's own project type settles what each one is.
	// Packs are placed on the client side, so the add line names them rather
	// than reading as if the pack went nowhere.
	if stdout := h.mustRun(t, "add", "fresh-animations"); !strings.Contains(stdout, "» all sides") || !strings.Contains(stdout, "client only") {
		t.Fatalf("a pack should name the sides it reaches: %s", stdout)
	}
	h.mustRun(t, "shader", "add", "complementary-reimagined")
	h.mustRun(t, "add", "irisshaders")
	h.mustRun(t, "set", "client.shader", "complementary-reimagined")

	var l packLock
	h.readJSON(t, "shulker.lock", &l)
	if got := l.ResourcePacks["fresh-animations"]; got.Filename != "FreshAnimations_v1.9.4.zip" || got.ProviderFilename != "FreshAnimations_v1.9.4.zip" || got.Channel != "release" {
		t.Fatalf("locked resource pack: %+v", l.ResourcePacks)
	}
	if got := l.Shaders["complementary-reimagined"]; got.Filename != "ComplementaryReimagined_r5.5.1.zip" || got.ProviderFilename != "ComplementaryReimagined_r5.5.1.zip" || !slices.Equal(got.Loaders, []string{"iris"}) {
		t.Fatalf("locked shader: %+v", l.Shaders)
	}
	var manifest struct {
		Requires map[string]map[string]any `json:"requires"`
	}
	h.readJSON(t, "shulker.json", &manifest)
	if manifest.Requires["fresh-animations"]["type"] != "resourcepack" || manifest.Requires["complementary-reimagined"]["type"] != "shader" {
		t.Fatalf("requires: %+v", manifest.Requires)
	}
	// The provider's file name is recorded as the entry's own, so it stays put when the pack updates.
	if manifest.Requires["fresh-animations"]["filename"] != "FreshAnimations_v1.9.4.zip" || manifest.Requires["complementary-reimagined"]["filename"] != "ComplementaryReimagined_r5.5.1.zip" {
		t.Fatalf("requires: %+v", manifest.Requires)
	}

	stdout := h.mustRun(t, "list")
	for _, want := range []string{"Resource packs\n", "• fresh-animations", "Shaders\n", "• complementary-reimagined"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("list output has no %q: %s", want, stdout)
		}
	}

	h.mustRun(t, "install")
	if got := readBuilt(t, h, "resourcepacks/FreshAnimations_v1.9.4.zip"); got == "" {
		t.Fatal("resource pack was not placed")
	}
	if got := readBuilt(t, h, "shaderpacks/ComplementaryReimagined_r5.5.1.zip"); got == "" {
		t.Fatal("shader was not placed")
	}
	if got := readBuilt(t, h, "options.txt"); !strings.Contains(got, `resourcePacks:["vanilla","file/FreshAnimations_v1.9.4.zip"]`) {
		t.Fatalf("options.txt: %q", got)
	}
	iris := readBuilt(t, h, "config/iris.properties")
	if !strings.Contains(iris, "shaderPack=ComplementaryReimagined_r5.5.1.zip") || !strings.Contains(iris, "enableShaders=true") {
		t.Fatalf("iris.properties: %q", iris)
	}

	// The enabled list is seeded once and then held: a list the player has made
	// their own survives a rebuild, and shulker never reorders it.
	options := filepath.Join(h.dir, "build", "client", "options.txt")
	if err := os.WriteFile(options, []byte("fov:0.5\nresourcePacks:[\"vanilla\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "build")
	got := readBuilt(t, h, "options.txt")
	if !strings.Contains(got, "resourcePacks:[\"vanilla\"]\n") || strings.Contains(got, "file/FreshAnimations_v1.9.4.zip") {
		t.Fatalf("a list the player turned off was seeded again: %q", got)
	}
	if !strings.Contains(got, "fov:0.5") {
		t.Fatalf("an in-game edit was lost: %q", got)
	}

	h.mustRun(t, "remove", "fresh-animations")
	var after packLock
	h.readJSON(t, "shulker.lock", &after)
	if _, still := after.ResourcePacks["fresh-animations"]; still {
		t.Fatalf("removed resource pack is still locked: %+v", after.ResourcePacks)
	}
	if _, kept := after.Shaders["complementary-reimagined"]; !kept {
		t.Fatalf("removing a resource pack dropped the shader: %+v", after.Shaders)
	}
}

func TestPackFilename(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "fresh-animations")
	h.mustRun(t, "shader", "add", "complementary-reimagined")
	h.mustRun(t, "add", "irisshaders")
	h.mustRun(t, "set", "client.shader", "complementary-reimagined")
	h.mustRun(t, "install")
	options := filepath.Join(h.dir, "build", "client", "options.txt")
	if err := os.WriteFile(options, []byte("resourcePacks:[\"vanilla\",\"file/FreshAnimations_v1.9.4.zip\",\"file/other.zip\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.editManifest(t, func(m map[string]any) {
		requires := m["requires"].(map[string]any)
		requires["fresh-animations"].(map[string]any)["filename"] = "Fresh Animations.zip"
		requires["complementary-reimagined"].(map[string]any)["filename"] = "Complementary.zip"
	})
	h.mustRun(t, "lock")
	var l packLock
	h.readJSON(t, "shulker.lock", &l)
	if got := l.ResourcePacks["fresh-animations"]; got.Filename != "Fresh Animations.zip" || got.ProviderFilename != "FreshAnimations_v1.9.4.zip" {
		t.Fatalf("locked resource pack: %+v", got)
	}
	h.mustRun(t, "build")

	if readBuilt(t, h, "resourcepacks/Fresh Animations.zip") == "" {
		t.Fatal("the pack was not placed under its filename")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "resourcepacks", "FreshAnimations_v1.9.4.zip")); !os.IsNotExist(err) {
		t.Fatalf("the pack's old placement is still there: %v", err)
	}
	// The renamed entry keeps its place in the player's list, which is its priority.
	if got := readBuilt(t, h, "options.txt"); !strings.Contains(got, `resourcePacks:["vanilla","file/Fresh Animations.zip","file/other.zip"]`) {
		t.Fatalf("options.txt: %q", got)
	}
	if got := readBuilt(t, h, "config/iris.properties"); !strings.Contains(got, "shaderPack=Complementary.zip") {
		t.Fatalf("iris.properties: %q", got)
	}
}

func TestPackFilenameRefused(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "fresh-animations")
	h.mustRun(t, "shader", "add", "complementary-reimagined", "--as", "fresh")

	for _, name := range []string{"packs/Fresh.zip", "Fresh.jar"} {
		h.editManifest(t, func(m map[string]any) {
			m["requires"].(map[string]any)["fresh-animations"].(map[string]any)["filename"] = name
		})
		if code, _, stderr := h.run(t, "lock"); code == 0 || !strings.Contains(stderr, "manifest-invalid") {
			t.Fatalf("filename %q: exit %d: %s", name, code, stderr)
		}
	}

	// A shader in shaderpacks/ can't clash with a resource pack, but two names in one folder can, whatever their case.
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["fresh-animations"].(map[string]any)["filename"] = "fresh.zip"
	})
	h.mustRun(t, "lock")
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["fresh-animations"].(map[string]any)["filename"] = "Taken.zip"
		m["requires"].(map[string]any)["taken"] = map[string]any{"type": "resourcepack", "file": "files/taken.zip"}
	})
	writeProjectFile(t, h, "files/taken.zip", []byte("taken"))
	if code, _, stderr := h.run(t, "lock"); code == 0 || !strings.Contains(stderr, "pack-filename-taken") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestPackTypeDisagreesWithProvider(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	code, stdout, _ := h.run(t, "add", "fresh-animations", "--type", "shader", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "type-mismatch" {
		t.Fatalf("a type that disagrees with the provider should fail: code=%d env=%+v", code, env)
	}
}

func TestShippedPackListIsTheEnabledList(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "fresh-animations")
	h.mustRun(t, "set", "requires.fresh-animations.filename", "Fresh's Pack.zip")
	h.mustRun(t, "lock")
	shipped := `resourcePacks:["vanilla","fabric","file/Fresh's Pack.zip","file/Missing Pack.zip"]`
	writeFile(t, filepath.Join(h.dir, "overrides", "options.txt"), "fov:0.5\n"+shipped+"\n")
	h.mustRun(t, "install")

	// The pack's own list is already set, so it isn't seeded over, and a name
	// Minecraft writes escaped still counts as enabled.
	for range 2 {
		_, _, stderr := h.run(t, "build")
		if got := readBuilt(t, h, "options.txt"); !strings.Contains(got, shipped+"\n") {
			t.Fatalf("the shipped list was replaced: %q", got)
		}
		if strings.Contains(stderr, "placed but not enabled") {
			t.Fatalf("a pack the shipped list enables was reported: %s", stderr)
		}
		if !strings.Contains(stderr, "! options.txt enables Missing Pack.zip, but no pack is placed under that name") {
			t.Fatalf("no warning for a shipped entry nothing places: %s", stderr)
		}
	}
}

func TestPlayersPackListReportsOnlyWhatItLeavesOff(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "fresh-animations")
	h.mustRun(t, "install")
	options := filepath.Join(h.dir, "build", "client", "options.txt")
	writeFile(t, options, "resourcePacks:[\"vanilla\",\"file/Gone.zip\"]\n")

	_, _, stderr := h.run(t, "build")
	if !strings.Contains(stderr, "! FreshAnimations_v1.9.4 is placed but not enabled") {
		t.Fatalf("a pack the player's list leaves off wasn't reported: %s", stderr)
	}
	if strings.Contains(stderr, "Gone.zip") {
		t.Fatalf("an entry in the player's own list was reported: %s", stderr)
	}
}

func TestPlayersEditToAShippedListIsWhatIsReported(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "fresh-animations")
	writeFile(t, filepath.Join(h.dir, "overrides", "options.txt"), "resourcePacks:[\"vanilla\",\"file/FreshAnimations_v1.9.4.zip\",\"file/Missing.zip\"]\n")
	h.mustRun(t, "install")

	// The build keeps a file the player edited while its override is unchanged,
	// so the player's list is the one the game reads.
	writeFile(t, filepath.Join(h.dir, "build", "client", "options.txt"), "resourcePacks:[\"vanilla\"]\n")
	_, _, stderr := h.run(t, "build")
	if got := readBuilt(t, h, "options.txt"); got != "resourcePacks:[\"vanilla\"]\n" {
		t.Fatalf("the player's edit was overwritten: %q", got)
	}
	if !strings.Contains(stderr, "! FreshAnimations_v1.9.4 is placed but not enabled") {
		t.Fatalf("a pack the player's list leaves off wasn't reported: %s", stderr)
	}
	if strings.Contains(stderr, "Missing.zip") {
		t.Fatalf("the shipped list was reported in place of the player's: %s", stderr)
	}
}
