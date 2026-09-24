package resolve

import (
	"archive/zip"
	"bytes"
	"cmp"
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

	"shulker.sh/shulker/internal/cfpack"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/provider"
)

// hosted is a file found on a provider: the provider, the version it is and that version's
// project.
type hosted struct {
	p    provider.Provider
	proj *provider.Project
	v    *provider.Version
}

type Imported struct {
	Locked  []LockedFile `json:"locked"`
	Reused  []string     `json:"reused"`
	Dropped []string     `json:"dropped"`
	// Duplicates are the pack's copies of a datapack a global datapack mod's folder also holds,
	// left out for the copy that mod loads: an index file by its index path, an override by its
	// path in the archive.
	Duplicates []string `json:"duplicates"`
	Unmanaged  []string `json:"unmanaged"`
	// Sides are the mods the index's env widens beyond their provider's side.
	Sides     []SideChoice      `json:"sides"`
	Warnings  []string          `json:"-"`
	Overrides []mrpack.Override `json:"-"`
}

// SideChoice is a mod locked on a wider side than its provider's, since the index's env places it
// on a side the project builds.
type SideChoice struct {
	ID       string `json:"id"`
	Pack     string `json:"pack"`
	Provider string `json:"provider"`
}

// LockedFile is a file an import locked from a provider, under its key in the lock.
type LockedFile struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Provider string `json:"provider"`
}

// LockedIDs are the keys of the files the import locked.
func (rep *Imported) LockedIDs() []string {
	ids := make([]string, len(rep.Locked))
	for i, f := range rep.Locked {
		ids[i] = f.ID
	}
	return ids
}

func (rep *Imported) locked(id, kind, provider string) {
	rep.Locked = append(rep.Locked, LockedFile{ID: id, Type: kind, Provider: provider})
}

func (rep *Imported) sort() {
	slices.SortFunc(rep.Locked, func(a, b LockedFile) int {
		if c := strings.Compare(a.ID, b.ID); c != 0 {
			return c
		}
		return strings.Compare(a.Type, b.Type)
	})
	sort.Strings(rep.Reused)
	sort.Strings(rep.Dropped)
	sort.Strings(rep.Duplicates)
	sort.Strings(rep.Unmanaged)
	slices.SortFunc(rep.Sides, func(a, b SideChoice) int { return strings.Compare(a.ID, b.ID) })
}

// warnSides warns once for every mod that takes its side from the pack rather than its provider.
func (rep *Imported) warnSides() {
	if len(rep.Sides) == 0 {
		return
	}
	mods := make([]string, len(rep.Sides))
	for i, s := range rep.Sides {
		mods[i] = fmt.Sprintf("%s (%s → %s)", s.ID, s.Provider, s.Pack)
	}
	rep.Warnings = append(rep.Warnings, fmt.Sprintf("%d mod(s) take their side from the pack rather than their provider: %s", len(rep.Sides), strings.Join(mods, ", ")))
}

type importer struct {
	r         *Resolver
	a         *mrpack.Archive
	rep       *Imported
	bySha     map[string]string
	packBySha map[string]importedPack
	matched   map[string]bool
	// found is what the providers found of the files to identify, by layer and path.
	found map[string]hosted
	// sides are the sides to lock the files to identify with, by layer and path: an index file's
	// env, or an override's layer, and empty where neither says.
	sides map[string]string
	// served is the URL each downloaded index file came from, by layer and path.
	served map[string]string
	// loadedDatapacks are the sha512s of the zips in a global datapack mod's folder.
	loadedDatapacks map[string]bool
	// hybrids are the resourcepacks/ copies of loaded datapacks that carry assets/, by sha512,
	// held back until the loaded copy is locked.
	hybrids map[string][]hybridCopy
	// unmatched are the mod jars and pack zips to identify, index files and overrides alike, in
	// the order the pack lists them.
	unmatched []mrpack.Override
	// indexSides are the sides the index's env gives its files, by layer and path.
	indexSides map[string]string
	// keepSides reuses the marker's side for each entry, for an archive whose layers don't say.
	keepSides bool
	// inProject marks the files as a project's own overrides rather than a pack's: one whose key
	// requires already holds stays an override instead of being dropped as a duplicate.
	inProject bool
}

// hybridCopy is a hybrid datapack's copy under resourcepacks/, an index file or an override.
type hybridCopy struct {
	file     *mrpack.File
	override *mrpack.Override
}

// importedPack is a pack the pack's own lock names, found by
// the digest of the file the archive ships.
type importedPack struct {
	key  string
	kind string
}

func newImporter(r *Resolver, a *mrpack.Archive, reuseLocal bool) *importer {
	im := &importer{r: r, a: a, rep: &Imported{Locked: []LockedFile{}, Reused: []string{}, Dropped: []string{}, Duplicates: []string{}, Unmanaged: []string{}, Sides: []SideChoice{}, Warnings: []string{}}, bySha: map[string]string{}, indexSides: map[string]string{}, packBySha: map[string]importedPack{}, matched: map[string]bool{}, found: map[string]hosted{}, sides: map[string]string{}, served: map[string]string{}, loadedDatapacks: map[string]bool{}, hybrids: map[string][]hybridCopy{}}
	if a.Marker == nil {
		return im
	}
	for id, m := range a.Marker.Lock.Mods {
		if reuseLocal || m.File == "" {
			im.bySha[m.Sha512] = id
		}
	}
	for _, kind := range manifest.PackKinds {
		for key, p := range a.Marker.Lock.Packs(kind) {
			if reuseLocal || p.File == "" {
				im.packBySha[p.Sha512] = importedPack{key: key, kind: kind}
			}
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
	im.findLoadedDatapacks(a)
	for _, f := range a.Index.Files {
		if err := im.indexFile(ctx, f); err != nil {
			return nil, err
		}
	}
	for _, o := range a.Overrides {
		if err := im.override(ctx, o); err != nil {
			return nil, err
		}
	}
	if err := im.identify(ctx); err != nil {
		return nil, err
	}
	if err := im.lockIdentified(ctx); err != nil {
		return nil, err
	}
	if err := im.settleHybrids(ctx); err != nil {
		return nil, err
	}
	im.dropUnmatched()
	im.rep.sort()
	im.rep.warnSides()
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
	if dup, err := im.duplicateDatapack(ctx, hybridCopy{file: &f}, f.Path, f.Path, sha512Sum); err != nil || dup {
		return err
	}
	if len(f.Env) > 0 {
		im.indexSides[f.Layer()+"/"+f.Path] = f.Side()
	}
	o, err := im.download(ctx, f)
	if err != nil {
		return err
	}
	// A pack's side is its env; a mod's is too when the index gives one, else its provider's.
	side := ""
	if mrpack.IsPackZip(f.Path) || len(f.Env) > 0 {
		side = f.Side()
	}
	im.toIdentify(o, side)
	return nil
}

// toIdentify queues a mod jar or pack zip for the providers to identify, to be locked on side
// when one hosts it.
func (im *importer) toIdentify(o mrpack.Override, side string) {
	im.sides[o.Layer+"/"+o.Path] = side
	im.unmatched = append(im.unmatched, o)
}

// findLoadedDatapacks notes the zips the pack keeps in a global datapack mod's folder, which are
// the copies that load.
func (im *importer) findLoadedDatapacks(a *mrpack.Archive) {
	for _, f := range a.Index.Files {
		if mrpack.IsLoadedDatapackZip(f.Path) {
			im.loadedDatapacks[f.Hashes["sha512"]] = true
		}
	}
	for _, o := range a.Overrides {
		if mrpack.IsLoadedDatapackZip(o.Path) {
			sum := sha512.Sum512(o.Data)
			im.loadedDatapacks[hex.EncodeToString(sum[:])] = true
		}
	}
}

// duplicateDatapack reports, and records, a pack zip outside a global datapack mod's folder with
// the bytes of one inside it: a leftover copy the game never loads. A copy under resourcepacks/
// that carries assets/ is a hybrid's, which loads its assets, and is held for settleHybrids.
func (im *importer) duplicateDatapack(ctx context.Context, c hybridCopy, archivePath, filePath, sha512Sum string) (bool, error) {
	if mrpack.IsLoadedDatapackZip(filePath) || !im.loadedDatapacks[sha512Sum] {
		return false, nil
	}
	if path.Dir(filePath) == "resourcepacks" {
		hybrid, err := im.carriesAssets(ctx, c)
		if err != nil {
			return false, err
		}
		if hybrid {
			im.hybrids[sha512Sum] = append(im.hybrids[sha512Sum], c)
			return true, nil
		}
	}
	im.rep.Duplicates = append(im.rep.Duplicates, archivePath)
	return true, nil
}

func (im *importer) carriesAssets(ctx context.Context, c hybridCopy) (bool, error) {
	if c.override != nil {
		zr, err := zip.NewReader(bytes.NewReader(c.override.Data), int64(len(c.override.Data)))
		return err == nil && zipHasAssets(zr), nil
	}
	o, err := im.download(ctx, *c.file)
	if err != nil {
		return false, err
	}
	return im.carriesAssets(ctx, hybridCopy{override: &o})
}

// name is the copy's file name under resourcepacks/.
func (c hybridCopy) name() string {
	if c.file != nil {
		return path.Base(c.file.Path)
	}
	return path.Base(c.override.Path)
}

// settleHybrids places each held hybrid copy through the datapack its loaded copy locked as, so one
// entry keeps both on one version. A hybrid whose loaded copy stayed an override keeps this copy
// as one too.
func (im *importer) settleHybrids(ctx context.Context) error {
	for _, sum := range slices.Sorted(maps.Keys(im.hybrids)) {
		key, locked := "", false
		for k, p := range im.r.Lock.Datapacks {
			if p.Sha512 == sum {
				key, locked = k, true
				break
			}
		}
		for _, c := range im.hybrids[sum] {
			if !locked {
				if c.override != nil {
					im.unmanaged(*c.override)
				} else if err := im.unmanagedDownload(ctx, *c.file); err != nil {
					return err
				}
				continue
			}
			p := im.r.Lock.Datapacks[key]
			p.ResourcePack = true
			im.r.Lock.Datapacks[key] = p
			entry := im.r.Manifest.Requires[key]
			entry.ResourcePack = true
			im.r.Manifest.Requires[key] = entry
			if name := c.name(); name != p.Filename {
				im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s: its resource pack copy was resourcepacks/%s and is now placed as resourcepacks/%s; enable it again in game", key, name, p.Filename))
			}
		}
	}
	return nil
}

// identify asks each provider the manifest names, in its order, which of the queued files it
// hosts, each in one round of requests whatever the count. A provider that can't be asked leaves
// its files for the next, with a warning.
func (im *importer) identify(ctx context.Context) error {
	files := make(map[string][]byte, len(im.unmatched))
	for _, o := range im.unmatched {
		files[o.Layer+"/"+o.Path] = o.Data
	}
	for _, name := range im.r.Manifest.ProviderOrder() {
		if len(files) == 0 {
			return nil
		}
		p, err := im.r.Providers.Get(name)
		if err != nil {
			im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%d file(s) weren't looked up on %s (%s); kept as overrides", len(files), im.r.Providers.Title(name), out.AsError(err).Message))
			continue
		}
		im.r.log("looking up %d file(s) on %s", len(files), p.Title())
		found, err := p.Identify(ctx, files)
		if err != nil {
			return lookupFailed(p, fmt.Sprintf("%d file(s)", len(files)), err)
		}
		for key, h := range found {
			proj, v := h.Project, h.Version
			im.found[key] = hosted{p: p, proj: &proj, v: &v}
			delete(files, key)
		}
	}
	return nil
}

// lockIdentified locks each queued file its provider hosts, and keeps the rest as overrides: one
// no provider found, one whose key requires already holds, one its author doesn't let third
// parties download, and one the provider fails to serve.
func (im *importer) lockIdentified(ctx context.Context) error {
	queued := im.unmatched
	im.unmatched = nil
	for _, o := range queued {
		file := o.Layer + "/" + o.Path
		h, ok := im.found[file]
		if !ok || im.alreadyRequired(o, h.proj) {
			im.unmanaged(o)
			continue
		}
		if h.v.File.URL == "" {
			im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s is on %s, but its author doesn't allow third-party downloads; kept as an override", file, h.p.Title()))
			im.unmanaged(o)
			continue
		}
		if im.served[file] == h.v.File.URL {
			if _, err := im.r.Cache.Put(bytes.NewReader(o.Data)); err != nil {
				return err
			}
		}
		locked, err := im.lockOverride(ctx, h.p, o, im.sides[file], h.proj, h.v)
		if err != nil {
			return err
		}
		if !locked {
			im.unmanaged(o)
		}
	}
	return nil
}

// lockFile locks the mod jar or pack zip at filePath in layer as p's version v, reporting false,
// with nothing locked, when v isn't the kind its folder holds or p fails to serve it. The lock
// downloads it from p even when the pack ships the same bytes, since every later install will.
func (im *importer) lockFile(ctx context.Context, p provider.Provider, layer, filePath, side string, proj *provider.Project, v *provider.Version) (locked bool, failed string, err error) {
	kind, err := im.fileKind(ctx, filePath, proj, v)
	if err == nil && kind == "" {
		return false, "", nil
	}
	if err == nil && kind == manifest.TypeMod {
		err = im.lockMod(ctx, p, layer+"/"+filePath, side, proj, v)
	} else if err == nil {
		err = im.lockPack(ctx, p, path.Base(filePath), kind, side, proj, v)
	}
	if fault, ok := downloadFailure(err, p.Title()); ok {
		return false, fault.why, nil
	}
	return err == nil, "", err
}

// lockOverride locks an override the pack ships as lockFile does, keeping it as an override, with a
// warning, when p fails to serve it.
func (im *importer) lockOverride(ctx context.Context, p provider.Provider, o mrpack.Override, side string, proj *provider.Project, v *provider.Version) (bool, error) {
	locked, failed, err := im.lockFile(ctx, p, o.Layer, o.Path, side, proj, v)
	if failed != "" {
		im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s/%s: %s's download failed (%s); kept as an override, so try matching it again later", o.Layer, o.Path, p.Title(), failed))
	}
	return locked, err
}

// downloadFault is how a provider failed to serve a file: why, as a warning puts it, and help
// saying what to do about it.
type downloadFault struct {
	why  string
	help string
}

// downloadFailure says how err is host failing to serve a file, as a CDN cutting one short does,
// rather than a failure of shulker's own, such as the cache's disk.
func downloadFailure(err error, host string) (downloadFault, bool) {
	var status *fetch.StatusError
	switch {
	case err == nil, errors.Is(err, fetch.ErrOffline):
		return downloadFault{}, false
	case errors.Is(err, fetch.ErrNotFound):
		return downloadFault{"HTTP 404", fmt.Sprintf("%s no longer serves this file; `shulker update` locks another version", host)}, true
	case errors.As(err, &status):
		return downloadFault{fmt.Sprintf("HTTP %d", status.Status), fmt.Sprintf("%s answered HTTP %d; try again later", host, status.Status)}, true
	case errors.Is(err, io.ErrUnexpectedEOF):
		return downloadFault{"the file was cut short", fmt.Sprintf("%s's CDN served a partial file; try again later", host)}, true
	case out.CodeOf(err) == "checksum-mismatch":
		return downloadFault{"the file doesn't match its hash", fmt.Sprintf("%s served a different file than it lists; try again later", host)}, true
	case out.CodeOf(err) == "manual-download":
		return downloadFault{"it refused the download", fmt.Sprintf("%s won't serve this file; download it by hand", host)}, true
	case fetch.IsNetwork(err):
		return downloadFault{"the connection failed", fmt.Sprintf("the connection to %s failed; check your network and try again", host)}, true
	}
	return downloadFault{}, false
}

// fileKind is what the file at filePath locks as, empty when v isn't the kind its folder holds.
// A datapack version, which Modrinth files under a mod project, locks as a datapack in a datapack
// folder, and under resourcepacks/ as well unless it carries assets/, which make it load as a
// resource pack there.
func (im *importer) fileKind(ctx context.Context, filePath string, proj *provider.Project, v *provider.Version) (string, error) {
	kind := manifest.TypeMod
	switch {
	case path.Dir(filePath) == "resourcepacks":
		kind = manifest.TypeResourcePack
	case path.Dir(filePath) == "shaderpacks":
		kind = manifest.TypeShader
	case mrpack.IsDatapackZip(filePath):
		kind = manifest.TypeDatapack
	}
	datapack := proj.Type == manifest.TypeDatapack || slices.Contains(v.Loaders, provider.DatapackLoader)
	switch {
	case kind == manifest.TypeDatapack && datapack:
		return kind, nil
	case kind == manifest.TypeResourcePack && datapack && proj.Type != manifest.TypeResourcePack:
		got, err := im.r.obtain(ctx, proj, v)
		if err != nil {
			return "", err
		}
		if hasAssets(got.path) {
			return kind, nil
		}
		return manifest.TypeDatapack, nil
	case kind == manifest.TypeDatapack:
		return "", nil
	}
	if cmp.Or(proj.Type, manifest.TypeMod) != kind {
		return "", nil
	}
	return kind, nil
}

// hasAssets reports whether the zip at path carries assets/, which the game reads as a resource
// pack's.
func hasAssets(path string) bool {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer zr.Close()
	return zipHasAssets(&zr.Reader)
}

func zipHasAssets(zr *zip.Reader) bool {
	return slices.ContainsFunc(zr.File, func(f *zip.File) bool { return strings.HasPrefix(f.Name, "assets/") })
}

// lockMod locks the mod the pack ships at file on its provider's side. An override layer's side
// replaces it without a word. The index's env widens it to both, with a report, only where the
// env places the mod on a side the project builds and the provider's side doesn't: packwiz
// marks every mod as needed on both sides, so the env alone says little.
func (im *importer) lockMod(ctx context.Context, p provider.Provider, file, packSide string, proj *provider.Project, v *provider.Version) error {
	id, prior, err := im.r.place(ctx, p, proj, v, "", "", "", "", false)
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
		if entry.Pin != "" {
			im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s: the marker pinned %v but the pack ships %s; pin dropped", id, entry.Pin, v.Number))
			entry.Pin = ""
		}
	}
	providerSide := im.r.Lock.Mods[id].Side
	if _, fromEnv := im.indexSides[file]; fromEnv {
		if im.addsBuiltSide(packSide, providerSide) {
			entry.Side = "both"
			im.rep.Sides = append(im.rep.Sides, SideChoice{ID: id, Pack: entry.Side, Provider: providerSide})
		}
	} else if packSide != "" && (packSide != providerSide || entry.Side != "") {
		// A marker's own side stays explicit even where the provider agrees with it.
		entry.Side = packSide
	}
	if entry.Side != "" {
		locked := im.r.Lock.Mods[id]
		locked.Side = entry.Side
		im.r.Lock.Mods[id] = locked
	}
	im.r.setSource(&entry, id, p, proj)
	im.r.Manifest.Requires[id] = entry
	im.rep.locked(id, manifest.TypeMod, p.Name())
	return nil
}

func (im *importer) addsBuiltSide(packSide, providerSide string) bool {
	covers := func(side, built string) bool { return side == "both" || side == built }
	return slices.ContainsFunc(im.r.Manifest.Sides(), func(built string) bool {
		return covers(packSide, built) && !covers(providerSide, built)
	})
}

// lockPack locks a resource pack or shader the pack carries under the file name it ships, which
// the game enables it by, pinned to the version it ships.
func (im *importer) lockPack(ctx context.Context, p provider.Provider, filename, kind, side string, proj *provider.Project, v *provider.Version) error {
	r := im.r
	key := proj.Slug
	if !manifest.IsValidKey(key) {
		// Modrinth slugs may hold what a key can't, such as parentheses.
		key = nameKey(key)
	}
	if ok, err := im.canListPack(key, kind); !ok {
		return err
	}
	defer func() {
		if _, locked := r.Lock.Packs(kind)[key]; !locked {
			delete(r.Manifest.Requires, key)
		}
	}()
	listed := manifest.Require{Type: kind, Pin: v.ID}
	if filename != key+manifest.FileExtension(kind) {
		listed.Filename = filename
	}
	r.setSource(&listed, key, p, proj)
	if kind == manifest.TypeDatapack && (side == "client" || side == "server") {
		listed.Side = side
	}
	r.Manifest.Requires[key] = listed
	if err := r.lockPackVersion(ctx, p, proj, v, key, kind, ""); err != nil {
		return err
	}
	im.rep.locked(key, kind, p.Name())
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

// lookupFailed is a file in the pack that couldn't be looked up on p, with its error in a row. A
// network failure stays one for fetch.IsNetwork.
func lookupFailed(p provider.Provider, file string, err error) error {
	e := out.Errorf("mrpack-lookup", "couldn't look up %s on %s", file, p.Title()).WithCause(p.Name(), err)
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

// download fetches an index file from the first of its URLs that serves it, into memory rather
// than the cache: a file identified on a provider is fetched from the provider when it is locked,
// which proves the provider serves it, unless the URL that served it was the provider's own.
func (im *importer) download(ctx context.Context, f mrpack.File) (mrpack.Override, error) {
	im.r.log("fetching %s", f.Path)
	o := mrpack.Override{Layer: f.Layer(), Path: f.Path}
	want := f.Hashes["sha512"]
	if im.r.Cache.Has(want) {
		data, err := os.ReadFile(im.r.Cache.Object(want))
		o.Data = data
		return o, err
	}
	var err error
	for _, url := range f.Downloads {
		var buf bytes.Buffer
		var got string
		if got, err = im.r.Fetch.Download(ctx, url, &buf); err != nil {
			continue
		}
		if got != want {
			err = out.Errorf("checksum-mismatch", "the download from %s doesn't match its sha512", url)
			continue
		}
		o.Data = buf.Bytes()
		im.served[o.Layer+"/"+o.Path] = url
		return o, nil
	}
	e := out.Errorf("mrpack-download", "couldn't download %s", f.Path).WithCause("download", err)
	e.Help = "every URL the pack lists for it failed"
	if len(f.Downloads) == 1 {
		e.Help = "the pack lists no other URL for it"
	}
	return mrpack.Override{}, e
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

func (im *importer) override(ctx context.Context, o mrpack.Override) error {
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
	if dup, err := im.duplicateDatapack(ctx, hybridCopy{override: &o}, o.Layer+"/"+o.Path, o.Path, digest); err != nil || dup {
		return err
	}
	side := layerSide(o.Layer)
	if side == "both" {
		side = ""
	}
	im.toIdentify(o, side)
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
	im.r.Lock.Packs(p.kind)[p.key] = im.a.Marker.Lock.Packs(p.kind)[p.key]
	if entry, ok := im.a.Marker.Manifest.Packs(p.kind)[p.key]; ok {
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
	for _, kind := range manifest.PackKinds {
		dropPacks(im.a.Marker.Lock.Packs(kind), im.r.Lock.Packs(kind))
	}
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
	for _, packs := range r.Lock.PackSections() {
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
		e = e.WithCause(cfpack.Provider, cause)
	}
	return fetch.Unreachable(e)
}
