package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func (a *app) removeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <mod>...",
		Aliases: []string{"rm"},
		Short:   "Remove mods from the manifest and prune orphaned dependencies",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			r, err := a.resolver(p)
			if err != nil {
				return err
			}
			res, err := r.Remove(args)
			if err != nil {
				return err
			}
			if err := p.SaveManifest(); err != nil {
				return err
			}
			if err := p.SaveLock(); err != nil {
				return err
			}
			a.printer.LockStale = false
			return a.printer.Emit(res, func(w io.Writer) {
				for _, id := range res.Removed {
					fmt.Fprintf(w, "- %s\n", id)
				}
				for _, id := range res.Pruned {
					fmt.Fprintf(w, "  pruned %s\n", id)
				}
				fmt.Fprintln(w, "Next: shulker install")
			})
		},
	}
}
