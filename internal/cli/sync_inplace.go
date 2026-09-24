package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

// inPlaceProject is the project in dir when one of its sides builds into dir itself, which is
// what makes dir an instance rather than a project that builds elsewhere.
func (a *app) inPlaceProject(dir string) (*project.Project, string, bool, error) {
	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); errors.Is(err, os.ErrNotExist) {
		return nil, "", false, nil
	} else if err != nil {
		return nil, "", false, err
	}
	p, err := a.openProjectAt(dir)
	if err != nil {
		return nil, "", false, err
	}
	side, ok := p.Manifest.InPlaceSide()
	return p, side, ok, nil
}

// syncInPlace refreshes the modpacks that follow their source, relocks without moving the
// project's own mods, and builds the instance where it stands.
func (a *app) syncInPlace(cmd *cobra.Command, p *project.Project, side string, req syncRequest) (syncResult, error) {
	rl, err := a.relockProject(cmd, p, relockOptions{keepUnchanged: true, linked: req.linked}, func(p *project.Project, r *resolve.Resolver) (string, error) {
		store, err := a.packStore(p)
		if err != nil {
			return "", err
		}
		_, err = r.RefreshModpacks(cmd.Context(), store, p, manifest.Require.AutoUpdates, resolve.KeepUnreachable)
		return "", err
	})
	if err != nil {
		return syncResult{}, err
	}
	req.side = side
	res, err := a.buildInPlace(cmd.Context(), p.Dir, req)
	if err != nil {
		return syncResult{}, err
	}
	if rl.wasSaved {
		res.Changes = &rl.lockChanges
	}
	return res, nil
}

func (a *app) buildInPlace(ctx context.Context, dir string, req syncRequest) (syncResult, error) {
	src, err := a.openSource(ctx, dir, pack.At{})
	if err != nil {
		return syncResult{}, err
	}
	req.at, req.into = pack.At{}, ""
	return a.sync(ctx, src, req)
}

// syncInPlaceForLaunch never stands between the player and the game: a refresh that fails falls
// back to building the lock already there, and a build that fails leaves what is on disk.
func (a *app) syncInPlaceForLaunch(cmd *cobra.Command, p *project.Project, side string) (syncResult, error) {
	res, err := a.syncInPlace(cmd, p, side, syncRequest{backup: "sync", keepConflicts: true})
	if err == nil || errors.Is(cmd.Context().Err(), context.DeadlineExceeded) {
		return res, err
	}
	a.printer.Drop()
	a.printer.Warn("couldn't update, building what the lock already has: %v", err)
	return a.buildInPlace(cmd.Context(), p.Dir, syncRequest{side: side, backup: "sync", keepConflicts: true})
}

// syncTree syncs an instance that is also a source, then every directory built from it, since
// those would otherwise keep building from the lock it just replaced.
func (a *app) syncTree(cmd *cobra.Command, p *project.Project, side string, sel instanceSelection, req syncRequest) error {
	own, err := a.syncInPlace(cmd, p, side, req)
	if err != nil {
		return err
	}
	children, _, err := a.projectInstances(sel)
	if out.CodeOf(err) == "no-instances" {
		children, err = nil, nil
	}
	if err != nil {
		return err
	}
	if len(children) == 0 {
		return a.printer.Emit(own, own.print)
	}
	if !a.printer.JSON {
		own.print(a.printer.Out())
	}
	results, failed := a.syncEach(cmd, children, req, true)
	own.Instances = results
	if failed > 0 {
		e := out.Errorf("sync-failed", "%d of %d instances built from %s failed to sync", failed, len(children), p.Manifest.Name)
		e.Data = own
		return e
	}
	return a.printer.Emit(own, func(*out.Lines) {})
}
