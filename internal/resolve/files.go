package resolve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

// localCopy is a local file's bytes as the cache holds them.
type localCopy struct {
	path   string
	sha512 string
	size   int64
}

// cacheLocal hashes a requires entry's file into the cache. A file that is gone is served from the
// cache by the sha512 it was locked at, with a warning, so the lock is only wrong when neither has it.
func (r *Resolver) cacheLocal(key, rel, lockedSha512 string) (localCopy, error) {
	f, err := os.Open(filepath.Join(r.Dir, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		if lockedSha512 == "" || !r.Cache.Has(lockedSha512) {
			return localCopy{}, localFileMissing(key, rel)
		}
		r.warnOnce(project.FileGone(key, rel))
		path := r.Cache.Object(lockedSha512)
		st, err := os.Stat(path)
		if err != nil {
			return localCopy{}, err
		}
		return localCopy{path: path, sha512: lockedSha512, size: st.Size()}, nil
	}
	if err != nil {
		return localCopy{}, err
	}
	defer f.Close()
	counted := &countingReader{r: f}
	sha, err := r.Cache.Put(counted)
	if err != nil {
		return localCopy{}, err
	}
	return localCopy{path: r.Cache.Object(sha), sha512: sha, size: counted.n}, nil
}

// checkLocalFiles holds the lock's local file entries to what the cache can serve: one whose file is
// gone is kept, with a warning, while the cache still has its bytes, and is an error once it doesn't.
func (r *Resolver) checkLocalFiles() error {
	for _, f := range r.lockFiles() {
		if f.file == "" || r.Manifest.Requires[f.id].File != f.file {
			continue
		}
		if _, err := os.Stat(filepath.Join(r.Dir, filepath.FromSlash(f.file))); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if !r.Cache.Has(f.sha512) {
			return localFileMissing(f.id, f.file)
		}
		r.warnOnce(project.FileGone(f.id, f.file))
	}
	return nil
}

func (r *Resolver) warnOnce(warning string) {
	if !slices.Contains(r.Warnings, warning) {
		r.Warnings = append(r.Warnings, warning)
	}
}

// restoreLocal fills the cache from a local file entry's file, and says what is wrong when the file
// can't: it is gone, or its bytes are no longer the ones locked.
func (r *Resolver) restoreLocal(f downloadable) string {
	file, err := os.Open(filepath.Join(r.Dir, filepath.FromSlash(f.file)))
	if err != nil {
		return fmt.Sprintf("%s: %s is gone, and the cache has no copy of it", f.id, f.file)
	}
	defer file.Close()
	sha, err := r.Cache.Put(file)
	if err != nil || sha != f.sha512 {
		return fmt.Sprintf("%s: %s changed since it was locked; run `shulker lock`", f.id, f.file)
	}
	return ""
}

func localFileMissing(key, rel string) *out.Error {
	e := out.Errorf("local-file-missing", "%s: %s is gone, and the cache has no copy of it", key, rel)
	e.Help = fmt.Sprintf("put the file back at %s, or remove %s from shulker.json", rel, key)
	return e
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// relockFile locks a local mod jar from its own metadata: the jar says its id and side, and the
// dependencies it declares are resolved from the providers as a hosted mod's are.
func (r *Resolver) relockFile(ctx context.Context, id string, entry manifest.Require, prev lock.Mod) error {
	locked := ""
	if prev.File == entry.File {
		locked = prev.Sha512
	}
	got, err := r.cacheLocal(id, entry.File, locked)
	if err != nil {
		return err
	}
	info, err := jarmeta.Read(got.path, r.Lock.Loader.Type)
	if err != nil {
		return prefixed("mod "+id, err)
	}
	for _, other := range r.lockIDs() {
		if other != id && r.Lock.JarID(other) == info.ID {
			return out.Errorf("requires-taken", "mod id %s is already locked as %s; a mod id can only be locked once", info.ID, other)
		}
	}
	side := info.Side
	if entry.Side != "" {
		side = entry.Side
	}
	m := lock.Mod{
		File:       entry.File,
		Filename:   filepath.Base(filepath.FromSlash(entry.File)),
		Sha512:     got.sha512,
		Size:       got.size,
		Side:       side,
		RequiredBy: []string{},
	}
	if info.ID != id {
		m.ModID = info.ID
	}
	r.Lock.Mods[id] = m
	return r.addFileDeps(ctx, id, info)
}

// addFileDeps resolves what a local jar depends on. Ids the platform, another locked mod or a
// shulker.json entry already answer are left alone, and one no provider has is left for
// validation to report against the jar's own declared range.
func (r *Resolver) addFileDeps(ctx context.Context, id string, info *jarmeta.Info) error {
	listed := r.Manifest.Mods()
	for _, on := range sortedKeys(info.Depends) {
		if isBuiltin(on) || r.isProvided(on, info) {
			continue
		}
		if _, ok := listed[on]; ok {
			r.Lock.AddRequiredBy(on, id)
			continue
		}
		if held := r.lockedAs(on); held != "" {
			r.Lock.AddRequiredBy(held, id)
			continue
		}
		p, proj, err := r.lookup(ctx, on, "", manifest.TypeMod)
		if out.CodeOf(err) == "mod-not-found" {
			r.log("no provider has %s, which %s depends on", on, id)
			continue
		}
		if err != nil {
			return err
		}
		v, err := r.pick(ctx, p, proj, "", "")
		if err != nil {
			return prefixed("dependency of "+id, err)
		}
		dep, _, err := r.place(ctx, p, proj, v, "", id, "", "", false)
		if err != nil {
			return err
		}
		if err := r.addDeps(ctx, p, v, dep, "", map[string]bool{proj.ID: true}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) isProvided(on string, info *jarmeta.Info) bool {
	if _, ok := r.Lock.Loader.Provides[on]; ok {
		return true
	}
	_, ok := info.Provides[on]
	return ok
}

// lockedAs is the key a mod id is locked under, or "" when none is.
func (r *Resolver) lockedAs(modID string) string {
	for _, key := range r.lockIDs() {
		if r.Lock.JarID(key) == modID {
			return key
		}
	}
	return ""
}

// lockFilePack locks a local resource pack or shader by its bytes.
func (r *Resolver) lockFilePack(key, kind string, entry manifest.Require) error {
	section := r.packSection(kind)
	locked := ""
	if prev, ok := section[key]; ok && prev.File == entry.File {
		locked = prev.Sha512
	}
	got, err := r.cacheLocal(key, entry.File, locked)
	if err != nil {
		return err
	}
	p := lock.Pack{
		File:     entry.File,
		Filename: filepath.Base(filepath.FromSlash(entry.File)),
		Sha512:   got.sha512,
		Size:     got.size,
	}
	if kind == manifest.TypeShader {
		p.Loader = r.installedShaderLoader()
	}
	section[key] = p
	return nil
}
