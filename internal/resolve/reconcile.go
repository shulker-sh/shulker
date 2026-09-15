package resolve

import (
	"context"
	"sort"

	"shulker.sh/shulker/internal/project"
)

func (r *Resolver) Reconcile(ctx context.Context) (reresolved []string, err error) {
	platform := project.PlatformDifferences(r.Manifest, r.Lock)
	if len(platform) > 0 {
		pf, err := r.Meta.Platform(ctx, r.Manifest)
		if err != nil {
			return nil, err
		}
		r.Lock.Minecraft, r.Lock.Loader, r.Lock.Java = pf.Minecraft, pf.Loader, pf.Java
	}
	if err := r.RefreshPacks(r.Packs); err != nil {
		return nil, err
	}
	for _, l := range r.Packs {
		r.dropRequiredBy(l.Name)
		for id := range l.Manifest.Mods() {
			r.Lock.AddRequiredBy(id, l.Name)
		}
	}
	if reasons := append(platform, project.ProviderDifferences(r.Manifest, r.Lock)...); len(reasons) > 0 {
		return reasons, r.Update(ctx, nil)
	}
	r.pruneOrphans()
	var targets []string
	for id, d := range r.directMods() {
		if m, ok := r.Lock.Mods[id]; !ok || len(project.ModDifferences(id, d.entry, m)) > 0 {
			targets = append(targets, id)
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	sort.Strings(targets)
	return nil, r.Update(ctx, targets)
}
