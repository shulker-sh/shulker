package build

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andrewmast/shulker/internal/lock"
	"github.com/andrewmast/shulker/internal/manifest"
)

//go:embed assets/icon.png
var markerIcon []byte

var markerTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func markerModID(name string) string {
	return "shulker_" + strings.NewReplacer(".", "_", "-", "_").Replace(name)
}

func markerJarPath(name string) string {
	return "mods/shulker-" + name + ".jar"
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
	id := markerModID(b.Manifest.Name)
	meta := map[string]any{
		"schemaVersion": 1,
		"id":            id,
		"version":       lockHash[:8],
		"name":          b.Manifest.Name,
		"description":   b.markerDescription(side),
		"icon":          "assets/" + id + "/icon.png",
		"environment":   "*",
		"custom":        map[string]any{"modmenu": map[string]any{"update_checker": false}},
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := []struct {
		name string
		data []byte
	}{
		{"fabric.mod.json", metaData},
		{"assets/" + id + "/icon.png", markerIcon},
		{manifest.FileName, manifestData},
		{lock.FileName, lockData},
	}
	for _, f := range files {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: markerTime})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (b *Builder) markerDescription(side string) string {
	var direct, deps []string
	for id, m := range b.Lock.Mods {
		if m.Side != "both" && m.Side != side {
			continue
		}
		line := id + " " + m.VersionNumber
		if _, ok := b.Manifest.Mods[id]; ok {
			direct = append(direct, line)
		} else {
			deps = append(deps, line)
		}
	}
	sort.Strings(direct)
	sort.Strings(deps)
	var sb strings.Builder
	fmt.Fprintf(&sb, "Built by shulker: Minecraft %s, %s %s, %d mods.", b.Lock.Minecraft, b.Lock.Loader.Type, b.Lock.Loader.Version, len(direct)+len(deps))
	if len(direct) > 0 {
		sb.WriteString("\n\nMods:\n" + strings.Join(direct, "\n"))
	}
	if len(deps) > 0 {
		sb.WriteString("\n\nDependencies:\n" + strings.Join(deps, "\n"))
	}
	return sb.String()
}
