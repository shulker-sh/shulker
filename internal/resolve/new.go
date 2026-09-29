package resolve

import (
	"context"

	"shulker.sh/shulker/internal/env"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/project"
)

// New opens p's packs in mode and returns a resolver on e over them, warning about the packs it
// had to leave out. Its LockModpack replaces a hosted modpack through p's store.
func New(ctx context.Context, e *env.Env, p *project.Project, mode PackMode) (*Resolver, error) {
	store := NewStore(e, p)
	packs, warnings, err := OpenPacks(ctx, store, p, mode)
	if err != nil {
		return nil, err
	}
	e.WarnEach(warnings)
	r := NewAt(e, p.Dir)
	r.Manifest, r.Lock, r.Packs = p.Manifest, p.Lock, packs
	r.LockModpack = func(ctx context.Context, key string, entry manifest.Require) error {
		return r.LockHosted(ctx, store, p, key, entry)
	}
	return r, nil
}

// NewAt is a resolver on e for dir alone, as an import has before dir holds a project.
func NewAt(e *env.Env, dir string) *Resolver {
	return &Resolver{
		Dir:        dir,
		Providers:  e.Providers,
		Cache:      e.Cache,
		Fetch:      e.Fetch,
		Meta:       &Meta{Piston: e.Piston, Loaders: e.Loaders},
		Log:        e.Log,
		Note:       e.Note,
		Progress:   e.Progress,
		FailFast:   e.FailFast,
		EveryFetch: e.EveryFetch,
	}
}

// NewStore is p's modpack store on e: a hosted modpack is obtained and its archive consumed
// through a resolver over p's manifest.
func NewStore(e *env.Env, p *project.Project) *modpack.Store {
	consume := func(ctx context.Context, l *modpack.Loaded) error {
		r := NewAt(e, p.Dir)
		r.Manifest = p.Manifest
		return r.ConsumeArchive(ctx, l)
	}
	obtain := func(ctx context.Context, name string, entry manifest.Require) (lock.Modpack, error) {
		r := NewAt(e, p.Dir)
		r.Manifest, r.Lock = p.Manifest, p.Lock
		pin, err := r.ObtainModpack(ctx, name, entry)
		e.WarnEach(r.Warnings)
		return pin, err
	}
	return &modpack.Store{Cache: e.Cache, ProjectDir: p.Dir, Fetch: e.Fetch, Log: e.Log, Working: e.Working, Warn: e.Warn, WarnsRawURL: e.WarnsRawURL, Lock: p.Lock, Consume: consume, Obtain: obtain}
}

// Packs reads p's modpacks at their pins, once: a later call answers from what the first read.
func Packs(ctx context.Context, e *env.Env, p *project.Project) ([]*modpack.Loaded, error) {
	packs, warnings, err := OpenPacks(ctx, NewStore(e, p), p, PackMode{})
	if err != nil {
		return nil, err
	}
	e.WarnEach(warnings)
	return packs, nil
}
