package cli

import (
	"context"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/project"
)

// installServerLoader runs the loader's own installer into a built server dir when the locked
// loader isn't installed there yet, recording it on the report. The build already placed every
// file the installer would download, so it runs offline and only generates the rest.
func (a *app) installServerLoader(ctx context.Context, p *project.Project, rep *build.Report) error {
	l, _ := loader.Lookup(p.Lock.Loader.Type)
	t, err := p.Manifest.Target(rep.Target)
	if err != nil || l.InstallServerFlag == "" || t.Side != "server" {
		return err
	}
	want := build.InstalledLoader{Type: l.Name, Version: p.Lock.Loader.Version}
	if installed := build.LoadState(rep.Dir).Loader; installed != nil && *installed == want {
		if _, err := os.Stat(filepath.Join(rep.Dir, build.InstallerArgsFile(p.Lock.Loader))); err == nil {
			return nil
		}
	}
	d, err := a.deps()
	if err != nil {
		return err
	}
	java, err := a.projectJava(ctx, p)
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(rep.Dir)
	if err != nil {
		return err
	}
	a.progress("installing %s %s into %s", want.Type, want.Version, rep.Dir)
	if err := a.installer(ctx, java.Path, d.cache.Path(p.Lock.Loader.Server.Sha512), []string{l.InstallServerFlag, dir, "--offline"}); err != nil {
		return err
	}
	rep.InstalledLoader = &want
	return build.RecordLoader(rep.Dir, want)
}
