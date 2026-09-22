package resolve

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/curseforge"
)

type hashLookup interface {
	provider.Provider
	VersionsByHash(ctx context.Context, sha1s []string) (map[string]provider.Version, error)
	Projects(ctx context.Context, ids []string) (map[string]provider.Project, error)
}

type curseForgeLookup interface {
	provider.Provider
	MatchFingerprints(ctx context.Context, fingerprints []uint32) (map[uint32]curseforge.Match, error)
	Mods(ctx context.Context, ids []int) (map[int]*provider.Project, error)
	Files(ctx context.Context, ids []int) (found map[int]provider.Version, unusable map[int]error, err error)
}

// hosted is a file found on a provider: the version it is and that version's project.
type hosted struct {
	proj *provider.Project
	v    *provider.Version
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
	// onModrinth is what Modrinth found of the index's files, by sha1.
	onModrinth map[string]hosted
	// unmatched are the mod jars and pack zips Modrinth didn't find, for CurseForge to look up.
	unmatched []mrpack.Override
	// keepSides reuses the marker's side for each entry, for an archive whose layers don't say.
	keepSides bool
}

// importedPack is a resource pack or shader the pack's own lock names, found by
// the digest of the file the archive ships.
type importedPack struct {
	key  string
	kind string
}

func newImporter(r *Resolver, a *mrpack.Archive, reuseLocal bool) *importer {
	im := &importer{r: r, a: a, rep: &Imported{Locked: []string{}, Reused: []string{}, Dropped: []string{}, Unmanaged: []string{}, Warnings: []string{}}, bySha: map[string]string{}, packBySha: map[string]importedPack{}, matched: map[string]bool{}, onModrinth: map[string]hosted{}}
	if a.Marker == nil {
		return im
	}
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
	return im
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
	im := newImporter(r, a, reuseLocal)
	if p, ok := r.Providers["modrinth"].(hashLookup); ok {
		im.modrinth = p
	}
	if err := im.findOnModrinth(ctx, a.Index.Files); err != nil {
		return nil, err
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
	if err := im.matchCurseForge(ctx); err != nil {
		return nil, err
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
	if !mrpack.IsModJar(f.Path) && !mrpack.IsPackZip(f.Path) {
		return im.unmanagedDownload(ctx, f)
	}
	found, ok := im.onModrinth[sha1Sum]
	if !ok {
		return im.leaveForCurseForge(ctx, f)
	}
	locked, err := im.lockFile(ctx, im.modrinth, f.Layer(), f.Path, "", found.proj, found.v)
	if err != nil || locked {
		return err
	}
	return im.unmanagedDownload(ctx, f)
}

// findOnModrinth looks every mod jar and pack zip the index lists up on Modrinth in two requests,
// whatever the pack's size, since Modrinth rate-limits by the request. A version whose project
// Modrinth no longer has counts as not found.
func (im *importer) findOnModrinth(ctx context.Context, files []mrpack.File) error {
	if im.modrinth == nil {
		return nil
	}
	var sha1s []string
	for _, f := range files {
		sha1Sum, sha512Sum := f.Hashes["sha1"], f.Hashes["sha512"]
		_, reused := im.bySha[sha512Sum]
		_, reusedPack := im.packBySha[sha512Sum]
		if sha1Sum != "" && !reused && !reusedPack && (mrpack.IsModJar(f.Path) || mrpack.IsPackZip(f.Path)) {
			sha1s = append(sha1s, sha1Sum)
		}
	}
	if len(sha1s) == 0 {
		return nil
	}
	im.r.log("looking up %d file(s) on Modrinth", len(sha1s))
	failed := func(err error) error { return lookupFailed("Modrinth", fmt.Sprintf("%d file(s)", len(sha1s)), err) }
	versions, err := im.modrinth.VersionsByHash(ctx, sha1s)
	if err != nil {
		return failed(err)
	}
	var ids []string
	for _, v := range versions {
		ids = append(ids, v.ProjectID)
	}
	if len(ids) == 0 {
		return nil
	}
	slices.Sort(ids)
	projects, err := im.modrinth.Projects(ctx, slices.Compact(ids))
	if err != nil {
		return failed(err)
	}
	for sha1Sum, v := range versions {
		if proj, ok := projects[v.ProjectID]; ok {
			im.onModrinth[sha1Sum] = hosted{proj: &proj, v: &v}
		}
	}
	return nil
}

func (im *importer) leaveForCurseForge(ctx context.Context, f mrpack.File) error {
	o, err := im.download(ctx, f)
	if err != nil {
		return err
	}
	im.unmatched = append(im.unmatched, o)
	return nil
}

// matchCurseForge looks every file Modrinth didn't find up on CurseForge by fingerprint, in one
// request, and locks each exact match CurseForge lets third parties download. The rest stay
// unmanaged.
func (im *importer) matchCurseForge(ctx context.Context) error {
	left := im.unmatched
	im.unmatched = nil
	if len(left) == 0 {
		return nil
	}
	cf, ok := im.r.Providers["curseforge"].(curseForgeLookup)
	if !ok {
		im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%d file(s) Modrinth doesn't host weren't looked up on CurseForge, which needs an API key; kept as overrides", len(left)))
		im.keepUnmanaged(left)
		return nil
	}
	fingerprints := make([]uint32, len(left))
	for i, o := range left {
		fingerprints[i] = curseforge.Fingerprint(o.Data)
	}
	im.r.log("looking up %d file(s) on CurseForge", len(left))
	matches, err := cf.MatchFingerprints(ctx, fingerprints)
	if err != nil {
		return lookupFailed("CurseForge", fmt.Sprintf("%d file(s)", len(left)), err)
	}
	found, err := findOnCurseForge(ctx, cf, matches)
	if err != nil {
		return lookupFailed("CurseForge", fmt.Sprintf("%d file(s)", len(left)), err)
	}
	for i, o := range left {
		m, ok := found[fingerprints[i]]
		if !ok {
			im.unmanaged(o)
			continue
		}
		if err := im.lockCurseForgeMatch(ctx, cf, o, m); err != nil {
			return err
		}
	}
	return nil
}

// findOnCurseForge fetches the projects and files of every match in two requests, leaving out a
// match whose project or file CurseForge no longer has.
func findOnCurseForge(ctx context.Context, cf curseForgeLookup, matches map[uint32]curseforge.Match) (map[uint32]hosted, error) {
	if len(matches) == 0 {
		return nil, nil
	}
	var modIDs, fileIDs []int
	for _, m := range matches {
		modIDs = append(modIDs, m.ModID)
		fileIDs = append(fileIDs, m.FileID)
	}
	slices.Sort(modIDs)
	slices.Sort(fileIDs)
	projects, err := cf.Mods(ctx, slices.Compact(modIDs))
	if err != nil {
		return nil, err
	}
	files, _, err := cf.Files(ctx, slices.Compact(fileIDs))
	if err != nil {
		return nil, err
	}
	found := map[uint32]hosted{}
	for fp, m := range matches {
		proj, hasProject := projects[m.ModID]
		v, hasFile := files[m.FileID]
		if hasProject && hasFile {
			found[fp] = hosted{proj: proj, v: &v}
		}
	}
	return found, nil
}

func (im *importer) lockCurseForgeMatch(ctx context.Context, cf provider.Provider, o mrpack.Override, m hosted) error {
	file := o.Layer + "/" + o.Path
	proj, v := m.proj, m.v
	if v.File.URL == "" {
		im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s is on CurseForge, but its author doesn't allow third-party downloads; kept as an override", file))
		im.unmanaged(o)
		return nil
	}
	side := layerSide(o.Layer)
	if side == "both" {
		side = ""
	}
	locked, err := im.lockFile(ctx, cf, o.Layer, o.Path, side, proj, v)
	if err != nil || locked {
		return err
	}
	im.unmanaged(o)
	return nil
}

// lockFile locks the mod jar, resource pack or shader at filePath in layer as p's version v,
// reporting false, with nothing locked, when proj isn't the kind its folder holds (a datapack
// under resourcepacks/, which Modrinth calls a mod, stays unmanaged) or p fails to serve it. The
// lock downloads it from p even when the pack ships the same bytes, since every later install will.
func (im *importer) lockFile(ctx context.Context, p provider.Provider, layer, filePath, side string, proj *provider.Project, v *provider.Version) (bool, error) {
	kind := manifest.TypeMod
	switch path.Dir(filePath) {
	case "resourcepacks":
		kind = manifest.TypeResourcePack
	case "shaderpacks":
		kind = manifest.TypeShader
	}
	projKind := proj.Type
	if projKind == "" {
		projKind = manifest.TypeMod
	}
	if projKind != kind {
		return false, nil
	}
	var err error
	if kind == manifest.TypeMod {
		err = im.lockMod(ctx, p, side, proj, v)
	} else {
		err = im.lockPack(ctx, p, path.Base(filePath), kind, proj, v)
	}
	if why, failed := downloadFailure(err); failed {
		im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s/%s: %s's download failed (%s); kept as an override, so try matching it again later", layer, filePath, provider.Title(p.Name()), why))
		return false, nil
	}
	return err == nil, err
}

// downloadFailure says why err is a provider failing to serve a file, as a CDN cutting one short
// does, rather than a failure of shulker's own, such as the cache's disk.
func downloadFailure(err error) (string, bool) {
	var status *fetch.StatusError
	switch {
	case err == nil, errors.Is(err, fetch.ErrOffline):
		return "", false
	case errors.As(err, &status):
		return fmt.Sprintf("HTTP %d", status.Status), true
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "the file was cut short", true
	case out.CodeOf(err) == "checksum-mismatch":
		return "the file doesn't match its hash", true
	case out.CodeOf(err) == "manual-download":
		return "it refused the download", true
	case fetch.IsNetwork(err):
		return "the connection failed", true
	}
	return "", false
}

func (im *importer) lockMod(ctx context.Context, p provider.Provider, side string, proj *provider.Project, v *provider.Version) error {
	id, prior, err := im.r.place(ctx, p, proj, v, "", "", side, "", false)
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
	if proj.Slug != id || p.Name() != "modrinth" {
		entry.Project = lockID(p.Name(), proj.ID)
	}
	if p.Name() != im.r.Manifest.ProviderOrder()[0] {
		entry.Provider = p.Name()
	}
	im.r.Manifest.Requires[id] = entry
	im.rep.Locked = append(im.rep.Locked, id)
	return nil
}

// lockPack locks a resource pack or shader the pack carries under the file name it ships, which
// the game enables it by, pinned to the version it ships.
func (im *importer) lockPack(ctx context.Context, p provider.Provider, filename, kind string, proj *provider.Project, v *provider.Version) error {
	r := im.r
	key := proj.Slug
	if ok, err := im.canListPack(key, kind); !ok {
		return err
	}
	defer func() {
		if _, locked := r.packSection(kind)[key]; !locked {
			delete(r.Manifest.Requires, key)
		}
	}()
	listed := manifest.Require{Type: kind, Pin: lockID(p.Name(), v.ID)}
	if filename != key+manifest.FileExtension(kind) {
		listed.Filename = filename
	}
	if p.Name() != "modrinth" {
		listed.Project = lockID(p.Name(), proj.ID)
	}
	if p.Name() != r.Manifest.ProviderOrder()[0] {
		listed.Provider = p.Name()
	}
	r.Manifest.Requires[key] = listed
	if err := r.lockPackVersion(ctx, p, proj, v, key, kind, ""); err != nil {
		return err
	}
	im.rep.Locked = append(im.rep.Locked, key)
	return nil
}

// canListPack reports whether the pack's resource pack or shader can be listed under key. One the
// pack already listed is a duplicate: the first is kept, with a warning.
func (im *importer) canListPack(key, kind string) (bool, error) {
	if held, taken := im.r.Manifest.Requires[key]; taken && held.Kind() == kind {
		im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s appears twice in the pack; kept the first", key))
		return false, nil
	}
	if !manifest.IsValidKey(key) {
		return false, out.Errorf("requires-unsupported", "%s can't be a requires key", key)
	}
	return true, im.r.packKeyFree(key, kind)
}

// lookupFailed is a file in the pack that couldn't be looked up on a provider, with the provider's
// error in a row. A network failure stays one for fetch.IsNetwork.
func lookupFailed(on, file string, err error) error {
	e := out.Errorf("mrpack-lookup", "couldn't look up %s on %s", file, on).WithCause(strings.ToLower(on), err)
	if !fetch.IsNetwork(err) {
		return e
	}
	e.Help = "looking a pack's files up needs the network"
	return fetch.Unreachable(e)
}

func (im *importer) unmanagedDownload(ctx context.Context, f mrpack.File) error {
	o, err := im.download(ctx, f)
	if err != nil {
		return err
	}
	im.unmanaged(o)
	return nil
}

func (im *importer) download(ctx context.Context, f mrpack.File) (mrpack.Override, error) {
	im.r.log("fetching %s", f.Path)
	p, err := im.r.Cache.Ensure(ctx, im.r.Fetch, f.Downloads[0], f.Hashes["sha512"])
	if err != nil {
		return mrpack.Override{}, out.Errorf("mrpack-download", "couldn't download %s", f.Path).WithCause("download", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return mrpack.Override{}, err
	}
	return mrpack.Override{Layer: f.Layer(), Path: f.Path, Data: data}, nil
}

func (im *importer) unmanaged(o mrpack.Override) {
	im.rep.Overrides = append(im.rep.Overrides, o)
	im.rep.Unmanaged = append(im.rep.Unmanaged, o.Layer+"/"+o.Path)
}

func (im *importer) keepUnmanaged(files []mrpack.Override) {
	for _, o := range files {
		im.unmanaged(o)
	}
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
		side := layerSide(o.Layer)
		if im.keepSides {
			side = ""
		}
		im.reuse(id, side)
		return nil
	}
	im.unmatched = append(im.unmatched, o)
	return nil
}

func (im *importer) reuse(id, side string) {
	if im.matched[id] {
		return
	}
	im.matched[id] = true
	entry := im.a.Marker.Lock.Mods[id]
	if side != "" && entry.Side != side {
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
	type local struct {
		key, sha512, to string
		isFolder        bool
		set             func(string)
	}
	var locals []local
	for _, key := range slices.Sorted(maps.Keys(r.Lock.Mods)) {
		if m := r.Lock.Mods[key]; m.File != "" {
			locals = append(locals, local{key, m.Sha512, manifest.FilesDir + "/" + path.Base(m.File), false, func(to string) { m.File = to; r.Lock.Mods[key] = m }})
		}
	}
	for _, packs := range []map[string]lock.Pack{r.Lock.ResourcePacks, r.Lock.Shaders} {
		for _, key := range slices.Sorted(maps.Keys(packs)) {
			if p := packs[key]; p.File != "" {
				// A pack's file that isn't a .zip is a folder, which the lock holds as its zip.
				isFolder := !strings.HasSuffix(p.File, manifest.FileExtension(manifest.TypeResourcePack))
				locals = append(locals, local{key, p.Sha512, manifest.FilesDir + "/" + path.Base(p.File), isFolder, func(to string) { p.File = to; packs[key] = p }})
			}
		}
	}
	owner := map[string]local{}
	for _, l := range locals {
		if prior, ok := owner[l.to]; ok && prior.sha512 != l.sha512 {
			e := out.Errorf("file-taken", "%s and %s are both local files named %s", prior.key, l.key, path.Base(l.to))
			e.Help = "rename one of them in the project that exported the pack, then export it again"
			return e
		}
		owner[l.to] = l
	}
	for _, l := range locals {
		to := filepath.Join(r.Dir, filepath.FromSlash(l.to))
		copyOut := r.Cache.CopyTo
		if l.isFolder {
			copyOut = r.unpackFolder
		}
		if err := copyOut(l.sha512, to); err != nil {
			return err
		}
		l.set(l.to)
		if req, ok := r.Manifest.Requires[l.key]; ok {
			req.File = l.to
			r.Manifest.Requires[l.key] = req
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

// unpackFolder lays the pack folder zip the cache holds under sha512 out as the folder at to. It
// zips back to the same bytes, since zipfile.Folder writes a folder's files alone, in path order.
func (r *Resolver) unpackFolder(sha512, to string) error {
	zr, err := zip.OpenReader(r.Cache.Object(sha512))
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.RemoveAll(to); err != nil {
		return err
	}
	for _, f := range zr.File {
		rel := filepath.FromSlash(f.Name)
		if !filepath.IsLocal(rel) || strings.HasSuffix(f.Name, "/") {
			return out.Errorf("lock-invalid", "the pack folder %s holds %s, which can't be laid out in it", filepath.Base(to), f.Name)
		}
		if err := unpackFile(f, filepath.Join(to, rel)); err != nil {
			return err
		}
	}
	return nil
}

func unpackFile(f *zip.File, to string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFrom(to, src)
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
