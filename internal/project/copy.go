package project

import (
	"os"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/security"
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
		if modpack.Classify(req.Source) == modpack.Local && req.Source != "" && filepath.IsLocal(req.Source) {
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
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return created, security.Refusal(security.Paths, out.Errorf("path-outside", "%s is outside the project", rel))
		}
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

// CopySource lays a new project at dir out of the shulker source at srcDir: the paths that are m's
// own, less the other side's override folders when side names one. It returns what it created,
// the folders it left out for side, and an undo that removes what it created, the manifest the
// caller writes next included, for a later step that fails.
func CopySource(srcDir, dir string, m *manifest.Manifest, side string) (created, leftOut []string, undo func(), err error) {
	paths := OwnPaths(m)
	leftOut = []string{}
	if side != "" {
		other := OtherSide(side)
		paths = slices.DeleteFunc(paths, func(p string) bool {
			if !IsSideLayer(m, other, p) {
				return false
			}
			if _, err := os.Stat(filepath.Join(srcDir, filepath.FromSlash(p))); err == nil {
				leftOut = append(leftOut, p)
			}
			return true
		})
	}
	created, err = CopyOwnFiles(srcDir, dir, paths)
	undo = func() {
		os.Remove(filepath.Join(dir, manifest.FileName))
		for _, path := range slices.Backward(created) {
			os.RemoveAll(path)
		}
	}
	return created, leftOut, undo, err
}

// OtherSide is the side that isn't side.
func OtherSide(side string) string {
	if side == "server" {
		return "client"
	}
	return "server"
}
