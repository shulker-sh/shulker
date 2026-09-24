package build

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

// markerFor is the marker jar's entries for a client build of a pack on the named loader.
func markerFor(t *testing.T, loaderName string, edit func(m *manifest.Manifest)) map[string]string {
	t.Helper()
	dir := t.TempDir()
	m := &manifest.Manifest{
		Name: "pack", Minecraft: "26.2",
		Loader:   manifest.Loader{Type: loaderName},
		Requires: map[string]manifest.Require{},
		Client:   &manifest.Client{},
	}
	if edit != nil {
		edit(m)
	}
	lk := lock.New()
	lk.Minecraft = "26.2"
	lk.Loader = lock.Loader{Type: loaderName, Version: "1.0.0"}
	lk.Java = lock.Java{Major: 25, Component: "java-runtime-epsilon"}
	b := &Builder{Dir: dir, Manifest: m, Lock: lk, LockPath: filepath.Join(dir, lock.FileName), Cache: &cache.Cache{Dir: t.TempDir()}}
	if err := m.Save(filepath.Join(dir, manifest.FileName)); err != nil {
		t.Fatal(err)
	}
	if err := lk.Save(b.LockPath); err != nil {
		t.Fatal(err)
	}
	cond := b.conditions(Options{NoOS: true})
	jar, err := b.markerJar("client", cond, b.selectMods(cond))
	if err != nil {
		t.Fatal(err)
	}
	return unzip(t, jar)
}

func TestNeoForgeMarkerJar(t *testing.T) {
	entries := markerFor(t, "neoforge", func(m *manifest.Manifest) {
		m.Description = "Survival with <friends>."
		m.Authors = []string{"Alice", "shulker.sh"}
		m.License = "MIT"
		m.Links = map[string]string{
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
	entries := markerFor(t, "neoforge", nil)
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
	entries := markerFor(t, "quilt", nil)
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

func TestForgeMarkerNamesItsLanguageLoader(t *testing.T) {
	entries := markerFor(t, "forge", nil)
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
