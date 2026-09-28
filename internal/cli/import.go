package cli

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/resolve"
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
	at                        modpack.At
	ignoreShulker             bool
	noServerPack              bool
}

func (a *app) importCmd() *cobra.Command {
	var f importFlags
	cmd := &cobra.Command{
		Use:         "import <modpack>",
		Annotations: acts(),
		Short:       "Create a project from a modpack file, URL, slug or shulker source",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a.printer.ClearFetches = true
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
	cmd.Flags().BoolVar(&f.noServerPack, "no-server-pack", false, "don't read the server files the pack pairs with for which mods are client-only")
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
	arc, source, hosted, err := a.findImport(ctx, d, dir, target, arg, f)
	if err != nil {
		return err
	}
	var record *lock.Imported
	if arc != nil {
		if record, err = resolve.ImportRecord(d.Cache, arc, hosted); err != nil {
			return err
		}
	}
	if target != nil {
		return a.mergeImport(cmd, d, target, arc, source, record, f)
	}
	if source != nil {
		return a.importSource(cmd, dir, source, f)
	}
	staged, err := project.Stage(dir)
	if err != nil {
		return err
	}
	defer staged.Discard()
	var r *resolve.Resolver
	var mods *resolve.Imported
	err = a.awaitingDownloads(ctx, func() string { return dir }, true, func(skip bool) error {
		r, mods, err = a.importPack(ctx, d, arc, staged.Dir, filepath.Join(dir, resolve.DownloadsDir), skip, f)
		return err
	})
	if err != nil {
		return err
	}
	m, l := r.Manifest, r.Lock
	l.Imported = record
	leftOut := []string{}
	if f.side != "" {
		mods.Overrides, leftOut = resolve.KeepSide(m, l, mods.Overrides, f.side)
	}
	if err := project.WriteIcon(staged.Dir, m, arc.Icon); err != nil {
		return err
	}
	if err := project.Create(staged.Dir, m, l, mods.Overrides); err != nil {
		return err
	}
	if err := staged.Commit(); err != nil {
		return err
	}
	res := importResult{Dir: dir, Name: m.Name, Version: m.Version, Minecraft: l.Minecraft, Loader: l.Loader, Marker: arc.Marker != nil, Sides: m.Sides(), Mods: mods, Overrides: overridePaths(mods.Overrides), KeptYours: []string{}, LeftOut: leftOut}
	return a.emitImport(res, importedRows(a.titles(), mods, len(res.Overrides))...)
}

// importedRows count what an import locked and kept as files, leaving out what it has none of.
func importedRows(providers provider.Providers, mods *resolve.Imported, overrides int) []out.Row {
	var rows []out.Row
	if len(mods.Locked) > 0 {
		rows = append(rows, out.Row{Text: lockedCounts(providers, mods.Locked)})
	}
	if len(mods.Unmanaged) > 0 {
		rows = append(rows, out.Row{Text: out.Count(len(mods.Unmanaged), "unmanaged file", "unmanaged files")})
	}
	if overrides > 0 {
		rows = append(rows, out.Row{Text: out.Count(overrides, "override file", "override files")})
	}
	return rows
}

// emitImport reports an import: the result line, rows, then the rows every import ends with, and
// how to build the project.
func (a *app) emitImport(res importResult, rows ...out.Row) error {
	return a.printer.Emit(res, func(l *out.Lines) {
		l.OKInto("Imported "+res.Name+" "+res.Version, res.Dir, resolve.PlatformLabel(res.Minecraft, res.Loader.Type, res.Loader.Version), append(rows, importRows(l.T, res.Mods, res.KeptYours, res.LeftOut)...)...)
		l.Nudge("Play it in a launcher", "shulker link <launcher>")
	})
}

// shownKept is how many kept-yours entries an import's report lists before counting the rest.
const shownKept = 10

// importRows are the rows an import's report ends with, each only when it has something to say.
func importRows(t out.Theme, mods *resolve.Imported, keptYours, leftOut []string) []out.Row {
	var rows []out.Row
	if len(keptYours) > 0 {
		children := slices.Clone(keptYours[:min(len(keptYours), shownKept)])
		if more := len(keptYours) - shownKept; more > 0 {
			children = append(children, t.Grey(fmt.Sprintf("…and %d more", more)))
		}
		rows = append(rows, out.Row{Label: "kept yours", Children: children})
	}
	if len(leftOut) > 0 {
		rows = append(rows, out.Row{Label: "left out for side", Children: leftOut})
	}
	if mods != nil && len(mods.Pending) > 0 {
		rows = append(rows, out.Row{Label: "waiting for a manual download", Children: mods.Pending})
	}
	if mods != nil && mods.ServerPack != nil && len(mods.ServerPack.Client)+len(mods.ServerPack.Both) > 0 {
		sp := mods.ServerPack
		rows = append(rows, out.Row{Label: "sides from the pack's server files", Text: fmt.Sprintf("%d client-only, %d on both sides", len(sp.Client), len(sp.Both))})
	}
	if mods != nil && len(mods.Dropped) > 0 {
		rows = append(rows, out.Row{Label: "dropped, no longer in the pack", Text: strings.Join(mods.Dropped, ", ")})
	}
	if mods != nil {
		for _, folder := range slices.Sorted(maps.Keys(mods.Seeded)) {
			rows = append(rows, out.Row{Text: out.Count(len(mods.Seeded[folder]), "seeded file", "seeded files") + " from " + folder})
		}
	}
	if mods != nil && len(mods.Duplicates) > 0 {
		rows = append(rows, out.Row{Label: "left out as copies of a datapack a global datapack mod loads", Children: mods.Duplicates})
	}
	return rows
}

// importPack reads arc as the project it would make at dir, its local files copied out there,
// and returns the resolver holding that project's manifest and lock.
func (a *app) importPack(ctx context.Context, d *deps, arc *packarchive.Archive, dir, downloads string, skip bool, f *importFlags) (*resolve.Resolver, *resolve.Imported, error) {
	r := resolve.NewAt(d.Env, dir)
	r.DownloadsIn, r.SkipPending = downloads, skip
	mods, err := r.ImportProject(ctx, arc, resolve.ImportOptions{Name: f.name, IgnoreMarker: f.ignoreShulker, ServerPack: !f.noServerPack})
	a.warn(r.Warnings)
	if err != nil {
		return nil, nil, err
	}
	a.warn(mods.Warnings)
	return r, mods, nil
}

// findImport reads what an import argument names: an existing file as an archive and a folder
// as a source; a URL as an archive by its content, else a git or manifest source; and anything
// else as a modpack slug, fitting target's platform when there is a target to merge into. It
// returns the archive, or the source's checkout.
func (a *app) findImport(ctx context.Context, d *deps, dir string, target *project.Project, arg string, f *importFlags) (*packarchive.Archive, *modpack.Checkout, *lock.Modpack, error) {
	if !isImportURL(arg) {
		if !looksLikePath(arg) {
			return a.importHosted(ctx, d, dir, target, arg, f)
		}
		path, err := importPath(arg)
		if err != nil {
			return nil, nil, nil, err
		}
		if path == dir {
			return nil, nil, nil, out.Errorf("import-into-self", "can't import %s into itself", filepath.Base(dir))
		}
		if resolve.IsLocalFolder(path) {
			arc, c, err := a.importCheckout(ctx, d, dir, path, f)
			return arc, c, nil, err
		}
		if _, err := os.Stat(path); err != nil {
			return nil, nil, nil, out.Errorf("file-not-found", "%s isn't there", arg)
		}
		if err := refuseImportFlags(f, modpack.File); err != nil {
			return nil, nil, nil, err
		}
		arc, err := readImportArchive(path, f.typ)
		return arc, nil, nil, err
	}
	if (strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")) && modpack.Classify(arg) == modpack.Git {
		store := &modpack.Store{Cache: d.Cache, Fetch: d.Fetch, Log: a.progress, Working: a.printer.Working}
		path, err := store.FetchArchive(ctx, arg)
		if err != nil {
			return nil, nil, nil, err
		}
		if path != "" {
			if err := refuseImportFlags(f, modpack.File); err != nil {
				return nil, nil, nil, err
			}
			arc, err := readImportArchive(path, f.typ)
			return arc, nil, nil, err
		}
	}
	arc, c, err := a.importCheckout(ctx, d, dir, arg, f)
	return arc, c, nil, err
}

// looksLikePath reports whether import reads arg as a path rather than a modpack slug. A bare word
// is a slug even where a folder of that name exists, which the -C folder of an earlier import often
// is.
func looksLikePath(arg string) bool {
	if strings.ContainsAny(arg, `/\`) || strings.HasPrefix(arg, ".") || strings.HasPrefix(arg, "~") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(arg))
	return ext == ".zip" || ext == ".mrpack"
}

// importPath is arg as an absolute path. A relative one is read from the current folder, never -C,
// which names where the import goes.
func importPath(arg string) (string, error) {
	if rest, ok := strings.CutPrefix(arg, "~"); ok && (rest == "" || rest[0] == '/' || rest[0] == filepath.Separator) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		arg = filepath.Join(home, rest)
	}
	return filepath.Abs(arg)
}

func isImportURL(arg string) bool {
	return modpack.Classify(arg) != modpack.Local || strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")
}

// refuseImportFlags refuses the flags that don't apply to a modpack of kind, with the codes a
// source uses for --ref and --path.
func refuseImportFlags(f *importFlags, kind modpack.Kind) error {
	if f.at.Ref != "" && kind != modpack.Git {
		return out.Errorf("source-ref", "--ref only applies to git sources")
	}
	if err := modpack.CheckPath(f.at.Path, kind); err != nil {
		return err
	}
	if f.provider != "" && kind != modpack.Hosted {
		return out.Errorf("usage", "--provider only applies to a modpack slug")
	}
	if f.typ == "source" && (kind == modpack.File || kind == modpack.Hosted) {
		return out.Errorf("usage", "--type source names a shulker project, and this is a modpack archive")
	}
	return nil
}

// importHosted picks a hosted modpack's newest release that fits target's platform, or any
// platform without a target, and reads its archive from the cache.
func (a *app) importHosted(ctx context.Context, d *deps, dir string, target *project.Project, slug string, f *importFlags) (*packarchive.Archive, *modpack.Checkout, *lock.Modpack, error) {
	if err := refuseImportFlags(f, modpack.Hosted); err != nil {
		return nil, nil, nil, err
	}
	r := resolve.NewAt(d.Env, dir)
	r.Manifest = &manifest.Manifest{}
	var was *lock.Imported
	if target != nil {
		r.Manifest, r.Lock, was = target.Manifest, target.Lock, target.Lock.Imported
	}
	pin, err := r.ObtainImport(ctx, slug, manifest.Require{Type: manifest.TypeModpack, Provider: f.provider}, was)
	a.warn(r.Warnings)
	if err != nil {
		return nil, nil, nil, err
	}
	arc, err := readImportArchive(d.Cache.Object(pin.Sha512), f.typ)
	return arc, nil, &pin, err
}

// importCheckout fetches a shulker source for an import to copy.
func (a *app) importCheckout(ctx context.Context, d *deps, dir, source string, f *importFlags) (*packarchive.Archive, *modpack.Checkout, error) {
	kind := modpack.Classify(source)
	if err := refuseImportFlags(f, kind); err != nil {
		return nil, nil, err
	}
	if want, ok := packarchive.Lookup(f.typ); ok {
		return nil, nil, out.Errorf("usage", "%s is a shulker source, not a %s modpack", source, want.Title())
	}
	store := &modpack.Store{Cache: d.Cache, ProjectDir: dir, Fetch: d.Fetch, Log: a.progress, Working: a.printer.Working, Warn: a.printer.Warn, WarnsRawURL: true}
	c, err := store.Checkout(ctx, source, f.at)
	return nil, c, err
}

// importSource creates the project as a copy of a shulker source: its manifest, its lock, whose
// entries the relock reuses, and its files and override folders. A failure removes what it copied.
func (a *app) importSource(cmd *cobra.Command, dir string, c *modpack.Checkout, f *importFlags) error {
	src, err := project.OpenReplacingLock(c.Dir)
	if err != nil {
		return err
	}
	m := src.Manifest
	if f.name != "" {
		m.Name = f.name
	}
	_, leftOut, undo, err := project.CopySource(c.Dir, dir, m, f.side)
	if err != nil {
		undo()
		return err
	}
	p := &project.Project{Dir: dir, Manifest: m, Lock: src.Lock}
	if p.Lock == nil {
		p.Lock = lock.New()
	}
	if f.side != "" {
		_, dropped := resolve.KeepSide(m, p.Lock, nil, f.side)
		leftOut = append(leftOut, dropped...)
	}
	if err := p.SaveManifest(); err != nil {
		undo()
		return err
	}
	if _, err := a.relockOpened(cmd, p, relockOptions{}, func(*project.Project, *resolve.Resolver) (string, error) { return "", nil }); err != nil {
		undo()
		return err
	}
	slices.Sort(leftOut)
	res := importResult{Dir: dir, Name: m.Name, Version: m.Version, Minecraft: p.Lock.Minecraft, Loader: p.Lock.Loader, Source: c.Source, Sides: m.Sides(), KeptYours: []string{}, LeftOut: leftOut, Overrides: []string{}}
	return a.emitImport(res, out.Row{Text: fmt.Sprintf("%s copied from %s", out.Count(len(m.Requires), "entry", "entries"), c.Source)})
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

// lockedSummary counts an import's locked files by type, naming each type's providers when the
// files came from more than one, or the one provider after them all when they didn't.
func lockedSummary(providers provider.Providers, files []resolve.LockedFile) string {
	if len(files) == 0 {
		return out.Count(0, "file", "files") + " locked"
	}
	counts, host := countLocked(providers, files)
	return counts + " locked" + host
}

// lockedCounts counts files by type the way lockedSummary does, without saying they were locked.
func lockedCounts(providers provider.Providers, files []resolve.LockedFile) string {
	counts, host := countLocked(providers, files)
	return counts + host
}

// countLocked counts files by type, naming each type's providers when there are several, and
// returns the one provider as a " from <provider>" suffix when there is one.
func countLocked(providers provider.Providers, files []resolve.LockedFile) (counts, host string) {
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
		part := out.Count(total, one, many)
		if len(hosts) > 1 {
			part += " (" + strings.Join(from, ", ") + ")"
		}
		parts = append(parts, part)
	}
	if len(hosts) == 1 {
		host = " from " + providers.Title(files[0].Provider)
	}
	return strings.Join(parts, ", "), host
}

func overridePaths(overrides []packarchive.Override) []string {
	paths := make([]string, 0, len(overrides))
	for _, o := range overrides {
		paths = append(paths, o.Layer+"/"+o.Path)
	}
	sort.Strings(paths)
	return paths
}
