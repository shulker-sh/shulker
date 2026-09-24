package cli

import (
	"cmp"
	"fmt"
	"os"
	"slices"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

// mergeImport merges a modpack into the project p, the project winning on every clash.
func (a *app) mergeImport(cmd *cobra.Command, d *deps, p *project.Project, arc *packarchive.Archive, source *pack.Checkout, f *importFlags) error {
	ctx := cmd.Context()
	dir := p.Dir
	sides, err := mergeSides(p.Manifest, f.side)
	if err != nil {
		return err
	}
	var inc *resolve.Incoming
	var mods *resolve.Imported
	if source != nil {
		if inc, err = readSourcePack(source); err != nil {
			return err
		}
		if err := checkImportPlatform(p, inc.Lock.Minecraft, inc.Lock.Loader.Type, inc.Lock.Loader.Version); err != nil {
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
		r, imported, err := a.importPack(ctx, d, arc, staging, f)
		if err != nil {
			return err
		}
		mods = imported
		inc = &resolve.Incoming{Manifest: r.Manifest, Lock: r.Lock, Overrides: mods.Overrides, Dir: staging, HasBlocks: arc.Marker != nil}
	}
	name, version := inc.Manifest.Name, inc.Manifest.Version
	var rep *resolve.Merged
	run := func(p *project.Project, _ *resolve.Resolver) (string, error) {
		rep, err = resolve.Merge(p, inc, sides)
		return "", err
	}
	if _, err := a.relockProject(cmd, p, relockOptions{}, run); err != nil {
		if rep != nil {
			rep.Undo()
		}
		return err
	}
	res := importResult{Dir: dir, Name: name, Version: version, Minecraft: p.Lock.Minecraft, Loader: p.Lock.Loader, Marker: inc.HasBlocks && source == nil, Sides: p.Manifest.Sides(), Mods: mods, Overrides: []string{}, Merged: true, KeptYours: rep.KeptYours, LeftOut: rep.LeftOut}
	if source != nil {
		res.Source = source.Source
	}
	summary := plural(len(rep.Entries), "entry", "entries") + " merged"
	if mods != nil {
		locked := slices.DeleteFunc(slices.Clone(mods.Locked), func(f resolve.LockedFile) bool { return !slices.Contains(rep.Entries, f.ID) })
		summary = lockedSummary(d.providers, locked)
	}
	return a.emitImport(res, out.Row{Text: summary}, overrideRow(rep))
}

func overrideRow(rep *resolve.Merged) out.Row {
	return out.Row{Text: fmt.Sprintf("%s copied, %d kept", plural(rep.Copied, "override file", "override files"), rep.Kept)}
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
func readSourcePack(c *pack.Checkout) (*resolve.Incoming, error) {
	src, err := project.OpenReplacingLock(c.Dir)
	if err != nil {
		return nil, err
	}
	if err := src.RequireLock(); err != nil {
		return nil, err
	}
	overrides, err := project.ReadOverrideFolders(c.Dir, src.Manifest)
	if err != nil {
		return nil, err
	}
	return &resolve.Incoming{Manifest: src.Manifest, Lock: src.Lock, Overrides: overrides, Dir: c.Dir, HasBlocks: true}, nil
}
