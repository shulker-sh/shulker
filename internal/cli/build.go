package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
)

func (a *app) buildCmd() *cobra.Command {
	var force, acceptPlayerChange bool
	var osName string
	var ff featureFlags
	cmd := &cobra.Command{
		Use:   "build [target]",
		Short: "Assemble build directories from the lock and overrides",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := checkOS(osName); err != nil {
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
			lf, err := local.Load(p.Dir)
			if err != nil {
				return err
			}
			overrides, err := featureOverrides(b, lf.Features, ff)
			if err != nil {
				return err
			}
			names := targetNames(p.Manifest.Targets)
			if len(args) == 1 {
				names = args
			}
			var reports []*build.Report
			for _, name := range names {
				rep, err := b.Build(name, build.Options{Force: force, OS: osName, Features: overrides})
				if err != nil {
					return err
				}
				a.warnFor(name, len(names) > 1, rep.Warnings)
				if err := a.installServerLoader(cmd.Context(), p, rep); err != nil {
					return err
				}
				reports = append(reports, rep)
			}
			a.refreshLocal(lf, true, false)
			return a.printer.Emit(reports, func(w io.Writer) {
				for _, rep := range reports {
					fmt.Fprintln(w, rep.Summary())
					printReportDetails(w, rep)
				}
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the build directory and ignore a stale lock")
	cmd.Flags().BoolVar(&acceptPlayerChange, "accept-player-change", false, "relock a player name that now belongs to a different account")
	cmd.Flags().StringVar(&osName, "os", "", "build for this os instead of the detected one: macos, windows, or linux")
	ff.register(cmd, "for this run only")
	return cmd
}

func printReportDetails(w io.Writer, rep *build.Report) {
	for _, m := range rep.Moved {
		fmt.Fprintf(w, "  moved %s into %s\n", m, filepath.Join(build.DataDir, rep.Target, m))
	}
	for _, m := range rep.MovedBack {
		fmt.Fprintf(w, "  moved %s back into %s\n", m, rep.Dir)
	}
	for _, e := range rep.Excluded {
		fmt.Fprintf(w, "  excluded %s\n", e)
	}
	for _, k := range rep.Kept {
		fmt.Fprintf(w, "  kept %s\n", k)
	}
	if l := rep.InstalledLoader; l != nil {
		fmt.Fprintf(w, "  installed %s %s\n", l.Type, l.Version)
	}
}
