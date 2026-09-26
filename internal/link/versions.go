package link

import (
	"context"
	"encoding/json"

	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/sync"
)

// versions is what a link hands a launcher to install the locked platform: the versions the
// env's Mojang and loader clients can fetch or build.
type versions struct {
	e   *Env
	p   *project.Project
	row loader.Loader
}

func (v versions) Vanilla(ctx context.Context) (json.RawMessage, error) {
	return v.e.Piston.Version(ctx, v.p.Lock.Minecraft)
}

func (v versions) HasInstaller() bool { return v.row.HasInstaller() }

func (v versions) LoaderProfile(ctx context.Context) (json.RawMessage, error) {
	meta := &resolve.Meta{Piston: v.e.Piston, Loaders: v.e.Loaders}
	return meta.LoaderProfile(ctx, v.p.Lock.Loader, v.p.Lock.Minecraft)
}

func (v versions) InstallClient(ctx context.Context, launcherDir string) (string, error) {
	src, err := sync.GameSources(v.e.Env, v.p)
	if err != nil {
		return "", err
	}
	id, err := game.InstallLoader(ctx, launcherDir, v.p.Lock, src)
	if err != nil {
		return "", sync.KeepInstallerOutput(v.e.Env, err)
	}
	return id, nil
}

func (v versions) InstallerVersion(ctx context.Context) (json.RawMessage, error) {
	raw, changed, err := v.row.InstallerVersion(ctx, v.e.Loaders, v.p.Lock)
	return raw, v.p.SaveIfChanged(changed, err)
}
