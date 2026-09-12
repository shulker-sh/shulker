package cli

import (
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) lockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lock",
		Short: "Bring shulker.lock in line with shulker.json without upgrading anything",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.relock(cmd, func(*project.Project, *resolve.Resolver) (string, error) {
				return "", nil
			})
		},
	}
}
