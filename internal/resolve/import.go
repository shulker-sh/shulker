package resolve

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/provider"
)

type hashLookup interface {
	provider.Provider
	VersionByHash(ctx context.Context, sha1 string) (*provider.Version, bool, error)
}

type Imported struct {
	Locked    []string          `json:"locked"`
	Reused    []string          `json:"reused"`
	Dropped   []string          `json:"dropped"`
	Unmanaged []string          `json:"unmanaged"`
	Warnings  []string          `json:"-"`
	Overrides []mrpack.Override `json:"-"`
}

type importer struct {
	r         *Resolver
	a         *mrpack.Archive
	rep       *Imported
	modrinth  hashLookup
	bySha     map[string]string
	packBySha map[string]importedPack
	matched   map[string]bool
}

// importedPack is a resource pack or shader the pack's own lock names, found by
// the digest of the file the archive ships.
type importedPack struct {
	key  string
	kind string
}

// ImportMrpack locks the mods a Modrinth pack lists, reporting what it locked, reused, dropped and
// left unmanaged.
func (r *Resolver) ImportMrpack(ctx context.Context, a *mrpack.Archive) (*Imported, error) {
	return r.importMrpack(ctx, a, true)
}

// importMrpack is ImportMrpack. Without reuseLocal, a local file the pack's own lock names is laid
// as an override rather than reused: its path is the exporter's, and outside the archive only the
// cache holds its bytes.
func (r *Resolver) importMrpack(ctx context.Context, a *mrpack.Archive, reuseLocal bool) (*Imported, error) {
	im := &importer{r: r, a: a, rep: &Imported{Locked: []string{}, Reused: []string{}, Dropped: []string{}, Unmanaged: []string{}, Warnings: []string{}}, bySha: map[string]string{}, packBySha: map[string]importedPack{}, matched: map[string]bool{}}
	if p, ok := r.Providers["modrinth"].(hashLookup); ok {
		im.modrinth = p
	}
	if a.Marker != nil {
		for id, m := range a.Marker.Lock.Mods {
			if reuseLocal || m.File == "" {
				im.bySha[m.Sha512] = id
			}
		}
		for key, p := range a.Marker.Lock.ResourcePacks {
			if reuseLocal || p.File == "" {
				im.packBySha[p.Sha512] = importedPack{key: key, kind: manifest.TypeResourcePack}
			}
		}
		for key, p := range a.Marker.Lock.Shaders {
			if reuseLocal || p.File == "" {
				im.packBySha[p.Sha512] = importedPack{key: key, kind: manifest.TypeShader}
			}
		}
	}
	for _, f := range a.Index.Files {
		if err := im.indexFile(ctx, f); err != nil {
			return nil, err
		}
	}
	for _, o := range a.Overrides {
		if err := im.override(o); err != nil {
			return nil, err
		}
	}
	im.dropUnmatched()
	sort.Strings(im.rep.Locked)
	sort.Strings(im.rep.Reused)
	sort.Strings(im.rep.Dropped)
	sort.Strings(im.rep.Unmanaged)
	return im.rep, nil
}

func (im *importer) indexFile(ctx context.Context, f mrpack.File) error {
	sha512Sum, sha1Sum := f.Hashes["sha512"], f.Hashes["sha1"]
	if sha512Sum == "" || sha1Sum == "" || len(f.Downloads) == 0 {
		return out.Errorf("mrpack-invalid", "index file %s lacks sha1, sha512, or a download url", f.Path)
	}
	if id, ok := im.bySha[sha512Sum]; ok {
		im.reuse(id, f.Side())
		return nil
	}
	if p, ok := im.packBySha[sha512Sum]; ok {
		im.reusePack(p)
		return nil
	}
	if !mrpack.IsModJar(f.Path) || im.modrinth == nil {
		return im.unmanagedDownload(ctx, f)
	}
	v, found, err := im.modrinth.VersionByHash(ctx, sha1Sum)
	if err != nil {
		return lookupFailed(f.Path, err)
	}
	if !found {
		return im.unmanagedDownload(ctx, f)
	}
	proj, err := im.modrinth.Project(ctx, v.ProjectID, "")
	if err != nil {
		return lookupFailed(f.Path, err)
	}
	id, prior, err := im.r.place(ctx, im.modrinth, proj, v, "", "", "", "", false)
	if err != nil {
		return err
	}
	if prior != nil {
		im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s appears twice in the pack; kept %s", id, im.r.Lock.Mods[id].Filename))
		return nil
	}
	entry := manifest.Require{}
	if prior, ok := im.markerManifestMod(id); ok {
		entry = prior
		if entry.Pin != nil {
			im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s: the marker pinned %v but the pack ships %s; pin dropped", id, entry.Pin, v.Number))
			entry.Pin = nil
		}
	}
	if proj.Slug != id {
		entry.Project = proj.ID
	}
	if im.modrinth.Name() != im.r.Manifest.ProviderOrder()[0] {
		entry.Provider = im.modrinth.Name()
	}
	im.r.Manifest.Requires[id] = entry
	im.rep.Locked = append(im.rep.Locked, id)
	return nil
}

// lookupFailed is a file in the pack that couldn't be looked up on Modrinth, with the provider's
// error in a row. A network failure stays one for fetch.IsNetwork.
func lookupFailed(file string, err error) error {
	e := out.Errorf("mrpack-lookup", "couldn't look up %s on Modrinth", file).WithCause("modrinth", err)
	if !fetch.IsNetwork(err) {
		return e
	}
	e.Help = "looking a pack's files up needs the network"
	return fetch.Unreachable(e)
}

func (im *importer) unmanagedDownload(ctx context.Context, f mrpack.File) error {
	im.r.log("fetching %s", f.Path)
	p, err := im.r.Cache.Ensure(ctx, im.r.Fetch, f.Downloads[0], f.Hashes["sha512"])
	if err != nil {
		return out.Errorf("mrpack-download", "couldn't download %s", f.Path).WithCause("download", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	im.rep.Overrides = append(im.rep.Overrides, mrpack.Override{Layer: f.Layer(), Path: f.Path, Data: data})
	im.rep.Unmanaged = append(im.rep.Unmanaged, f.Layer()+"/"+f.Path)
	return nil
}

func (im *importer) override(o mrpack.Override) error {
	if !mrpack.IsModJar(o.Path) && !mrpack.IsPackZip(o.Path) {
		im.rep.Overrides = append(im.rep.Overrides, o)
		return nil
	}
	sum := sha512.Sum512(o.Data)
	digest := hex.EncodeToString(sum[:])
	if p, ok := im.packBySha[digest]; ok {
		if _, err := im.r.Cache.Put(bytes.NewReader(o.Data)); err != nil {
			return err
		}
		im.reusePack(p)
		return nil
	}
	if id, ok := im.bySha[digest]; ok {
		if _, err := im.r.Cache.Put(bytes.NewReader(o.Data)); err != nil {
			return err
		}
		im.reuse(id, layerSide(o.Layer))
		return nil
	}
	im.rep.Overrides = append(im.rep.Overrides, o)
	im.rep.Unmanaged = append(im.rep.Unmanaged, o.Layer+"/"+o.Path)
	return nil
}

func (im *importer) reuse(id, side string) {
	if im.matched[id] {
		return
	}
	im.matched[id] = true
	entry := im.a.Marker.Lock.Mods[id]
	if entry.Side != side {
		im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s: the marker says side %s but the pack ships it for %s; using %s", id, entry.Side, side, side))
		entry.Side = side
	}
	im.r.Lock.Mods[id] = entry
	if prior, ok := im.markerManifestMod(id); ok {
		im.r.Manifest.Requires[id] = prior
	}
	im.rep.Reused = append(im.rep.Reused, id)
}

// reusePack puts a pack back exactly as the modpack locked it, in the section
// its kind belongs to.
func (im *importer) reusePack(p importedPack) {
	if im.matched[p.key] {
		return
	}
	im.matched[p.key] = true
	locked, listed, into := im.a.Marker.Lock.ResourcePacks, im.a.Marker.Manifest.ResourcePacks(), im.r.Lock.ResourcePacks
	if p.kind == manifest.TypeShader {
		locked, listed, into = im.a.Marker.Lock.Shaders, im.a.Marker.Manifest.Shaders(), im.r.Lock.Shaders
	}
	into[p.key] = locked[p.key]
	if entry, ok := listed[p.key]; ok {
		im.r.Manifest.Requires[p.key] = entry
	}
	im.rep.Reused = append(im.rep.Reused, p.key)
}

func (im *importer) markerManifestMod(id string) (manifest.Require, bool) {
	if im.a.Marker == nil {
		return manifest.Require{}, false
	}
	m, ok := im.a.Marker.Manifest.Mods()[id]
	return m, ok
}

func (im *importer) dropUnmatched() {
	if im.a.Marker == nil {
		return
	}
	for id := range im.a.Marker.Lock.Mods {
		if im.matched[id] {
			continue
		}
		if _, replaced := im.r.Lock.Mods[id]; replaced {
			continue
		}
		delete(im.r.Manifest.Requires, id)
		im.rep.Dropped = append(im.rep.Dropped, id)
	}
	dropPacks := func(marker, mine map[string]lock.Pack) {
		for key := range marker {
			if im.matched[key] {
				continue
			}
			if _, replaced := mine[key]; replaced {
				continue
			}
			delete(im.r.Manifest.Requires, key)
			im.rep.Dropped = append(im.rep.Dropped, key)
		}
	}
	dropPacks(im.a.Marker.Lock.ResourcePacks, im.r.Lock.ResourcePacks)
	dropPacks(im.a.Marker.Lock.Shaders, im.r.Lock.Shaders)
	for id, m := range im.r.Lock.Mods {
		kept := m.RequiredBy[:0]
		for _, by := range m.RequiredBy {
			if _, ok := im.r.Lock.Mods[by]; ok {
				kept = append(kept, by)
			}
		}
		m.RequiredBy = kept
		im.r.Lock.Mods[id] = m
	}
}

// AdoptLocalFiles makes the local files an imported lock names the project's own: each is copied
// from the cache into manifest.FilesDir, and its lock and requires entries point at the copy. Their
// paths are the exporter's, and until then only the cache holds their bytes.
func (r *Resolver) AdoptLocalFiles() error {
	owner := map[string]string{}
	digest := map[string]string{}
	claim := func(key, file, sha512 string) error {
		if file == "" {
			return nil
		}
		to := manifest.FilesDir + "/" + path.Base(file)
		if prior, ok := owner[to]; ok && digest[to] != sha512 {
			return out.Errorf("file-taken", "%s and %s are both local files named %s", prior, key, path.Base(to))
		}
		owner[to], digest[to] = key, sha512
		return nil
	}
	for _, key := range slices.Sorted(maps.Keys(r.Lock.Mods)) {
		if err := claim(key, r.Lock.Mods[key].File, r.Lock.Mods[key].Sha512); err != nil {
			return err
		}
	}
	for _, packs := range []map[string]lock.Pack{r.Lock.ResourcePacks, r.Lock.Shaders} {
		for _, key := range slices.Sorted(maps.Keys(packs)) {
			if err := claim(key, packs[key].File, packs[key].Sha512); err != nil {
				return err
			}
		}
	}

	adopt := func(key, file, sha512 string) (string, error) {
		to := manifest.FilesDir + "/" + path.Base(file)
		if err := r.Cache.CopyTo(sha512, filepath.Join(r.Dir, filepath.FromSlash(to))); err != nil {
			return "", err
		}
		if req, ok := r.Manifest.Requires[key]; ok {
			req.File = to
			r.Manifest.Requires[key] = req
		}
		return to, nil
	}
	for key, m := range r.Lock.Mods {
		if m.File == "" {
			continue
		}
		to, err := adopt(key, m.File, m.Sha512)
		if err != nil {
			return err
		}
		m.File = to
		r.Lock.Mods[key] = m
	}
	for _, packs := range []map[string]lock.Pack{r.Lock.ResourcePacks, r.Lock.Shaders} {
		for key, p := range packs {
			if p.File == "" {
				continue
			}
			to, err := adopt(key, p.File, p.Sha512)
			if err != nil {
				return err
			}
			p.File = to
			packs[key] = p
		}
	}
	return nil
}

func layerSide(layer string) string {
	switch layer {
	case "client-overrides":
		return "client"
	case "server-overrides":
		return "server"
	}
	return "both"
}

// ConsumeArchive locks what a modpack archive holds as that modpack's own lock and manifest, the
// way ImportMrpack or ImportCurseForge locks one into a new project, and records in its pin the
// files the archive lays itself. r's own lock is left alone; only its providers, provider order,
// cache and fetch client are used.
func (r *Resolver) ConsumeArchive(ctx context.Context, l *pack.Loaded) error {
	a := l.Archive
	minecraft := a.Index.Dependencies["minecraft"]
	if minecraft == "" {
		return out.Errorf("mrpack-invalid", "modpack %s: the archive's index names no minecraft version", l.Name)
	}
	typ, version, _ := a.Loader()
	m := &manifest.Manifest{Name: l.Name, Minecraft: minecraft, Loader: manifest.Loader{Type: typ, Version: version}, Requires: map[string]manifest.Require{}, Providers: r.Manifest.Providers}
	pl := lock.New()
	pl.Minecraft, pl.Loader = minecraft, lock.Loader{Type: typ, Version: version}
	scratch := &Resolver{Dir: r.Dir, Manifest: m, Lock: pl, Providers: r.Providers, Cache: r.Cache, Fetch: r.Fetch, Log: r.Log}
	var rep *Imported
	var err error
	if l.CurseForge != nil {
		if r.Fetch != nil && r.Fetch.Offline {
			return curseForgeOffline(l.Name, nil)
		}
		rep, err = scratch.ImportCurseForge(ctx, l.CurseForge)
		if fetch.IsNetwork(err) {
			return curseForgeOffline(l.Name, err)
		}
	} else {
		rep, err = scratch.importMrpack(ctx, a, false)
	}
	if err != nil {
		return prefixed("modpack "+l.Name, err)
	}
	unmanaged := map[string]bool{}
	for _, key := range rep.Unmanaged {
		unmanaged[key] = true
	}
	l.Pin.Unmanaged = map[string]string{}
	for _, o := range rep.Overrides {
		if key := o.Layer + "/" + o.Path; unmanaged[key] {
			sum := sha512.Sum512(o.Data)
			l.Pin.Unmanaged[key] = hex.EncodeToString(sum[:])
		}
	}
	if len(l.Pin.Unmanaged) == 0 {
		l.Pin.Unmanaged = nil
	}
	l.Manifest, l.Lock, l.Warnings = m, pl, rep.Warnings
	return nil
}

// curseForgeOffline is a CurseForge modpack read without the network. Its files are named by id
// alone, with no hash to find them in the cache by, so even a warm cache can't stand in.
func curseForgeOffline(name string, cause error) error {
	e := out.Errorf("curseforge-offline", "modpack %s: a CurseForge modpack can't be read offline, even with every file it names in the cache", name)
	e.Help = "it names its files by CurseForge id, which only the CurseForge API resolves; run the command again online"
	if cause != nil {
		e = e.WithCause("curseforge", cause)
	}
	return fetch.Unreachable(e)
}
