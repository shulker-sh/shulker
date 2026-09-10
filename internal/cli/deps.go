package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/andrewmast/shulker/internal/build"
	"github.com/andrewmast/shulker/internal/cache"
	"github.com/andrewmast/shulker/internal/fetch"
	"github.com/andrewmast/shulker/internal/meta"
	"github.com/andrewmast/shulker/internal/project"
	"github.com/andrewmast/shulker/internal/provider"
	"github.com/andrewmast/shulker/internal/provider/modrinth"
	"github.com/andrewmast/shulker/internal/resolve"
	"github.com/andrewmast/shulker/internal/server"
)

type deps struct {
	fetch     *fetch.Client
	cache     *cache.Cache
	providers map[string]provider.Provider
	meta      *resolve.Meta
	runtimes  *meta.Runtimes
}

func (a *app) deps() (*deps, error) {
	if a.d != nil {
		return a.d, nil
	}
	c, err := cache.Open()
	if err != nil {
		return nil, err
	}
	f := fetch.New(version)
	a.d = &deps{
		fetch:     f,
		cache:     c,
		providers: map[string]provider.Provider{"modrinth": modrinth.New(f)},
		meta:      &resolve.Meta{Piston: meta.NewPiston(f), Fabric: meta.NewFabric(f)},
		runtimes:  meta.NewRuntimes(f),
	}
	return a.d, nil
}

func (a *app) openProject() (*project.Project, error) {
	dir := a.dir
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return nil, err
		}
	}
	p, err := project.Open(dir)
	if err != nil {
		return nil, err
	}
	a.printer.LockStale = p.LockStale()
	return p, nil
}

func (a *app) resolver(p *project.Project) (*resolve.Resolver, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	return &resolve.Resolver{
		Manifest:  p.Manifest,
		Lock:      p.Lock,
		Providers: d.providers,
		Cache:     d.cache,
		Fetch:     d.fetch,
		Log:       a.progress,
	}, nil
}

func (a *app) builder(p *project.Project) (*build.Builder, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	return &build.Builder{Dir: p.Dir, Manifest: p.Manifest, Lock: p.Lock, LockPath: p.LockPath(), Cache: d.cache}, nil
}

func (a *app) managedJava(ctx context.Context, p *project.Project, refresh bool) (server.Runtime, error) {
	d, err := a.deps()
	if err != nil {
		return server.Runtime{}, err
	}
	opts := server.RuntimeOptions{Refresh: refresh, Progress: func(msg string) { a.progress("%s", msg) }}
	return server.EnsureRuntime(ctx, d.fetch, d.runtimes, d.cache.Dir, p.Lock.Java.Component, opts)
}

func (a *app) progress(format string, args ...any) {
	if !a.printer.JSON {
		fmt.Fprintf(a.printer.Stderr, format+"\n", args...)
	}
}

func (a *app) warn(warnings []string) {
	for _, w := range warnings {
		a.progress("warning: %s", w)
	}
}

func (a *app) commit(p *project.Project, r *resolve.Resolver) (*resolve.Validation, error) {
	v, err := r.Validate()
	if err != nil {
		return nil, err
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := p.SaveManifest(); err != nil {
		return nil, err
	}
	if err := p.SaveLock(); err != nil {
		return nil, err
	}
	a.printer.LockStale = false
	a.warn(v.Warnings)
	return v, nil
}
