package cli

import (
	"context"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/project"
)

// installServerLoader runs the loader's own installer into a built server dir when the locked
// loader isn't installed there yet, recording it on the report. It returns whether the installer
// jar was downloaded.
func (a *app) installServerLoader(ctx context.Context, p *project.Project, rep *build.Report) (bool, error) {
	l, _ := loader.Lookup(p.Lock.Loader.Type)
	t, err := p.Manifest.Target(rep.Target)
	if err != nil || l.InstallServerFlag == "" || t.Side != "server" {
		return false, err
	}
	want := build.InstalledLoader{Type: l.Name, Version: p.Lock.Loader.Version}
	installed := build.LoadState(rep.Dir).Loader
	if installed != nil && *installed == want {
		if _, err := os.Stat(filepath.Join(rep.Dir, build.InstallerArgsFile(p.Lock.Loader))); err == nil {
			return false, nil
		}
	}
	d, err := a.deps()
	if err != nil {
		return false, err
	}
	r, err := a.resolver(ctx, p)
	if err != nil {
		return false, err
	}
	jar, err := r.EnsureServerJar(ctx, d.meta)
	if err != nil {
		if installed != nil && fetch.IsNetwork(err) {
			a.printer.Warn("offline, keeping %s %s installed in %s", installed.Type, installed.Version, rep.Dir)
			return false, nil
		}
		return false, err
	}
	if jar.Locked {
		if err := p.Lock.Save(p.LockPath()); err != nil {
			return jar.Fetched, err
		}
	}
	java, err := a.serveJava(ctx, p)
	if err != nil {
		return jar.Fetched, err
	}
	dir, err := filepath.Abs(rep.Dir)
	if err != nil {
		return jar.Fetched, err
	}
	a.progress("installing %s %s into %s", want.Type, want.Version, rep.Dir)
	if err := a.installer(ctx, java.Path, d.cache.Path(p.Lock.Loader.Server.Sha512), []string{l.InstallServerFlag, dir}); err != nil {
		return jar.Fetched, err
	}
	rep.InstalledLoader = &want
	return jar.Fetched, build.RecordLoader(rep.Dir, want)
}
