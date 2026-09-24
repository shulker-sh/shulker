package project

import (
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/local"
)

// SyncedFrom lists what is synced from p: the registry rows that name it as their source, then
// the sync directories its local file records that have no row, as detached builds under ids no
// row holds. The project's own directory is never among them, however it was synced.
func SyncedFrom(p *Project, registry []config.Instance, lf *local.File) ([]InstanceEntry, error) {
	dir, err := filepath.Abs(p.Dir)
	if err != nil {
		return nil, err
	}
	var all []InstanceEntry
	for _, in := range registry {
		if config.SameDir(in.Source, dir) && !config.SameDir(in.Dir, dir) {
			all = append(all, Inspect(in))
		}
	}
	taken := slices.Clone(registry)
	for _, side := range p.Manifest.Sides() {
		for _, d := range lf.ExistingSyncDirs(side) {
			if config.SameDir(d, dir) || slices.ContainsFunc(all, func(e InstanceEntry) bool { return config.SameDir(e.Dir, d) }) {
				continue
			}
			e := Inspect(config.Instance{Name: p.Manifest.DisplayName(side), Dir: d, Source: dir})
			e.ID = config.InstanceID(taken, "", filepath.Base(d), d)
			taken = append(taken, config.Instance{ID: e.ID, Dir: d})
			e.Detached = true
			if e.Side == "" {
				e.Side = side
			}
			all = append(all, e)
		}
	}
	return all, nil
}
