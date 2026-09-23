package resolve

import (
	"context"
	"slices"
	"sort"

	"shulker.sh/shulker/internal/project"
)

// Reconcile brings the lock in line with the manifest. A platform or provider order that moved
// re-resolves every mod and returns why; otherwise only the mods whose entries changed re-resolve.
func (r *Resolver) Reconcile(ctx context.Context) (reresolved []string, err error) {
	platform := project.PlatformDifferences(r.Manifest, r.Lock)
	inherited, err := r.inheritedDifferences()
	if err != nil {
		return nil, err
	}
	platform = append(platform, inherited...)
	if len(platform) > 0 {
		pf, err := r.Meta.Platform(ctx, r.Manifest, r.Packs)
		if err != nil {
			return nil, err
		}
		if pf != nil {
			r.Lock.Minecraft, r.Lock.DataVersion, r.Lock.Loader, r.Lock.Java = pf.Minecraft, 0, pf.Loader, pf.Java
		}
	}
	r.fillDataVersion(ctx)
	if err := r.RefreshPacks(r.Packs); err != nil {
		return nil, err
	}
	for _, l := range r.Packs {
		r.dropRequiredBy(l.Name)
		for id := range l.Manifest.Mods() {
			r.Lock.AddRequiredBy(id, l.Name)
		}
	}
	if err := r.applyLockedPacks(); err != nil {
		return nil, err
	}
	if err := r.reconcilePacks(ctx); err != nil {
		return nil, err
	}
	if err := r.checkPackFilenames(); err != nil {
		return nil, err
	}
	if err := r.checkLocalFiles(); err != nil {
		return nil, err
	}
	if reasons := append(platform, project.ProviderDifferences(r.Manifest, r.Lock)...); len(reasons) > 0 {
		return reasons, r.Update(ctx, nil)
	}
	r.pruneOrphans()
	var targets []string
	for id, d := range r.directMods() {
		if d.locked != "" {
			continue
		}
		if m, ok := r.Lock.Mods[id]; !ok || len(project.ModDifferences(r.fileDir(id, "", d.entry.File), id, d.entry, m)) > 0 {
			targets = append(targets, id)
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	sort.Strings(targets)
	return nil, r.Update(ctx, targets)
}

// fillDataVersion gives a lock that has a Minecraft version but no data version one.
func (r *Resolver) fillDataVersion(ctx context.Context) {
	if r.Lock.Minecraft == "" || r.Lock.DataVersion != 0 {
		return
	}
	dataVersion, warning := r.Meta.DataVersion(ctx, r.Lock.Minecraft)
	r.Lock.DataVersion = dataVersion
	if warning != "" && !slices.Contains(r.Warnings, warning) {
		r.Warnings = append(r.Warnings, warning)
	}
}
