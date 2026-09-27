package cli

import (
	"errors"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

func (a *app) projectInstances(s instanceSelection) (entries []project.InstanceEntry, inProject bool, err error) {
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

// syncedFrom is everything synced from p, registered instances and detached builds alike.
func (a *app) syncedFrom(p *project.Project) ([]project.InstanceEntry, error) {
	dir, err := filepath.Abs(p.Dir)
	if err != nil {
		return nil, err
	}
	registry, err := a.loadInstances()
	if err != nil {
		return nil, err
	}
	lf, err := a.loadLocal(dir)
	if err != nil {
		return nil, err
	}
	return project.SyncedFrom(p, registry, lf)
}

func (a *app) projectEntries(p *project.Project, s instanceSelection) ([]project.InstanceEntry, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(p.Dir)
	if err != nil {
		return nil, err
	}
	all, err := a.syncedFrom(p)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		e := out.Errorf("no-instances", "nothing is synced from %s yet", dir)
		e.Nudge = out.Nudge{Lead: "Link it first", Command: "shulker link <launcher>"}
		return nil, e
	}
	project.SortInstances(all)
	entries := s.Narrow(all)
	if len(entries) == 0 {
		e := out.Errorf("instance-not-found", "no instance synced from %s matches %s", dir, describeSelection("", s))
		e.Candidates = instanceCandidates(all)
		return nil, e
	}
	return entries, nil
}

// selectProjectDetached falls back, when no registered instance matches, to the id of a detached
// build among the project's entries, which only the project knows.
func (a *app) selectProjectDetached(query string, s instanceSelection, miss error) ([]project.InstanceEntry, error) {
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
	for _, e := range entries {
		if e.Detached && e.ID == query {
			return []project.InstanceEntry{e}, nil
		}
	}
	return nil, miss
}
