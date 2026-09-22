package resolve

import (
	"bytes"
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
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/zipfile"
)

// localCopy is a local file's bytes as the cache holds them. isFile is false for a folder, whose
// bytes are its zip, and for a file that is gone.
type localCopy struct {
	path   string
	sha512 string
	size   int64
	isFile bool
}

// cacheLocal hashes a local file entry's file, rel under dir, into the cache, and a folder's zip
// when folders is set. A file that is gone is served from the cache by the sha512 it was locked at,
// with a warning, so the lock is only wrong when neither has it.
func (r *Resolver) cacheLocal(dir, key, rel, lockedSha512 string, folders bool) (localCopy, error) {
	path := filepath.Join(dir, filepath.FromSlash(rel))
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if lockedSha512 == "" || !r.Cache.Has(lockedSha512) {
			return localCopy{}, pack.FileMissing(key, rel)
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
	if !st.Mode().IsRegular() && !(folders && st.IsDir()) {
		return localCopy{}, pack.NotAFile(key, rel)
	}
	return r.putLocal(path, st)
}

// putLocal puts the file at path in the cache, or the zip of the folder there.
func (r *Resolver) putLocal(path string, st os.FileInfo) (localCopy, error) {
	if st.IsDir() {
		data, err := zipfile.Folder(path)
		if err != nil {
			return localCopy{}, err
		}
		sha, err := r.Cache.Put(bytes.NewReader(data))
		if err != nil {
			return localCopy{}, err
		}
		return localCopy{path: r.Cache.Object(sha), sha512: sha, size: int64(len(data))}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return localCopy{}, err
	}
	defer f.Close()
	counted := &countingReader{r: f}
	sha, err := r.Cache.Put(counted)
	if err != nil {
		return localCopy{}, err
	}
	return localCopy{path: r.Cache.Object(sha), sha512: sha, size: counted.n, isFile: true}, nil
}

// fileDir is the directory a locked local file entry's path is relative to: the project's own for
// an entry shulker.json lists, and the modpack's for one a modpack supplies. It is "" when neither
// is known, as for a URL modpack, which has no directory.
func (r *Resolver) fileDir(id, modpack, file string) string {
	if r.Manifest.Requires[id].File == file {
		return r.Dir
	}
	for _, p := range r.Packs {
		if p.Name == modpack || (modpack == "" && p.Manifest.Requires[id].File == file) {
			return p.Dir
		}
	}
	return ""
}

// cachePackFiles hashes the local files a locked modpack's own lock names into the cache, from the
// modpack's directory. An entry the modpack took from a modpack of its own names a path in a
// directory this project never sees, so only the cache can serve it.
func (r *Resolver) cachePackFiles(p *pack.Loaded) error {
	check := func(key, file, modpack, sha512 string, folders bool) error {
		if file == "" {
			return nil
		}
		label := "modpack " + p.Name + ": " + key
		if modpack != "" || p.Dir == "" {
			if !r.Cache.Has(sha512) {
				return pack.FileMissing(label, file)
			}
			return nil
		}
		got, err := r.cacheLocal(p.Dir, label, file, sha512, folders)
		if err != nil {
			return err
		}
		if got.sha512 != sha512 && !r.Cache.Has(sha512) {
			e := out.Errorf("modpack-changed", "%s: %s has changed since the modpack was locked", label, file)
			e.Help = "run `shulker lock` in the modpack"
			return e
		}
		return nil
	}
	for _, id := range sortedKeys(p.Lock.Mods) {
		m := p.Lock.Mods[id]
		if err := check(id, m.File, m.Modpack, m.Sha512, false); err != nil {
			return err
		}
	}
	for _, section := range []map[string]lock.Pack{p.Lock.ResourcePacks, p.Lock.Shaders} {
		for _, key := range sortedKeys(section) {
			if err := check(key, section[key].File, section[key].Modpack, section[key].Sha512, true); err != nil {
				return err
			}
		}
	}
	return nil
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
			return pack.FileMissing(f.id, f.file)
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

// nestedPack names the modpack a locked modpack took a local file entry from, or "" when the locked
// modpack supplies the entry itself.
func (r *Resolver) nestedPack(id, modpack, file string) string {
	for _, p := range r.Packs {
		if p.Name != modpack || p.Lock == nil {
			continue
		}
		if m, ok := p.Lock.Mods[id]; ok && m.File == file {
			return m.Modpack
		}
		for _, section := range []map[string]lock.Pack{p.Lock.ResourcePacks, p.Lock.Shaders} {
			if e, ok := section[id]; ok && e.File == file {
				return e.Modpack
			}
		}
	}
	return ""
}

// restoreLocal fills the cache from a local file entry's file, and says what is wrong when the file
// can't: it is gone, its bytes are no longer the ones locked, or it lives in a modpack nested inside
// a locked one, whose directory this project never sees.
func (r *Resolver) restoreLocal(f downloadable) string {
	if inner := r.nestedPack(f.id, f.modpack, f.file); inner != "" {
		return fmt.Sprintf("%s: %s comes from modpack %s inside modpack %s, so only the cache can serve it, and the cache has no copy of it", f.id, f.file, inner, f.modpack)
	}
	dir := r.fileDir(f.id, f.modpack, f.file)
	path := filepath.Join(dir, filepath.FromSlash(f.file))
	st, err := os.Stat(path)
	if dir == "" || err != nil {
		return fmt.Sprintf("%s: %s is gone, and the cache has no copy of it", f.id, f.file)
	}
	got, err := r.putLocal(path, st)
	if err != nil || got.sha512 != f.sha512 {
		return fmt.Sprintf("%s: %s changed since it was locked; run `shulker lock`", f.id, f.file)
	}
	return ""
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

// relockFile locks a local mod jar, its path relative to dir, from its own metadata: the jar says its
// id and side, and the dependencies it declares are resolved from the providers as a hosted mod's are.
func (r *Resolver) relockFile(ctx context.Context, dir, id string, entry manifest.Require, prev lock.Mod) error {
	locked := ""
	if prev.File == entry.File {
		locked = prev.Sha512
	}
	got, err := r.cacheLocal(dir, id, entry.File, locked, false)
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
	got, err := r.cacheLocal(r.Dir, key, entry.File, locked, true)
	if err != nil {
		return err
	}
	if got.isFile {
		if err := manifest.CheckFileExtension(key, entry); err != nil {
			return err
		}
	}
	p := lock.Pack{
		File:     entry.File,
		Filename: manifest.PackFilename(key, entry),
		Sha512:   got.sha512,
		Size:     got.size,
	}
	if kind == manifest.TypeShader {
		p.Loader = r.installedShaderLoader()
	}
	section[key] = p
	return nil
}
