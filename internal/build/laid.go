package build

import (
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Laid is a file an override folder lays: where it lands in the side's directory, the folder
// it comes from as the build labels it, and the modpack that brings the folder, if one does.
type Laid struct {
	Path    string `json:"path"`
	Folder  string `json:"folder"`
	Modpack string `json:"modpack,omitempty"`
}

// LaidJars is every jar, and every pack zip in a pack folder, that the side's override folders
// lay, with every feature on, since each is a file that reaches the game without any provider.
func (b *Builder) LaidJars(side string) ([]Laid, error) { return b.laid(side, isJarOrPack) }

// LaidFiles is every file the side's override folders lay, with every feature on.
func (b *Builder) LaidFiles(side string) ([]Laid, error) {
	return b.laid(side, func(string) bool { return true })
}

func (b *Builder) laid(side string, keep func(rel string) bool) ([]Laid, error) {
	cond := b.conditions(Options{})
	for name := range cond.features {
		cond.features[name] = true
	}
	var laid []Laid
	add := func(l overrideLayer, rel string) {
		if !l.skips(rel) && keep(rel) {
			laid = append(laid, Laid{Path: rel, Folder: l.label, Modpack: l.pack})
		}
	}
	for _, l := range b.overrideLayers(side, cond, nil) {
		if l.archived {
			for _, o := range l.files {
				add(l, o.Path)
			}
			continue
		}
		err := filepath.WalkDir(l.root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) && p == l.root {
					return nil
				}
				return err
			}
			if d.Type().IsRegular() {
				rel, _ := filepath.Rel(l.root, p)
				add(l, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return laid, nil
}

func isJarOrPack(rel string) bool {
	switch path.Ext(rel) {
	case ".jar":
		return true
	case ".zip":
		first, _, _ := strings.Cut(rel, "/")
		return slices.Contains([]string{"resourcepacks", "shaderpacks", "datapacks"}, first) || strings.Contains(rel, "/datapacks/")
	}
	return false
}
