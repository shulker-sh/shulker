package build

import (
	"shulker.sh/shulker/internal/env"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/project"
)

// New is a builder on e for p, over the packs already opened at their pins.
func New(e *env.Env, p *project.Project, packs []*modpack.Loaded) *Builder {
	return &Builder{Dir: p.Dir, Manifest: p.Manifest, Lock: p.Lock, LockPath: p.LockPath(), Cache: e.Cache, Packs: packs, Providers: e.Providers, Fetch: e.Fetch, Log: e.Log, EULA: e.EULA}
}
