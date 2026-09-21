package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
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

// inPlaceManifest answers the same question for a reader deciding what a directory is, which needs
// no lock: nothing it does builds.
func inPlaceManifest(dir string) (*manifest.Manifest, string, bool, error) {
	m, err := manifest.Load(filepath.Join(dir, manifest.FileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	side, ok := m.InPlaceSide()
	return m, side, ok, nil
}

// inPlaceIntent is what an instance that is a project reads from its manifest rather than from its
// instance file: the modpack it follows, the ref it follows it at, and the side that builds where
// it stands. With several modpacks required, none of them is the one it was linked from, so the
// source and the ref come back empty.
func inPlaceIntent(dir string) (source, ref, side string, inPlace bool) {
	m, side, inPlace, err := inPlaceManifest(dir)
	if err != nil || !inPlace {
		return "", "", "", false
	}
	var pack manifest.Require
	if key := modpackKey(m, ""); key != "" {
		pack = m.Requires[key]
	}
	return pack.Source, pack.Ref, side, true
}

// syncInPlace refreshes the modpacks that follow their source, relocks without moving the
// project's own mods, and builds the instance where it stands.
func (a *app) syncInPlace(cmd *cobra.Command, p *project.Project, side string, req syncRequest) (syncResult, error) {
	a.packs = nil
	rl, err := a.relockProject(cmd, p, true, func(p *project.Project, r *resolve.Resolver) (string, error) {
		_, err := a.refreshModpacks(cmd.Context(), p, r, manifest.Require.AutoUpdates)
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
	if rl.saved {
		res.Changes = &rl.lockChanges
	}
	return res, nil
}

func (a *app) buildInPlace(ctx context.Context, dir string, req syncRequest) (syncResult, error) {
	a.packs = nil
	src, err := a.openSource(ctx, dir, "")
	if err != nil {
		return syncResult{}, err
	}
	req.ref, req.into = "", ""
	return a.sync(ctx, src, req)
}

// syncInPlaceForLaunch never stands between the player and the game: a refresh that fails falls
// back to building the lock already there, and a build that fails leaves what is on disk.
func (a *app) syncInPlaceForLaunch(cmd *cobra.Command, p *project.Project, side string) (syncResult, error) {
	res, err := a.syncInPlace(cmd, p, side, syncRequest{backup: "sync"})
	if err == nil || errors.Is(cmd.Context().Err(), context.DeadlineExceeded) {
		return res, err
	}
	a.printer.Drop()
	a.printer.Warn("couldn't update, building what the lock already has: %v", err)
	return a.buildInPlace(cmd.Context(), p.Dir, syncRequest{side: side, backup: "sync"})
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
