package cache

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/lock"
)

// A Root is one lock whose files must survive a prune, together with the remote
// source its directory syncs from. Roots are the registered instances and the
// project the command runs in, each contributing its own lock and the lock of
// every history entry it keeps.
type Root struct {
	Lock   *lock.Lock
	Source string
	Ref    string
	Path   string
}

// Pruned counts what a prune removed, by kind.
type Pruned struct {
	Files     int   `json:"files"`
	Checkouts int   `json:"checkouts"`
	Installs  int   `json:"installs"`
	Logs      int   `json:"logs"`
	Temp      int   `json:"temp"`
	Listings  int   `json:"listings"`
	Bytes     int64 `json:"bytes"`
}

func (p Pruned) Empty() bool {
	return p.Files+p.Checkouts+p.Installs+p.Logs+p.Temp+p.Listings == 0
}

type Usage struct {
	Dir      string `json:"dir"`
	Bytes    int64  `json:"bytes"`
	Objects  int    `json:"objects"`
	Manual   int    `json:"manual"`
	Listings int    `json:"listings"`
}

func (c *Cache) Usage() (Usage, error) {
	u := Usage{Dir: c.Dir}
	size, err := treeSize(c.Dir)
	if err != nil {
		return Usage{}, err
	}
	objects, err := entriesAt(filepath.Join(c.Dir, "objects"), 2)
	if err != nil {
		return Usage{}, err
	}
	ix, err := c.ReadListings()
	if err != nil {
		return Usage{}, err
	}
	manual, err := c.manualObjects()
	if err != nil {
		return Usage{}, err
	}
	u.Bytes, u.Objects, u.Manual, u.Listings = size, len(objects), len(manual), len(ix.Pairs)
	return u, nil
}

// manualObjects are the paths of the objects marked manual that are still there.
func (c *Cache) manualObjects() (map[string]bool, error) {
	markers, err := entriesAt(filepath.Join(c.Dir, "index", "manual"), 1)
	if err != nil {
		return nil, err
	}
	objects := map[string]bool{}
	for _, m := range markers {
		if sha := filepath.Base(m); c.Has(sha) {
			objects[c.Object(sha)] = true
		}
	}
	return objects, nil
}

// PruneOptions are how a prune runs: a dry run reports what would go and removes nothing, and
// Manual lets the manual downloads go too, which nothing can fetch again.
type PruneOptions struct {
	DryRun bool
	Manual bool
}

// Prune removes everything the roots don't reference but the manual downloads,
// and the listing index's pairs unused for 90 days. It walks only the trees
// listed here, so the managed Java runtimes and the shared CurseForge key are
// never candidates.
func (c *Cache) Prune(roots []Root, o PruneOptions) (Pruned, error) {
	keep := c.keep(roots)
	if !o.Manual {
		manual, err := c.manualObjects()
		if err != nil {
			return Pruned{}, err
		}
		maps.Copy(keep, manual)
	}
	var p Pruned
	trees := []struct {
		dir   string
		depth int
		count *int
		// pruneAll removes every entry, referenced or not: these hold leftovers and
		// records of runs that are already over.
		pruneAll bool
	}{
		{filepath.Join(c.Dir, "objects"), 2, &p.Files, false},
		{filepath.Join(c.Dir, "packs", "git"), 1, &p.Checkouts, false},
		{filepath.Join(c.Dir, "packs", "src"), 1, &p.Checkouts, false},
		{filepath.Join(c.Dir, "packs", "url"), 1, &p.Checkouts, false},
		{filepath.Join(c.Dir, "projects", "url"), 1, &p.Checkouts, false},
		{filepath.Join(c.Dir, "projects", "last-good"), 1, &p.Checkouts, false},
		{filepath.Join(c.Dir, "atlauncher"), 1, &p.Installs, false},
		{filepath.Join(c.Dir, "logs"), 1, &p.Logs, true},
		{filepath.Join(c.Dir, "tmp"), 1, &p.Temp, true},
	}
	for _, t := range trees {
		paths, err := entriesAt(t.dir, t.depth)
		if err != nil {
			return Pruned{}, err
		}
		for _, path := range paths {
			if !t.pruneAll && keep[path] {
				continue
			}
			size, err := treeSize(path)
			if err != nil {
				return Pruned{}, err
			}
			if !o.DryRun {
				if err := os.RemoveAll(path); err != nil {
					return Pruned{}, err
				}
			}
			*t.count++
			p.Bytes += size
		}
	}
	if !o.DryRun {
		if err := c.dropIndexEntries(); err != nil {
			return Pruned{}, err
		}
	}
	listings, err := c.pruneListings(o.DryRun)
	if err != nil {
		return Pruned{}, err
	}
	p.Listings = listings
	return p, nil
}

func (c *Cache) keep(roots []Root) map[string]bool {
	keep := map[string]bool{}
	for _, r := range roots {
		c.keepSource(keep, r)
		l := r.Lock
		if l == nil {
			continue
		}
		for _, m := range l.Mods {
			keep[c.Object(m.Sha512)] = true
		}
		for _, packs := range l.PackSections() {
			for _, p := range packs {
				keep[c.Object(p.Sha512)] = true
			}
		}
		if l.Server != nil {
			keep[c.Object(l.Server.Sha512)] = true
		}
		if l.Loader.Client != nil {
			keep[c.Object(l.Loader.Client.Sha512)] = true
		}
		if s := l.Loader.Server; s != nil {
			keep[c.Object(s.Sha512)] = true
			for _, lib := range s.Libraries {
				keep[c.Object(lib.Sha512)] = true
			}
		}
		if l.Loader.Type != "" && l.Loader.Version != "" {
			keep[c.ATLauncherInstall(l.Loader.Type, l.Loader.Version)] = true
		}
		for _, mp := range l.Modpacks {
			c.keepSource(keep, Root{Source: mp.Source, Ref: mp.Ref, Path: mp.Path})
			if mp.Commit != "" {
				keep[c.PackSource(mp.Commit)] = true
			}
			if mp.Sha256 != "" {
				keep[c.PackManifest(mp.Sha256)] = true
				keep[c.ProjectCheckout(mp.Sha256)] = true
			}
			if mp.LockSha256 != "" {
				keep[c.PackLock(mp.LockSha256)] = true
			}
			if mp.Sha512 != "" {
				keep[c.Object(mp.Sha512)] = true
			}
			for _, sha := range mp.Unmanaged {
				keep[c.Object(sha)] = true
			}
		}
		// A provider's archive is fetched again for a re-import; a local one can't be.
		if im := l.Imported; im != nil && im.Provider == "" {
			keep[c.Object(im.Sha512)] = true
		}
	}
	return keep
}

// SourceLocks names the locks the checkouts of r's remote source hold: the one at the commit or
// digest a directory was built from, and the ones its offline fallback records point at.
func (c *Cache) SourceLocks(r Root, commit, sha256 string) []string {
	var paths []string
	add := func(commit, sha256 string) {
		if commit != "" {
			paths = append(paths, filepath.Join(c.PackSource(commit), filepath.FromSlash(r.Path), lock.FileName))
		}
		if sha256 != "" {
			paths = append(paths, filepath.Join(c.ProjectCheckout(sha256), lock.FileName))
		}
	}
	add(commit, sha256)
	for _, record := range []string{c.LastGood(r.Source, r.Ref, r.Path), c.LastGood(r.Source, "", r.Path)} {
		rec, ok := readLastGood(record)
		if ok {
			add(rec.Commit, rec.Sha256)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

type lastGoodRecord struct {
	Commit string `json:"commit"`
	Sha256 string `json:"sha256"`
}

func readLastGood(path string) (lastGoodRecord, bool) {
	var rec lastGoodRecord
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &rec) != nil {
		return rec, false
	}
	return rec, true
}

// keepSource keeps a remote source's mirror, its offline fallback record, and
// whatever that record points at, so a sync from an unreachable source still
// finds the copy it falls back to.
func (c *Cache) keepSource(keep map[string]bool, r Root) {
	if r.Source == "" {
		return
	}
	keep[c.PackMirror(r.Source)] = true
	for _, path := range []string{c.LastGood(r.Source, r.Ref, r.Path), c.LastGood(r.Source, "", r.Path)} {
		keep[path] = true
		rec, ok := readLastGood(path)
		if !ok {
			continue
		}
		if rec.Commit != "" {
			keep[c.PackSource(rec.Commit)] = true
		}
		if rec.Sha256 != "" {
			keep[c.PackManifest(rec.Sha256)] = true
			keep[c.ProjectCheckout(rec.Sha256)] = true
		}
	}
}

// Ingest copies a file's bytes into the cache unless they are there already, so
// nothing shulker placed leaves the disk without the cache holding it and a
// rollback works offline.
func (c *Cache) Ingest(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = c.Put(f)
	return err
}

// entriesAt lists the paths depth levels below dir, so objects are listed as
// files under their two-character shard and every other tree by its own entry.
func entriesAt(dir string, depth int) ([]string, error) {
	names, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, name := range names {
		path := filepath.Join(dir, name.Name())
		if depth > 1 {
			if !name.IsDir() {
				continue
			}
			deeper, err := entriesAt(path, depth-1)
			if err != nil {
				return nil, err
			}
			paths = append(paths, deeper...)
			continue
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func treeSize(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}
