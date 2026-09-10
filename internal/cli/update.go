package cli

import (
	"fmt"
	"io"

	"github.com/andrewmast/shulker/internal/resolve"
	"github.com/spf13/cobra"
)

type updateResult struct {
	*resolve.Updated
	Pin         string   `json:"pin,omitempty"`
	Warnings    []string `json:"warnings"`
	Suggestions []string `json:"suggestions"`
}

func (a *app) updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "update [mod...]",
		Aliases: []string{"upgrade"},
		Short:   "Re-resolve mods to the newest compatible versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.relock(cmd, func(r *resolve.Resolver) (*resolve.Updated, string, error) {
				res, err := r.Update(cmd.Context(), args)
				return res, "", err
			})
		},
	}
}

func (a *app) pinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pin <mod> [version]",
		Short: "Pin a mod to a provider version id, or to its locked version",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			version := ""
			if len(args) == 2 {
				version = args[1]
			}
			return a.relock(cmd, func(r *resolve.Resolver) (*resolve.Updated, string, error) {
				return r.Pin(cmd.Context(), args[0], version)
			})
		},
	}
}

func (a *app) unpinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unpin <mod>",
		Short: "Remove a mod's pin and re-resolve it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.relock(cmd, func(r *resolve.Resolver) (*resolve.Updated, string, error) {
				res, err := r.Unpin(cmd.Context(), args[0])
				return res, "", err
			})
		},
	}
}

func (a *app) relock(cmd *cobra.Command, run func(*resolve.Resolver) (*resolve.Updated, string, error)) error {
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
	updated, pin, err := run(r)
	if err != nil {
		return err
	}
	v, err := a.commit(p, r)
	if err != nil {
		return err
	}
	res := updateResult{Updated: updated, Pin: pin, Warnings: v.Warnings, Suggestions: v.Suggestions}
	return a.printer.Emit(res, func(w io.Writer) {
		if cmd.Name() == "pin" {
			fmt.Fprintf(w, "pinned %s to %s\n", cmd.Flags().Arg(0), pin)
		}
		if cmd.Name() == "unpin" {
			fmt.Fprintf(w, "unpinned %s\n", cmd.Flags().Arg(0))
		}
		for _, c := range res.Updated.Updated {
			fmt.Fprintf(w, "~ %s %s -> %s\n", c.ID, c.From, c.To)
		}
		for _, id := range res.Added {
			fmt.Fprintf(w, "+ %s %s\n", id, r.Lock.Mods[id].VersionNumber)
		}
		for _, id := range res.Removed {
			fmt.Fprintf(w, "- %s\n", id)
		}
		for _, s := range res.Suggestions {
			fmt.Fprintf(w, "  %s (not installed)\n", s)
		}
		if len(res.Updated.Updated)+len(res.Added)+len(res.Removed) == 0 {
			fmt.Fprintln(w, "Already up to date.")
			return
		}
		fmt.Fprintln(w, "Next: shulker install")
	})
}

func (a *app) outdatedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "outdated [mod...]",
		Short: "Show mods with a newer compatible version (dry run of update)",
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
			res, err := r.Outdated(cmd.Context(), args)
			if err != nil {
				return err
			}
			return a.printer.Emit(res, func(w io.Writer) {
				if len(res) == 0 {
					fmt.Fprintln(w, "All mods are up to date.")
					return
				}
				for _, o := range res {
					fmt.Fprintf(w, "%s %s -> %s", o.ID, o.Current, o.Latest)
					if o.Pinned {
						fmt.Fprint(w, " (pinned)")
					}
					fmt.Fprintln(w)
				}
				fmt.Fprintln(w, "Next: shulker update")
			})
		},
	}
}
