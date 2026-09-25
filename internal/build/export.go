package build

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/integrations"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/provider"
)

type ExportOptions struct {
	Format   packarchive.Format
	Sides    []string
	Version  string
	Output   string
	Bundle   bool
	OS       string
	Features map[string]bool
}

type ExportReport struct {
	Path          string   `json:"path"`
	Version       string   `json:"version"`
	Name          string   `json:"name"`
	Sides         []string `json:"sides"`
	Mods          []string `json:"mods"`
	ResourcePacks []string `json:"resourcepacks"`
	Shaders       []string `json:"shaders"`
	Datapacks     []string `json:"datapacks"`
	// Matched are the files the format's provider was found to host under other ids than the
	// lock's, and listed by those.
	Matched              []string `json:"matched"`
	BundledMods          []string `json:"bundledMods"`
	BundledResourcePacks []string `json:"bundledResourcepacks"`
	BundledShaders       []string `json:"bundledShaders"`
	BundledDatapacks     []string `json:"bundledDatapacks"`
	Overrides            []string `json:"overrides"`
	Warnings             []string `json:"-"`
}

type exportSide struct {
	side  string
	files map[string][]byte
	// layers is the override folder each bundled file goes in, by path: its entry's side decides,
	// not which sides the export happens to have.
	layers map[string]string
	mods   map[string]bool
	packs  map[string]bool
	// datapacks is where the side places each datapack, by key.
	datapacks map[string]string
	// seeded are the paths of the side's seeded files, and seedMod the first seed mod it places.
	seeded  []string
	seedMod *integrations.SeedMod
}

// exportEntry is one file the export ships, whatever kind it is, so mods and packs go through
// the same listing and bundling.
type exportEntry struct {
	key  string
	kind string
	// installsAs is the kind a launcher installs the file as, which decides where it may be
	// placed: a hybrid datapack's resource pack copy is a datapack to the launcher.
	installsAs string
	path       string
	side       string
	provider   string
	project    string
	version    string
	sha512     string
	url        *string
	// providerFilename is the provider's own name for the file, which a lookalike on another
	// provider would share.
	providerFilename string
	aliases          map[string]string
	loaders          []string
	owners           []*exportSide
}

// ExportFileName is the file an export of the project's version gets by default.
func ExportFileName(m *manifest.Manifest, version string, f packarchive.Format) string {
	return m.Name + "-" + version + f.Extension()
}

// ExportPath is where an export lands when --output names nothing: under build/ in the project at
// dir, or under cwd for a remote source, whose project is not the user's to write in, named by
// ExportFileName.
func ExportPath(dir string, m *manifest.Manifest, version string, f packarchive.Format, remote bool, cwd string) string {
	into := filepath.Join(dir, "build")
	if remote {
		into = cwd
	}
	return filepath.Join(into, ExportFileName(m, version, f))
}

// Export writes the project as a pack archive in the format opts names.
func (b *Builder) Export(ctx context.Context, opts ExportOptions) (*ExportReport, error) {
	f := opts.Format
	sides, err := b.exportSides(opts.Sides)
	if err != nil {
		return nil, err
	}
	report := &ExportReport{Path: opts.Output, Version: opts.Version, Name: b.exportName(sides), Sides: []string{}, Mods: []string{}, ResourcePacks: []string{}, Shaders: []string{}, Datapacks: []string{}, Matched: []string{}, BundledMods: []string{}, BundledResourcePacks: []string{}, BundledShaders: []string{}, BundledDatapacks: []string{}, Overrides: []string{}, Warnings: []string{}}
	for _, t := range sides {
		report.Sides = append(report.Sides, t.side)
		warnings, err := b.exportCollect(t, opts.Version, opts.OS, opts.Features)
		if err != nil {
			return nil, err
		}
		report.Warnings = append(report.Warnings, warnings...)
	}
	files, bundled, err := b.exportFiles(ctx, f, sides, opts.Bundle, report)
	if err != nil {
		return nil, err
	}
	if f.Renames() {
		for _, t := range sides {
			enableByListedNames(t, files, b.Manifest.OptionsPath(), b.listForm())
		}
	}
	if plain := relocateSeeded(sides); len(plain) > 0 {
		var keys []string
		for _, m := range integrations.SeedMods {
			keys = append(keys, m.Key)
		}
		report.Warnings = append(report.Warnings, fmt.Sprintf("seeded files ship as plain overrides, which launchers write over the player's copy on each update: %s; add %s to keep them seeded", strings.Join(plain, ", "), strings.Join(keys[:len(keys)-1], ", ")+" or "+keys[len(keys)-1]))
	}
	overrides := splitOverrides(sides)
	for _, o := range overrides {
		if !bundled[o.Layer+"/"+o.Path] {
			report.Overrides = append(report.Overrides, o.Layer+"/"+o.Path)
		}
	}
	x := &packarchive.Export{
		Name:      report.Name,
		Version:   opts.Version,
		Summary:   exportSummary(b.Manifest),
		Authors:   b.Manifest.Authors,
		Minecraft: b.Lock.Minecraft,
		Loader:    packarchive.Loader{Type: b.Lock.Loader.Type, Version: b.Lock.Loader.Version},
		Files:     files,
		Overrides: overrides,
	}
	if x.Icon, x.IconName, err = b.exportIcon(); err != nil {
		return nil, err
	}
	if x.Manifest, x.Lock, err = b.identity(); err != nil {
		return nil, err
	}
	if err := packarchive.Write(f, opts.Output, x); err != nil {
		return nil, err
	}
	return report, nil
}

// identity is the project's own manifest and lock, for the archive root, so an export imports
// back as the project it came from. The marker jar carries them too, but only a client-side
// export of a project with a loader has one. A manifest that turns the marker off leaves both out.
func (b *Builder) identity() ([]byte, []byte, error) {
	if !b.Manifest.UsesMarker() {
		return nil, nil, nil
	}
	manifestData, err := os.ReadFile(filepath.Join(b.Dir, manifest.FileName))
	if err != nil {
		return nil, nil, err
	}
	lockData, err := os.ReadFile(b.LockPath)
	if err != nil {
		return nil, nil, err
	}
	return manifestData, lockData, nil
}

func (b *Builder) exportSides(names []string) ([]*exportSide, error) {
	if len(names) == 0 {
		names = b.Manifest.Sides()
	}
	var sides []*exportSide
	seen := map[string]bool{}
	for _, name := range names {
		if !manifest.IsSide(name) {
			return nil, manifest.NotASide(name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		sides = append(sides, &exportSide{side: name, files: map[string][]byte{}, layers: map[string]string{}})
	}
	return sides, nil
}

func (b *Builder) exportName(sides []*exportSide) string {
	pick := sides[0].side
	for _, t := range sides {
		if t.side == "client" {
			pick = t.side
		}
	}
	return b.Manifest.DisplayName(pick)
}

// exportCollect fills a side's override files and the mods it ships, returning the warnings.
func (b *Builder) exportCollect(t *exportSide, version, osName string, features map[string]bool) ([]string, error) {
	rep := &Report{}
	desired, dirs, err := b.collect(t.side, Options{OS: osName, NoOS: osName == "", Features: features, NoLauncher: true, NoEULA: true, ProjectVersion: version}, rep)
	if err != nil {
		return nil, err
	}
	warnings := append([]string{}, rep.Warnings...)
	t.mods = map[string]bool{}
	placed := map[string]bool{}
	for id, m := range b.Lock.Mods {
		if _, ok := desired["mods/"+m.Filename]; ok {
			t.mods[id] = true
			placed[b.Lock.JarID(id)] = true
		}
	}
	if m, ok := integrations.FirstSeedMod(integrations.Match(placed, b.Manifest.Integrations)); ok {
		t.seedMod = &m
	}
	t.packs = map[string]bool{}
	for _, ref := range b.packRefs() {
		if _, ok := desired[ref.path]; ok {
			t.packs[ref.key] = true
		}
	}
	t.datapacks = map[string]string{}
	for key, p := range b.Lock.Datapacks {
		if rel := b.Lock.PackPath(manifest.TypeDatapack, p, t.side, dirs[0], b.Manifest.Integrations); desired[rel].isCached() {
			t.datapacks[key] = rel
		}
	}
	for _, e := range rep.Excluded {
		if strings.Contains(e, "(needs os ") {
			warnings = append(warnings, fmt.Sprintf("%s: left out of %s; pass --os to export that variation", e, t.side))
		}
	}
	for path, s := range desired {
		if s.isCached() {
			continue
		}
		if s.seeded {
			t.seeded = append(t.seeded, path)
		}
		if f := s.owned(); f != nil {
			data, err := f.render(nil, nil, nil)
			if err != nil {
				return nil, err
			}
			t.files[path] = data
			continue
		}
		t.files[path] = s.data()
	}
	return warnings, nil
}

// exportEntries are the files the sides ship, in listing order: mods by id, then resource packs
// and shaders, then datapacks, each folder a side places one in a file of its own.
func (b *Builder) exportEntries(sides []*exportSide) []exportEntry {
	owners := func(ships func(*exportSide) bool) []*exportSide {
		var owners []*exportSide
		for _, t := range sides {
			if ships(t) {
				owners = append(owners, t)
			}
		}
		return owners
	}
	var entries []exportEntry
	for _, id := range slices.Sorted(maps.Keys(b.Lock.Mods)) {
		m := b.Lock.Mods[id]
		ships := owners(func(t *exportSide) bool { return t.mods[id] })
		if len(ships) == 0 {
			continue
		}
		entries = append(entries, exportEntry{key: id, kind: manifest.TypeMod, installsAs: manifest.TypeMod, path: "mods/" + m.Filename, side: m.Side, provider: m.Provider, project: m.Project, version: m.Version, sha512: m.Sha512, url: m.URL, providerFilename: m.Filename, aliases: m.Aliases, loaders: []string{b.Lock.Loader.Type}, owners: ships})
	}
	for _, ref := range b.packRefs() {
		ships := owners(func(t *exportSide) bool { return t.packs[ref.key] })
		if len(ships) == 0 {
			continue
		}
		p := ref.pack
		installsAs := ref.kind
		if ref.hybrid {
			installsAs = manifest.TypeDatapack
		}
		entries = append(entries, exportEntry{key: ref.key, kind: ref.kind, installsAs: installsAs, path: ref.path, side: "client", provider: p.Provider, project: p.Project, version: p.Version, sha512: p.Sha512, url: p.URL, providerFilename: p.ProviderFilename, loaders: p.Loaders, owners: ships})
	}
	for _, key := range sortedPacks(b.Lock.Datapacks) {
		p := b.Lock.Datapacks[key]
		bySide := map[string][]*exportSide{}
		for _, t := range sides {
			if rel, ok := t.datapacks[key]; ok {
				bySide[rel] = append(bySide[rel], t)
			}
		}
		for _, rel := range slices.Sorted(maps.Keys(bySide)) {
			ships := bySide[rel]
			side := "both"
			if len(ships) == 1 {
				side = ships[0].side
			}
			entries = append(entries, exportEntry{key: key, kind: manifest.TypeDatapack, installsAs: manifest.TypeDatapack, path: rel, side: side, provider: p.Provider, project: p.Project, version: p.Version, sha512: p.Sha512, url: p.URL, providerFilename: p.ProviderFilename, owners: ships})
		}
	}
	return entries
}

// exportFiles lists what the format can carry by listing and bundles the rest into the sides'
// override files, or refuses when bundling wasn't asked for. It returns the listing and the
// override paths the bundled files took, as layer/path.
func (b *Builder) exportFiles(ctx context.Context, f packarchive.Format, sides []*exportSide, bundle bool, report *ExportReport) ([]packarchive.File, map[string]bool, error) {
	entries := b.exportEntries(sides)
	bundled := map[string]bool{}
	bundleInto := func(e exportEntry, data []byte, why string) {
		layer := packarchive.LayerFor("both")
		if f.Sided() {
			layer = packarchive.LayerFor(e.side)
		}
		for _, t := range e.owners {
			t.files[e.path] = data
			t.layers[e.path] = layer
			bundled[layer+"/"+e.path] = true
		}
		list := reportList(report, e.kind, true)
		if !slices.Contains(*list, e.key) {
			*list = append(*list, e.key)
		}
		report.Warnings = append(report.Warnings, why)
	}
	command := "shulker export " + f.Name() + " --bundle"
	unplaced := &kindTally{}
	// candidates are the files to list, by entry; nil where the entry was bundled unplaced.
	candidates := make([]*packarchive.File, len(entries))
	blobs := map[string][]byte{}
	byKey := map[string]exportEntry{}
	var lookup []string
	for i, e := range entries {
		data, err := os.ReadFile(b.Cache.Object(e.sha512))
		if err != nil {
			return nil, nil, notInstalled(e.key)
		}
		if !f.Places(e.installsAs, e.path) {
			if !bundle {
				unplaced.add(e.installsAs, e.key+" ("+path.Dir(e.path)+"/)")
				continue
			}
			bundleInto(e, data, fmt.Sprintf("bundled %s into the archive at %s, since a %s launcher wouldn't place it there", e.key, e.path, f.Title()))
			continue
		}
		sum := sha1.Sum(data)
		c := &packarchive.File{Path: e.path, Hashes: map[string]string{"sha1": hex.EncodeToString(sum[:]), "sha512": e.sha512}, Side: e.side, Size: int64(len(data)), Provider: e.provider, Project: e.project, Version: e.version, Filename: e.providerFilename, Title: e.key}
		if e.url != nil {
			c.Downloads = []string{*e.url}
		}
		if _, seen := byKey[e.key]; !seen {
			byKey[e.key] = e
			if f.Provider() != "" && e.provider != f.Provider() {
				lookup = append(lookup, e.key)
			}
		}
		candidates[i] = c
		blobs[e.key] = data
	}
	if unplaced.total() > 0 {
		return nil, nil, bundleNudge(f.CantPlace(kindCount(unplaced.counts)), unplaced.items, command)
	}
	matched, err := b.identifyOn(ctx, f, lookup, blobs, byKey, bundle, report)
	if err != nil {
		return nil, nil, err
	}
	missing := &kindTally{}
	files := []packarchive.File{}
	for i, e := range entries {
		c := candidates[i]
		if c == nil {
			continue
		}
		if v, ok := matched[e.key]; ok {
			c.Provider, c.Project, c.Version, c.Filename = f.Provider(), v.ProjectID, v.ID, v.File.Filename
		}
		if f.Lists(*c) {
			files = append(files, *c)
			list := reportList(report, e.kind, false)
			if !slices.Contains(*list, e.key) {
				*list = append(*list, e.key)
			}
			continue
		}
		from := origin(b.Providers, e.provider, e.url)
		if !bundle {
			missing.add(e.kind, e.key+" ("+from+")")
			continue
		}
		bundleInto(e, blobs[e.key], fmt.Sprintf("bundled %s from %s into the archive; %s", e.key, from, f.Usage().Bundled))
	}
	if missing.total() > 0 {
		return nil, nil, bundleNudge(f.NotListed(kindCount(missing.counts), missing.total()), missing.items, command)
	}
	for key := range matched {
		report.Matched = append(report.Matched, key)
	}
	sort.Strings(report.Matched)
	if f.Provider() != "" {
		b.describeListed(ctx, f, files, report)
	}
	return files, bundled, nil
}

// identifyOn finds, on the format's provider, the files the lock has from elsewhere: by the
// provider's own fingerprint in one request, then a lookalike for each miss. A lookup that fails
// bundles them all with a warning when bundling is on, and fails the export otherwise.
func (b *Builder) identifyOn(ctx context.Context, f packarchive.Format, lookup []string, blobs map[string][]byte, byKey map[string]exportEntry, bundle bool, report *ExportReport) (map[string]provider.Version, error) {
	matched := map[string]provider.Version{}
	if len(lookup) == 0 {
		return matched, nil
	}
	p, err := b.Providers.Get(f.Provider())
	var found map[string]provider.Hosted
	if err == nil {
		files := make(map[string][]byte, len(lookup))
		for _, key := range lookup {
			files[key] = blobs[key]
		}
		b.log("looking up %s on %s", countOf(len(files), "file", "files"), p.Title())
		found, err = p.Identify(ctx, files)
	}
	switch {
	case err == nil:
	case bundle:
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s lookup failed, so these are bundled: %s", f.Title(), out.AsError(err).Message))
		return matched, nil
	default:
		e := out.AsError(err)
		e.Items = lookup
		return nil, e
	}
	for _, key := range lookup {
		if h, ok := found[key]; ok {
			matched[key] = h.Version
			continue
		}
		v, ok, err := b.lookalike(ctx, p, byKey[key], blobs[key])
		if err != nil {
			if !bundle {
				fail := out.AsError(err)
				fail.Items = []string{key}
				return nil, fail
			}
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s lookup for %s failed, so it is bundled: %s", f.Title(), key, out.AsError(err).Message))
			continue
		}
		if ok {
			matched[key] = v
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s matches %s file %s by contents, but its bytes differ from the locked file", key, f.Title(), v.ID))
		}
	}
	return matched, nil
}

// lookalike finds the project e came from on p, through the lock's alias or the slug of the
// locked provider's project, and downloads p's file with e's name and size. It accepts the file
// only when every zip entry unpacks to the same bytes, since the same build uploaded twice
// differs in entry timestamps.
func (b *Builder) lookalike(ctx context.Context, p provider.Provider, e exportEntry, locked []byte) (provider.Version, bool, error) {
	none := provider.Version{}
	if e.providerFilename == "" {
		return none, false, nil
	}
	projectID := e.aliases[p.Name()]
	if projectID == "" {
		from, err := b.Providers.Get(e.provider)
		if err != nil {
			return none, false, nil
		}
		b.log("looking up %s on %s by slug", e.key, p.Title())
		proj, err := from.Project(ctx, e.project, e.kind)
		if errors.Is(err, provider.ErrNotFound) {
			return none, false, nil
		}
		if err != nil {
			return none, false, err
		}
		found, err := p.Project(ctx, proj.Slug, e.kind)
		if errors.Is(err, provider.ErrNotFound) {
			return none, false, nil
		}
		if err != nil {
			return none, false, err
		}
		projectID = found.ID
	}
	versions, err := p.Versions(ctx, projectID, b.Lock.Minecraft, e.loaders)
	if errors.Is(err, provider.ErrNotFound) || errors.Is(err, fetch.ErrNotFound) {
		return none, false, nil
	}
	if err != nil {
		return none, false, err
	}
	for _, v := range versions {
		if v.File.Filename != e.providerFilename || v.File.Size != int64(len(locked)) || v.File.URL == "" {
			continue
		}
		b.log("comparing %s with %s file %s", e.key, p.Title(), v.ID)
		var buf bytes.Buffer
		_, err := b.Fetch.Download(ctx, v.File.URL, &buf)
		if errors.Is(err, fetch.ErrNotFound) || errors.Is(err, fetch.ErrForbidden) {
			return none, false, nil
		}
		if err != nil {
			return none, false, err
		}
		return v, sameZipContents(locked, buf.Bytes()), nil
	}
	return none, false, nil
}

// describeListed names each listed file's project for a listing that shows them: its page from
// the provider, and its title and author from one lookup of every project, which failing leaves
// the ids in place with a warning, since the listing is cosmetic.
func (b *Builder) describeListed(ctx context.Context, f packarchive.Format, files []packarchive.File, report *ExportReport) {
	if len(files) == 0 {
		return
	}
	ids := make([]string, len(files))
	for i := range files {
		ids[i] = files[i].Project
		if p, ok := b.Providers[f.Provider()]; ok {
			files[i].Page = p.ProjectPage("", files[i].Project)
		}
	}
	p, err := b.Providers.Get(f.Provider())
	var projects map[string]provider.Project
	if err == nil {
		b.log("looking up %s for the pack's listing", countOf(len(ids), f.Title()+" project", f.Title()+" projects"))
		projects, err = p.Projects(ctx, ids)
	}
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s project lookup failed, so the pack's listing names project IDs: %s", f.Title(), out.AsError(err).Message))
		return
	}
	for i := range files {
		if rec, ok := projects[files[i].Project]; ok && rec.Page != "" && rec.Title != "" {
			files[i].Title, files[i].Author, files[i].Page = rec.Title, rec.Author, rec.Page
		}
	}
}

func sameZipContents(a, b []byte) bool {
	left, err := zipContents(a)
	if err != nil {
		return false
	}
	right, err := zipContents(b)
	if err != nil {
		return false
	}
	return maps.EqualFunc(left, right, bytes.Equal)
}

func zipContents(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	contents := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		if _, dup := contents[f.Name]; dup {
			return nil, fmt.Errorf("zip entry %s appears twice", f.Name)
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return nil, err
		}
		contents[f.Name] = content
	}
	return contents, nil
}

// relocateSeeded moves each side's seeded files into the folder of the seed mod it places, which
// copies them back to their own path in game only where the player has none. It returns the seeded
// files a side ships as plain overrides, having no seed mod.
func relocateSeeded(sides []*exportSide) []string {
	plain := map[string]bool{}
	for _, t := range sides {
		for _, path := range t.seeded {
			if t.seedMod == nil {
				plain[path] = true
				continue
			}
			t.files[t.seedMod.Folder+"/"+path] = t.files[path]
			delete(t.files, path)
		}
	}
	return slices.Sorted(maps.Keys(plain))
}

// enableByListedNames points the options file and the shader loader's config at the names the
// launcher saves listed packs under, which are the provider's own file names rather than the
// <key>.zip a build places.
func enableByListedNames(t *exportSide, files []packarchive.File, optionsPath string, form packForm) {
	fileNames := map[string]string{}
	for _, f := range files {
		fileNames[f.Path] = f.Filename
	}
	renamed := map[string]string{}
	for placed, name := range fileNames {
		if base, ok := strings.CutPrefix(placed, "resourcepacks/"); ok && name != "" {
			renamed[base] = name
		}
	}
	rewriteProperty(t.files, optionsPath, resourcePacksKey+":", func(list string) string { return form.rename(list, renamed) })
	for _, s := range integrations.Shaders {
		if s.Config == "" {
			continue
		}
		rewriteProperty(t.files, s.Config, "shaderPack=", func(v string) string {
			if name := fileNames["shaderpacks/"+v]; name != "" {
				return name
			}
			return v
		})
	}
}

func rewriteProperty(files map[string][]byte, path, prefix string, rewrite func(string) string) {
	data, ok := files[path]
	if !ok {
		return
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if v, ok := strings.CutPrefix(line, prefix); ok {
			lines[i] = prefix + rewrite(v)
		}
	}
	files[path] = []byte(strings.Join(lines, "\n"))
}

func reportList(report *ExportReport, kind string, bundled bool) *[]string {
	switch {
	case kind == manifest.TypeResourcePack && bundled:
		return &report.BundledResourcePacks
	case kind == manifest.TypeResourcePack:
		return &report.ResourcePacks
	case kind == manifest.TypeShader && bundled:
		return &report.BundledShaders
	case kind == manifest.TypeShader:
		return &report.Shaders
	case kind == manifest.TypeDatapack && bundled:
		return &report.BundledDatapacks
	case kind == manifest.TypeDatapack:
		return &report.Datapacks
	case bundled:
		return &report.BundledMods
	}
	return &report.Mods
}

// origin names where a mod came from: its provider alone for a download from one of the
// provider's own hosts, and the host beside it for a download from anywhere else.
func origin(ps provider.Providers, name string, u *string) string {
	if name == "" {
		return "local file"
	}
	if u == nil {
		return name + ", manual download"
	}
	parsed, err := url.Parse(*u)
	if err != nil || parsed.Host == "" {
		return name
	}
	host := parsed.Hostname()
	var hosts []string
	if p, ok := ps[name]; ok {
		hosts = p.Hosts()
	}
	for _, domain := range hosts {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return name
		}
	}
	return name + ", " + parsed.Host
}

// splitOverrides lays the sides' files out by layer: a bundled file goes where its entry's side
// says, a file identical in every side goes to the shared folder, and the rest to each side's own.
func splitOverrides(sides []*exportSide) []packarchive.Override {
	entries := map[string][]byte{}
	shared := packarchive.LayerFor("both")
	everywhere := func(path string, data []byte) bool {
		for _, t := range sides {
			if other, ok := t.files[path]; !ok || !bytes.Equal(other, data) {
				return false
			}
		}
		return true
	}
	for _, t := range sides {
		for path, data := range t.files {
			layer := packarchive.LayerFor(t.side)
			if bundledIn, ok := t.layers[path]; ok {
				layer = bundledIn
			} else if everywhere(path, data) {
				layer = shared
			}
			entries[layer+"/"+path] = data
		}
	}
	overrides := make([]packarchive.Override, 0, len(entries))
	for _, rel := range slices.Sorted(maps.Keys(entries)) {
		layer, within, _ := strings.Cut(rel, "/")
		overrides = append(overrides, packarchive.Override{Layer: layer, Path: within, Data: entries[rel]})
	}
	return overrides
}

func exportSummary(m *manifest.Manifest) string {
	var parts []string
	for _, p := range []string{m.Description, m.Note} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n\n")
}

// bundleNudge lists the mods an export can't point at and suggests shipping them in the archive.
func bundleNudge(e *out.Error, mods []string, command string) *out.Error {
	e.Items = mods
	lead := "Ship them inside the archive instead"
	if len(mods) == 1 {
		lead = "Ship it inside the archive instead"
	}
	e.Nudge = out.Nudge{Lead: lead, Command: command}
	return e
}

func countOf(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (b *Builder) log(format string, args ...any) {
	if b.Log != nil {
		b.Log(format, args...)
	}
}
