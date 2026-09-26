// Package sync brings a directory up to one side of a source: it fetches what the source's lock
// names, builds the side into the directory, and records on the directory, its instance file and
// its registry row that it follows that source. The CLI's sync, play, link and hook commands are
// its callers.
package sync

import (
	"context"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/env"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/saves"
)

// Env is what a sync reaches beyond the run's env: where the registry and the save groups live,
// how many automatic backups a group keeps, how it asks, and what it remembers between the
// syncs of one run.
type Env struct {
	*env.Env
	// Registry is the path of registry.json.
	Registry string
	Saves    saves.Roots
	// SaveBackups is how many automatic backups a save group keeps, play.saveBackups.
	SaveBackups int
	// AskUnlock is asked whether to unlock a modpack built for another Minecraft than the
	// project's; nil keeps the refusal.
	AskUnlock func(key, minecraft string) (bool, error)
	// AwaitDownloads is shown the files a fetch needs downloaded by hand into downloads, and
	// waits for them: skip carries on without the ones the lock can hold pending. Nil has nobody
	// to ask, so mods already pending stay left out and any other missing file fails the fetch.
	AwaitDownloads func(ctx context.Context, downloads string, missing *out.Error) (skip bool, err error)
	// backedUp is the save groups this run already backed up, so each is zipped once however
	// many syncs touch it.
	backedUp map[saves.Home]bool
}

func (e *Env) resolver(ctx context.Context, p *project.Project, mode resolve.PackMode) (*resolve.Resolver, error) {
	r, err := resolve.New(ctx, e.Env, p, mode)
	if err != nil {
		return nil, err
	}
	r.AskUnlock = e.AskUnlock
	return r, nil
}

func (e *Env) builder(ctx context.Context, p *project.Project) (*build.Builder, error) {
	packs, err := resolve.Packs(ctx, e.Env, p)
	if err != nil {
		return nil, err
	}
	return build.New(e.Env, p, packs), nil
}

func (e *Env) instances() ([]config.Instance, error) {
	return config.LoadInstances(e.Registry)
}

// registered is the registry row for a directory, when there is one.
func (e *Env) registered(dir string) (config.Instance, bool) {
	instances, err := e.instances()
	if err != nil {
		return config.Instance{}, false
	}
	if i, ok := config.FindInstance(instances, dir); ok {
		return instances[i], true
	}
	return config.Instance{}, false
}

func (e *Env) updateInstances(update func([]config.Instance) []config.Instance) bool {
	changed, err := config.UpdateInstances(e.Registry, update)
	if err != nil {
		e.Warn("registry not updated: %v", err)
		return false
	}
	return changed
}
