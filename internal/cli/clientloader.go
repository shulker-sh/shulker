package cli

import (
	"context"

	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

// installClientLoader sets a launcher up by running the loader's own installer into it, the way
// NeoForge and Forge install a client. The installer writes a launcher profile of its own; shulker
// puts launcher_profiles.json back the way it was afterwards, so only shulker's profile shows, and
// reads the version id it installed off the entry it wrote.
func (a *app) installClientLoader(ctx context.Context, p *project.Project, launcherDir string, l loader.Loader) (string, error) {
	v := &launcher.Mojang{Dir: launcherDir}
	jar, err := a.clientInstaller(ctx, p)
	if err != nil {
		return "", err
	}
	java, err := a.projectJava(ctx, p)
	if err != nil {
		return "", err
	}
	if err := v.EnsureProfilesFile(); err != nil {
		return "", err
	}
	before, err := v.Profiles()
	if err != nil {
		return "", err
	}
	a.progress("installing %s %s", l.Name, p.Lock.Loader.Version)
	if err := a.installer(ctx, java.Path, jar, []string{l.InstallClientFlag, v.Dir}); err != nil {
		return "", a.keepInstallerOutput(err)
	}
	versionID, err := v.RestoreProfiles(before)
	if err != nil {
		return "", err
	}
	if versionID == "" {
		return "", out.Errorf("loader-install-incomplete", "the %s installer wrote no launcher profile, so shulker can't tell which version it installed", l.Name)
	}
	return versionID, nil
}

// clientInstaller is the path to the locked client installer jar, recording it in the lock the first
// time.
func (a *app) clientInstaller(ctx context.Context, p *project.Project) (string, error) {
	d, err := a.deps()
	if err != nil {
		return "", err
	}
	r, err := a.resolver(ctx, p)
	if err != nil {
		return "", err
	}
	jar, err := r.EnsureClientInstaller(ctx, d.meta)
	if err != nil {
		return "", err
	}
	if jar.ChangedLock {
		if err := p.Lock.Save(p.LockPath()); err != nil {
			return "", err
		}
	}
	return jar.Path, nil
}
