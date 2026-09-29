package cli

import (
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) lockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "lock [key...]",
		Annotations: acts(),
		Short:       "Bring shulker.lock in line with shulker.json without upgrading anything",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.relock(cmd, relockPlan{open: a.openForLock}, func(_ *project.Project, r *resolve.Resolver) (string, error) {
				return "", r.LockAgain(cmd.Context(), args)
			})
		},
	}
	a.scopeFlags(cmd)
	return cmd
}
