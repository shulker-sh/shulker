package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/meta"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/curseforge"
	"shulker.sh/shulker/internal/provider/modrinth"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/server"
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
	if out.CodeOf(err) == "config-invalid" {
		a.printer.Warn("%s; ignoring it", out.AsError(err).Message)
		cfg = config.Config{}
	} else if err != nil {
		return nil, err
	}
	f := fetch.New(version)
	providers := map[string]provider.Provider{"modrinth": modrinth.New(f)}
	if key := curseforge.Key(cfg.CurseForge.Key); key != "" {
		providers["curseforge"] = curseforge.New(f, key)
	} else if key := curseforge.SharedKey(c.Dir); key != "" {
		providers["curseforge"] = curseforge.NewShared(f, key, c.Dir)
	}
	a.d = &deps{
		fetch:     f,
		cache:     c,
		providers: providers,
		meta:      &resolve.Meta{Piston: meta.NewPiston(f), Fabric: meta.NewFabric(f), Quilt: meta.NewQuilt(f), NeoForge: meta.NewNeoForge(f), Forge: meta.NewForge(f), Cache: c},
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
		Meta:      d.meta,
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
	return &pack.Store{Cache: d.cache, ProjectDir: p.Dir, Fetch: d.fetch, Log: a.progress}, nil
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
		// A relock reads local packs as they are on disk: they have no version to hold back.
		if !ok || (a.relocking && pack.Classify(mp.Source) == pack.Local) {
			if !ok {
				name, _ := pack.Name(mp)
				a.printer.Warn("pack %s is not in the lock yet; resolving it", name)
			}
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
			a.printer.Warn("%s", warning)
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
	opts := server.RuntimeOptions{Refresh: refresh, Log: a.progress}
	return server.EnsureRuntime(ctx, d.fetch, d.runtimes, d.cache.Dir, p.Lock.Java.Component, opts)
}

func (a *app) progress(format string, args ...any) {
	if !a.printer.JSON {
		fmt.Fprintf(a.printer.Stderr, format+"\n", args...)
	}
}

func (a *app) warn(warnings []string) {
	for _, w := range warnings {
		a.printer.Warn("%s", w)
	}
}

func (a *app) warnFor(name string, several bool, warnings []string) {
	if several {
		defer a.scopeWarnings(name)()
	}
	a.warn(warnings)
}

func (a *app) scopeWarnings(name string) (restore func()) {
	prev := a.printer.WarnPrefix
	a.printer.WarnPrefix = name + ": "
	return func() { a.printer.WarnPrefix = prev }
}

func (a *app) requireLock(p *project.Project) error {
	if err := p.RequireLock(); err != nil {
		return err
	}
	a.warnLockDifferences(p)
	return nil
}

func (a *app) warnLockDifferences(p *project.Project) {
	if diffs := p.LockDifferences(); len(diffs) > 0 {
		a.printer.Warn("shulker.lock is out of date with shulker.json (%s); run `shulker lock`", strings.Join(diffs, "; "))
	}
}
