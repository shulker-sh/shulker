package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
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
			return a.relock(cmd, func(p *project.Project, r *resolve.Resolver) (*resolve.Updated, string, error) {
				packNames := map[string]bool{}
				for _, l := range r.Packs {
					packNames[l.Name] = true
				}
				var targets []string
				requested := map[string]bool{}
				for _, arg := range args {
					if packNames[arg] {
						requested[arg] = true
						continue
					}
					targets = append(targets, arg)
				}
				refresh := len(args) == 0 || len(requested) > 0
				if refresh {
					loaded, err := a.resolvePacks(cmd.Context(), p)
					if err != nil {
						return nil, "", err
					}
					if err := r.RefreshPacks(loaded); err != nil {
						return nil, "", err
					}
					for _, l := range loaded {
						if requested[l.Name] {
							for id := range l.Manifest.Mods {
								targets = append(targets, id)
							}
						}
					}
				}
				if len(args) > 0 && len(targets) == 0 {
					return &resolve.Updated{Updated: []resolve.Change{}, Added: []string{}, Removed: []string{}}, "", nil
				}
				res, err := r.Update(cmd.Context(), targets)
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
			return a.relock(cmd, func(_ *project.Project, r *resolve.Resolver) (*resolve.Updated, string, error) {
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
			return a.relock(cmd, func(_ *project.Project, r *resolve.Resolver) (*resolve.Updated, string, error) {
				res, err := r.Unpin(cmd.Context(), args[0])
				return res, "", err
			})
		},
	}
}

func (a *app) relock(cmd *cobra.Command, run func(*project.Project, *resolve.Resolver) (*resolve.Updated, string, error)) error {
	p, err := a.openProject()
	if err != nil {
		return err
	}
	if err := p.RequireLock(); err != nil {
		return err
	}
	r, err := a.resolver(cmd.Context(), p)
	if err != nil {
		return err
	}
	pinsBefore := map[string]lock.Pack{}
	for source, pin := range p.Lock.Packs {
		pinsBefore[source] = pin
	}
	updated, pin, err := run(p, r)
	if err != nil {
		return err
	}
	v, err := a.commit(p, r)
	if err != nil {
		return err
	}
	updated.Packs = resolve.PackChanges(pinsBefore, p.Lock.Packs)
	res := updateResult{Updated: updated, Pin: pin, Warnings: v.Warnings, Suggestions: v.Suggestions}
	return a.printer.Emit(res, func(w io.Writer) {
		if cmd.Name() == "pin" {
			fmt.Fprintf(w, "pinned %s to %s\n", cmd.Flags().Arg(0), pin)
		}
		if cmd.Name() == "unpin" {
			fmt.Fprintf(w, "unpinned %s\n", cmd.Flags().Arg(0))
		}
		printPackChanges(w, res.Packs)
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
		if len(res.Updated.Updated)+len(res.Added)+len(res.Removed)+len(res.Packs) == 0 {
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
			r, err := a.resolver(cmd.Context(), p)
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

func printPackChanges(w io.Writer, changes []resolve.PackChange) {
	for _, c := range changes {
		switch {
		case c.From == "":
			fmt.Fprintf(w, "+ pack %s %s\n", c.Name, c.To)
		case c.To == "":
			fmt.Fprintf(w, "- pack %s\n", c.Name)
		default:
			fmt.Fprintf(w, "~ pack %s %s -> %s\n", c.Name, c.From, c.To)
		}
	}
}
