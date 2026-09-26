package sync

import (
	"context"
	"errors"
	"strings"

	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

// Source is a checkout of what a sync builds from, with the project it holds.
type Source struct {
	*modpack.Checkout
	// Name is the source as the user named it, or the directory of a local one.
	Name    string
	Project *project.Project
	// IsAuthor is a link's answers rather than a source: a project with no directory yet, which
	// the link writes into the instance it creates.
	IsAuthor bool
}

func (s *Source) IsRemote() bool { return s.Kind != modpack.Local }

// ForLink is the source as the project a link writes takes it.
func (s *Source) ForLink() *project.LinkSource {
	return &project.LinkSource{Checkout: s.Checkout, Name: s.Name, Project: s.Project, IsAuthor: s.IsAuthor}
}

// Store is the checkout store every source comes through.
func (e *Env) Store() *modpack.Store {
	return &modpack.Store{Cache: e.Cache, Fetch: e.Fetch, Log: e.Log, Warn: e.Warn}
}

// Open checks out from at at and opens the project there, which has to hold a lock.
func Open(ctx context.Context, e *Env, from string, at modpack.At) (*Source, error) {
	co, err := e.Store().Checkout(ctx, from, at)
	if err != nil {
		return nil, err
	}
	s := &Source{Checkout: co, Name: co.Source}
	if co.Kind == modpack.Local {
		s.Name = co.Dir
	}
	if co.Warning != "" {
		e.Warn("%s", co.Warning)
	}
	s.Project, err = project.Open(co.Dir)
	if errors.Is(err, project.ErrNoManifest) {
		return nil, out.Errorf("manifest-not-found", "no shulker.json in %s", s.Name)
	}
	if err != nil {
		return nil, err
	}
	if err := RequireLock(e, s.Project); err != nil {
		return nil, err
	}
	return s, nil
}

// RequireLock refuses a project without a lock, and warns about a lock that is out of date with
// the manifest or names files the cache no longer has.
func RequireLock(e *Env, p *project.Project) error {
	if err := p.RequireLock(); err != nil {
		return err
	}
	if diffs := p.LockDifferences(); len(diffs) > 0 {
		e.Warn("shulker.lock is out of date with shulker.json (%s); run `shulker lock`", strings.Join(diffs, "; "))
	}
	e.WarnEach(p.GoneFiles(e.Cache.Has))
	return nil
}
