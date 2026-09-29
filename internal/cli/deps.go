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
	"shulker.sh/shulker/internal/env"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/mojang"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/curseforge"
	"shulker.sh/shulker/internal/provider/modrinth"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/sync"
	"shulker.sh/shulker/schema"
)

type deps struct {
	*env.Env
	meta *resolve.Meta
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
		return d.Providers
	}
	return nil
}

func (a *app) deps() (*deps, error) {
	if a.d != nil {
		a.d.FailFast, a.d.EveryFetch = a.failFast, a.everyFetch
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
	f := fetch.New(a.build().Version)
	f.Waiting = a.printer.Waiting
	mr := modrinth.New(f)
	mr.Log = a.progress
	cf := curseforge.Open(f, cfg.CurseForge.Key, c.Dir)
	providers := provider.Providers{mr.Name(): mr, cf.Name(): cf}
	loaders := &loader.Remote{Fetch: f, Cache: c, Log: a.progress, RunInstaller: a.installer}
	a.d = a.newDeps(&env.Env{
		Fetch:     f,
		Cache:     c,
		Providers: providers,
		Loaders:   loaders,
		Piston:    mojang.NewPiston(f),
		Runtimes:  mojang.NewRuntimes(f),
		Players:   player.NewResolver(mojang.NewProfiles(f)),
		EULA:      cfg.EULA,
	})
	a.d.signin = account.NewSignIn(f)
	a.d.resources = game.MojangResources
	return a.d, nil
}

// newDeps completes e with the app's own sinks and flags and wraps it as the app's deps.
func (a *app) newDeps(e *env.Env) *deps {
	e.FailFast, e.EveryFetch = a.failFast, a.everyFetch
	e.Log = a.progress
	e.Note = a.printer.Note
	e.Working = a.printer.Working
	e.Progress = a.printer.Progress
	e.Warn = a.printer.Warn
	e.WarnNudge = a.printer.WarnNudge
	e.WarnsRawURL = a.warnsRawURL
	return &deps{Env: e, meta: &resolve.Meta{Piston: e.Piston, Loaders: e.Loaders}}
}

// warnRawURLs has this run warn that a raw manifest URL brings no overrides, for a command that
// adds or links a source.
func (a *app) warnRawURLs() {
	a.warnsRawURL = true
	if a.d != nil {
		a.d.WarnsRawURL = true
	}
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
	r, err := resolve.New(ctx, d.Env, p, mode)
	if err != nil {
		return nil, err
	}
	if a.asksYes() {
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
	packs, err := a.openPacks(ctx, p)
	if err != nil {
		return nil, err
	}
	return build.New(d.Env, p, packs), nil
}

// openPacks reads p's modpacks at their pins, once: a later call answers from what the first read.
func (a *app) openPacks(ctx context.Context, p *project.Project) ([]*modpack.Loaded, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	return resolve.Packs(ctx, d.Env, p)
}

func (a *app) packStore(p *project.Project) (*modpack.Store, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	return resolve.NewStore(d.Env, p), nil
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
	se, err := a.syncEnv()
	if err != nil {
		return err
	}
	return sync.RequireLock(se, p)
}

func (a *app) warnLockDifferences(p *project.Project) {
	if diffs := p.LockDifferences(); len(diffs) > 0 {
		a.printer.Warn("shulker.lock is out of date with shulker.json (%s); run `shulker lock`", strings.Join(diffs, "; "))
	}
}
