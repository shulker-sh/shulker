package cli

import (
	"context"
	"maps"
	"os"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/manifest"
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
	fetch      *fetch.Client
	cache      *cache.Cache
	providers  map[string]provider.Provider
	meta       *resolve.Meta
	runtimes   *meta.Runtimes
	players    *player.Client
	gdlauncher *meta.GDLauncher
	signin     *account.SignIn
	// resources is where the game store fetches asset objects from.
	resources string
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
	f.Waiting = a.printer.Waiting
	providers := map[string]provider.Provider{"modrinth": modrinth.New(f)}
	if key := curseforge.Key(cfg.CurseForge.Key); key != "" {
		providers["curseforge"] = curseforge.New(f, key)
	} else if key := curseforge.SharedKey(c.Dir); key != "" {
		providers["curseforge"] = curseforge.NewShared(f, key, c.Dir)
	}
	a.d = &deps{
		fetch:      f,
		cache:      c,
		providers:  providers,
		meta:       &resolve.Meta{Piston: meta.NewPiston(f), Fabric: meta.NewFabric(f), Quilt: meta.NewQuilt(f), NeoForge: meta.NewNeoForge(f), Forge: meta.NewForge(f), Cache: c},
		runtimes:   meta.NewRuntimes(f),
		players:    player.New(f),
		gdlauncher: meta.NewGDLauncher(f),
		signin:     account.NewSignIn(f),
		resources:  game.MojangResources,
	}
	return a.d, nil
}

func (a *app) openProject() (*project.Project, error) {
	dir, err := a.scopeDir()
	if err != nil {
		return nil, err
	}
	return a.openProjectAt(dir)
}

// scopeDir is the directory a command acts on: -i resolves a registered instance to its own
// directory, -C names one, and neither means the current one.
func (a *app) scopeDir() (string, error) {
	if a.instance == "" {
		if a.dir != "" {
			return a.dir, nil
		}
		return os.Getwd()
	}
	if a.dir != "" {
		return "", out.Errorf("usage", "pass -C or -i, not both: -i already says which directory to act on")
	}
	entries, err := a.selectInstances(a.instance, instanceSelection{})
	if err != nil {
		return "", err
	}
	return entries[0].Dir, nil
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
	if err := p.Manifest.CheckSupported(); err != nil {
		return nil, err
	}
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
		Progress:  a.printer.Progress,
	}, nil
}

func (a *app) builder(ctx context.Context, p *project.Project) (*build.Builder, error) {
	if err := p.Manifest.CheckSupported(); err != nil {
		return nil, err
	}
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
	store, err := a.packStore(p)
	if err != nil {
		return nil, err
	}
	modpacks := p.Manifest.Modpacks()
	loaded := []*pack.Loaded{}
	for _, name := range slices.Sorted(maps.Keys(modpacks)) {
		mp := modpacks[name]
		pinned, ok := p.Lock.Modpacks[name]
		moved := ok && pinned.Source != mp.Source
		// A relock reads local packs as they are on disk: they have no version to hold back.
		if !ok || moved || (a.relocking && pack.Classify(mp.Source) == pack.Local) {
			switch {
			case name == a.linkedPack:
			case !ok && len(p.Lock.Modpacks) > 0:
				a.printer.Warn("modpack %s is not in the lock yet; resolving it", name)
			case moved:
				a.printer.Warn("modpack %s has a new source since the lock; resolving it", name)
			}
			l, err := store.Resolve(ctx, name, mp)
			if err != nil {
				return nil, err
			}
			loaded = append(loaded, l)
			continue
		}
		l, warning, err := store.Open(ctx, name, mp, pinned)
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

// refreshModpacks re-resolves the modpacks refresh picks from their sources, keeps the rest at
// their locked pins, and hands the result to the resolver.
func (a *app) refreshModpacks(ctx context.Context, p *project.Project, r *resolve.Resolver, refresh func(manifest.Require) bool) ([]*pack.Loaded, error) {
	store, err := a.packStore(p)
	if err != nil {
		return nil, err
	}
	modpacks := p.Manifest.Modpacks()
	loaded := make([]*pack.Loaded, 0, len(r.Packs))
	for _, l := range r.Packs {
		mp := modpacks[l.Name]
		if !refresh(mp) {
			loaded = append(loaded, l)
			continue
		}
		fresh, err := store.Resolve(ctx, l.Name, mp)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, fresh)
	}
	if err := r.RefreshPacks(loaded); err != nil {
		return nil, err
	}
	a.packs = loaded
	return loaded, nil
}

// managedJava ensures the lock's runtime component. fix is the Fix row a runtime-unavailable error
// carries, which depends on which side needs the Java.
func (a *app) managedJava(ctx context.Context, p *project.Project, refresh bool, fix out.Detail) (server.Runtime, error) {
	d, err := a.deps()
	if err != nil {
		return server.Runtime{}, err
	}
	opts := server.RuntimeOptions{Refresh: refresh, Log: a.progress}
	rt, err := server.EnsureRuntime(ctx, d.fetch, d.runtimes, d.cache.Dir, p.Lock.Java.Component, opts)
	if out.CodeOf(err) == "runtime-unavailable" {
		out.AsError(err).Rows = []out.Detail{fix}
	}
	return rt, err
}

var serverJavaFix = out.Detail{Label: "Fix", Text: `set "java" in shulker.json to a JDK path`}

func linkJavaFix(launcherName string) out.Detail {
	return out.Detail{Label: "Fix", Text: "shulker link " + launcherName + " --java <path>", Command: true}
}

// runtimeWarning is a runtime-unavailable error as one line, for a command that carries on without
// the managed runtime and so never shows the error's rows.
func runtimeWarning(err error) string {
	e := out.AsError(err)
	if len(e.Rows) == 0 {
		return e.Message
	}
	return e.Message + "; " + e.Rows[0].Text
}

func (a *app) progress(format string, args ...any) {
	a.printer.Step(format, args...)
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
