package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/server"
)

type importResult struct {
	Dir       string            `json:"dir"`
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	Minecraft string            `json:"minecraft"`
	Loader    lock.Loader       `json:"loader"`
	Marker    bool              `json:"marker"`
	Source    string            `json:"source,omitempty"`
	Sides     []string          `json:"sides"`
	Mods      *resolve.Imported `json:"mods"`
	Overrides []string          `json:"overrides"`
	Merged    bool              `json:"merged"`
	KeptYours []string          `json:"keptYours"`
	LeftOut   []string          `json:"leftOut"`
}

// importFlags are import's own flags.
type importFlags struct {
	name, typ, side, provider string
	at                        pack.At
	ignoreShulker             bool
}

func (a *app) importCmd() *cobra.Command {
	var f importFlags
	cmd := &cobra.Command{
		Use:         "import <modpack>",
		Annotations: acts(),
		Short:       "Create a project from a modpack file, URL, slug or shulker source",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runImport(cmd, args[0], &f)
		},
	}
	cmd.Flags().StringVar(&f.name, "name", "", "project name (default: the pack name, slugified)")
	cmd.Flags().StringVar(&f.typ, "type", "", "refuse the modpack unless it is this kind: "+strings.Join(packarchive.Names(), ", ")+", source, or modpack for one the project requires (default: detected)")
	cmd.Flags().StringVar(&f.provider, "provider", "", "look a slug up on this provider only: modrinth or curseforge (default: the first that has it)")
	cmd.Flags().StringVar(&f.at.Ref, "ref", "", "git ref of a git source (default: the remote HEAD)")
	cmd.Flags().StringVar(&f.at.Path, "path", "", "folder of a git source's repository holding its shulker.json (default: the root)")
	cmd.Flags().StringVar(&f.side, "side", "", "take one side only: client or server (default: every side the pack declares)")
	cmd.Flags().BoolVar(&f.ignoreShulker, "ignore-shulker", false, "ignore the shulker manifest and lock inside the modpack and import it as any other one")
	return cmd
}

func (a *app) runImport(cmd *cobra.Command, arg string, f *importFlags) error {
	ctx := cmd.Context()
	err := checkFlagValues(
		flagValue{"type", f.typ, append(packarchive.Names(), "source", "modpack")},
		flagValue{"side", f.side, []string{"client", "server"}},
		flagValue{"provider", f.provider, manifest.DefaultProviders},
	)
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(cmp.Or(a.dir, "."))
	if err != nil {
		return err
	}
	var target *project.Project
	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
		if f.name != "" {
			return out.Errorf("usage", "--name names a new project, and %s already holds a %s to merge into", dir, manifest.FileName)
		}
		if target, err = a.openProjectAt(dir); err != nil {
			return err
		}
		if err := target.RequireLock(); err != nil {
			return err
		}
	}
	if isKey, err := isModpackKey(target, arg, f.typ); err != nil || isKey {
		if err != nil {
			return err
		}
		return a.inlineImport(cmd, target, arg, f)
	}
	d, err := a.deps()
	if err != nil {
		return err
	}
	arc, source, err := a.findImport(ctx, d, dir, target, arg, f)
	if err != nil {
		return err
	}
	if target != nil {
		return a.mergeImport(cmd, d, target, arc, source, f)
	}
	if source != nil {
		return a.importSource(cmd, dir, source, f)
	}
	pk, err := a.readImportPack(ctx, d, arc, dir, f)
	if err != nil {
		return err
	}
	m, l, mods := pk.manifest, pk.lock, pk.mods
	leftOut := []string{}
	if f.side != "" {
		mods.Overrides, leftOut = keepSide(m, l, mods.Overrides, f.side)
	}
	if err := writeImportIcon(dir, m, arc.Icon); err != nil {
		return err
	}
	if err := writeImport(dir, m, l, mods.Overrides); err != nil {
		return err
	}
	res := importResult{Dir: dir, Name: m.Name, Version: m.Version, Minecraft: l.Minecraft, Loader: l.Loader, Marker: arc.Marker != nil, Sides: m.Sides(), Mods: mods, Overrides: overridePaths(mods.Overrides), KeptYours: []string{}, LeftOut: leftOut}
	return a.emitImport(res,
		out.Row{Text: importedSummary(a.titles(), mods, res.Marker)},
		out.Row{Text: fmt.Sprintf("%s, %s", plural(len(mods.Unmanaged), "unmanaged file", "unmanaged files"), plural(len(res.Overrides), "override file", "override files"))},
	)
}

// emitImport reports an import: the result line, rows, then the rows every import ends with, and
// how to build the project.
func (a *app) emitImport(res importResult, rows ...out.Row) error {
	return a.printer.Emit(res, func(l *out.Lines) {
		l.OKInto("imported "+res.Name+" "+res.Version, res.Dir, platformLabel(res.Minecraft, res.Loader.Type, res.Loader.Version))
		l.Tree(append(rows, importRows(res.Mods, res.KeptYours, res.LeftOut)...)...)
		l.Nudge("Download and build it", "shulker install")
	})
}

// importedSummary counts what an import locked, and what it reused from a shulker marker.
func importedSummary(providers provider.Providers, mods *resolve.Imported, marker bool) string {
	locked := lockedSummary(providers, mods.Locked)
	if marker {
		locked += fmt.Sprintf(", %d reused from the shulker marker", len(mods.Reused))
	}
	return locked
}

// importRows are the rows an import's report ends with, each only when it has something to say.
func importRows(mods *resolve.Imported, keptYours, leftOut []string) []out.Row {
	var rows []out.Row
	if len(keptYours) > 0 {
		rows = append(rows, out.Row{Label: "kept yours", Children: keptYours})
	}
	if len(leftOut) > 0 {
		rows = append(rows, out.Row{Label: "left out for side", Children: leftOut})
	}
	if mods != nil && len(mods.Dropped) > 0 {
		rows = append(rows, out.Row{Label: "dropped from the marker, not in the pack", Text: strings.Join(mods.Dropped, ", ")})
	}
	if mods != nil && len(mods.Duplicates) > 0 {
		rows = append(rows, out.Row{Label: "left out as copies of a datapack a global datapack mod loads", Children: mods.Duplicates})
	}
	return rows
}

// packProject is a modpack archive read as a project of its own: the manifest and lock a new
// project would get, with the files the resolver found and the overrides it left.
type packProject struct {
	manifest *manifest.Manifest
	lock     *lock.Lock
	mods     *resolve.Imported
}

// readImportPack locks what an archive holds as the project it would make, with dir as that
// project's directory: its local files are copied out there.
func (a *app) readImportPack(ctx context.Context, d *deps, arc *packarchive.Archive, dir string, f *importFlags) (*packProject, error) {
	if f.ignoreShulker {
		arc.Marker = nil
	}
	name := f.name
	if name == "" {
		name = slugify(arc.Name)
	}
	m, warnings := arc.Manifest(name)
	a.warn(warnings)
	// The platform the pack names is exact, where a marker's manifest may hold a range.
	exact := *m
	exact.Minecraft, exact.Loader = arc.Minecraft, manifest.Loader{Type: arc.Loader.Type, Version: arc.Loader.Version}
	l, err := a.importLock(ctx, d, &exact)
	if err != nil {
		return nil, err
	}
	if marker := arc.Marker; marker != nil && marker.Manifest.Server != nil && marker.Manifest.Server.Players != nil {
		l.Players = marker.Lock.Players
	}
	r := &resolve.Resolver{Dir: dir, Manifest: m, Lock: l, Providers: d.providers, Cache: d.cache, Fetch: d.fetch, Log: a.progress}
	mods, err := r.Import(ctx, arc)
	if err != nil {
		return nil, err
	}
	a.warn(mods.Warnings)
	if err := r.AdoptLocalFiles(); err != nil {
		return nil, err
	}
	if arc.Marker != nil {
		mods.Overrides = dropManifestOwned(m, mods.Overrides)
	}
	return &packProject{manifest: m, lock: l, mods: mods}, nil
}

// findImport reads what an import argument names: an existing file as an archive and a folder
// as a source; a URL as an archive by its content, else a git or manifest source; and anything
// else as a modpack slug, fitting target's platform when there is a target to merge into. It
// returns the archive, or the source's checkout.
func (a *app) findImport(ctx context.Context, d *deps, dir string, target *project.Project, arg string, f *importFlags) (*packarchive.Archive, *pack.Checkout, error) {
	if !isImportURL(arg) {
		path := a.localPath(arg)
		if resolve.IsLocalFolder(path) {
			return a.importCheckout(ctx, d, dir, path, f)
		}
		if _, err := os.Stat(path); err == nil {
			if err := refuseImportFlags(f, pack.File); err != nil {
				return nil, nil, err
			}
			arc, err := readImportArchive(path, f.typ)
			return arc, nil, err
		}
		return a.importHosted(ctx, d, dir, target, arg, f)
	}
	if (strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")) && pack.Classify(arg) == pack.Git {
		path, err := a.fetchImportArchive(ctx, d, arg)
		if err != nil {
			return nil, nil, err
		}
		if path != "" {
			if err := refuseImportFlags(f, pack.File); err != nil {
				return nil, nil, err
			}
			arc, err := readImportArchive(path, f.typ)
			return arc, nil, err
		}
	}
	return a.importCheckout(ctx, d, dir, arg, f)
}

// fetchImportArchive downloads url and, when it is a modpack archive, keeps it in the cache at its
// sha512 and returns its path there. Anything else, or nothing at the URL, returns no path, for the
// URL to be read as a git source, unless its name says it is an archive; a download that fails
// otherwise fails the import.
func (a *app) fetchImportArchive(ctx context.Context, d *deps, url string) (string, error) {
	a.progress("fetching %s", url)
	tmp, err := d.cache.TempFile("import")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	_, err = d.fetch.Download(ctx, url, tmp)
	tmp.Close()
	isNamedArchive := packarchive.HasArchiveExtension(strings.SplitN(url, "?", 2)[0])
	if errors.Is(err, fetch.ErrNotFound) && !isNamedArchive {
		return "", nil
	}
	if err != nil {
		e := out.Errorf("modpack-fetch", "couldn't download %s", url)
		return "", e.WithCause("http", err)
	}
	if !packarchive.IsArchive(tmp.Name()) {
		if isNamedArchive {
			return "", packarchive.NotArchive(url)
		}
		return "", nil
	}
	file, err := os.Open(tmp.Name())
	if err != nil {
		return "", err
	}
	defer file.Close()
	sha, err := d.cache.Put(file)
	if err != nil {
		return "", err
	}
	return d.cache.Object(sha), nil
}

func isImportURL(arg string) bool {
	return pack.Classify(arg) != pack.Local || strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")
}

// refuseImportFlags refuses the flags that don't apply to a modpack of kind, with the codes a
// source uses for --ref and --path.
func refuseImportFlags(f *importFlags, kind pack.Kind) error {
	if f.at.Ref != "" && kind != pack.Git {
		return out.Errorf("source-ref", "--ref only applies to git sources")
	}
	if err := pack.CheckPath(f.at.Path, kind); err != nil {
		return err
	}
	if f.provider != "" && kind != pack.Hosted {
		return out.Errorf("usage", "--provider only applies to a modpack slug")
	}
	if f.typ == "source" && (kind == pack.File || kind == pack.Hosted) {
		return out.Errorf("usage", "--type source names a shulker project, and this is a modpack archive")
	}
	return nil
}

// importHosted picks a hosted modpack's newest release that fits target's platform, or any
// platform without a target, and reads its archive from the cache.
func (a *app) importHosted(ctx context.Context, d *deps, dir string, target *project.Project, slug string, f *importFlags) (*packarchive.Archive, *pack.Checkout, error) {
	if err := refuseImportFlags(f, pack.Hosted); err != nil {
		return nil, nil, err
	}
	r := &resolve.Resolver{Dir: dir, Manifest: &manifest.Manifest{}, Providers: d.providers, Cache: d.cache, Fetch: d.fetch, Log: a.progress}
	if target != nil {
		r.Manifest, r.Lock = target.Manifest, target.Lock
	}
	pin, err := r.ObtainModpack(ctx, slug, manifest.Require{Type: manifest.TypeModpack, Provider: f.provider})
	a.warn(r.Warnings)
	if err != nil {
		return nil, nil, err
	}
	arc, err := readImportArchive(d.cache.Object(pin.Sha512), f.typ)
	return arc, nil, err
}

// importCheckout fetches a shulker source for an import to copy.
func (a *app) importCheckout(ctx context.Context, d *deps, dir, source string, f *importFlags) (*packarchive.Archive, *pack.Checkout, error) {
	kind := pack.Classify(source)
	if err := refuseImportFlags(f, kind); err != nil {
		return nil, nil, err
	}
	if want, ok := packarchive.Lookup(f.typ); ok {
		return nil, nil, out.Errorf("usage", "%s is a shulker source, not a %s modpack", source, want.Title())
	}
	store := &pack.Store{Cache: d.cache, ProjectDir: dir, Fetch: d.fetch, Log: a.progress, Warn: a.printer.Warn}
	c, err := store.Checkout(ctx, source, f.at)
	return nil, c, err
}

// importSource creates the project as a copy of a shulker source: its manifest, its lock, whose
// entries the relock reuses, and its files and override folders. A failure removes what it copied.
func (a *app) importSource(cmd *cobra.Command, dir string, c *pack.Checkout, f *importFlags) error {
	src, err := project.OpenReplacingLock(c.Dir)
	if err != nil {
		return err
	}
	m := src.Manifest
	if f.name != "" {
		m.Name = f.name
	}
	paths := projectPaths(m)
	leftOut := []string{}
	if f.side != "" {
		other := otherSide(f.side)
		paths = slices.DeleteFunc(paths, func(p string) bool {
			if !isSideLayer(m, other, p) {
				return false
			}
			if _, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(p))); err == nil {
				leftOut = append(leftOut, p)
			}
			return true
		})
	}
	created, err := copyProjectFiles(c.Dir, dir, paths)
	undo := func() {
		for _, path := range slices.Backward(created) {
			os.RemoveAll(path)
		}
	}
	if err != nil {
		undo()
		return err
	}
	created = append(created, filepath.Join(dir, manifest.FileName))
	p := &project.Project{Dir: dir, Manifest: m, Lock: src.Lock}
	if p.Lock == nil {
		p.Lock = lock.New()
	}
	if f.side != "" {
		_, dropped := keepSide(m, p.Lock, nil, f.side)
		leftOut = append(leftOut, dropped...)
	}
	if err := p.SaveManifest(); err != nil {
		undo()
		return err
	}
	if _, err := a.relockProject(cmd, p, relockOptions{}, func(*project.Project, *resolve.Resolver) (string, error) { return "", nil }); err != nil {
		undo()
		return err
	}
	slices.Sort(leftOut)
	res := importResult{Dir: dir, Name: m.Name, Version: m.Version, Minecraft: p.Lock.Minecraft, Loader: p.Lock.Loader, Source: c.Source, Sides: m.Sides(), KeptYours: []string{}, LeftOut: leftOut, Overrides: []string{}}
	return a.emitImport(res, out.Row{Text: fmt.Sprintf("%s copied from %s", plural(len(m.Requires), "entry", "entries"), c.Source)})
}

// projectPaths are the paths of a project's own files, relative to its folder: its lock, override
// folders, a feature's included, local files, icon and .gitignore. Anything else in the folder, such
// as a build the project makes in place, is not the project's to copy.
func projectPaths(m *manifest.Manifest) []string {
	paths := []string{lock.FileName, ".gitignore", manifest.FilesDir}
	paths = append(paths, overrideLayers(m)...)
	if m.Icon != "" {
		paths = append(paths, m.Icon)
	}
	for _, req := range m.Requires {
		if req.File != "" {
			paths = append(paths, req.File)
		}
		if pack.Classify(req.Source) == pack.Local && req.Source != "" && filepath.IsLocal(req.Source) {
			paths = append(paths, filepath.ToSlash(filepath.Clean(req.Source)))
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

// overrideLayers are a project's override folders: the three every project has, then each
// feature's.
func overrideLayers(m *manifest.Manifest) []string {
	layers := slices.Clone(packarchive.Layers)
	for _, name := range slices.Sorted(maps.Keys(m.Features)) {
		o := m.Features[name].Overrides
		for _, layer := range []string{o.Both, o.Client, o.Server} {
			if layer != "" && !slices.Contains(layers, layer) {
				layers = append(layers, layer)
			}
		}
	}
	return layers
}

// isSideLayer reports whether layer is one of side's own override folders.
func isSideLayer(m *manifest.Manifest, side, layer string) bool {
	layers := []string{side + "-overrides"}
	for _, f := range m.Features {
		if side == "client" && f.Overrides.Client != "" {
			layers = append(layers, f.Overrides.Client)
		}
		if side == "server" && f.Overrides.Server != "" {
			layers = append(layers, f.Overrides.Server)
		}
	}
	return slices.Contains(layers, layer)
}

func otherSide(side string) string {
	if side == "server" {
		return "client"
	}
	return "server"
}

// copyProjectFiles copies each path in src that exists into dir, leaving alone one dir already
// has, and returns the paths it created.
func copyProjectFiles(src, dir string, paths []string) ([]string, error) {
	var created []string
	for _, rel := range paths {
		from, to := filepath.Join(src, filepath.FromSlash(rel)), filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		created = append(created, to)
		if err := copyPath(from, to); err != nil {
			return created, err
		}
	}
	return created, nil
}

// readImportArchive reads a modpack archive by its content, refusing one that isn't the format
// typ names when it names one.
func readImportArchive(file, typ string) (*packarchive.Archive, error) {
	arc, err := packarchive.Read(file)
	if err != nil {
		return nil, err
	}
	if want, ok := packarchive.Lookup(typ); ok && want.Name() != arc.Format.Name() {
		return nil, out.Errorf("usage", "%s is a %s modpack, not a %s one", file, arc.Format.Title(), want.Title())
	}
	return arc, nil
}

// keepSide narrows a project to one side: the other side's block, its entries and its override
// folders, a feature's included, go, and what went is returned by key and by override path.
func keepSide(m *manifest.Manifest, l *lock.Lock, overrides []packarchive.Override, side string) ([]packarchive.Override, []string) {
	other := otherSide(side)
	leftOut := []string{}
	switch side {
	case "client":
		m.Server = nil
		l.Players = []lock.Player{}
		if m.Client == nil {
			m.Client = &manifest.Client{}
		}
	case "server":
		m.Client = nil
		if m.Server == nil {
			m.Server = &manifest.Server{Memory: server.DefaultMemory}
		}
	}
	for key, req := range m.Requires {
		if req.Side == other {
			delete(m.Requires, key)
			leftOut = append(leftOut, key)
		}
	}
	for key, mod := range l.Mods {
		if mod.Side == other {
			delete(l.Mods, key)
			delete(m.Requires, key)
			leftOut = append(leftOut, key)
		}
	}
	for _, kind := range manifest.PackKinds {
		packs := l.Packs(kind)
		for key, p := range packs {
			if p.Side == other {
				delete(packs, key)
				delete(m.Requires, key)
				leftOut = append(leftOut, key)
			}
		}
	}
	kept := overrides[:0]
	for _, o := range overrides {
		if isSideLayer(m, other, o.Layer) {
			leftOut = append(leftOut, o.Layer+"/"+o.Path)
			continue
		}
		kept = append(kept, o)
	}
	slices.Sort(leftOut)
	return kept, slices.Compact(leftOut)
}

// lockedSummary counts an import's locked files by type, naming each type's providers when the
// files came from more than one, or the one provider after them all when they didn't.
func lockedSummary(providers provider.Providers, files []resolve.LockedFile) string {
	if len(files) == 0 {
		return plural(0, "file", "files") + " locked"
	}
	byType := map[string]map[string]int{}
	hosts := map[string]bool{}
	for _, f := range files {
		if byType[f.Type] == nil {
			byType[f.Type] = map[string]int{}
		}
		byType[f.Type][f.Provider]++
		hosts[f.Provider] = true
	}
	var parts []string
	for _, kind := range []string{manifest.TypeMod, manifest.TypeResourcePack, manifest.TypeShader, manifest.TypeDatapack} {
		counts := byType[kind]
		if counts == nil {
			continue
		}
		names := slices.Collect(maps.Keys(counts))
		slices.SortFunc(names, func(a, b string) int {
			return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
		})
		total := 0
		var from []string
		for _, name := range names {
			total += counts[name]
			if len(names) == 1 {
				from = append(from, providers.Title(name))
			} else {
				from = append(from, fmt.Sprintf("%d %s", counts[name], providers.Title(name)))
			}
		}
		one, many := manifest.TypeNouns(kind)
		part := plural(total, one, many)
		if len(hosts) > 1 {
			part += " (" + strings.Join(from, ", ") + ")"
		}
		parts = append(parts, part)
	}
	summary := strings.Join(parts, ", ") + " locked"
	if len(hosts) == 1 {
		summary += " from " + providers.Title(files[0].Provider)
	}
	return summary
}

// importLock starts the new project's lock from the exact platform the pack names.
func (a *app) importLock(ctx context.Context, d *deps, exact *manifest.Manifest) (*lock.Lock, error) {
	a.progress("%s", resolvingLine(exact.Minecraft, exact.Loader))
	platform, err := d.meta.Platform(ctx, exact, nil)
	if err != nil {
		return nil, err
	}
	return a.platformLock(ctx, d, platform), nil
}

// platformLock is a new project's lock, holding just the platform it resolved to.
func (a *app) platformLock(ctx context.Context, d *deps, platform *resolve.Platform) *lock.Lock {
	l := lock.New()
	l.Minecraft = platform.Minecraft
	l.Loader = platform.Loader
	l.Java = platform.Java
	if warning := d.meta.FillDataVersion(ctx, l); warning != "" {
		a.printer.Warn("%s", warning)
	}
	return l
}

func dropManifestOwned(m *manifest.Manifest, overrides []packarchive.Override) []packarchive.Override {
	owned := map[string]bool{}
	if m.Client != nil && m.Client.Options != nil {
		owned[m.OptionsPath()] = true
	}
	if m.Server != nil {
		owned[build.PropertiesFile] = true
		owned[build.EulaFile] = true
		if m.Server.Players != nil {
			owned[build.WhitelistFile] = true
			owned[build.OpsFile] = true
			owned[build.BansFile] = true
		}
	}
	kept := overrides[:0]
	for _, o := range overrides {
		if !owned[o.Path] {
			kept = append(kept, o)
		}
	}
	return kept
}

func writeImport(dir string, m *manifest.Manifest, l *lock.Lock, overrides []packarchive.Override) error {
	if err := scaffold(dir); err != nil {
		return err
	}
	for _, o := range overrides {
		abs := filepath.Join(dir, o.Layer, filepath.FromSlash(o.Path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, o.Data, 0o644); err != nil {
			return err
		}
	}
	p := &project.Project{Dir: dir, Manifest: m, Lock: l}
	if err := p.SaveManifest(); err != nil {
		return err
	}
	return p.SaveLock()
}

// writeImportIcon puts the archive's icon back where the restored manifest names it, and drops
// the key from an archive that carries none, so the project stays valid.
func writeImportIcon(dir string, m *manifest.Manifest, icon []byte) error {
	if m.Icon == "" {
		return nil
	}
	if icon == nil {
		m.Icon = ""
		return nil
	}
	path := filepath.Join(dir, filepath.FromSlash(m.Icon))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.Write(path, icon)
}

func overridePaths(overrides []packarchive.Override) []string {
	paths := make([]string, 0, len(overrides))
	for _, o := range overrides {
		paths = append(paths, o.Layer+"/"+o.Path)
	}
	sort.Strings(paths)
	return paths
}
