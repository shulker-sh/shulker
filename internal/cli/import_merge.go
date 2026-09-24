package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

// incoming is a modpack read as a project of its own, for a merge: its manifest and lock, the
// override files it lays, and the folder its local files lie in. hasBlocks is set when the
// manifest is the pack's own, a marker's or a source's, whose blocks merge too.
type incoming struct {
	manifest  *manifest.Manifest
	lock      *lock.Lock
	overrides []packarchive.Override
	dir       string
	hasBlocks bool
}

// mergeReport is what a merge brought in and what it left alone.
type mergeReport struct {
	merged    []string
	keptYours []string
	leftOut   []string
	copied    int
	kept      int
	// created are the files and folders the merge wrote, which a failed relock takes away again.
	created []string
}

func (rep *mergeReport) undo() {
	for _, path := range slices.Backward(rep.created) {
		os.RemoveAll(path)
	}
}

// mergeImport merges a modpack into the project p, the project winning on every clash.
func (a *app) mergeImport(cmd *cobra.Command, d *deps, p *project.Project, arc *packarchive.Archive, source *pack.Checkout, f *importFlags) error {
	ctx := cmd.Context()
	dir := p.Dir
	sides, err := mergeSides(p.Manifest, f.side)
	if err != nil {
		return err
	}
	var inc *incoming
	var mods *resolve.Imported
	if source != nil {
		if inc, err = readSourcePack(source); err != nil {
			return err
		}
		if err := checkImportPlatform(p, inc.lock.Minecraft, inc.lock.Loader.Type, inc.lock.Loader.Version); err != nil {
			return err
		}
	} else {
		if err := checkImportPlatform(p, arc.Minecraft, arc.Loader.Type, arc.Loader.Version); err != nil {
			return err
		}
		staging, err := os.MkdirTemp("", "shulker-import-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(staging)
		pk, err := a.readImportPack(ctx, d, arc, staging, f)
		if err != nil {
			return err
		}
		mods = pk.mods
		inc = &incoming{manifest: pk.manifest, lock: pk.lock, overrides: mods.Overrides, dir: staging, hasBlocks: arc.Marker != nil}
	}
	name, version := inc.manifest.Name, inc.manifest.Version
	var rep *mergeReport
	run := func(p *project.Project, _ *resolve.Resolver) (string, error) {
		rep, err = mergePack(p, inc, sides)
		return "", err
	}
	if _, err := a.relockProject(cmd, p, relockOptions{}, run); err != nil {
		if rep != nil {
			rep.undo()
		}
		return err
	}
	res := importResult{Dir: dir, Name: name, Version: version, Minecraft: p.Lock.Minecraft, Loader: p.Lock.Loader, Marker: inc.hasBlocks && source == nil, Sides: p.Manifest.Sides(), Mods: mods, Overrides: []string{}, Merged: true, KeptYours: rep.keptYours, LeftOut: rep.leftOut}
	if source != nil {
		res.Source = source.Source
	}
	summary := plural(len(rep.merged), "entry", "entries") + " merged"
	if mods != nil {
		locked := slices.DeleteFunc(slices.Clone(mods.Locked), func(f resolve.LockedFile) bool { return !slices.Contains(rep.merged, f.ID) })
		summary = lockedSummary(d.providers, locked)
	}
	return a.emitImport(res, out.Row{Text: summary}, rep.overrideRow())
}

func (rep *mergeReport) overrideRow() out.Row {
	return out.Row{Text: fmt.Sprintf("%s copied, %d kept", plural(rep.copied, "override file", "override files"), rep.kept)}
}

// mergeSides are the sides a merge into m takes: the ones it declares, or the one --side names.
func mergeSides(m *manifest.Manifest, side string) ([]string, error) {
	sides := m.Sides()
	if side == "" {
		return sides, nil
	}
	if !slices.Contains(sides, side) {
		return nil, out.Errorf("usage", "--side %s names a side the project doesn't declare", side)
	}
	return []string{side}, nil
}

// checkImportPlatform refuses a pack whose Minecraft version, loader or loader version differs
// from the one the project sets or inherits. A project that has neither takes the pack's, which
// the merge writes.
func checkImportPlatform(p *project.Project, minecraft, loaderType, loaderVersion string) error {
	have := cmp.Or(p.Lock.Minecraft, p.Manifest.Minecraft)
	haveLoader := cmp.Or(p.Lock.Loader.Type, p.Manifest.Loader.Type)
	haveVersion := cmp.Or(p.Lock.Loader.Version, p.Manifest.Loader.Version)
	sameLoader := haveLoader == "" || (haveLoader == loaderType && (haveVersion == "" || haveVersion == loaderVersion))
	if (have == "" || have == minecraft) && sameLoader {
		return nil
	}
	e := out.Errorf("import-mismatch", "the pack is for %s, and the project for %s", platformLabel(minecraft, loaderType, loaderVersion), platformLabel(have, haveLoader, haveVersion))
	e.Help = "import it into a new project with -C <dir>"
	return e
}

// readSourcePack reads a shulker source for a merge: its manifest, its lock, and the files in its
// override folders.
func readSourcePack(c *pack.Checkout) (*incoming, error) {
	src, err := project.OpenReplacingLock(c.Dir)
	if err != nil {
		return nil, err
	}
	if err := src.RequireLock(); err != nil {
		return nil, err
	}
	overrides, err := readOverrideFolders(c.Dir, src.Manifest)
	if err != nil {
		return nil, err
	}
	return &incoming{manifest: src.Manifest, lock: src.Lock, overrides: overrides, dir: c.Dir, hasBlocks: true}, nil
}

// readOverrideFolders reads the files in a project's override folders, a feature's included.
func readOverrideFolders(dir string, m *manifest.Manifest) ([]packarchive.Override, error) {
	var overrides []packarchive.Override
	for _, layer := range overrideLayers(m) {
		root := filepath.Join(dir, filepath.FromSlash(layer))
		err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
			if err != nil || !e.Type().IsRegular() {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			overrides = append(overrides, packarchive.Override{Layer: layer, Path: filepath.ToSlash(rel), Data: data})
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return overrides, nil
}

// mergePack merges inc into p within sides: the pack's entries, lock entries, local files and
// overrides, and a marker's or source's blocks, the project keeping its own on every clash.
func mergePack(p *project.Project, inc *incoming, sides []string) (*mergeReport, error) {
	rep := &mergeReport{leftOut: []string{}, keptYours: []string{}}
	pm, pl := inc.manifest, inc.lock
	overrides := inc.overrides
	if len(sides) == 1 {
		overrides, rep.leftOut = keepSide(pm, pl, overrides, sides[0])
	}
	takePlatform(p, pm, pl)
	projectJars := map[string]bool{}
	for key := range p.Lock.Mods {
		projectJars[p.Lock.JarID(key)] = true
	}
	kept := map[string]bool{}
	for _, key := range slices.Sorted(maps.Keys(pm.Requires)) {
		_, isMod := pl.Mods[key]
		if _, listed := p.Manifest.Requires[key]; listed || (isMod && projectJars[pl.JarID(key)]) {
			rep.keptYours = append(rep.keptYours, key)
			kept[key] = true
			continue
		}
		req := pm.Requires[key]
		req.Pin = ""
		p.Manifest.Requires[key] = req
		rep.merged = append(rep.merged, key)
	}
	var files []string
	for _, key := range slices.Sorted(maps.Keys(pl.Mods)) {
		mod := pl.Mods[key]
		if _, held := p.Lock.Mods[key]; held || kept[key] || kept[mod.Modpack] || projectJars[pl.JarID(key)] {
			continue
		}
		p.Lock.Mods[key] = mod
		files = append(files, mod.File)
	}
	for _, kind := range manifest.PackKinds {
		mine := p.Lock.Packs(kind)
		for key, lp := range pl.Packs(kind) {
			if _, held := mine[key]; held || kept[key] || kept[lp.Modpack] {
				continue
			}
			mine[key] = lp
			files = append(files, lp.File)
		}
	}
	for key, mp := range pl.Modpacks {
		if _, held := p.Lock.Modpacks[key]; held || kept[key] {
			continue
		}
		p.Lock.Modpacks[key] = mp
	}
	for _, rel := range files {
		if rel == "" {
			continue
		}
		from, to := filepath.Join(inc.dir, filepath.FromSlash(rel)), filepath.Join(p.Dir, filepath.FromSlash(rel))
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		if _, err := os.Stat(from); inc.dir == "" || err != nil {
			return rep, pack.FileMissing(inc.manifest.Name, rel)
		}
		rep.created = append(rep.created, to)
		if err := fsutil.CopyPath(from, to); err != nil {
			return rep, err
		}
	}
	for _, o := range overrides {
		to := filepath.Join(p.Dir, filepath.FromSlash(o.Layer), filepath.FromSlash(o.Path))
		if _, err := os.Lstat(to); err == nil {
			rep.keptYours = append(rep.keptYours, o.Layer+"/"+o.Path)
			rep.kept++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return rep, err
		}
		rep.created = append(rep.created, to)
		if err := fsutil.Write(to, o.Data); err != nil {
			return rep, err
		}
		rep.copied++
	}
	if inc.hasBlocks {
		if err := mergeBlocks(p.Manifest, pm, sides); err != nil {
			return rep, err
		}
		if p.Manifest.Server != nil && p.Manifest.Server.Players != nil && len(p.Lock.Players) == 0 {
			p.Lock.Players = pl.Players
		}
	}
	slices.Sort(rep.keptYours)
	slices.Sort(rep.leftOut)
	return rep, nil
}

// takePlatform gives a project that sets and inherits no Minecraft version or loader the pack's.
func takePlatform(p *project.Project, pm *manifest.Manifest, pl *lock.Lock) {
	if p.Manifest.Minecraft == "" && p.Lock.Minecraft == "" {
		p.Manifest.Minecraft = pm.Minecraft
		p.Lock.Minecraft, p.Lock.DataVersion, p.Lock.Java = pl.Minecraft, pl.DataVersion, pl.Java
	}
	if p.Manifest.Loader.Type == "" && p.Lock.Loader.Type == "" && pl.Loader.Type != "" {
		p.Manifest.Loader = pm.Loader
		p.Lock.Loader = pl.Loader
	}
}

// mergeBlocks merges the pack manifest's blocks into the project's, key by key, the project's value
// winning: features, variables, providers, java and note, and the side blocks the project declares
// within sides, less the folder each side builds into.
func mergeBlocks(m, pm *manifest.Manifest, sides []string) error {
	mine, err := asObject(m)
	if err != nil {
		return err
	}
	theirs, err := asObject(pm)
	if err != nil {
		return err
	}
	keys := []string{"features", "variables", "providers", "java", "note"}
	for _, side := range sides {
		if m.HasSide(side) {
			keys = append(keys, side)
			if block, ok := theirs[side].(map[string]any); ok {
				delete(block, "build")
			}
		}
	}
	for _, key := range keys {
		if v, ok := theirs[key]; ok {
			mine[key] = mergeValue(mine[key], v)
		}
	}
	data, err := json.Marshal(mine)
	if err != nil {
		return err
	}
	var merged manifest.Manifest
	if err := json.Unmarshal(data, &merged); err != nil {
		return err
	}
	*m = merged
	return nil
}

func asObject(m *manifest.Manifest) (map[string]any, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	return obj, json.Unmarshal(data, &obj)
}

// mergeValue merges theirs into mine: objects key by key, and anything else stays mine when mine
// is set.
func mergeValue(mine, theirs any) any {
	if mine == nil {
		return theirs
	}
	mineObj, ok := mine.(map[string]any)
	theirObj, ok2 := theirs.(map[string]any)
	if !ok || !ok2 {
		return mine
	}
	for k, v := range theirObj {
		mineObj[k] = mergeValue(mineObj[k], v)
	}
	return mineObj
}
