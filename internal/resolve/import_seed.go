package resolve

import (
	"fmt"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/integrations"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
)

// seedFromSeedMods turns the overrides under the folder of each seed mod the pack places back into
// seeded files: <folder>/P moves to P in its own layer and P joins m's seedFiles, so an export puts
// it back. A plain P in the same layer or the shared one wins, since a launcher extracts it before
// the mod could seed it, and so does the first seed mod's default; the other is dropped with a
// warning.
func seedFromSeedMods(m *manifest.Manifest, l *lock.Lock, overrides []packarchive.Override, rep *Imported) []packarchive.Override {
	placed := map[string]bool{}
	for key := range l.Mods {
		placed[l.JarID(key)] = true
	}
	present := integrations.Match(placed, m.Integrations)
	shared := packarchive.LayerFor("both")
	taken := map[string]string{}
	for _, o := range overrides {
		taken[o.Layer+"/"+o.Path] = o.Path
	}
	kept := overrides[:0]
	shadowed := map[string][]string{}
	var folders []string
	for _, o := range overrides {
		mod, rel, ok := underSeedMod(o.Path, present)
		if !ok {
			kept = append(kept, o)
			continue
		}
		by, ok := taken[o.Layer+"/"+rel]
		if !ok {
			by, ok = taken[shared+"/"+rel]
		}
		if ok && by == rel {
			if _, seen := shadowed[mod.Folder]; !seen {
				folders = append(folders, mod.Folder)
			}
			shadowed[mod.Folder] = append(shadowed[mod.Folder], rel)
			continue
		}
		if ok {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s/%s: dropped, since %s already seeds %s", o.Layer, o.Path, by, rel))
			continue
		}
		taken[o.Layer+"/"+rel] = o.Path
		o.Path = rel
		kept = append(kept, o)
		if !m.Seeds(rel) {
			m.SeedFiles = append(m.SeedFiles, rel)
		}
		if rep.Seeded == nil {
			rep.Seeded = map[string][]string{}
		}
		rep.Seeded[mod.Folder] = append(rep.Seeded[mod.Folder], rel)
	}
	// The pack's own copy is extracted before the mod could seed its default, so each dropped
	// default is one row under its folder's warning.
	for _, folder := range folders {
		files := shadowed[folder]
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("Dropped %s in %s that the pack also ships\n%s", out.Count(len(files), "default", "defaults"), folder, strings.Join(files, "\n")))
	}
	for folder := range rep.Seeded {
		slices.Sort(rep.Seeded[folder])
	}
	return kept
}

func underSeedMod(p string, present map[string]bool) (integrations.SeedMod, string, bool) {
	for _, mod := range integrations.SeedMods {
		if rel, ok := strings.CutPrefix(p, mod.Folder+"/"); ok && present[mod.ID] && rel != "" {
			return mod, rel, true
		}
	}
	return integrations.SeedMod{}, "", false
}
