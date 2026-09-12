package cli

import (
	"context"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

type lockChanges struct {
	*resolve.Changes
	Reresolved  []string `json:"reresolved"`
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
	if p.Lock == nil && cmd.Name() == "lock" {
		p.Lock = lock.New()
	}
	if err := p.RequireLock(); err != nil {
		return err
	}
	a.relocking = true
	r, err := a.resolver(cmd.Context(), p)
	if err != nil {
		return err
	}
	before := r.Snapshot()
	if err := a.resolveMovedRefs(cmd.Context(), p, r); err != nil {
		return err
	}
	reresolved, err := r.Reconcile(cmd.Context())
	if err != nil {
		return err
	}
	if reresolved == nil {
		reresolved = []string{}
	}
	pin, err := run(p, r)
	if err != nil {
		return err
	}
	if _, err := r.Reconcile(cmd.Context()); err != nil {
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
	res := lockChanges{Changes: r.Changes(before), Reresolved: reresolved, Pin: pin, Suggestions: v.Recommended()}
	optional := 0
	for _, s := range v.Suggestions {
		if s.Kind == "optional" && slices.ContainsFunc(res.Added, func(m resolve.AddedMod) bool { return m.ID == s.Mod }) {
			optional++
		}
	}
	return a.printer.Emit(res, func(l *out.Lines) {
		if cmd.Name() == "pin" {
			l.OK("pinned "+cmd.Flags().Arg(0), pin)
		}
		if cmd.Name() == "unpin" {
			l.OK("unpinned "+cmd.Flags().Arg(0), "")
		}
		if len(res.Reresolved) > 0 {
			l.Info("re-resolved every mod: " + strings.Join(res.Reresolved, "; "))
		}
		printChanges(l, res.Changes, v.Suggestions, p.Manifest.Targets)
		if optional > 0 {
			optionalNudge(l, optional)
		}
		if res.Empty() {
			l.OK("already up to date", "")
			return
		}
		l.Nudge("Download and build what changed", "shulker install")
	})
}

func (a *app) resolveMovedRefs(ctx context.Context, p *project.Project, r *resolve.Resolver) error {
	for i, mp := range p.Manifest.Packs {
		pinned, locked := p.Lock.Packs[mp.Source]
		if !locked || pinned.Ref == mp.Ref {
			continue
		}
		store, err := a.packStore(p)
		if err != nil {
			return err
		}
		loaded, err := store.Resolve(ctx, mp)
		if err != nil {
			return err
		}
		r.Packs[i] = loaded
	}
	return nil
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
			return a.printer.Emit(res, func(l *out.Lines) {
				if len(res) == 0 {
					l.OK("all mods are up to date", "")
					return
				}
				var items []out.Item
				for _, o := range res {
					it := out.Item{Kind: out.Change, Name: o.ID, From: o.Current, To: o.Latest}
					if o.Pinned {
						it.Aside = []string{"pinned"}
					}
					items = append(items, it)
				}
				l.Items(items...)
				l.Nudge("Move the lock to these versions", "shulker update")
			})
		},
	}
}

func printChanges(l *out.Lines, c *resolve.Changes, suggestions []resolve.Suggestion, targets map[string]manifest.Target) {
	var items []out.Item
	for _, p := range c.Platform {
		if p.From == "" {
			items = append(items, out.Item{Kind: out.Add, Name: p.ID, Version: p.To})
			continue
		}
		items = append(items, out.Item{Kind: out.Change, Name: p.ID, From: p.From, To: p.To})
	}
	for _, p := range c.Packs {
		switch {
		case p.From == "":
			items = append(items, out.Item{Kind: out.Add, Name: p.Name, Version: p.To, Aside: []string{"pack"}})
		case p.To == "":
			items = append(items, out.Item{Kind: out.Drop, Name: p.Name, Aside: []string{"pack"}})
		default:
			items = append(items, out.Item{Kind: out.Change, Name: p.Name, From: p.From, To: p.To, Aside: []string{"pack"}})
		}
	}
	for _, m := range c.Added {
		it := out.Item{Kind: out.Add, Name: m.ID, Version: m.VersionNumber, Targets: targetsForSide(targets, m.Side), OfTargets: len(targets)}
		if m.Side != "" && m.Side != "both" {
			it.Aside = append(it.Aside, m.Side+" only")
		}
		switch {
		case m.AlreadyLocked:
			it.Aside = append(it.Aside, "already locked")
		case len(m.RequiredBy) > 0:
			it.Aside = append(it.Aside, "required by "+strings.Join(m.RequiredBy, ", "))
		}
		items = append(items, it)
	}
	for _, u := range c.Updated {
		it := out.Item{Kind: out.Change, Name: u.ID}
		if u.From != u.To || (u.FromSide == "" && u.FromChannel == "") {
			it.From, it.To = u.From, u.To
		}
		if u.FromProvider != "" {
			it.Aside = append(it.Aside, u.FromProvider+" "+l.T.ArrowBump()+" "+u.ToProvider)
		}
		if u.FromSide != "" {
			it.Aside = append(it.Aside, "now "+sideText(u.ToSide))
		}
		if u.FromChannel != "" {
			it.Aside = append(it.Aside, "channel: "+u.FromChannel+" "+l.T.ArrowBump()+" "+u.ToChannel)
		}
		items = append(items, it)
	}
	for _, m := range c.Removed {
		it := out.Item{Kind: out.Drop, Name: m.ID}
		switch {
		case m.StillLocked:
			it.Aside = []string{"from shulker.json, still locked"}
		case len(m.RequiredBy) > 0:
			it.Aside = []string{"was required by " + strings.Join(m.RequiredBy, ", ")}
		}
		items = append(items, it)
	}
	for _, s := range suggestions {
		if s.Kind != "optional" {
			items = append(items, out.Item{Kind: out.Note, Name: s.Mod, Aside: []string{s.Kind + " " + s.On + ", not installed"}})
		}
	}
	l.Items(items...)
}

func targetsForSide(targets map[string]manifest.Target, side string) []string {
	var names []string
	for _, name := range targetNames(targets) {
		if side == "" || side == "both" || targets[name].Side == side {
			names = append(names, name)
		}
	}
	return names
}

func sideText(side string) string {
	if side == "" || side == "both" {
		return "client and server"
	}
	return side + " only"
}
