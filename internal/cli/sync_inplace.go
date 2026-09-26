package cli

import (
	"context"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/sync"
)

// syncInPlace refreshes the modpacks that follow their source, relocks without moving the
// project's own mods, and builds the instance where it stands.
func (a *app) syncInPlace(cmd *cobra.Command, p *project.Project, side string, req syncRequest) (syncResult, error) {
	se, err := a.syncEnv()
	if err != nil {
		return syncResult{}, err
	}
	req.Reason = cmd.Name()
	res, err := sync.InPlace(cmd.Context(), se, p, side, req.request())
	return a.synced(res, req, err)
}

func (a *app) buildInPlace(ctx context.Context, dir string, req syncRequest) (syncResult, error) {
	src, err := a.openSource(ctx, dir, modpack.At{})
	if err != nil {
		return syncResult{}, err
	}
	req.At, req.Into = modpack.At{}, ""
	return a.sync(ctx, src, req)
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
