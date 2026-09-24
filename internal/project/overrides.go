package project

import (
	"io/fs"
	"os"
	"path/filepath"

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
