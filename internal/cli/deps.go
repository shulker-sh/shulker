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
	"shulker.sh/shulker/internal/lock"
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
	"shulker.sh/shulker/schema"
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
	path, err := a.configFile()
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadFile(path)
	switch code := out.CodeOf(err); {
	case code == "config-invalid":
		a.printer.Warn("%s; ignoring it", out.AsError(err).Message)
		cfg = config.Config{}
	case code == "schema-newer":
		a.printer.WarnNudge(schema.UpdateNudge, "%s; ignoring it", out.AsError(err).Message)
		cfg = config.Config{}
	case err != nil:
		return nil, err
	}
	f := fetch.New(version)
	f.Waiting = a.printer.Waiting
	mr := modrinth.New(f)
	mr.Log = a.progress
	providers := map[string]provider.Provider{"modrinth": mr}
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
	a.logInstance(entries[0].ID)
	return entries[0].Dir, nil
}

func (a *app) openProjectAt(dir string) (*project.Project, error) {
	return a.openWith(project.Open, dir)
}

// openForLock opens the project the way `shulker lock` needs it: a lock that can't be read is left
// out, so the relock writes a fresh one over it, and a missing one starts empty.
func (a *app) openForLock() (*project.Project, error) {
	dir, err := a.scopeDir()
	if err != nil {
		return nil, err
	}
	p, err := a.openWith(project.OpenReplacingLock, dir)
	if err != nil {
		return nil, err
	}
	if p.Lock == nil {
		p.Lock = lock.New()
	}
	return p, nil
}

func (a *app) openWith(open func(string) (*project.Project, error), dir string) (*project.Project, error) {
	p, err := open(dir)
	if err != nil {
		return nil, err
	}
	a.printer.LockStale = p.IsLockStale()
	return p, nil
}

func (a *app) resolver(ctx context.Context, p *project.Project) (*resolve.Resolver, error) {
	return a.resolverFor(ctx, p, packMode{})
}

func (a *app) resolverFor(ctx context.Context, p *project.Project, mode packMode) (*resolve.Resolver, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	packs, err := a.openPacks(ctx, p, mode)
	if err != nil {
		return nil, err
	}
	r := &resolve.Resolver{
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
		FailFast:  a.failFast,
	}
	r.LockModpack = func(ctx context.Context, key string, entry manifest.Require) error {
		return a.lockHostedEntry(ctx, p, r, key, entry)
	}
	return r, nil
}

func (a *app) builder(ctx context.Context, p *project.Project) (*build.Builder, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	packs, err := a.openPacks(ctx, p, packMode{})
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
	consume := func(ctx context.Context, l *pack.Loaded) error {
		r := &resolve.Resolver{Dir: p.Dir, Manifest: p.Manifest, Providers: d.providers, Cache: d.cache, Fetch: d.fetch, Log: a.progress}
		return r.ConsumeArchive(ctx, l)
	}
	obtain := func(ctx context.Context, name string, entry manifest.Require) (lock.Modpack, error) {
		r := &resolve.Resolver{Dir: p.Dir, Manifest: p.Manifest, Lock: p.Lock, Providers: d.providers, Cache: d.cache, Fetch: d.fetch, Log: a.progress}
		pin, err := r.ObtainModpack(ctx, name, entry)
		a.warn(r.Warnings)
		return pin, err
	}
	return &pack.Store{Cache: d.cache, ProjectDir: p.Dir, Fetch: d.fetch, Log: a.progress, Lock: p.Lock, Consume: consume, Obtain: obtain}, nil
}

// packMode is how openPacks reads a project's modpacks. A relock reads local packs as they are on
// disk, since they have no version to hold back. linked is the modpack a link just pointed the
// project at, resolved without the warning a moved modpack gets.
type packMode struct {
	isRelocking bool
	linked      string
}

func (a *app) openPacks(ctx context.Context, p *project.Project, mode packMode) ([]*pack.Loaded, error) {
	prior := p.Packs
	if prior != nil && (prior.IsRelocking || !mode.isRelocking) {
		return prior.Loaded, nil
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
		moved := ok && (pinned.Source != mp.Source || pinned.Path != mp.Path || pinned.File != mp.File || (mp.IsHosted() && len(project.HostedDifferences(name, mp, pinned)) > 0))
		isFresh := !ok || moved
		// An earlier read already resolved a new or moved modpack, and holds the rest at the pins a
		// relock reads them at too, so a relock reads again only what it would take from disk.
		if prior != nil && (isFresh || !a.rereads(p, mp, pinned)) {
			if i := slices.IndexFunc(prior.Loaded, func(l *pack.Loaded) bool { return l.Name == name }); i >= 0 {
				loaded = append(loaded, prior.Loaded[i])
				continue
			}
		}
		if isFresh || (mode.isRelocking && a.rereads(p, mp, pinned)) {
			switch {
			case name == mode.linked:
			case !ok && len(p.Lock.Modpacks) > 0:
				a.printer.Warn("modpack %s is not in the lock yet; resolving it", name)
			case moved && mp.IsHosted():
				a.printer.Warn("modpack %s has changed since the lock; resolving it", name)
			case moved:
				a.printer.Warn("modpack %s has a new source since the lock; resolving it", name)
			}
			l, err := store.Resolve(ctx, name, mp)
			if err != nil {
				return nil, err
			}
			a.warnFor(name, true, l.Warnings)
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
	p.Packs = &project.OpenedPacks{Loaded: loaded, IsRelocking: mode.isRelocking}
	return loaded, nil
}

// replacePacks records packs a command resolved again in place of what openPacks read for p.
func replacePacks(p *project.Project, loaded []*pack.Loaded) {
	if p.Packs == nil {
		p.Packs = &project.OpenedPacks{}
	}
	p.Packs.Loaded = loaded
}

// rereads reports whether a relock reads a modpack afresh rather than at its pin: a local directory
// always, and an archive when archiveMoved says so, taking changed bytes only when it follows them.
func (a *app) rereads(p *project.Project, mp manifest.Require, pinned lock.Modpack) bool {
	switch pack.KindOf(mp) {
	case pack.Local:
		return true
	case pack.File:
		return archiveMoved(p, mp, pinned, mp.AutoUpdates())
	}
	return false
}

// archiveMoved reports whether an archive modpack has to be read again: it was locked or unlocked
// since, which the lock alone can't rebuild, or its bytes changed and followsBytes says to take them.
func archiveMoved(p *project.Project, mp manifest.Require, pinned lock.Modpack, followsBytes bool) bool {
	if isLocked := mp.Locked == nil || *mp.Locked; isLocked != pinned.UsesLock {
		return true
	}
	return followsBytes && len(project.FileDifferences(p.Dir, "", mp.File, pinned.File, pinned.Size, pinned.Sha512)) > 0
}

// unreachable is what refreshModpacks does with a modpack whose source can't be reached.
type unreachable int

const (
	failUnreachable unreachable = iota
	keepUnreachable
)

// refreshModpacks re-resolves the modpacks refresh picks from their sources, keeps the rest at
// their locked pins, and hands the result to the resolver. An archive is read again only when
// archiveMoved says there is something new in it, so an unchanged one needs no network. One whose
// source can't be reached stays as openPacks read it, at its pin, without holding back the others,
// when onUnreachable says so: sync does, but update was asked for new versions.
func (a *app) refreshModpacks(ctx context.Context, p *project.Project, r *resolve.Resolver, refresh func(manifest.Require) bool, onUnreachable unreachable) ([]*pack.Loaded, error) {
	store, err := a.packStore(p)
	if err != nil {
		return nil, err
	}
	modpacks := p.Manifest.Modpacks()
	loaded := make([]*pack.Loaded, 0, len(r.Packs))
	for _, l := range r.Packs {
		mp := modpacks[l.Name]
		if !refresh(mp) || (l.Kind == pack.File && !archiveMoved(p, mp, l.Pin, true)) {
			loaded = append(loaded, l)
			continue
		}
		fresh, err := store.Resolve(ctx, l.Name, mp)
		if err != nil && onUnreachable == keepUnreachable && ctx.Err() == nil && fetch.IsNetwork(err) {
			a.printer.Drop()
			a.printer.Warn("%s, keeping modpack %s at %s from the lock", offlineReason(store), l.Name, l.Pin.Label())
			loaded = append(loaded, l)
			continue
		}
		if err != nil {
			return nil, err
		}
		a.warnFor(l.Name, true, fresh.Warnings)
		loaded = append(loaded, fresh)
	}
	if err := r.RefreshPacks(loaded); err != nil {
		return nil, err
	}
	replacePacks(p, loaded)
	return loaded, nil
}

func offlineReason(store *pack.Store) string {
	if store.Fetch != nil && store.Fetch.Offline {
		return "--offline"
	}
	return "offline"
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
	return out.Detail{Label: "Fix", Text: "shulker link " + launcherName + " --java <path>", IsCommand: true}
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
	d, err := a.deps()
	if err != nil {
		return err
	}
	a.warn(p.GoneFiles(d.cache.Has))
	return nil
}

func (a *app) warnLockDifferences(p *project.Project) {
	if diffs := p.LockDifferences(); len(diffs) > 0 {
		a.printer.Warn("shulker.lock is out of date with shulker.json (%s); run `shulker lock`", strings.Join(diffs, "; "))
	}
}
