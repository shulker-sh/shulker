package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mojang"
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
	fetch     *fetch.Client
	cache     *cache.Cache
	providers provider.Providers
	meta      *resolve.Meta
	runtimes  *mojang.Runtimes
	// loaders is what every loader row reaches out with.
	loaders *loader.Remote
	players *player.Client
	// metaURLs replaces a launcher's metadata service, keyed by the URL its entry names, so a test
	// can point it at a fake.
	metaURLs map[string]string
	signin   *account.SignIn
	// resources is where the game store fetches asset objects from.
	resources string
}

// titles names providers as the user reads them, by whatever deps are open; a name stays a name
// when none are.
func (a *app) titles() provider.Providers {
	if d, err := a.deps(); err == nil {
		return d.providers
	}
	return nil
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
	cf := curseforge.Open(f, cfg.CurseForge.Key, c.Dir)
	providers := provider.Providers{mr.Name(): mr, cf.Name(): cf}
	loaders := &loader.Remote{Fetch: f, Cache: c, Log: a.progress, RunInstaller: a.installer}
	a.d = &deps{
		fetch:     f,
		cache:     c,
		providers: providers,
		loaders:   loaders,
		meta:      &resolve.Meta{Piston: mojang.NewPiston(f), Loaders: loaders},
		runtimes:  mojang.NewRuntimes(f),
		players:   player.New(f),
		signin:    account.NewSignIn(f),
		resources: game.MojangResources,
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
	return a.resolverFor(ctx, p, resolve.PackMode{})
}

func (a *app) resolverFor(ctx context.Context, p *project.Project, mode resolve.PackMode) (*resolve.Resolver, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	store, err := a.packStore(p)
	if err != nil {
		return nil, err
	}
	packs, warnings, err := resolve.OpenPacks(ctx, store, p, mode)
	if err != nil {
		return nil, err
	}
	a.warn(warnings)
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
		return r.LockHosted(ctx, store, p, key, entry)
	}
	if a.canPick() {
		r.AskUnlock = func(key, minecraft string) (bool, error) {
			return a.askYes(fmt.Sprintf("Unlock %s and resolve its mods for Minecraft %s?", key, minecraft))
		}
	}
	return r, nil
}

func (a *app) builder(ctx context.Context, p *project.Project) (*build.Builder, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	store, err := a.packStore(p)
	if err != nil {
		return nil, err
	}
	packs, warnings, err := resolve.OpenPacks(ctx, store, p, resolve.PackMode{})
	if err != nil {
		return nil, err
	}
	a.warn(warnings)
	return &build.Builder{Dir: p.Dir, Manifest: p.Manifest, Lock: p.Lock, LockPath: p.LockPath(), Cache: d.cache, Packs: packs, Providers: d.providers, Fetch: d.fetch, Log: a.progress}, nil
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
	return &pack.Store{Cache: d.cache, ProjectDir: p.Dir, Fetch: d.fetch, Log: a.progress, Warn: a.printer.Warn, Lock: p.Lock, Consume: consume, Obtain: obtain}, nil
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
	switch out.CodeOf(err) {
	case "runtime-unavailable":
		out.AsError(err).Rows = []out.Detail{fix}
	case "rosetta-required":
		e := out.AsError(err)
		e.Rows = append(e.Rows, fix)
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
