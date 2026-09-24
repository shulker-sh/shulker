package project

import (
	"os"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/fsutil"
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

// CopyOwnFiles copies each path in src that exists into dir, leaving alone one dir already has,
// and returns the paths it created, in order, a failure's included.
func CopyOwnFiles(src, dir string, paths []string) ([]string, error) {
	var created []string
	for _, rel := range paths {
		from, to := filepath.Join(src, filepath.FromSlash(rel)), filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		created = append(created, to)
		if err := fsutil.CopyPath(from, to); err != nil {
			return created, err
		}
	}
	return created, nil
}
