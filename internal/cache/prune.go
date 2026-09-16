package cache

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

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
}

// Pruned counts what a prune removed, by kind.
type Pruned struct {
	Files     int   `json:"files"`
	Checkouts int   `json:"checkouts"`
	Installs  int   `json:"installs"`
	Logs      int   `json:"logs"`
	Temp      int   `json:"temp"`
	Bytes     int64 `json:"bytes"`
}

func (p Pruned) Empty() bool {
	return p.Files+p.Checkouts+p.Installs+p.Logs+p.Temp == 0
}

type Usage struct {
	Dir     string `json:"dir"`
	Bytes   int64  `json:"bytes"`
	Objects int    `json:"objects"`
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
	u.Bytes, u.Objects = size, len(objects)
	return u, nil
}

// Prune removes everything the roots don't reference. It walks only the trees
// listed here, so the managed Java runtimes and the shared CurseForge key are
// never candidates; a dry run reports what would go and removes nothing.
func (c *Cache) Prune(roots []Root, dryRun bool) (Pruned, error) {
	keep := c.keep(roots)
	var p Pruned
	trees := []struct {
		dir   string
		depth int
		count *int
		// all removes every entry, referenced or not: these hold leftovers and
		// records of runs that are already over.
		all bool
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
			if !t.all && keep[path] {
				continue
			}
			size, err := treeSize(path)
			if err != nil {
				return Pruned{}, err
			}
			if !dryRun {
				if err := os.RemoveAll(path); err != nil {
					return Pruned{}, err
				}
			}
			*t.count++
			p.Bytes += size
		}
	}
	return p, nil
}

func (c *Cache) keep(roots []Root) map[string]bool {
	keep := map[string]bool{}
	for _, r := range roots {
		c.keepSource(keep, r.Source, r.Ref)
		l := r.Lock
		if l == nil {
			continue
		}
		for _, m := range l.Mods {
			keep[c.Object(m.Sha512)] = true
		}
		for _, packs := range []map[string]lock.Pack{l.ResourcePacks, l.Shaders} {
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
			c.keepSource(keep, mp.Source, mp.Ref)
			if mp.Commit != "" {
				keep[c.PackSource(mp.Commit)] = true
			}
			if mp.Sha256 != "" {
				keep[c.PackManifest(mp.Sha256)] = true
				keep[c.ProjectCheckout(mp.Sha256)] = true
			}
		}
	}
	return keep
}

// keepSource keeps a remote source's mirror, its offline fallback record, and
// whatever that record points at, so a sync from an unreachable source still
// finds the copy it falls back to.
func (c *Cache) keepSource(keep map[string]bool, source, ref string) {
	if source == "" {
		return
	}
	keep[c.PackMirror(source)] = true
	for _, path := range []string{c.LastGood(source, ref), c.LastGood(source, "")} {
		keep[path] = true
		var rec struct {
			Commit string `json:"commit"`
			Sha256 string `json:"sha256"`
		}
		data, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(data, &rec) != nil {
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
