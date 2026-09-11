package cli

import (
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) removeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <mod>...",
		Aliases: []string{"rm"},
		Short:   "Remove mods from the manifest and prune orphaned dependencies",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.relock(cmd, func(_ *project.Project, r *resolve.Resolver) (string, error) {
				return "", r.Remove(args)
			})
		},
	}
}
