package build

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

//go:generate sh modmenu/compile.sh

//go:embed assets/icon.png
var markerIcon []byte

//go:embed modmenu/classes
var markerClasses embed.FS

const (
	markerClassesDir = "modmenu/classes"
	markerEntrypoint = "shulker.marker.ShulkerModMenu"
	markerModsPath   = "shulker/mods.txt"
)

var markerTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func markerModID(name string) string {
	return "shulker_" + strings.NewReplacer(".", "_", "-", "_").Replace(name)
}

// markerVersion has to start with a digit: both FML loaders reject a mod whose version does not,
// so a manifest without a version gets a zero version with the lock hash as build metadata.
func markerVersion(packVersion, lockHash string) string {
	build := lockHash[:8]
	if packVersion == "" {
		packVersion = "0.0.0"
	}
	return packVersion + "+" + build
}

func markerJarPath(name string) string {
	return "mods/shulker-" + name + ".jar"
}

type markerEntry struct {
	name string
	data []byte
}

func (b *Builder) markerJar(side string, cond conditions, sel selection) ([]byte, error) {
	lockData, err := os.ReadFile(b.LockPath)
	if err != nil {
		return nil, err
	}
	manifestData, err := os.ReadFile(filepath.Join(b.Dir, manifest.FileName))
	if err != nil {
		return nil, err
	}
	lockHash, err := lock.FileSha256(b.LockPath)
	if err != nil {
		return nil, err
	}
	direct, deps := b.markerMods(side, sel)
	l, _ := loader.Lookup(b.Lock.Loader.Type)
	var entries []markerEntry
	// The marker declares itself in the file its loader reads, and that file decides the format.
	if strings.HasSuffix(l.MarkerFile, ".json") {
		entries, err = b.fabricMarker(side, lockHash, direct, deps, cond)
	} else {
		entries, err = b.tomlMarker(l, side, lockHash, direct, deps, cond)
	}
	if err != nil {
		return nil, err
	}
	entries = append(entries,
		markerEntry{manifest.FileName, manifestData},
		markerEntry{lock.FileName, lockData},
		markerEntry{markerModsPath, markerModList(direct, deps)},
	)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: markerTime})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(e.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (b *Builder) fabricMarker(side, lockHash string, direct, deps []string, cond conditions) ([]markerEntry, error) {
	id := markerModID(b.Manifest.Name)
	contact, links, labels := markerLinks(b.Manifest.Links)
	modmenu := map[string]any{"update_checker": false}
	meta := map[string]any{
		"schemaVersion": 1,
		"id":            id,
		"version":       markerVersion(b.Manifest.Version, lockHash),
		"name":          b.Manifest.DisplayName(side),
		"description":   b.markerDescription(direct, deps, cond, quickText),
		"icon":          "assets/" + id + "/icon.png",
		"environment":   "*",
		"entrypoints":   map[string]any{"modmenu": []string{markerEntrypoint}},
		"custom":        map[string]any{"modmenu": modmenu},
	}
	if len(links) > 0 {
		modmenu["links"] = links
	}
	if len(b.Manifest.Authors) > 0 {
		meta["authors"] = b.Manifest.Authors
	}
	// Fabric, unlike FML, is content with a mod that names no license, so this one stays unset.
	if b.Manifest.License != "" {
		meta["license"] = b.Manifest.License
	}
	if len(contact) > 0 {
		meta["contact"] = contact
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	entries := []markerEntry{
		{"fabric.mod.json", metaData},
		{"assets/" + id + "/icon.png", markerIcon},
	}
	if len(labels) > 0 {
		lang, err := json.MarshalIndent(labels, "", "  ")
		if err != nil {
			return nil, err
		}
		entries = append(entries, markerEntry{"assets/" + id + "/lang/en_us.json", lang})
	}
	classes, err := markerClassEntries()
	if err != nil {
		return nil, err
	}
	return append(entries, classes...), nil
}

const markerLogo = "icon.png"

// markerLicense falls back rather than leaving the field empty, which both FML loaders read as a
// mod file declaring no license and refuse to load.
func (b *Builder) markerLicense() string {
	if b.Manifest.License == "" {
		return "All rights reserved"
	}
	return b.Manifest.License
}

type markerToml struct {
	ModLoader       string          `toml:"modLoader,omitempty"`
	LoaderVersion   string          `toml:"loaderVersion,omitempty"`
	License         string          `toml:"license"`
	LicenseURL      string          `toml:"licenseURL,omitempty"`
	IssueTrackerURL string          `toml:"issueTrackerURL,omitempty"`
	Mods            []markerTomlMod `toml:"mods"`
}

type markerTomlMod struct {
	ModID       string `toml:"modId"`
	Version     string `toml:"version"`
	DisplayName string `toml:"displayName"`
	LogoFile    string `toml:"logoFile"`
	IconFile    string `toml:"iconFile,omitempty"`
	IconBlur    bool   `toml:"iconBlur,omitempty"`
	Authors     string `toml:"authors,omitempty"`
	DisplayURL  string `toml:"displayURL,omitempty"`
	Description string `toml:"description"`
}

// tomlMarker builds the NeoForge and Forge marker: no classes, since both loaders load a mod that
// declares none, and a pack.mcmeta so Forge doesn't warn that the mod's pack metadata is missing.
func (b *Builder) tomlMarker(l loader.Loader, side, lockHash string, direct, deps []string, cond conditions) ([]markerEntry, error) {
	mod := markerTomlMod{
		ModID:       markerModID(b.Manifest.Name),
		Version:     markerVersion(b.Manifest.Version, lockHash),
		DisplayName: b.Manifest.DisplayName(side),
		LogoFile:    markerLogo,
		Authors:     strings.Join(b.Manifest.Authors, ", "),
		DisplayURL:  b.Manifest.Links["website"],
		Description: b.markerDescription(direct, deps, cond, plainText),
	}
	meta := markerToml{
		ModLoader:       "lowcodefml",
		LoaderVersion:   "[1,)",
		License:         b.markerLicense(),
		LicenseURL:      b.Manifest.Links["license"],
		IssueTrackerURL: b.Manifest.Links["issues"],
	}
	if l.Name == "neoforge" {
		// NeoForge reads logoFile only as the wide banner on the detail pane; the square icon beside
		// the name in the list comes from iconFile, which has no fallback, so a mod that sets just
		// logoFile shows no icon at all. iconBlur scales the 128px icon into the 24px slot smoothly
		// rather than by nearest neighbour.
		mod.IconFile, mod.IconBlur = markerLogo, true
		// It also deprecated lowcodefml, mapping it to javafml, which loads a mod that declares no
		// code; naming it only earns a warning. Both keys go together, since a loaderVersion without
		// a modLoader is rejected. Forge has no such default and refuses a file missing either key.
		meta.ModLoader, meta.LoaderVersion = "", ""
	}
	meta.Mods = []markerTomlMod{mod}
	var metaData bytes.Buffer
	if err := toml.NewEncoder(&metaData).Encode(meta); err != nil {
		return nil, err
	}
	// The marker ships no assets or data; the pack metadata only exists so FML doesn't report it as
	// a mod with missing pack metadata, and it has to read as compatible or the game leaves the mod
	// out of its pack list. Both schemas are written because the field names changed: Minecraft
	// read `pack_format` with a `supported_formats` range until 26.x, which reads `min_format` and
	// `max_format` (26.2 is resource format 88, data format 107 — well past the old 1–99 range
	// this used to declare). The ranges say "whatever is running", since there is nothing to break.
	pack, err := json.MarshalIndent(map[string]any{"pack": map[string]any{
		"description":       b.Manifest.DisplayName(side),
		"pack_format":       markerPackFormat,
		"supported_formats": map[string]int{"min_inclusive": 1, "max_inclusive": markerPackFormatMax},
		"min_format":        []int{1, 0},
		"max_format":        markerPackFormatMax,
	}}, "", "  ")
	if err != nil {
		return nil, err
	}
	return []markerEntry{
		{l.MarkerFile, metaData.Bytes()},
		{"pack.mcmeta", pack},
		{markerLogo, markerIcon},
	}, nil
}

const (
	markerPackFormat    = 15
	markerPackFormatMax = 9999
)

func markerClassEntries() ([]markerEntry, error) {
	var entries []markerEntry
	err := fs.WalkDir(markerClasses, markerClassesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := markerClasses.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, markerEntry{strings.TrimPrefix(path, markerClassesDir+"/"), data})
		return nil
	})
	return entries, err
}

func (b *Builder) markerMods(side string, sel selection) (direct, deps []string) {
	for id, m := range b.Lock.Mods {
		if !sel.included[id] || (m.Side != "both" && m.Side != side) {
			continue
		}
		if _, ok := b.Manifest.Mods()[id]; ok {
			direct = append(direct, id)
		} else {
			deps = append(deps, id)
		}
	}
	sort.Strings(direct)
	sort.Strings(deps)
	return direct, deps
}

func markerModList(direct, deps []string) []byte {
	ids := append(append([]string{}, direct...), deps...)
	sort.Strings(ids)
	return []byte(strings.Join(ids, "\n") + "\n")
}

var markerContactKeys = map[string]string{"website": "homepage", "issues": "issues", "source": "sources"}

var markerKnownLinks = map[string]bool{
	"buymeacoffee": true, "coindrop": true, "crowdin": true, "curseforge": true, "discord": true,
	"donate": true, "flattr": true, "github_releases": true, "github_sponsors": true, "kofi": true,
	"liberapay": true, "mastodon": true, "modrinth": true, "opencollective": true, "patreon": true,
	"paypal": true, "reddit": true, "twitch": true, "twitter": true, "wiki": true, "youtube": true,
}

func markerLinks(links map[string]string) (contact, modmenu, labels map[string]string) {
	contact, modmenu, labels = map[string]string{}, map[string]string{}, map[string]string{}
	for label, url := range links {
		if key, ok := markerContactKeys[label]; ok {
			contact[key] = url
			continue
		}
		if markerKnownLinks[label] {
			modmenu["modmenu."+label] = url
			continue
		}
		key := "shulker.link." + markerLangKey(label)
		modmenu[key] = url
		labels[key] = label
	}
	return contact, modmenu, labels
}

func markerLangKey(label string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(label) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	return sb.String()
}

// markerStyle renders a description for ModMenu, which parses it as QuickText, or as the plain text
// the FML loaders show verbatim.
type markerStyle struct{ rich bool }

var quickText, plainText = markerStyle{rich: true}, markerStyle{}

func (s markerStyle) tag(name, text string) string {
	if !s.rich {
		return text
	}
	return "<" + name + ">" + text + "</" + name + ">"
}

func (s markerStyle) escape(text string) string {
	if !s.rich {
		return text
	}
	return strings.ReplaceAll(text, "<", "\\<")
}

func (b *Builder) markerDescription(direct, deps []string, cond conditions, style markerStyle) string {
	entries := b.directEntries(cond)
	section := func(title string, ids []string) string {
		lines := make([]string, 0, len(ids)+1)
		lines = append(lines, style.tag("bold", title))
		for _, id := range ids {
			line := "  \u2022 " + id
			if text := cond.admittedBy(entries[id]); text != "" {
				line += " " + style.tag("gray", "("+text+")")
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}
	var parts []string
	if b.Manifest.Description != "" {
		parts = append(parts, style.escape(strings.TrimSpace(b.Manifest.Description)))
	}
	summary := fmt.Sprintf("Minecraft %s \u2022 %s %s \u2022 %d mods", b.Lock.Minecraft, b.Lock.Loader.Type, b.Lock.Loader.Version, len(direct)+len(deps))
	var variation []string
	if b.mentionsOS() {
		variation = append(variation, style.tag("gray", style.tag("bold", "OS:"))+" "+cond.osLabel())
	}
	if on := cond.featureLabels(); len(on) > 0 {
		variation = append(variation, style.tag("gray", style.tag("bold", "Features:"))+" "+strings.Join(on, ", "))
	}
	if len(variation) > 0 {
		summary += "\n" + strings.Join(variation, " \u2022 ")
	}
	parts = append(parts, summary)
	if len(direct) > 0 {
		parts = append(parts, section("Mods", direct))
	}
	if len(deps) > 0 {
		parts = append(parts, section("Dependencies", deps))
	}
	return strings.Join(parts, "\n\n")
}
