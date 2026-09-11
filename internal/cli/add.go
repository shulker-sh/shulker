package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/resolve"
)

type addResult struct {
	Added       []*resolve.Added `json:"added"`
	Warnings    []string         `json:"warnings"`
	Suggestions []string         `json:"suggestions"`
}

func (a *app) addCmd() *cobra.Command {
	var opts resolve.AddOptions
	cmd := &cobra.Command{
		Use:   "add <mod>...",
		Short: "Add mods to the manifest and lock",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if opts.Pin != "" && len(args) > 1 {
				return fmt.Errorf("--pin applies to a single mod")
			}
			r, err := a.resolver(cmd.Context(), p)
			if err != nil {
				return err
			}
			var results []*resolve.Added
			for _, slug := range args {
				added, err := r.Add(cmd.Context(), slug, opts)
				if err != nil {
					return err
				}
				results = append(results, added)
			}
			v, err := a.commit(p, r)
			if err != nil {
				return err
			}
			res := addResult{Added: results, Warnings: v.Warnings, Suggestions: v.Suggestions}
			return a.printer.Emit(res, func(w io.Writer) {
				for _, r := range res.Added {
					if r.SwitchedFrom != "" {
						fmt.Fprintf(w, "~ %s %s (%s) %s -> %s", r.ID, r.VersionNumber, r.Side, r.SwitchedFrom, r.Provider)
					} else {
						fmt.Fprintf(w, "+ %s %s (%s)", r.ID, r.VersionNumber, r.Side)
					}
					if len(r.Dependencies) > 0 {
						fmt.Fprintf(w, " with %s", strings.Join(r.Dependencies, ", "))
					}
					fmt.Fprintln(w)
					for _, id := range r.Pruned {
						fmt.Fprintf(w, "  pruned %s\n", id)
					}
				}
				for _, s := range res.Suggestions {
					fmt.Fprintf(w, "  %s (not installed)\n", s)
				}
				fmt.Fprintln(w, "Next: shulker install")
			})
		},
	}
	cmd.Flags().StringVar(&opts.Side, "side", "", "override side: client, server, both")
	cmd.Flags().StringVar(&opts.Channel, "channel", "", "least stable channel accepted: release, beta, alpha")
	cmd.Flags().StringVar(&opts.Pin, "pin", "", "pin to a provider version id")
	cmd.Flags().StringVar(&opts.Provider, "provider", "", "provider to use for this mod")
	return cmd
}
