package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

type lockChanges struct {
	*resolve.Changes
	Suggestions []string `json:"suggestions"`
	Pin         string   `json:"pin,omitempty"`
}

func (a *app) updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "update [mod...]",
		Aliases: []string{"upgrade"},
		Short:   "Re-resolve mods to the newest compatible versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.relock(cmd, func(p *project.Project, r *resolve.Resolver) (string, error) {
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
						return "", err
					}
					if err := r.RefreshPacks(loaded); err != nil {
						return "", err
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
					return "", nil
				}
				return "", r.Update(cmd.Context(), targets)
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
			return a.relock(cmd, func(_ *project.Project, r *resolve.Resolver) (string, error) {
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
			return a.relock(cmd, func(_ *project.Project, r *resolve.Resolver) (string, error) {
				return "", r.Unpin(cmd.Context(), args[0])
			})
		},
	}
}

func (a *app) relock(cmd *cobra.Command, run func(*project.Project, *resolve.Resolver) (pin string, err error)) error {
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
	before := r.Snapshot()
	pin, err := run(p, r)
	if err != nil {
		return err
	}
	if err := r.RefreshPacks(r.Packs); err != nil {
		return err
	}
	v, err := r.Validate()
	if err != nil {
		return err
	}
	if err := v.Err(); err != nil {
		return err
	}
	if err := p.SaveManifest(); err != nil {
		return err
	}
	if err := p.SaveLock(); err != nil {
		return err
	}
	a.printer.LockStale = false
	a.warn(v.Warnings)
	res := lockChanges{Changes: r.Changes(before), Pin: pin, Suggestions: v.Recommended()}
	optional := 0
	for _, s := range v.Suggestions {
		if s.Kind == "optional" && slices.ContainsFunc(res.Added, func(m resolve.AddedMod) bool { return m.ID == s.Mod }) {
			optional++
		}
	}
	return a.printer.Emit(res, func(w io.Writer) {
		if cmd.Name() == "pin" {
			fmt.Fprintf(w, "pinned %s to %s\n", cmd.Flags().Arg(0), pin)
		}
		if cmd.Name() == "unpin" {
			fmt.Fprintf(w, "unpinned %s\n", cmd.Flags().Arg(0))
		}
		printChanges(w, res.Changes)
		for _, s := range res.Suggestions {
			fmt.Fprintf(w, "  %s (not installed)\n", s)
		}
		if optional > 0 {
			fmt.Fprintln(w, optionalHint(optional))
		}
		if res.Empty() {
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

func printChanges(w io.Writer, c *resolve.Changes) {
	for _, p := range c.Packs {
		switch {
		case p.From == "":
			fmt.Fprintf(w, "+ pack %s %s\n", p.Name, p.To)
		case p.To == "":
			fmt.Fprintf(w, "- pack %s\n", p.Name)
		default:
			fmt.Fprintf(w, "~ pack %s %s -> %s\n", p.Name, p.From, p.To)
		}
	}
	for _, m := range c.Added {
		fmt.Fprintf(w, "+ %s %s (%s)", m.ID, m.VersionNumber, m.Side)
		switch {
		case m.AlreadyLocked:
			fmt.Fprint(w, ", already locked")
		case len(m.RequiredBy) > 0:
			fmt.Fprintf(w, ", required by %s", strings.Join(m.RequiredBy, ", "))
		}
		fmt.Fprintln(w)
	}
	for _, u := range c.Updated {
		fmt.Fprintf(w, "~ %s %s -> %s", u.ID, u.From, u.To)
		if u.FromProvider != "" {
			fmt.Fprintf(w, " (%s -> %s)", u.FromProvider, u.ToProvider)
		}
		fmt.Fprintln(w)
	}
	for _, m := range c.Removed {
		fmt.Fprintf(w, "- %s", m.ID)
		switch {
		case m.StillLocked:
			fmt.Fprint(w, " from shulker.json, still locked")
		case len(m.RequiredBy) > 0:
			fmt.Fprintf(w, ", was required by %s", strings.Join(m.RequiredBy, ", "))
		}
		fmt.Fprintln(w)
	}
}
