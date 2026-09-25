package build

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
)

// CacheRoots reads the locks one directory keeps alive: its own and one per
// history entry. A directory that is gone contributes nothing, since the
// instance it held was deleted; one whose lock is there but can't be read is
// returned as a problem, which stops a prune because it may be an instance that
// still needs its files, but leaves an inspection free to report.
func CacheRoots(c *cache.Cache, dir string, from cache.Root) (roots []cache.Root, present bool, unreadable []string, err error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil, nil
	} else if err != nil {
		return nil, false, nil, err
	}
	dirs := []string{dir}
	// A separate-dir instance keeps its lock in the project it was built from,
	// not in the game directory the registry records.
	if src := filepath.Clean(from.Source); src != "." && src != dir {
		if _, err := os.Stat(filepath.Join(src, manifest.FileName)); err == nil {
			dirs = append(dirs, src)
		}
	}
	var paths []string
	// A directory synced from a git or manifest URL runs on the lock of the checkout it was
	// built from, and falls back offline to the one its last good sync recorded.
	if kind := modpack.Classify(from.Source); from.Source != "" && kind != modpack.Local {
		state, _ := ReadState(dir)
		paths = append(paths, c.SourceLocks(from, state.Commit, state.Sha256)...)
	}
	for _, d := range dirs {
		paths = append(paths, filepath.Join(d, lock.FileName))
		entries, err := History(d)
		if err != nil {
			return nil, false, nil, err
		}
		for _, e := range entries {
			paths = append(paths, filepath.Join(HistoryPath(d), e.ID, lock.FileName))
		}
	}
	for _, path := range paths {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, false, nil, err
		}
		lk, err := lock.Load(path)
		if err != nil {
			unreadable = append(unreadable, path+" can't be read, so pruning could remove files it needs ("+out.AsError(err).Message+"); fix it, or run `shulker unlink` for that instance")
			continue
		}
		root := from
		root.Lock = lk
		roots = append(roots, root)
	}
	return roots, true, unreadable, nil
}
