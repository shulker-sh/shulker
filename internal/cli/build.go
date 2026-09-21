package cli

import (
	"fmt"
	"path/filepath"
	"strings"

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
		Use:   "build [side]",
		Short: "Assemble build directories from the lock and overrides",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := checkOS(osName); err != nil {
				return err
			}
			if err := a.requireLock(p); err != nil {
				return err
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
			sides, err := projectSides(p, args)
			if err != nil {
				return err
			}
			var reports []*build.Report
			for _, side := range sides {
				rep, err := b.Build(side, build.Options{Force: force, OS: osName, Features: overrides})
				if err != nil {
					return err
				}
				a.warnFor(side, len(sides) > 1, rep.Warnings)
				if err := a.installServerLoader(cmd.Context(), p, rep); err != nil {
					return err
				}
				reports = append(reports, rep)
			}
			a.refreshLocal(lf, true, false)
			return a.printer.Emit(reports, func(l *out.Lines) {
				for _, rep := range reports {
					printReport(l, rep)
				}
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the build directory")
	cmd.Flags().BoolVar(&acceptPlayerChange, "accept-player-change", false, "relock a player name that now belongs to a different account")
	cmd.Flags().StringVar(&osName, "os", "", "build for this os instead of the detected one: macos, windows, or linux")
	ff.register(cmd, "for this run only")
	return cmd
}

func printReport(l *out.Lines, rep *build.Report) {
	l.OK("built "+rep.Side, reportAside(rep))
	printReportDetails(l, rep)
}

func reportAside(rep *build.Report) string {
	var parts []string
	count := func(n int, what string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, what))
		}
	}
	count(len(rep.Written), "written")
	count(rep.Unchanged, "unchanged")
	count(len(rep.Kept), "kept")
	count(len(rep.Removed), "removed")
	count(len(rep.Linked), "linked")
	count(len(rep.Moved), "moved")
	count(len(rep.Excluded), "excluded")
	if len(parts) == 0 {
		return "nothing to do"
	}
	return strings.Join(parts, ", ")
}

func printReportDetails(l *out.Lines, rep *build.Report) {
	l.Tree(reportDetailRows(l, rep)...)
}

func reportDetailRows(l *out.Lines, rep *build.Report) []out.Row {
	var rows []out.Row
	for _, m := range rep.Moved {
		rows = append(rows, out.Row{Label: "moved", Text: m + " " + l.T.Grey(l.T.ArrowInto()) + " " + filepath.Join(build.DataDir, rep.Side, m)})
	}
	for _, m := range rep.MovedBack {
		rows = append(rows, out.Row{Label: "moved back", Text: m + " " + l.T.Grey(l.T.ArrowInto()) + " " + rep.Dir})
	}
	for _, e := range rep.Excluded {
		rows = append(rows, out.Row{Label: "excluded", Text: e})
	}
	for _, k := range rep.Kept {
		rows = append(rows, out.Row{Label: "kept", Text: k})
	}
	if ld := rep.InstalledLoader; ld != nil {
		rows = append(rows, out.Row{Label: "installed", Text: ld.Type + " " + ld.Version})
	}
	return rows
}
