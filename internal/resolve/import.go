package resolve

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
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

func (r *Resolver) ImportMrpack(ctx context.Context, a *mrpack.Archive) (*Imported, error) {
	im := &importer{r: r, a: a, rep: &Imported{Locked: []string{}, Reused: []string{}, Dropped: []string{}, Unmanaged: []string{}, Warnings: []string{}}, bySha: map[string]string{}, packBySha: map[string]importedPack{}, matched: map[string]bool{}}
	if p, ok := r.Providers["modrinth"].(hashLookup); ok {
		im.modrinth = p
	}
	if a.Marker != nil {
		for id, m := range a.Marker.Lock.Mods {
			im.bySha[m.Sha512] = id
		}
		for key, p := range a.Marker.Lock.ResourcePacks {
			im.packBySha[p.Sha512] = importedPack{key: key, kind: manifest.TypeResourcePack}
		}
		for key, p := range a.Marker.Lock.Shaders {
			im.packBySha[p.Sha512] = importedPack{key: key, kind: manifest.TypeShader}
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
	if !isModJar(f.Path) || im.modrinth == nil {
		return im.unmanagedDownload(ctx, f)
	}
	v, found, err := im.modrinth.VersionByHash(ctx, sha1Sum)
	if err != nil {
		return err
	}
	if !found {
		return im.unmanagedDownload(ctx, f)
	}
	proj, err := im.modrinth.Project(ctx, v.ProjectID, "")
	if err != nil {
		return err
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

func (im *importer) unmanagedDownload(ctx context.Context, f mrpack.File) error {
	im.r.log("fetching %s", f.Path)
	p, err := im.r.Cache.Ensure(ctx, im.r.Fetch, f.Downloads[0], f.Hashes["sha512"])
	if err != nil {
		return out.Errorf("mrpack-download", "%s: %v", f.Path, err)
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
	if !isModJar(o.Path) && !isPackZip(o.Path) {
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

func isModJar(p string) bool {
	return path.Dir(p) == "mods" && strings.EqualFold(path.Ext(p), ".jar")
}

func isPackZip(p string) bool {
	dir := path.Dir(p)
	return (dir == "resourcepacks" || dir == "shaderpacks") && strings.EqualFold(path.Ext(p), ".zip")
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
