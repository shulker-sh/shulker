package cli

import (
	"context"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/sync"
)

func (a *app) installServerLoader(ctx context.Context, p *project.Project, rep *build.Report) error {
	se, err := a.syncEnv()
	if err != nil {
		return err
	}
	return sync.InstallServerLoader(ctx, se, p, rep)
}

func (a *app) keepInstallerOutput(err error) error {
	se, envErr := a.syncEnv()
	if envErr != nil {
		return err
	}
	return sync.KeepInstallerOutput(se, err)
}
