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

func markerJarPath(name string) string {
	return "mods/shulker-" + name + ".jar"
}

type markerEntry struct {
	name string
	data []byte
}

func (b *Builder) markerJar(side string) ([]byte, error) {
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
	direct, deps := b.markerMods(side)
	id := markerModID(b.Manifest.Name)
	meta := map[string]any{
		"schemaVersion": 1,
		"id":            id,
		"version":       lockHash[:8],
		"name":          b.Manifest.Name,
		"description":   b.markerDescription(direct, deps),
		"icon":          "assets/" + id + "/icon.png",
		"environment":   "*",
		"entrypoints":   map[string]any{"modmenu": []string{markerEntrypoint}},
		"custom":        map[string]any{"modmenu": map[string]any{"update_checker": false}},
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	entries := []markerEntry{
		{"fabric.mod.json", metaData},
		{"assets/" + id + "/icon.png", markerIcon},
		{manifest.FileName, manifestData},
		{lock.FileName, lockData},
		{markerModsPath, markerModList(direct, deps)},
	}
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

func (b *Builder) markerMods(side string) (direct, deps []string) {
	for id, m := range b.Lock.Mods {
		if m.Side != "both" && m.Side != side {
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

func (b *Builder) markerDescription(direct, deps []string) string {
	lines := func(ids []string) []string {
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = id + " " + b.Lock.Mods[id].VersionNumber
		}
		return out
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Built by shulker: Minecraft %s, %s %s, %d mods.", b.Lock.Minecraft, b.Lock.Loader.Type, b.Lock.Loader.Version, len(direct)+len(deps))
	if len(direct) > 0 {
		sb.WriteString("\n\nMods:\n" + strings.Join(lines(direct), "\n"))
	}
	if len(deps) > 0 {
		sb.WriteString("\n\nDependencies:\n" + strings.Join(lines(deps), "\n"))
	}
	return sb.String()
}
