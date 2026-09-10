package cli

import (
	"fmt"
	"io"

	"github.com/andrewmast/shulker/internal/build"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/spf13/cobra"
)

func (a *app) buildCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "build [target]",
		Short: "Assemble build directories from the lock and overrides",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
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
			b, err := a.builder(p)
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
				reports = append(reports, rep)
			}
			return a.printer.Emit(reports, func(w io.Writer) {
				for _, rep := range reports {
					fmt.Fprintln(w, rep.Summary())
					for _, k := range rep.Kept {
						fmt.Fprintf(w, "  kept %s\n", k)
					}
				}
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the build directory and ignore a stale lock")
	return cmd
}
