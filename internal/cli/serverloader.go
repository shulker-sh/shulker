package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/server"
)

// installServerLoader runs the loader's own installer into a built server dir when the locked
// loader isn't installed there yet, recording it on the report. The build already placed every
// file the installer would download, so it runs offline and only generates the rest.
func (a *app) installServerLoader(ctx context.Context, p *project.Project, rep *build.Report) error {
	l, _ := loader.Lookup(p.Lock.Loader.Type)
	if l.ServerSetup != loader.ServerInstaller || rep.Side != "server" {
		return nil
	}
	want := build.InstalledLoader{Type: l.Name, Version: p.Lock.Loader.Version}
	if installed := build.LoadState(rep.Dir).InstalledLoader; installed != nil && *installed == want {
		if _, err := os.Stat(filepath.Join(rep.Dir, build.InstallerArgsFile(p.Lock))); err == nil {
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
	a.progress("installing %s %s", want.Type, want.Version)
	if err := a.installer(ctx, java.Path, d.cache.Object(p.Lock.Loader.Server.Sha512), []string{l.InstallServerFlag, dir, "--offline"}); err != nil {
		return a.keepInstallerOutput(err)
	}
	rep.InstalledLoader = &want
	return build.RecordLoader(rep.Dir, want)
}

// keepInstallerOutput saves a failed installer's whole output in shulker's cache and names the file
// at the end of the error.
func (a *app) keepInstallerOutput(err error) error {
	var failure *server.InstallerFailure
	if !errors.As(err, &failure) {
		return err
	}
	d, err := a.deps()
	if err != nil {
		return failure.Err
	}
	path, err := d.cache.InstallerLog(time.Now())
	if err == nil {
		err = fsutil.Write(path, []byte(failure.Output))
	}
	if err != nil {
		a.printer.Warn("installer output not saved: %v", err)
		return failure.Err
	}
	failure.Err.Message += "\nFull output: " + path
	failure.Err.Rows = append(failure.Err.Rows, out.Detail{Label: "Full output", Text: path})
	return failure.Err
}
