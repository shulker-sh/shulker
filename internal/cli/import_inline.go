package cli

import (
	"cmp"
	"slices"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
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
	if f.at != (pack.At{}) || f.provider != "" || f.ignoreShulker {
		return out.Errorf("usage", "--ref, --path, --provider and --ignore-shulker don't apply to a modpack the project requires")
	}
	sides, err := mergeSides(p.Manifest, f.side)
	if err != nil {
		return err
	}
	var rep *mergeReport
	var name, version string
	run := func(p *project.Project, r *resolve.Resolver) (string, error) {
		i := slices.IndexFunc(r.Packs, func(l *pack.Loaded) bool { return l.Name == key })
		if i < 0 {
			return "", out.Errorf("modpack-not-found", "modpack %s is not in the manifest", key)
		}
		loaded := r.Packs[i]
		name, version = cmp.Or(loaded.Manifest.Name, key), loaded.Manifest.Version
		inc, err := inlined(p, loaded)
		if err != nil {
			return "", err
		}
		delete(p.Manifest.Requires, key)
		packs := slices.Delete(slices.Clone(r.Packs), i, i+1)
		if err := r.RefreshPacks(packs); err != nil {
			return "", err
		}
		replacePacks(p, packs)
		rep, err = mergePack(p, inc, sides)
		return "", err
	}
	if _, err := a.relockProject(cmd, p, relockOptions{}, run); err != nil {
		if rep != nil {
			rep.undo()
		}
		return err
	}
	res := importResult{Dir: p.Dir, Name: name, Version: version, Minecraft: p.Lock.Minecraft, Loader: p.Lock.Loader, Source: key, Sides: p.Manifest.Sides(), Overrides: []string{}, Merged: true, KeptYours: rep.keptYours, LeftOut: rep.leftOut}
	return a.emitImport(res, out.Row{Text: plural(len(rep.merged), "entry", "entries") + " now the project's own"}, rep.overrideRow())
}

// inlined is a required modpack as a pack to merge: the project's lock entries it provides, taken
// out of the project's lock and listed under the pack's own requires entry where it has one, its
// manifest's blocks, and its override files.
func inlined(p *project.Project, loaded *pack.Loaded) (*incoming, error) {
	key := loaded.Name
	pm := *loaded.Manifest
	pm.Requires = map[string]manifest.Require{}
	pl := lock.New()
	provides := func(modpack string, requiredBy []string, id string) bool {
		_, own := p.Manifest.Requires[id]
		return modpack == key || (!own && slices.Contains(requiredBy, key))
	}
	entry := func(id, kind string, project manifest.ID, providerName string) manifest.Require {
		req, ok := loaded.Manifest.Requires[id]
		if !ok {
			req = manifest.Require{}
			if kind != manifest.TypeMod {
				req.Type = kind
			}
		}
		req.Pin = manifest.ID{}
		if !project.IsZero() {
			req.Project = project
			req.Provider = ""
			if providerName != "" && providerName != p.Manifest.ProviderOrder()[0] {
				req.Provider = providerName
			}
		}
		return req
	}
	for id, m := range p.Lock.Mods {
		if !provides(m.Modpack, m.RequiredBy, id) {
			for i, by := range m.RequiredBy {
				if by == key {
					m.RequiredBy = slices.Delete(slices.Clone(m.RequiredBy), i, i+1)
					p.Lock.Mods[id] = m
					break
				}
			}
			continue
		}
		delete(p.Lock.Mods, id)
		m.Modpack = ""
		m.RequiredBy = slices.DeleteFunc(slices.Clone(m.RequiredBy), func(by string) bool { return by == key })
		pl.Mods[id] = m
		pm.Requires[id] = entry(id, manifest.TypeMod, m.Project, m.Provider)
	}
	for _, kind := range manifest.PackKinds {
		section := p.Lock.Packs(kind)
		for id, lp := range section {
			if lp.Modpack != key {
				continue
			}
			delete(section, id)
			lp.Modpack = ""
			pl.Packs(kind)[id] = lp
			pm.Requires[id] = entry(id, kind, lp.Project, lp.Provider)
		}
	}
	inc := &incoming{manifest: &pm, lock: pl, dir: loaded.Dir, hasBlocks: true, overrides: loaded.Overrides}
	if loaded.Dir != "" && loaded.Archive == nil {
		overrides, err := readOverrideFolders(loaded.Dir, loaded.Manifest)
		if err != nil {
			return nil, err
		}
		inc.overrides = overrides
	}
	return inc, nil
}
