package sync

import (
	"context"

	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/project"
)

// LockedLoader is the row of the loader p's lock names; a vanilla lock names none and gets the
// zero row.
func LockedLoader(p *project.Project) (loader.Loader, error) {
	if p.Lock.Loader.Type == "" {
		return loader.Loader{}, nil
	}
	return loader.Require(p.Lock.Loader.Type)
}

// GameSources is what a store fill or a client install of p draws on: every client the env holds,
// the row the lock names, and the Java its installer would run with.
func GameSources(e *Env, p *project.Project) (game.Sources, error) {
	row, err := LockedLoader(p)
	if err != nil {
		return game.Sources{}, err
	}
	return game.Sources{
		Fetch:   e.Fetch,
		Piston:  e.Piston,
		Loader:  row,
		Loaders: e.Loaders,
		InstallerJava: func(ctx context.Context) (string, error) {
			java, err := ProjectJava(ctx, e, p)
			return java.Path, err
		},
		SaveLock: func() error { return p.Lock.Save(p.LockPath()) },
		Log:      e.Log,
		Progress: e.Progress,
	}, nil
}
