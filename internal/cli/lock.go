package cli

import (
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) lockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "lock",
		Annotations: acts(),
		Short:       "Bring shulker.lock in line with shulker.json without upgrading anything",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.relock(cmd, relockPlan{open: a.openForLock}, func(*project.Project, *resolve.Resolver) (string, error) {
				return "", nil
			})
		},
	}
	a.scopeFlags(cmd)
	return cmd
}
