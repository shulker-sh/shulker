package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

// InPlaceProject is the project in dir when one of its sides builds into dir itself, which is
// what makes dir an instance rather than a project that builds elsewhere.
func InPlaceProject(dir string) (*project.Project, string, bool, error) {
	_, side, ok, err := project.InPlace(dir)
	if err != nil || !ok {
		return nil, "", false, err
	}
	p, err := project.Open(dir)
	if err != nil {
		return nil, "", false, err
	}
	return p, side, true, nil
}

// InPlace refreshes the modpacks that follow their source, relocks without moving the project's
// own mods, and builds the instance where it stands.
func InPlace(ctx context.Context, e *Env, p *project.Project, side string, req Request) (Result, error) {
	rl, err := e.relock(ctx, p, req)
	if err != nil {
		return Result{}, err
	}
	req.Side = side
	res, err := buildInPlace(ctx, e, p.Dir, req)
	if err != nil {
		return Result{}, err
	}
	res.Relock = &rl
	return res, nil
}

func (e *Env) relock(ctx context.Context, p *project.Project, req Request) (resolve.Relocked, error) {
	r, err := e.resolver(ctx, p, resolve.PackMode{IsRelocking: true, Linked: req.Linked})
	if err != nil {
		return resolve.Relocked{}, err
	}
	store := resolve.NewStore(e.Env, p)
	refresh := func(p *project.Project, r *resolve.Resolver) (string, error) {
		_, err := r.RefreshModpacks(ctx, store, p, manifest.Require.AutoUpdates, resolve.KeepUnreachable)
		return "", err
	}
	res, err := r.Relock(ctx, store, p, refresh, resolve.RelockOptions{KeepUnchanged: true, Reason: req.Reason})
	if err != nil {
		return resolve.Relocked{}, err
	}
	e.WarnEach(res.Warnings)
	return res, nil
}

func buildInPlace(ctx context.Context, e *Env, dir string, req Request) (Result, error) {
	src, err := Open(ctx, e, dir, modpack.At{})
	if err != nil {
		return Result{}, err
	}
	req.At, req.Into = modpack.At{}, ""
	return Run(ctx, e, src, req)
}

// Instance syncs a registered instance: in place when it is a project of its own, otherwise from
// the source its entry records.
func Instance(ctx context.Context, e *Env, entry project.InstanceEntry, req Request) (Result, error) {
	if l := launcher.Find(entry.Launcher); l != nil && l.IsInstanced {
		if _, err := os.Stat(l.InstanceDir(entry.Dir)); errors.Is(err, os.ErrNotExist) {
			fail := out.Errorf("instance-missing", "the %s instance %q is gone (%s)", launcher.Title(entry.Launcher), entry.Label(), l.InstanceDir(entry.Dir))
			fail.Help = fmt.Sprintf("`shulker unlink %s` forgets it", entry.ID)
			return Result{}, fail
		}
	}
	if p, side, ok, err := InPlaceProject(entry.Dir); err != nil {
		return Result{}, err
	} else if ok {
		return InPlace(ctx, e, p, side, req)
	}
	src, err := Open(ctx, e, entry.Source, modpack.At{Ref: entry.Ref, Path: entry.Path})
	if err != nil {
		return Result{}, err
	}
	req.Side, req.Into = entry.Side, entry.Dir
	req.AssumeClient = req.AssumeClient || entry.AssumesClient
	return Run(ctx, e, src, req)
}

// Recorded syncs the request's directory from the source its own instance file records, so a
// synced directory stays usable after the registry is gone.
func Recorded(ctx context.Context, e *Env, req Request) (Result, error) {
	into, err := filepath.Abs(req.Into)
	if err != nil {
		return Result{}, err
	}
	entry := project.Inspect(config.Instance{Dir: into})
	if entry.Source == "" {
		fail := out.Errorf("source-unknown", "%s has no record of what it was synced from", into)
		fail.Help = "name the source"
		return Result{}, fail
	}
	return Instance(ctx, e, entry, req)
}

// ForLaunch is the sync before a launch of dir, which never stands between the player and the
// game: it keeps the player's side of a conflict, a refresh that fails falls back to building the
// lock already there, and a build that fails leaves what is on disk. reason names the command for
// the history entry.
func ForLaunch(ctx context.Context, e *Env, dir, reason string) (Result, error) {
	req := Request{Into: dir, Backup: "sync", KeepConflicts: true, Reason: reason}
	p, side, inPlace, err := InPlaceProject(dir)
	switch {
	case err != nil:
		return Result{}, err
	case !inPlace:
		return Recorded(ctx, e, req)
	}
	req.Into = ""
	res, err := InPlace(ctx, e, p, side, req)
	if err == nil || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return res, err
	}
	e.Warn("couldn't update, building what the lock already has: %v", err)
	req.Side = side
	return buildInPlace(ctx, e, p.Dir, req)
}
