package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) addCmd() *cobra.Command {
	var opts resolve.AddOptions
	cmd := &cobra.Command{
		Use:   "add <mod>...",
		Short: "Add mods to the manifest and lock",
		Args:  minimumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Pin != "" && len(args) > 1 {
				return fmt.Errorf("--pin applies to a single mod")
			}
			for _, flag := range []struct {
				name, value string
				allowed     []string
			}{
				{"side", opts.Side, []string{"client", "server", "both"}},
				{"channel", opts.Channel, []string{"release", "beta", "alpha"}},
				{"provider", opts.Provider, []string{"modrinth", "curseforge"}},
			} {
				if flag.value != "" && !slices.Contains(flag.allowed, flag.value) {
					return out.Errorf("usage", "--%s takes one of %s, not %q", flag.name, strings.Join(flag.allowed, ", "), flag.value)
				}
			}
			return a.relock(cmd, func(_ *project.Project, r *resolve.Resolver) (string, error) {
				for _, slug := range args {
					if err := r.Add(cmd.Context(), slug, opts); err != nil {
						return "", err
					}
				}
				return "", nil
			})
		},
	}
	cmd.Flags().StringVar(&opts.Side, "side", "", "override side: client, server, both")
	cmd.Flags().StringVar(&opts.Channel, "channel", "", "least stable channel accepted: release, beta, alpha")
	cmd.Flags().StringVar(&opts.Pin, "pin", "", "pin to a provider version id")
	cmd.Flags().StringVar(&opts.Provider, "provider", "", "provider to use for this mod")
	return cmd
}
