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

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
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
	Sides []SideChoice `json:"sides"`
	// ServerPack is what the server files the pack pairs with decided, when it pairs some.
	ServerPack *ServerPack `json:"serverPack,omitempty"`
	// Seeded are the files moved out of a seed mod's folder to be seeded, by that folder.
	Seeded    map[string][]string    `json:"seeded,omitempty"`
	Warnings  []string               `json:"-"`
	Overrides []packarchive.Override `json:"-"`
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
	rep.Locked = slices.DeleteFunc(rep.Locked, func(f LockedFile) bool { return f.ID == id && f.Type == kind })
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
	a         *packarchive.Archive
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
	unmatched []packarchive.Override
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
	file     *packarchive.File
	override *packarchive.Override
}

// importedPack is a pack the pack's own lock names, found by
// the digest of the file the archive ships.
type importedPack struct {
	key  string
	kind string
}

func newImporter(r *Resolver, a *packarchive.Archive, reuseLocal bool) *importer {
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

// Import locks the files a pack archive lists and carries, whatever its format, reporting what it
// locked, reused, dropped and left unmanaged. A file the exporting shulker project locked, matched
// by sha512, comes back as that project locked it.
// ImportProject reads arc as the project it makes at r.Dir, into r.Manifest and r.Lock: the
// manifest arc gives under name, or its own when name is empty, a lock from the exact platform the
// pack names, where a marker's manifest may hold a range, the marker's players when it keeps some,
// the files Import locks, its local files adopted, and, after a marker, the overrides the manifest
// renders itself dropped.
// The manifest's and the lock's warnings go to r.Warnings; the import's own come back in Imported.
// ImportOptions shape ImportProject. Name is the project's, the pack's slugified when empty.
// IgnoreMarker imports a shulker export as any other pack, and ServerPack reads the server files
// the pack pairs with for its mods' sides.
type ImportOptions struct {
	Name         string
	IgnoreMarker bool
	ServerPack   bool
}

func (r *Resolver) ImportProject(ctx context.Context, arc *packarchive.Archive, opts ImportOptions) (*Imported, error) {
	if opts.IgnoreMarker {
		arc.Marker = nil
	}
	name := opts.Name
	if name == "" && arc.Marker == nil {
		name = project.Slugify(arc.Name)
	}
	m, warnings := arc.Manifest(name)
	r.Warnings = append(r.Warnings, warnings...)
	exact := *m
	exact.Minecraft, exact.Loader = arc.Minecraft, manifest.Loader{Type: arc.Loader.Type, Version: arc.Loader.Version}
	r.log("%s", ResolvingLine(exact.Minecraft, exact.Loader))
	l, warning, err := r.Meta.NewLock(ctx, &exact)
	if err != nil {
		return nil, err
	}
	if warning != "" {
		r.Warnings = append(r.Warnings, warning)
	}
	if marker := arc.Marker; marker != nil && marker.Manifest.Server != nil && marker.Manifest.Server.Players != nil {
		l.Players = marker.Lock.Players
	}
	r.Manifest, r.Lock = m, l
	mods, err := r.Import(ctx, arc)
	if err != nil {
		return nil, err
	}
	if err := r.AdoptLocalFiles(); err != nil {
		return nil, err
	}
	mods.Overrides = seedFromSeedMods(m, r.Lock, mods.Overrides, mods)
	if arc.Marker != nil {
		mods.Overrides = build.DropManifestOwned(m, mods.Overrides)
	} else {
		var warnings []string
		mods.Overrides, warnings = build.AdoptPackChoices(m, r.Lock, mods.Overrides)
		mods.Warnings = append(mods.Warnings, warnings...)
	}
	if opts.ServerPack && arc.Path != "" {
		mods.ServerPack, err = r.serverPackOf(ctx, arc)
		if err != nil {
			mods.Warnings = append(mods.Warnings, fmt.Sprintf("the pack's server files weren't read (%v); each mod's side comes from its own metadata", err))
		}
	}
	mods.Warnings = append(mods.Warnings, dependencySideNotes(r.Lock.Mods, slices.Sorted(maps.Keys(r.Lock.Mods)))...)
	return mods, nil
}

func (r *Resolver) Import(ctx context.Context, a *packarchive.Archive) (*Imported, error) {
	return r.importArchive(ctx, a, true)
}

// importArchive is Import. Without reuseLocal, a local file the pack's own lock names is laid as
// an override rather than reused: its path is the exporter's, and outside the archive only the
// cache holds its bytes.
func (r *Resolver) importArchive(ctx context.Context, a *packarchive.Archive, reuseLocal bool) (*Imported, error) {
	r.keepNewest = true
	defer func() { r.keepNewest = false }()
	im := newImporter(r, a, reuseLocal)
	im.keepSides = !a.Format.Sided()
	im.findLoadedDatapacks(a)
	if err := im.listedByID(ctx); err != nil {
		return nil, err
	}
	for _, f := range a.Files {
		if f.Provider != "" {
			continue
		}
		if err := im.listedByDownload(ctx, f); err != nil {
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

// listedByDownload takes a file the archive lists by hash and download URL.
func (im *importer) listedByDownload(ctx context.Context, f packarchive.File) error {
	sha512Sum := f.Hashes["sha512"]
	if id, ok := im.bySha[sha512Sum]; ok {
		im.reuse(id, f.Side)
		return nil
	}
	if p, ok := im.packBySha[sha512Sum]; ok {
		im.reusePack(p)
		return nil
	}
	if !packarchive.IsModJar(f.Path) && !packarchive.IsPackZip(f.Path) {
		return im.unmanagedDownload(ctx, f)
	}
	if dup, err := im.duplicateDatapack(ctx, hybridCopy{file: &f}, f.Path, f.Path, sha512Sum); err != nil || dup {
		return err
	}
	if f.Side != "" {
		im.indexSides[packarchive.LayerFor(f.Side)+"/"+f.Path] = f.Side
	}
	o, err := im.download(ctx, f)
	if err != nil {
		return err
	}
	im.toIdentify(o, f.Side)
	return nil
}

// toIdentify queues a mod jar or pack zip for the providers to identify, to be locked on side
// when one hosts it.
func (im *importer) toIdentify(o packarchive.Override, side string) {
	im.sides[o.Layer+"/"+o.Path] = side
	im.unmatched = append(im.unmatched, o)
}

// findLoadedDatapacks notes the zips the pack keeps in a global datapack mod's folder, which are
// the copies that load.
func (im *importer) findLoadedDatapacks(a *packarchive.Archive) {
	for _, f := range a.Files {
		if packarchive.IsLoadedDatapackZip(f.Path) {
			im.loadedDatapacks[f.Hashes["sha512"]] = true
		}
	}
	for _, o := range a.Overrides {
		if packarchive.IsLoadedDatapackZip(o.Path) {
			sum := sha512.Sum512(o.Data)
			im.loadedDatapacks[hex.EncodeToString(sum[:])] = true
		}
	}
}

// duplicateDatapack reports, and records, a pack zip outside a global datapack mod's folder with
// the bytes of one inside it: a leftover copy the game never loads. A copy under resourcepacks/
// that carries assets/ is a hybrid's, which loads its assets, and is held for settleHybrids.
func (im *importer) duplicateDatapack(ctx context.Context, c hybridCopy, archivePath, filePath, sha512Sum string) (bool, error) {
	if packarchive.IsLoadedDatapackZip(filePath) || !im.loadedDatapacks[sha512Sum] {
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
func (im *importer) lockOverride(ctx context.Context, p provider.Provider, o packarchive.Override, side string, proj *provider.Project, v *provider.Version) (bool, error) {
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
	case packarchive.IsDatapackZip(filePath):
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
	channel := shippedChannel(v)
	id, prior, err := im.r.place(ctx, p, proj, v, "", "", "", channel, false)
	if err != nil {
		return err
	}
	if prior != nil && !im.duplicate(id, prior, proj.ID, v.File.Filename) {
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
	if !provider.ChannelAllows(entry.Channel, v.Channel) {
		entry.Channel = v.Channel
	}
	locked := im.r.Lock.Mods[id]
	locked.Channel = channelLabel(entry.Channel)
	im.r.Lock.Mods[id] = locked
	providerSide := locked.Side
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
		locked.Side, locked.SideFrom = entry.Side, sideFromRequires
		im.r.Lock.Mods[id] = locked
	}
	im.r.setSource(&entry, id, p, proj)
	im.r.Manifest.Requires[id] = entry
	im.rep.locked(id, manifest.TypeMod, p.Name())
	return nil
}

// duplicate warns of a mod id the pack carries twice, and reports whether the file just placed
// replaced the one locked before it.
func (im *importer) duplicate(id string, prior *lock.Mod, project, filename string) bool {
	kept := im.r.Lock.Mods[id]
	switch {
	case prior.Project == project && prior.Filename == filename:
		im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s appears twice in the pack; kept %s", id, kept.Filename))
	case prior.Project == project:
		im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s appears twice in the pack (%s, %s); kept %s", id, prior.Filename, filename, kept.Filename))
	default:
		im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s appears twice in the pack (%s, %s); kept %s, the newest", id, prior.Filename, filename, kept.Filename))
	}
	return kept.Project != prior.Project
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
	listed := manifest.Require{Type: kind, Pin: v.ID, Channel: shippedChannel(v)}
	if filename != key+manifest.FileExtension(kind) {
		listed.Filename = filename
	}
	r.setSource(&listed, key, p, proj)
	if kind == manifest.TypeDatapack && (side == "client" || side == "server") {
		listed.Side = side
	}
	r.Manifest.Requires[key] = listed
	if err := r.lockPackVersion(ctx, p, proj, v, key, kind, listed.Channel); err != nil {
		return err
	}
	im.rep.locked(key, kind, p.Name())
	return nil
}

// shippedChannel is the channel an entry needs for the file a pack ships to stay locked: empty for a
// release, else the file's own.
func shippedChannel(v *provider.Version) string {
	if provider.ChannelAllows("", v.Channel) {
		return ""
	}
	return v.Channel
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
	e := out.Errorf("modpack-lookup", "couldn't look up %s on %s", file, p.Title()).WithCause(p.Name(), err)
	if !fetch.IsNetwork(err) {
		return e
	}
	e.Help = "looking a pack's files up needs the network"
	return fetch.Unreachable(e)
}

func (im *importer) unmanagedDownload(ctx context.Context, f packarchive.File) error {
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
func (im *importer) download(ctx context.Context, f packarchive.File) (packarchive.Override, error) {
	im.r.log("fetching %s", f.Path)
	o := packarchive.Override{Layer: packarchive.LayerFor(f.Side), Path: f.Path}
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
	e := out.Errorf("modpack-download", "couldn't download %s", f.Path).WithCause("download", err)
	e.Help = "every URL the pack lists for it failed"
	if len(f.Downloads) == 1 {
		e.Help = "the pack lists no other URL for it"
	}
	return packarchive.Override{}, e
}

func (im *importer) unmanaged(o packarchive.Override) {
	im.rep.Overrides = append(im.rep.Overrides, o)
	im.rep.Unmanaged = append(im.rep.Unmanaged, o.Layer+"/"+o.Path)
}

func (im *importer) keepUnmanaged(files []packarchive.Override) {
	for _, o := range files {
		im.unmanaged(o)
	}
}

func (im *importer) override(ctx context.Context, o packarchive.Override) error {
	if !packarchive.IsModJar(o.Path) && !packarchive.IsPackZip(o.Path) {
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
		side := packarchive.LayerSide(o.Layer)
		if im.keepSides {
			side = ""
		}
		im.reuse(id, side)
		return nil
	}
	if dup, err := im.duplicateDatapack(ctx, hybridCopy{override: &o}, o.Layer+"/"+o.Path, o.Path, digest); err != nil || dup {
		return err
	}
	side := packarchive.LayerSide(o.Layer)
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
// way Import locks one into a new project, and records in its pin the files the archive lays
// itself. r's own lock is left alone; only its providers, provider order, cache and fetch client
// are used.
func (r *Resolver) ConsumeArchive(ctx context.Context, l *modpack.Loaded) error {
	a := l.Archive
	typ, version := a.Loader.Type, a.Loader.Version
	m := &manifest.Manifest{Name: l.Name, Minecraft: a.Minecraft, Loader: manifest.Loader{Type: typ, Version: version}, Requires: map[string]manifest.Require{}, Providers: r.Manifest.Providers}
	pl := lock.New()
	pl.Minecraft, pl.Loader = a.Minecraft, lock.Loader{Type: typ, Version: version}
	scratch := &Resolver{Dir: r.Dir, Manifest: m, Lock: pl, Providers: r.Providers, Cache: r.Cache, Fetch: r.Fetch, Log: r.Log}
	byID := a.Format.Provider() != ""
	if byID && r.Fetch != nil && r.Fetch.Offline {
		return archiveOffline(l.Name, a.Format, nil)
	}
	rep, err := scratch.importArchive(ctx, a, false)
	if byID && fetch.IsNetwork(err) {
		return archiveOffline(l.Name, a.Format, err)
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

// archiveOffline is a modpack archive that lists its files by provider id read without the
// network. An id carries no hash to find the file in the cache by, so even a warm cache can't
// stand in.
func archiveOffline(name string, f packarchive.Format, cause error) error {
	e := out.Errorf("modpack-offline", "modpack %s: a %s modpack can't be read offline, even with every file it names in the cache", name, f.Title())
	e.Help = fmt.Sprintf("it names its files by %s id, which only the %s API resolves; run the command again online", f.Title(), f.Title())
	if cause != nil {
		e = e.WithCause(f.Provider(), cause)
	}
	return fetch.Unreachable(e)
}
