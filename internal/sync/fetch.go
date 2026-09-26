package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/server"
)

// FetchLocked puts the locked files the given sides use in the cache, every locked file with no
// sides, and the server jar and its Java runtime too when wantServer is set. It returns the names
// of what it fetched; a failed download is one of several joined errors unless the env fails fast.
func FetchLocked(ctx context.Context, e *Env, p *project.Project, sides []string, wantServer bool) ([]string, error) {
	r, err := e.resolver(ctx, p, resolve.PackMode{})
	if err != nil {
		return nil, err
	}
	fetched, dropWarnings, err := r.Install(ctx, sides...)
	e.WarnEach(dropWarnings)
	if err != nil {
		return nil, err
	}
	if fetched == nil {
		fetched = []string{}
	}
	if wantServer {
		jar, err := r.EnsureServerJar(ctx, r.Meta)
		if err != nil {
			return nil, err
		}
		if jar.WasFetched && p.Lock.Loader.Type == "" {
			fetched = append(fetched, "minecraft-server")
		} else if jar.WasFetched {
			fetched = append(fetched, p.Lock.Loader.Type+"-server-launcher")
		}
		if jar.ChangedLock {
			if err := p.Lock.Save(p.LockPath()); err != nil {
				return nil, err
			}
		}
		if p.Manifest.Java == "" {
			rt, err := FreshestJava(ctx, e, p, ServerJavaFix)
			if err != nil && out.CodeOf(err) != "runtime-unavailable" {
				return nil, err
			}
			if err != nil {
				e.Warn("%s", RuntimeWarning(err))
			} else if rt.Fetched {
				fetched = append(fetched, rt.Component+" "+rt.Version)
			}
		}
	}
	v, err := r.Validate(sides...)
	if err != nil {
		return nil, err
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	e.WarnEach(v.Warnings)
	return fetched, nil
}

// Players brings the lock's players in line with the manifest's, saving the lock when persist is
// set and otherwise leaving the change in memory for the build.
func Players(ctx context.Context, e *Env, p *project.Project, mode player.Mode, acceptChange, persist bool) error {
	refs := player.RefsOf(p.Manifest)
	if len(refs) == 0 && len(p.Lock.Players) == 0 {
		return nil
	}
	results, err := e.Players.Sync(ctx, refs, p.Lock.Players, mode)
	if err != nil {
		return err
	}
	warnings, err := player.Policy(results, acceptChange)
	e.WarnEach(warnings)
	if err != nil {
		return err
	}
	next := player.Apply(results, p.Lock.Players, acceptChange)
	if !persist || reflect.DeepEqual(next, p.Lock.Players) {
		p.Lock.Players = next
		return nil
	}
	p.Lock.Players = next
	return p.Lock.Save(p.LockPath())
}

// InstallServerLoader runs the loader's own installer into a built server dir when the locked
// loader isn't installed there yet, recording it on the report. The build already placed every
// file the installer would download, so it runs offline and only generates the rest.
func InstallServerLoader(ctx context.Context, e *Env, p *project.Project, rep *build.Report) error {
	l := loader.Running(p.Lock)
	if l.InstallServerFlag == "" || rep.Side != "server" {
		return nil
	}
	want := instance.InstalledLoader{Type: l.Name, Version: p.Lock.Loader.Version}
	if installed := instance.LoadState(rep.Dir).InstalledLoader; installed != nil && *installed == want {
		if _, err := os.Stat(filepath.Join(rep.Dir, l.InstalledServerFile(p.Lock))); err == nil {
			return nil
		}
	}
	java, err := ProjectJava(ctx, e, p)
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(rep.Dir)
	if err != nil {
		return err
	}
	if err := l.InstallServer(ctx, e.Loaders, p.Lock, dir, java.Path); err != nil {
		return KeepInstallerOutput(e, err)
	}
	rep.InstalledLoader = &want
	return instance.RecordLoader(rep.Dir, want)
}

// KeepInstallerOutput saves a failed installer's whole output in shulker's cache and names the
// file at the end of the error.
func KeepInstallerOutput(e *Env, err error) error {
	var failure *server.InstallerFailure
	if !errors.As(err, &failure) {
		return err
	}
	path, err := e.Cache.InstallerLog(time.Now())
	if err == nil {
		err = fsutil.Write(path, []byte(failure.Output))
	}
	if err != nil {
		e.Warn("installer output not saved: %v", err)
		return failure.Err
	}
	failure.Err.Message += "\nFull output: " + path
	failure.Err.Rows = append(failure.Err.Rows, out.Detail{Label: "Full output", Text: path})
	return failure.Err
}

// FeatureOverrides is the feature decisions a build takes: decisions with the named features
// turned on and off, once every name is a feature the build knows.
func FeatureOverrides(b *build.Builder, with, without []string, decisions map[string]bool) (map[string]bool, error) {
	for _, name := range with {
		if slices.Contains(without, name) {
			return nil, out.Errorf("usage", "--with and --without both name %s", name)
		}
	}
	known := FeatureNames(b.Features())
	for _, name := range append(slices.Clone(with), without...) {
		if !slices.Contains(known, name) {
			return nil, UnknownFeature(name, known)
		}
	}
	return build.FeatureOverrides(decisions, with, without), nil
}

func FeatureNames(features []build.Feature) []string {
	names := make([]string, 0, len(features))
	for _, f := range features {
		names = append(names, f.Name)
	}
	return names
}

func UnknownFeature(name string, known []string) error {
	fail := out.Errorf("feature-not-found", "no mod or feature declaration in shulker.json uses feature %q", name)
	fail.Candidates, fail.Given = known, name
	return fail
}
