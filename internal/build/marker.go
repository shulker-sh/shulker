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

	"github.com/shulker-sh/shulker/internal/lock"
	"github.com/shulker-sh/shulker/internal/manifest"
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

func markerVersion(packVersion, lockHash string) string {
	build := lockHash[:8]
	if packVersion == "" {
		return build
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

func (b *Builder) markerJar(targetName, side string, cond conditions, sel selection) ([]byte, error) {
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
	id := markerModID(b.Manifest.Name)
	contact, links, labels := markerLinks(b.Manifest.Links)
	modmenu := map[string]any{"update_checker": false}
	meta := map[string]any{
		"schemaVersion": 1,
		"id":            id,
		"version":       markerVersion(b.Manifest.Version, lockHash),
		"name":          b.Manifest.DisplayName(targetName),
		"description":   b.markerDescription(direct, deps, cond),
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
	entries = append(entries,
		markerEntry{manifest.FileName, manifestData},
		markerEntry{lock.FileName, lockData},
		markerEntry{markerModsPath, markerModList(direct, deps)},
	)
	classes, err := markerClassEntries()
	if err != nil {
		return nil, err
	}
	entries = append(entries, classes...)
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
		if _, ok := b.Manifest.Mods[id]; ok {
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

func (b *Builder) markerDescription(direct, deps []string, cond conditions) string {
	entries := b.directEntries(cond)
	section := func(title string, ids []string) string {
		lines := make([]string, 0, len(ids)+1)
		lines = append(lines, "<bold>"+title+"</bold>")
		for _, id := range ids {
			line := "  \u2022 " + id
			if text := cond.admittedBy(entries[id]); text != "" {
				line += " <gray>(" + text + ")</gray>"
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}
	var parts []string
	if b.Manifest.Description != "" {
		parts = append(parts, strings.ReplaceAll(strings.TrimSpace(b.Manifest.Description), "<", "\\<"))
	}
	summary := fmt.Sprintf("Minecraft %s \u00b7 %s %s \u00b7 %d mods", b.Lock.Minecraft, b.Lock.Loader.Type, b.Lock.Loader.Version, len(direct)+len(deps))
	var variation []string
	if b.mentionsOS() {
		variation = append(variation, "<gray><bold>OS:</bold></gray> <bold>"+cond.osLabel()+"</bold>")
	}
	if b.mentionsFeatures() {
		features := "none"
		if on := cond.featureLabels(); len(on) > 0 {
			features = "<bold>" + strings.Join(on, "</bold>, <bold>") + "</bold>"
		}
		variation = append(variation, "<gray><bold>Features:</bold></gray> "+features)
	}
	if len(variation) > 0 {
		summary += "\n" + strings.Join(variation, " \u00b7 ")
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
