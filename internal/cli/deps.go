package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/andrewmast/shulker/internal/build"
	"github.com/andrewmast/shulker/internal/cache"
	"github.com/andrewmast/shulker/internal/config"
	"github.com/andrewmast/shulker/internal/fetch"
	"github.com/andrewmast/shulker/internal/meta"
	"github.com/andrewmast/shulker/internal/pack"
	"github.com/andrewmast/shulker/internal/player"
	"github.com/andrewmast/shulker/internal/project"
	"github.com/andrewmast/shulker/internal/provider"
	"github.com/andrewmast/shulker/internal/provider/curseforge"
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
	players   *player.Client
}

func (a *app) deps() (*deps, error) {
	if a.d != nil {
		return a.d, nil
	}
	c, err := cache.Open()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	f := fetch.New(version)
	providers := map[string]provider.Provider{"modrinth": modrinth.New(f)}
	if key := curseforge.Key(cfg.CurseForge.Key); key != "" {
		providers["curseforge"] = curseforge.New(f, key)
	}
	a.d = &deps{
		fetch:     f,
		cache:     c,
		providers: providers,
		meta:      &resolve.Meta{Piston: meta.NewPiston(f), Fabric: meta.NewFabric(f)},
		runtimes:  meta.NewRuntimes(f),
		players:   player.New(f),
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
	return a.openProjectAt(dir)
}

func (a *app) openProjectAt(dir string) (*project.Project, error) {
	p, err := project.Open(dir)
	if err != nil {
		return nil, err
	}
	a.printer.LockStale = p.LockStale()
	return p, nil
}

func (a *app) resolver(ctx context.Context, p *project.Project) (*resolve.Resolver, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	packs, err := a.openPacks(ctx, p)
	if err != nil {
		return nil, err
	}
	return &resolve.Resolver{
		Dir:       p.Dir,
		Manifest:  p.Manifest,
		Lock:      p.Lock,
		Providers: d.providers,
		Cache:     d.cache,
		Fetch:     d.fetch,
		Packs:     packs,
		Log:       a.progress,
	}, nil
}

func (a *app) builder(ctx context.Context, p *project.Project) (*build.Builder, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	packs, err := a.openPacks(ctx, p)
	if err != nil {
		return nil, err
	}
	return &build.Builder{Dir: p.Dir, Manifest: p.Manifest, Lock: p.Lock, LockPath: p.LockPath(), Cache: d.cache, Packs: packs}, nil
}

func (a *app) packStore(p *project.Project) (*pack.Store, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	return &pack.Store{CacheDir: d.cache.Dir, ProjectDir: p.Dir, Fetch: d.fetch, Log: a.progress}, nil
}

func (a *app) openPacks(ctx context.Context, p *project.Project) ([]*pack.Loaded, error) {
	if a.packs != nil {
		return a.packs, nil
	}
	if _, err := pack.Names(p.Manifest.Packs); err != nil {
		return nil, err
	}
	store, err := a.packStore(p)
	if err != nil {
		return nil, err
	}
	loaded := []*pack.Loaded{}
	for _, mp := range p.Manifest.Packs {
		pinned, ok := p.Lock.Packs[mp.Source]
		if !ok {
			name, _ := pack.Name(mp)
			a.progress("warning: pack %s is not in the lock yet; resolving it", name)
			l, err := store.Resolve(ctx, mp)
			if err != nil {
				return nil, err
			}
			loaded = append(loaded, l)
			continue
		}
		l, warning, err := store.Open(ctx, mp, pinned)
		if err != nil {
			return nil, err
		}
		if warning != "" {
			a.progress("warning: %s", warning)
		}
		loaded = append(loaded, l)
	}
	a.packs = loaded
	return loaded, nil
}

func (a *app) resolvePacks(ctx context.Context, p *project.Project) ([]*pack.Loaded, error) {
	if _, err := pack.Names(p.Manifest.Packs); err != nil {
		return nil, err
	}
	store, err := a.packStore(p)
	if err != nil {
		return nil, err
	}
	loaded := []*pack.Loaded{}
	for _, mp := range p.Manifest.Packs {
		l, err := store.Resolve(ctx, mp)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, l)
	}
	a.packs = loaded
	return loaded, nil
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
	if err := r.RefreshPacks(r.Packs); err != nil {
		return nil, err
	}
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
