package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/config"
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
	entries, err = a.projectEntries(p, s)
	return entries, true, err
}

// hasSyncedInstances reports whether anything is synced from p: a registered instance or a
// detached build.
func (a *app) hasSyncedInstances(p *project.Project) (bool, error) {
	entries, err := a.projectEntries(p, instanceSelection{})
	if out.CodeOf(err) == "no-instances" {
		return false, nil
	}
	return len(entries) > 0, err
}

func (a *app) projectEntries(p *project.Project, s instanceSelection) (entries []instanceEntry, err error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(p.Dir)
	if err != nil {
		return nil, err
	}
	registry, err := a.loadInstances()
	if err != nil {
		return nil, err
	}
	var all []instanceEntry
	for _, in := range registry {
		if isSameDir(in.Source, dir) && !isSameDir(in.Dir, dir) {
			all = append(all, inspectInstance(in))
		}
	}
	lf, err := a.loadLocal(dir)
	if err != nil {
		return nil, err
	}
	for _, side := range p.Manifest.Sides() {
		for _, d := range lf.ExistingSyncDirs(side) {
			if isSameDir(d, dir) || slices.ContainsFunc(all, func(e instanceEntry) bool { return isSameDir(e.Dir, d) }) {
				continue
			}
			e := inspectInstance(config.Instance{Name: p.Manifest.DisplayName(side), Dir: d, Source: dir})
			e.ID = uniqueID(registry, "", filepath.Base(d), d)
			e.detached = true
			if e.Side == "" {
				e.Side = side
			}
			all = append(all, e)
		}
	}
	if len(all) == 0 {
		e := out.Errorf("no-instances", "nothing is synced from %s yet", dir)
		e.Help = "`shulker link prism` adds an instance and `shulker sync --into <dir>` a detached build, and `shulker sync --all` syncs every instance"
		return nil, e
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
		return nil, e
	}
	return entries, nil
}

// selectProjectDetached falls back, when no registered instance matches, to the id of a detached
// build among the project's entries, which only the project knows.
func (a *app) selectProjectDetached(query string, s instanceSelection, miss error) ([]instanceEntry, error) {
	if code := out.AsError(miss).Code; code != "instance-not-found" && code != "no-instances" {
		return nil, miss
	}
	dir := a.dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, miss
		}
		dir = wd
	}
	p, err := a.openProjectAt(dir)
	if err != nil {
		return nil, miss
	}
	entries, err := a.projectEntries(p, s)
	if err != nil {
		return nil, miss
	}
	var matches []instanceEntry
	for _, e := range entries {
		if e.detached && e.ID == query {
			matches = append(matches, e)
		}
	}
	if len(matches) > 1 {
		e := out.Errorf("ambiguous-instance", "%d detached builds match %s", len(matches), describeSelection(query, s))
		e.Help = "sync one with `shulker sync --into <dir>`"
		e.Candidates, e.Given = instanceCandidates(matches), query
		return nil, e
	}
	if len(matches) == 0 {
		return nil, miss
	}
	return matches, nil
}
