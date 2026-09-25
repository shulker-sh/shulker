package marker

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"shulker.sh/shulker/internal/loader"
)

// jarFor is the marker jar's entries for a pack on the named loader and Minecraft version.
func jarFor(t *testing.T, loaderName, minecraft string, edit func(info *Info)) map[string]string {
	t.Helper()
	l, ok := loader.For(loaderName, minecraft)
	if !ok {
		t.Fatalf("no loader %s", loaderName)
	}
	info := Info{
		ID: ModID("pack"), Version: Version("", "0123456789abcdef"), Name: "pack",
		Description: Description{Summary: "Minecraft " + minecraft + " • " + loaderName + " 1.0.0 • 0 mods"},
		Files:       map[string][]byte{"shulker.json": []byte("{}"), "shulker.lock": []byte("{}")},
	}
	if edit != nil {
		edit(&info)
	}
	jar, err := Jar(l, info)
	if err != nil {
		t.Fatal(err)
	}
	return unzip(t, jar)
}

func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name] = string(content)
	}
	return entries
}

func TestModIDAndVersion(t *testing.T) {
	if got := ModID("my.pack-x"); got != "shulker_my_pack_x" {
		t.Errorf("ModID = %s", got)
	}
	if got := Version("", "0123456789abcdef"); got != "0.0.0+01234567" {
		t.Errorf("Version without a pack version = %s", got)
	}
	if got := Version("2.1.0", "0123456789abcdef"); got != "2.1.0+01234567" {
		t.Errorf("Version = %s", got)
	}
}

func TestDescriptionRendersForModMenuAndFML(t *testing.T) {
	d := Description{
		Text:     "  Survival with <friends>.\n",
		Summary:  "Minecraft 26.2 • fabric 1.0.0 • 2 mods",
		Labels:   []Label{{Name: "OS", Value: "macOS"}, {Name: "Features", Value: "shaders"}},
		Sections: []Section{{Title: "Mods", Items: []Item{{Text: "iris", Note: "feature: shaders"}, {Text: "sodium"}}}, {Title: "Dependencies"}},
	}
	rich := "Survival with \\<friends>.\n\nMinecraft 26.2 • fabric 1.0.0 • 2 mods\n<gray><bold>OS:</bold></gray> macOS • <gray><bold>Features:</bold></gray> shaders\n\n<bold>Mods</bold>\n  • iris <gray>(feature: shaders)</gray>\n  • sodium"
	if got := d.render(quickText); got != rich {
		t.Errorf("QuickText:\n%s\nwant:\n%s", got, rich)
	}
	plain := "Survival with <friends>.\n\nMinecraft 26.2 • fabric 1.0.0 • 2 mods\nOS: macOS • Features: shaders\n\nMods\n  • iris (feature: shaders)\n  • sodium"
	if got := d.render(plainText); got != plain {
		t.Errorf("plain text:\n%s\nwant:\n%s", got, plain)
	}
}

func TestJarCarriesFilesAndModList(t *testing.T) {
	entries := jarFor(t, "fabric", "26.2", func(info *Info) {
		info.Direct, info.Deps = []string{"sodium", "iris"}, []string{"fabric-api"}
	})
	if entries["shulker/mods.txt"] != "fabric-api\niris\nsodium\n" {
		t.Errorf("mods.txt = %q", entries["shulker/mods.txt"])
	}
	if entries["shulker.json"] != "{}" || entries["shulker.lock"] != "{}" {
		t.Errorf("carried files: %v", slices.Sorted(maps.Keys(entries)))
	}
}

func TestFabricMarkerLinks(t *testing.T) {
	entries := jarFor(t, "fabric", "26.2", func(info *Info) {
		info.Links = map[string]string{"website": "https://e.com", "discord": "https://d.gg/x", "My Blog!": "https://blog.e.com"}
	})
	var meta struct {
		Contact map[string]string `json:"contact"`
		Custom  struct {
			ModMenu struct {
				Links         map[string]string `json:"links"`
				UpdateChecker bool              `json:"update_checker"`
			} `json:"modmenu"`
		} `json:"custom"`
	}
	if err := json.Unmarshal([]byte(entries["fabric.mod.json"]), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Contact["homepage"] != "https://e.com" || meta.Custom.ModMenu.UpdateChecker {
		t.Fatalf("fabric.mod.json: %+v", meta)
	}
	if want := map[string]string{"modmenu.discord": "https://d.gg/x", "shulker.link.my_blog_": "https://blog.e.com"}; !maps.Equal(meta.Custom.ModMenu.Links, want) {
		t.Fatalf("modmenu links: %v", meta.Custom.ModMenu.Links)
	}
	if !strings.Contains(entries["assets/shulker_pack/lang/en_us.json"], `"shulker.link.my_blog_": "My Blog!"`) {
		t.Fatalf("lang: %s", entries["assets/shulker_pack/lang/en_us.json"])
	}
	if _, ok := entries["shulker/marker/ShulkerModMenu.class"]; !ok {
		t.Fatalf("fabric marker carries no ModMenu entrypoint (%v)", slices.Sorted(maps.Keys(entries)))
	}
}

func TestNeoForgeMarkerJar(t *testing.T) {
	entries := jarFor(t, "neoforge", "26.2", func(info *Info) {
		info.Description.Text = "Survival with <friends>."
		info.Authors = []string{"Alice", "shulker.sh"}
		info.License = "MIT"
		info.Links = map[string]string{
			"website": "https://example.com",
			"issues":  "https://example.com/issues",
			"license": "https://example.com/license",
		}
	})
	for _, name := range []string{"META-INF/neoforge.mods.toml", "pack.mcmeta", "icon.png", "shulker.json", "shulker.lock", "shulker/mods.txt"} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("marker has no %s (%v)", name, slices.Sorted(maps.Keys(entries)))
		}
	}
	for name := range entries {
		if strings.HasSuffix(name, ".class") || name == "fabric.mod.json" {
			t.Fatalf("neoforge marker should carry no fabric metadata or classes: %s", name)
		}
	}
	var meta struct {
		ModLoader       string `toml:"modLoader"`
		LoaderVersion   string `toml:"loaderVersion"`
		License         string `toml:"license"`
		LicenseURL      string `toml:"licenseURL"`
		IssueTrackerURL string `toml:"issueTrackerURL"`
		Mods            []struct {
			ModID       string `toml:"modId"`
			DisplayName string `toml:"displayName"`
			LogoFile    string `toml:"logoFile"`
			IconFile    string `toml:"iconFile"`
			IconBlur    bool   `toml:"iconBlur"`
			Authors     string `toml:"authors"`
			DisplayURL  string `toml:"displayURL"`
			Description string `toml:"description"`
		} `toml:"mods"`
	}
	if err := toml.Unmarshal([]byte(entries["META-INF/neoforge.mods.toml"]), &meta); err != nil {
		t.Fatal(err)
	}
	// Naming a language loader is what NeoForge warns about; left out, it uses the one that loads a
	// mod declaring no code.
	if meta.ModLoader != "" || meta.LoaderVersion != "" {
		t.Fatalf("marker toml: %+v", meta)
	}
	if meta.License != "MIT" || meta.LicenseURL != "https://example.com/license" {
		t.Fatalf("marker toml: %+v", meta)
	}
	if meta.IssueTrackerURL != "https://example.com/issues" || len(meta.Mods) != 1 {
		t.Fatalf("marker toml: %+v", meta)
	}
	mod := meta.Mods[0]
	if mod.ModID != "shulker_pack" || mod.DisplayName != "pack" || mod.LogoFile != "icon.png" {
		t.Fatalf("marker mod: %+v", mod)
	}
	// NeoForge draws the mod list icon from iconFile alone, and falls back to nothing without it.
	if mod.IconFile != "icon.png" || !mod.IconBlur {
		t.Fatalf("neoforge marker needs an icon of its own: %+v", mod)
	}
	if mod.Authors != "Alice, shulker.sh" || mod.DisplayURL != "https://example.com" {
		t.Fatalf("marker mod: %+v", mod)
	}
	if !strings.HasPrefix(mod.Description, "Survival with <friends>.\n\nMinecraft 26.2 • neoforge ") {
		t.Fatalf("description should be plain text:\n%s", mod.Description)
	}
	if strings.ContainsAny(mod.Description, "\\") || strings.Contains(mod.Description, "<bold>") {
		t.Fatalf("description should carry no QuickText tags:\n%s", mod.Description)
	}
}

func TestFMLMarkerPackMetadataIsCompatible(t *testing.T) {
	entries := jarFor(t, "neoforge", "26.2", nil)
	var meta struct {
		Pack struct {
			MaxFormat        int   `json:"max_format"`
			MinFormat        []int `json:"min_format"`
			SupportedFormats struct {
				MaxInclusive int `json:"max_inclusive"`
			} `json:"supported_formats"`
		} `json:"pack"`
	}
	if err := json.Unmarshal([]byte(entries["pack.mcmeta"]), &meta); err != nil {
		t.Fatal(err)
	}
	// 26.2 is resource format 88 and data format 107; both schemas have to admit numbers that big.
	if meta.Pack.MaxFormat < 107 || meta.Pack.SupportedFormats.MaxInclusive < 107 || len(meta.Pack.MinFormat) != 2 {
		t.Fatalf("pack.mcmeta must read as compatible with a current game: %+v", meta.Pack)
	}
}

func TestQuiltMarkerJarIsFabricMetadata(t *testing.T) {
	entries := jarFor(t, "quilt", "26.2", nil)
	// Quilt reads fabric.mod.json as is; a quilt.mod.json holding TOML stops the game loading.
	if _, ok := entries["quilt.mod.json"]; ok {
		t.Fatalf("quilt marker must not carry quilt.mod.json (%v)", slices.Sorted(maps.Keys(entries)))
	}
	meta, ok := entries["fabric.mod.json"]
	if !ok {
		t.Fatalf("quilt marker has no fabric.mod.json (%v)", slices.Sorted(maps.Keys(entries)))
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(meta), &parsed); err != nil {
		t.Fatalf("fabric.mod.json must be JSON: %v\n%s", err, meta)
	}
	if parsed["id"] != "shulker_pack" {
		t.Fatalf("marker id: %v", parsed["id"])
	}
}

func TestNeoForgeMarkerNamesItsLanguageLoaderWhereFMLRequiresOne(t *testing.T) {
	for _, c := range []struct {
		minecraft string
		declares  bool
	}{
		{"1.20.4", true},
		{"1.21.1", true},
		{"1.21.4", true},
		{"1.21.5", false},
		{"26.2", false},
	} {
		meta := jarFor(t, "neoforge", c.minecraft, nil)["META-INF/neoforge.mods.toml"]
		named := strings.Contains(meta, `modLoader = "lowcodefml"`) && strings.Contains(meta, `loaderVersion = "[1,)"`)
		if named != c.declares || (!c.declares && strings.Contains(meta, "modLoader")) {
			t.Errorf("minecraft %s: marker should declare a language loader: %v\n%s", c.minecraft, c.declares, meta)
		}
	}
}

func TestForgeMarkerNamesItsLanguageLoader(t *testing.T) {
	entries := jarFor(t, "forge", "26.2", nil)
	if _, ok := entries["META-INF/mods.toml"]; !ok {
		t.Fatalf("forge marker should declare itself in mods.toml: %v", slices.Sorted(maps.Keys(entries)))
	}
	if _, ok := entries["META-INF/neoforge.mods.toml"]; ok {
		t.Fatal("forge marker should not carry neoforge.mods.toml")
	}
	meta := entries["META-INF/mods.toml"]
	if strings.Contains(meta, "iconFile") {
		t.Fatal("iconFile is a NeoForge key; Forge reads logoFile")
	}
	// Forge rejects a mod file that names no language loader, or names one without a version, and
	// both loaders reject one with a blank license, which a manifest need not fill in.
	if !strings.Contains(meta, `modLoader = "lowcodefml"`) || !strings.Contains(meta, `loaderVersion = "[1,)"`) {
		t.Fatalf("forge marker must name its language loader:\n%s", meta)
	}
	if !strings.Contains(meta, `license = "All rights reserved"`) {
		t.Fatalf("marker with no manifest license must fall back:\n%s", meta)
	}
}
