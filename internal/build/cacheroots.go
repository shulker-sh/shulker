package build

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

// Roots are the locks a prune must not strip: every registered instance's and the project the
// command runs in, each with the locks of its history entries, and each lock file named with
// --lock. A root whose lock won't load is carried as a problem rather than an error, so an
// inspection can still report while a prune refuses.
type Roots struct {
	Locks      []cache.Root
	Instances  int
	Project    bool
	LockFiles  int
	Unreadable []string
}

// Count is how many roots there are: the instances, the lock files and the project.
func (r Roots) Count() int {
	n := r.Instances + r.LockFiles
	if r.Project {
		n++
	}
	return n
}

// CacheRoots gathers the roots of a prune run from dir: the named lock files, every registered
// instance whose directory still exists, and dir itself when it is a project not already
// registered. A named lock file that doesn't exist is an error, since the user asked for it by
// name; one that won't load is carried as a problem like an instance's.
func CacheRoots(c *cache.Cache, instances []project.InstanceEntry, dir string, named []string) (Roots, error) {
	var r Roots
	for _, path := range named {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return Roots{}, out.Errorf("lock-not-found", "%s doesn't exist", path)
		} else if err != nil {
			return Roots{}, err
		}
		r.LockFiles++
		lk, err := lock.Load(path)
		if err != nil {
			r.Unreadable = append(r.Unreadable, path+" can't be read, so pruning could remove files it needs ("+out.AsError(err).Message+"); fix it, or leave out its --lock")
			continue
		}
		r.Locks = append(r.Locks, cache.Root{Lock: lk, Name: path})
	}
	seen := map[string]bool{}
	for _, in := range instances {
		at := filepath.Clean(in.Dir)
		if seen[at] {
			continue
		}
		seen[at] = true
		locks, present, unreadable, err := dirRoots(c, at, cache.Root{Source: in.Source, Ref: in.Ref, Path: in.Path, Name: in.Label()})
		if err != nil {
			return Roots{}, err
		}
		if !present {
			continue
		}
		r.Instances++
		r.Locks = append(r.Locks, locks...)
		r.Unreadable = append(r.Unreadable, unreadable...)
	}
	dir = filepath.Clean(dir)
	if seen[dir] {
		return r, nil
	}
	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err != nil {
		return r, nil
	}
	locks, _, unreadable, err := dirRoots(c, dir, cache.Root{Name: dir})
	if err != nil {
		return Roots{}, err
	}
	r.Project = true
	r.Locks = append(r.Locks, locks...)
	r.Unreadable = append(r.Unreadable, unreadable...)
	return r, nil
}

// Prune removes what no root references. A dry run reports even past an unreadable root; a real
// prune refuses one, since it may be a live instance whose files would go.
func (r Roots) Prune(c *cache.Cache, o cache.PruneOptions) (cache.Pruned, error) {
	if !o.DryRun && len(r.Unreadable) > 0 {
		return cache.Pruned{}, out.Errorf("cache-root-unreadable", "%s", r.Unreadable[0])
	}
	return c.Prune(r.Locks, o)
}

// dirRoots reads the locks one directory keeps alive: its own and one per
// history entry. A directory that is gone contributes nothing, since the
// instance it held was deleted; one whose lock is there but can't be read is
// returned as a problem, which stops a prune because it may be an instance that
// still needs its files, but leaves an inspection free to report.
func dirRoots(c *cache.Cache, dir string, from cache.Root) (roots []cache.Root, present bool, unreadable []string, err error) {
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
	names := map[string]string{}
	// A directory synced from a git or manifest URL runs on the lock of the checkout it was
	// built from, and falls back offline to the one its last good sync recorded.
	if kind := modpack.Classify(from.Source); from.Source != "" && kind != modpack.Local {
		state, _ := instance.ReadState(dir)
		paths = append(paths, c.SourceLocks(from, state.Commit, state.Sha256)...)
	}
	for _, d := range dirs {
		paths = append(paths, filepath.Join(d, lock.FileName))
		entries, err := History(d)
		if err != nil {
			return nil, false, nil, err
		}
		for _, e := range entries {
			path := filepath.Join(HistoryPath(d), e.ID, lock.FileName)
			paths = append(paths, path)
			names[path] = from.Name + " (history " + e.ID + ")"
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
		if name, ok := names[path]; ok {
			root.Name = name
		}
		roots = append(roots, root)
	}
	return roots, true, unreadable, nil
}
