package cli

import (
	"cmp"
	"slices"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

// isModpackKey reports whether an import argument is a modpack the target project requires,
// which comes before every other reading of it. --type modpack insists on it, and any other --type
// rules it out.
func isModpackKey(target *project.Project, arg, typ string) (bool, error) {
	isKey := false
	if target != nil && (typ == "" || typ == "modpack") {
		req, listed := target.Manifest.Requires[arg]
		isKey = listed && req.Kind() == manifest.TypeModpack
	}
	if typ == "modpack" && !isKey {
		return false, out.Errorf("usage", "--type modpack names a modpack the project requires, and %s isn't one", arg)
	}
	return isKey, nil
}

// inlineImport makes a modpack the project requires part of the project: its entries become the
// project's own, merged as an import merges a pack, and the requires entry and its lock section go.
func (a *app) inlineImport(cmd *cobra.Command, p *project.Project, key string, f *importFlags) error {
	if f.at != (modpack.At{}) || f.provider != "" || f.ignoreShulker {
		return out.Errorf("usage", "--ref, --path, --provider and --ignore-shulker don't apply to a modpack the project requires")
	}
	sides, err := mergeSides(p.Manifest, f.side)
	if err != nil {
		return err
	}
	var rep *resolve.Merged
	var name, version string
	run := func(p *project.Project, r *resolve.Resolver) (string, error) {
		i := slices.IndexFunc(r.Packs, func(l *modpack.Loaded) bool { return l.Name == key })
		if i < 0 {
			return "", out.Errorf("modpack-not-found", "modpack %s is not in the manifest", key)
		}
		loaded := r.Packs[i]
		name, version = cmp.Or(loaded.Manifest.Name, key), loaded.Manifest.Version
		inc, err := resolve.Inlined(p, loaded)
		if err != nil {
			return "", err
		}
		delete(p.Manifest.Requires, key)
		packs := slices.Delete(slices.Clone(r.Packs), i, i+1)
		if err := r.RefreshPacks(packs); err != nil {
			return "", err
		}
		p.ReplacePacks(packs)
		rep, err = resolve.Merge(p, inc, sides)
		return "", err
	}
	if _, err := a.relockOpened(cmd, p, relockOptions{}, run); err != nil {
		if rep != nil {
			rep.Undo()
		}
		return err
	}
	res := importResult{Dir: p.Dir, Name: name, Version: version, Minecraft: p.Lock.Minecraft, Loader: p.Lock.Loader, Source: key, Sides: p.Manifest.Sides(), Overrides: []string{}, Merged: true, KeptYours: rep.KeptYours, LeftOut: rep.LeftOut}
	return a.emitImport(res, out.Row{Text: out.Count(len(rep.Entries), "entry", "entries") + " now the project's own"}, overrideRow(rep))
}
