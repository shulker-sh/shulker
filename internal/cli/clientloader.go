package cli

import (
	"context"

	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/project"
)

// installClientLoader sets a launcher up by running the loader's own installer into it, and
// returns the version id the installer wrote. The row locks the installer jar the first time, so
// the lock is saved whether or not the run succeeds.
func (a *app) installClientLoader(ctx context.Context, p *project.Project, launcherDir string, l loader.Loader) (string, error) {
	d, err := a.deps()
	if err != nil {
		return "", err
	}
	java, err := a.projectJava(ctx, p)
	if err != nil {
		return "", err
	}
	v := &launcher.Mojang{Dir: launcherDir}
	id, err := v.InstallLoader(l.Name, func() error {
		changed, err := l.InstallClient(ctx, d.loaders, p.Lock, launcherDir, java.Path)
		return saveChangedLock(p, changed, err)
	})
	if err != nil {
		return "", a.keepInstallerOutput(err)
	}
	return id, nil
}

// saveChangedLock saves the lock when a row call changed it, whether or not the call succeeded,
// since what it locked stays valid; the call's own error wins over a save failure.
func saveChangedLock(p *project.Project, changed bool, err error) error {
	if !changed {
		return err
	}
	if saveErr := p.Lock.Save(p.LockPath()); saveErr != nil && err == nil {
		return saveErr
	}
	return err
}
