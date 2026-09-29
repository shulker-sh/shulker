package resolve

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"slices"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
)

// ImportRecord is the lock's record of an import from arc: the archive is put in the cache, where
// a re-import finds it again, and a hosted one also names its provider file for when it was pruned.
func ImportRecord(c *cache.Cache, arc *packarchive.Archive, hosted *lock.Modpack) (*lock.Imported, error) {
	f, err := os.Open(arc.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sha, err := c.Put(f)
	if err != nil {
		return nil, err
	}
	rec := &lock.Imported{Sha512: sha}
	if hosted != nil {
		rec.Provider, rec.Project, rec.Version = hosted.Provider, hosted.Project, hosted.Version
	}
	return rec, nil
}

// isSamePack reports whether two imports are of one pack: the same provider project, or the very
// same archive.
func isSamePack(was, now *lock.Imported) bool {
	if was == nil || now == nil {
		return false
	}
	return was.Sha512 == now.Sha512 || (was.Provider != "" && was.Provider == now.Provider && was.Project == now.Project)
}

// ReadEarlier reads the archive the project was last imported from when now imports the same pack,
// fetching a hosted one again when the cache no longer holds it, and places its overrides as an
// import of it did, by the manifest and lock the pack imports as now. It is nil when there is no
// such import, or its archive can't be had again, and the merge then keeps everything the project has.
func (r *Resolver) ReadEarlier(ctx context.Context, was, now *lock.Imported, m *manifest.Manifest, l *lock.Lock) *Earlier {
	if !isSamePack(was, now) {
		return nil
	}
	if !r.Cache.Has(was.Sha512) && was.Provider != "" {
		p, err := r.provider(was.Provider)
		if err != nil {
			return nil
		}
		v, err := p.Version(ctx, was.Version)
		if err != nil {
			return nil
		}
		proj, err := p.Project(ctx, was.Project, "")
		if err != nil {
			return nil
		}
		if got, err := r.obtainFrom(ctx, p, proj, v, false); err != nil || got.sha512 != was.Sha512 {
			return nil
		}
	}
	if !r.Cache.Has(was.Sha512) {
		return nil
	}
	arc, err := packarchive.Read(r.Cache.Object(was.Sha512))
	if err != nil {
		return nil
	}
	placed := placeOverrides(scratchManifest(m), l, arc.Marker, slices.Clone(arc.Overrides), &Imported{})
	return NewEarlier(arc, placed)
}

// scratchManifest is a copy of m that placing overrides can change without changing m.
func scratchManifest(m *manifest.Manifest) *manifest.Manifest {
	c := *m
	c.SeedFiles = slices.Clone(m.SeedFiles)
	if m.Client != nil {
		client := *m.Client
		c.Client = &client
	}
	return &c
}

// Earlier is what an earlier import of the same pack wrote: the files its archive listed and the
// override files it carried, whose hashes also name an override the import locked as an entry. A
// merge replaces an entry or file that still matches it.
type Earlier struct {
	files     []packarchive.File
	overrides map[string][]byte
	hashes    map[string]bool
}

// NewEarlier is what an import of arc wrote, with placed its overrides as the import placed them.
func NewEarlier(arc *packarchive.Archive, placed []packarchive.Override) *Earlier {
	e := &Earlier{files: arc.Files, overrides: map[string][]byte{}, hashes: map[string]bool{}}
	for _, o := range placed {
		e.overrides[o.Layer+"/"+o.Path] = o.Data
	}
	for _, o := range arc.Overrides {
		sum1, sum512 := sha1.Sum(o.Data), sha512.Sum512(o.Data)
		e.hashes[hex.EncodeToString(sum1[:])] = true
		e.hashes[hex.EncodeToString(sum512[:])] = true
	}
	return e
}

// wroteFile reports whether a locked file is one the earlier archive listed: by hash, or by its
// provider's project and file.
func (e *Earlier) wroteFile(providerName, project, version, sha1, sha512 string) bool {
	if e == nil {
		return false
	}
	if e.hashes[sha512] || e.hashes[sha1] {
		return true
	}
	for _, f := range e.files {
		switch {
		case sha512 != "" && f.Hashes["sha512"] == sha512, sha1 != "" && f.Hashes["sha1"] == sha1:
			return true
		case f.Provider != "" && f.Provider == providerName && f.Project == project && f.Version == version:
			return true
		}
	}
	return false
}

// wroteEntry reports whether the lock entry under key still holds a file the earlier archive listed.
func (e *Earlier) wroteEntry(l *lock.Lock, key string) bool {
	if m, ok := l.Mods[key]; ok {
		return e.wroteFile(m.Provider, m.Project, m.Version, m.Sha1, m.Sha512)
	}
	for _, kind := range manifest.PackKinds {
		if lp, ok := l.Packs(kind)[key]; ok {
			return e.wroteFile(lp.Provider, lp.Project, lp.Version, lp.Sha1, lp.Sha512)
		}
	}
	return false
}

// dropEntry takes key out of p's manifest and lock, for the pack's entry to take its place, and
// returns the local file it locked, which the pack's then replaces.
func dropEntry(p *project.Project, key string) string {
	delete(p.Manifest.Requires, key)
	if m, ok := p.Lock.Mods[key]; ok {
		delete(p.Lock.Mods, key)
		return m.File
	}
	for _, kind := range manifest.PackKinds {
		if lp, ok := p.Lock.Packs(kind)[key]; ok {
			delete(p.Lock.Packs(kind), key)
			return lp.File
		}
	}
	return ""
}

// renameRequiredBy has every entry key required name it as to instead.
func renameRequiredBy(l *lock.Lock, from, to string) {
	for id, m := range l.Mods {
		if i := slices.Index(m.RequiredBy, from); i >= 0 {
			m.RequiredBy = slices.Clone(m.RequiredBy)
			m.RequiredBy[i] = to
			l.Mods[id] = m
		}
	}
}

// wroteOverride reports whether an override file still holds what the earlier archive put there.
func (e *Earlier) wroteOverride(file string, data []byte) bool {
	if e == nil {
		return false
	}
	was, ok := e.overrides[file]
	return ok && bytes.Equal(was, data)
}
