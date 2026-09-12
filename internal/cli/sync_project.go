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

func (a *app) projectLinks(s linkSelection) (links []config.Link, inProject bool, err error) {
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
	registry, err := a.loadLinks()
	if err != nil {
		return nil, true, err
	}
	var all []config.Link
	for _, l := range registry {
		if sameDir(l.Source, dir) {
			all = append(all, l)
		}
	}
	lf, err := local.Load(dir)
	if err != nil {
		return nil, true, err
	}
	for _, name := range targetNames(p.Manifest.Targets) {
		for _, d := range lf.ExistingSyncDirs(name) {
			if !slices.ContainsFunc(all, func(l config.Link) bool { return sameDir(l.Dir, d) }) {
				all = append(all, config.Link{Side: p.Manifest.Targets[name].Side, Name: p.Manifest.DisplayName(name), Dir: d, Source: dir, Target: name})
			}
		}
	}
	if len(all) == 0 {
		return nil, true, out.Errorf("no-links", "nothing is synced from %s yet; `shulker sync --into <dir>` or `shulker link prism` adds an entry, and `shulker sync --all` syncs every entry", dir)
	}
	slices.SortStableFunc(all, compareLinks)
	for _, l := range all {
		if s.admits(l) {
			links = append(links, l)
		}
	}
	if len(links) == 0 {
		e := out.Errorf("instance-not-found", "no entry synced from %s matches %s", dir, describeSelection("", s))
		e.Candidates = linkCandidates(all)
		return nil, true, e
	}
	return links, true, nil
}
