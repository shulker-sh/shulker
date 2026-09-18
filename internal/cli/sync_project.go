package cli

import (
	"errors"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

func (a *app) projectInstances(s instanceSelection) (entries []instanceEntry, inProject bool, err error) {
	p, err := a.openProject()
	if errors.Is(err, project.ErrNoManifest) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	if err := s.check(); err != nil {
		return nil, true, err
	}
	dir, err := filepath.Abs(p.Dir)
	if err != nil {
		return nil, true, err
	}
	registry, err := a.loadInstances()
	if err != nil {
		return nil, true, err
	}
	var all []instanceEntry
	for _, in := range registry {
		if sameDir(in.Source, dir) && !sameDir(in.Dir, dir) {
			all = append(all, inspectInstance(in))
		}
	}
	lf, err := local.Load(dir)
	if err != nil {
		return nil, true, err
	}
	for _, side := range p.Manifest.Sides() {
		for _, d := range lf.ExistingSyncDirs(side) {
			if sameDir(d, dir) || slices.ContainsFunc(all, func(e instanceEntry) bool { return sameDir(e.Dir, d) }) {
				continue
			}
			e := inspectInstance(config.Instance{Name: p.Manifest.DisplayName(side), Dir: d, Source: dir})
			e.ID = slugID(e.Name)
			if e.Target == "" {
				e.Target = side
			}
			if e.Side == "" {
				e.Side = side
			}
			all = append(all, e)
		}
	}
	if len(all) == 0 {
		return nil, true, out.Errorf("no-instances", "nothing is synced from %s yet; `shulker sync --into <dir>` or `shulker link prism` adds an instance, and `shulker sync --all` syncs every one", dir)
	}
	sortInstanceEntries(all)
	for _, e := range all {
		if s.admits(e) {
			entries = append(entries, e)
		}
	}
	if len(entries) == 0 {
		e := out.Errorf("instance-not-found", "no instance synced from %s matches %s", dir, describeSelection("", s))
		e.Candidates = instanceCandidates(all)
		return nil, true, e
	}
	return entries, true, nil
}
