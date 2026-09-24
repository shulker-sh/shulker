package project

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
)

// ReadOverrideFolders reads the files in a project's override folders, a feature's included.
func ReadOverrideFolders(dir string, m *manifest.Manifest) ([]packarchive.Override, error) {
	var overrides []packarchive.Override
	for _, layer := range OverrideLayers(m) {
		root := filepath.Join(dir, filepath.FromSlash(layer))
		err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
			if err != nil || !e.Type().IsRegular() {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			overrides = append(overrides, packarchive.Override{Layer: layer, Path: filepath.ToSlash(rel), Data: data})
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return overrides, nil
}

// matchFolders are the folders of an override layer whose files a provider can host.
var matchFolders = append([]string{"mods", "resourcepacks", "shaderpacks"}, lock.DatapackFolders...)

// ScanOverrides lists every matchable file in the project's override layers, relative to dir.
func ScanOverrides(dir string) ([]string, error) {
	var rels []string
	for _, layer := range packarchive.Layers {
		for _, folder := range matchFolders {
			entries, err := os.ReadDir(filepath.Join(dir, layer, folder))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if rel := layer + "/" + folder + "/" + e.Name(); e.Type().IsRegular() && IsMatchable(rel) {
					rels = append(rels, rel)
				}
			}
		}
	}
	return rels, nil
}

// MatchableOverrides reads the override files at rels, each named by its layer and its path within
// it.
func MatchableOverrides(dir string, rels []string) ([]packarchive.Override, error) {
	files := make([]packarchive.Override, len(rels))
	for i, rel := range rels {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		layer, within, _ := strings.Cut(rel, "/")
		files[i] = packarchive.Override{Layer: layer, Path: within, Data: data}
	}
	return files, nil
}

// IsMatchable reports whether rel, relative to the project, is a jar in an override layer's mods
// folder or a zip in one of its pack folders.
func IsMatchable(rel string) bool {
	layer, within, _ := strings.Cut(rel, "/")
	return slices.Contains(packarchive.Layers, layer) && (packarchive.IsModJar(within) || packarchive.IsPackZip(within))
}
