package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/shulker-sh/shulker/internal/build"
	"github.com/shulker-sh/shulker/internal/out"
	"github.com/shulker-sh/shulker/internal/player"
	"github.com/spf13/cobra"
)

func (a *app) buildCmd() *cobra.Command {
	var force, acceptPlayerChange bool
	cmd := &cobra.Command{
		Use:   "build [target]",
		Short: "Assemble build directories from the lock and overrides",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if a.printer.LockStale && !force {
				return out.Errorf("lock-stale", "shulker.lock does not match shulker.json; run `shulker add`, `remove`, or `update`, or pass --force")
			}
			if err := a.syncPlayers(cmd.Context(), p, player.Recheck, acceptPlayerChange, true); err != nil {
				return err
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			names := targetNames(p.Manifest.Targets)
			if len(args) == 1 {
				names = args
			}
			var reports []*build.Report
			for _, name := range names {
				rep, err := b.Build(name, build.Options{Force: force})
				if err != nil {
					return err
				}
				a.warn(rep.Warnings)
				reports = append(reports, rep)
			}
			return a.printer.Emit(reports, func(w io.Writer) {
				for _, rep := range reports {
					fmt.Fprintln(w, rep.Summary())
					for _, m := range rep.Moved {
						fmt.Fprintf(w, "  moved %s into %s\n", m, filepath.Join(build.DataDir, rep.Target, m))
					}
					for _, e := range rep.Excluded {
						fmt.Fprintf(w, "  excluded %s\n", e)
					}
					for _, k := range rep.Kept {
						fmt.Fprintf(w, "  kept %s\n", k)
					}
				}
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the build directory and ignore a stale lock")
	cmd.Flags().BoolVar(&acceptPlayerChange, "accept-player-change", false, "relock a player name that now belongs to a different account")
	return cmd
}
