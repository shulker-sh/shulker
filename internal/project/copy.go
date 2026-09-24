package project

import (
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/pack"
)

// OwnPaths are the paths of a project's own files, relative to its folder: its lock, override
// folders, a feature's included, local files, icon and .gitignore. Anything else in the folder, such
// as a build the project makes in place, is not the project's to copy.
func OwnPaths(m *manifest.Manifest) []string {
	paths := []string{lock.FileName, ".gitignore", manifest.FilesDir}
	paths = append(paths, OverrideLayers(m)...)
	if m.Icon != "" {
		paths = append(paths, m.Icon)
	}
	for _, req := range m.Requires {
		if req.File != "" {
			paths = append(paths, req.File)
		}
		if pack.Classify(req.Source) == pack.Local && req.Source != "" && filepath.IsLocal(req.Source) {
			paths = append(paths, filepath.ToSlash(filepath.Clean(req.Source)))
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}
