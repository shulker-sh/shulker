package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "update [mod...]",
		Annotations: acts(),
		Aliases:     []string{"upgrade"},
		Short:       "Re-resolve mods to the newest compatible versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			plan := relockPlan{
				buildsInPlace: true,
				local: func(m *manifest.Manifest) []string {
					local, _ := splitLocalFiles(m, args)
					return local
				},
			}
			return a.relock(cmd, plan, func(p *project.Project, r *resolve.Resolver) (string, error) {
				local, args := splitLocalFiles(p.Manifest, args)
				if len(local) > 0 && len(args) == 0 {
					return "", nil
				}
				packNames := map[string]bool{}
				for _, l := range r.Packs {
					packNames[l.Name] = true
				}
				var ids []string
				requested := map[string]bool{}
				for _, arg := range args {
					if packNames[arg] {
						requested[arg] = true
						continue
					}
					ids = append(ids, arg)
				}
				if len(args) == 0 || len(requested) > 0 {
					loaded, err := a.refreshModpacks(cmd.Context(), p, r, func(manifest.Require) bool { return true }, failUnreachable)
					if err != nil {
						return "", err
					}
					for _, l := range loaded {
						if requested[l.Name] {
							for id := range l.Manifest.Mods() {
								ids = append(ids, id)
							}
						}
					}
				}
				if len(args) > 0 && len(ids) == 0 {
					return "", nil
				}
				return "", r.Update(cmd.Context(), ids)
			})
		},
	}
}

func (a *app) pinCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "pin <mod> [version|url]",
		Annotations: acts(),
		Short:       "Pin a mod to a provider version id or URL, or to its locked version",
		Args:        rangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			version := ""
			if len(args) == 2 {
				version = args[1]
			}
			u, isURL, err := resolve.ParseURL(version)
			if err != nil {
				return err
			}
			pinned := func(l *out.Lines, res lockChanges) { l.OK("pinned "+args[0], res.Pin) }
			return a.relock(cmd, relockPlan{ok: pinned}, func(_ *project.Project, r *resolve.Resolver) (string, error) {
				if isURL {
					return r.PinURL(cmd.Context(), args[0], u)
				}
				return r.Pin(cmd.Context(), args[0], version)
			})
		},
	}
}

func (a *app) unpinCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "unpin <mod>",
		Annotations: acts(),
		Short:       "Remove a mod's pin and re-resolve it",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			unpinned := func(l *out.Lines, _ lockChanges) { l.OK("unpinned "+args[0], "") }
			return a.relock(cmd, relockPlan{ok: unpinned}, func(_ *project.Project, r *resolve.Resolver) (string, error) {
				return "", r.Unpin(cmd.Context(), args[0])
			})
		},
	}
}

type lockChanges struct {
	*resolve.Changes
	Reresolved  []string    `json:"reresolved"`
	Suggestions []string    `json:"suggestions"`
	Pin         string      `json:"pin,omitempty"`
	Synced      *syncResult `json:"synced,omitempty"`
	printItems  func(*out.Lines)
}

type relocked struct {
	lockChanges
	validation *resolve.Validation
	wasSaved   bool
}

// relockPlan is what a lock-changing command asks of relock beyond its run.
type relockPlan struct {
	// open opens the project; nil opens it as every command but lock does.
	open func() (*project.Project, error)
	// buildsInPlace builds an instance once its lock is saved, since it plays its own directory.
	buildsInPlace bool
	// local is the local file entries among the command's arguments, reported as left alone.
	local func(*manifest.Manifest) []string
	// ok is the line that confirms the command, printed first.
	ok func(l *out.Lines, res lockChanges)
}

func (a *app) relock(cmd *cobra.Command, plan relockPlan, run func(*project.Project, *resolve.Resolver) (pin string, err error)) error {
	open := plan.open
	if open == nil {
		open = a.openProject
	}
	p, err := open()
	if err != nil {
		return err
	}
	rl, err := a.relockProject(cmd, p, relockOptions{}, run)
	if err != nil {
		return err
	}
	if p.ReplacedLock != "" {
		kept := p.ReplacedLock
		if rel, err := filepath.Rel(p.Dir, kept); err == nil && filepath.IsLocal(rel) {
			kept = rel
		}
		a.warnReplaced(p.UnreadableLock, kept)
	}
	hasChildren := false
	if side, ok := p.Manifest.InPlaceSide(); ok && plan.buildsInPlace {
		synced, err := a.buildInPlace(cmd.Context(), p.Dir, syncRequest{side: side, backup: "update"})
		if err != nil {
			return err
		}
		rl.Synced = &synced
		// The nudge is only a hint: a registry it can't read mustn't fail an update that is done.
		hasChildren, _ = a.hasSyncedInstances(p)
	}
	res := rl.lockChanges
	var local []string
	if plan.local != nil {
		local = plan.local(p.Manifest)
	}
	optional := 0
	for _, s := range rl.validation.Suggestions {
		if s.Kind == "optional" && s.InstalledAs == "" && slices.ContainsFunc(res.Added, func(m resolve.AddedMod) bool { return m.ID == s.Mod }) {
			optional++
		}
	}
	return a.printer.Emit(res, func(l *out.Lines) {
		if plan.ok != nil {
			plan.ok(l, res)
		}
		if len(res.Reresolved) > 0 {
			l.Info("re-resolved every mod: " + strings.Join(res.Reresolved, "; "))
		}
		printLocalFiles(l, local)
		res.printItems(l)
		if optional > 0 {
			optionalNudge(l, optional)
		}
		if res.IsEmpty() {
			printUpToDate(l, "already up to date", local, cmd.Flags().Args())
		}
		if res.Synced != nil {
			res.Synced.print(l)
			if hasChildren {
				l.Nudge("Build the instances synced from here", "shulker sync")
			}
			return
		}
		if !res.IsEmpty() {
			l.Nudge("Download and build what changed", "shulker install")
		}
	})
}

// relockOptions shape a relock. With keepUnchanged, a relock that changes nothing writes nothing,
// so a sync on every launch doesn't fill the history with copies. linked is the modpack a link just
// pointed the project at.
type relockOptions struct {
	keepUnchanged bool
	linked        string
}

// relockProject re-resolves p's lock with run and saves it.
func (a *app) relockProject(cmd *cobra.Command, p *project.Project, opts relockOptions, run func(*project.Project, *resolve.Resolver) (pin string, err error)) (relocked, error) {
	if err := p.RequireLock(); err != nil {
		return relocked{}, err
	}
	stale := p.IsLockStale()
	original, err := json.Marshal(p.Lock)
	if err != nil {
		return relocked{}, err
	}
	r, err := a.resolverFor(cmd.Context(), p, packMode{isRelocking: true, linked: opts.linked})
	if err != nil {
		return relocked{}, err
	}
	before := r.Snapshot()
	if err := a.resolveMovedRefs(cmd.Context(), p, r); err != nil {
		return relocked{}, err
	}
	reresolved, err := r.Reconcile(cmd.Context())
	if err != nil {
		return relocked{}, err
	}
	if reresolved == nil {
		reresolved = []string{}
	}
	pin, err := run(p, r)
	if err != nil {
		return relocked{}, err
	}
	if _, err := r.Reconcile(cmd.Context()); err != nil {
		return relocked{}, err
	}
	v, err := r.Validate()
	if err != nil {
		return relocked{}, err
	}
	if err := v.Err(); err != nil {
		return relocked{}, err
	}
	rl := relocked{
		lockChanges: lockChanges{Changes: r.Changes(before), Reresolved: reresolved, Pin: pin, Suggestions: v.Recommended()},
		validation:  v,
	}
	placements := (&build.Builder{Manifest: r.Manifest, Lock: r.Lock, Packs: r.Packs}).Placements()
	rl.printItems = func(l *out.Lines) {
		printChanges(l, rl.Changes, v.Suggestions, p.Manifest.Sides(), placements)
	}
	a.warn(r.Warnings)
	a.warn(v.Warnings)
	a.warn(unshippedWarnings(rl.Changes, p.Manifest.Sides(), r.Lock.Mods, placements))
	if opts.keepUnchanged && !stale {
		now, err := json.Marshal(p.Lock)
		if err != nil {
			return relocked{}, err
		}
		if bytes.Equal(original, now) {
			return rl, nil
		}
	}
	// An instance keeps what it had before the manifest and lock are rewritten,
	// which is the state a rollback puts back.
	if side, ok := p.Manifest.InPlaceSide(); ok {
		keep := p.Manifest.HistoryKeep()
		if _, err := build.TakeHistory(p.Dir, keep, build.HistoryEntry{Side: side, Reason: cmd.Name()}); err != nil {
			return relocked{}, err
		}
		warning, err := build.HistoryWarning(p.Dir, keep)
		if err != nil {
			return relocked{}, err
		}
		if warning != "" {
			a.printer.Warn("%s", warning)
		}
	}
	if err := p.SaveManifest(); err != nil {
		return relocked{}, err
	}
	if err := p.SaveLock(); err != nil {
		return relocked{}, err
	}
	a.printer.LockStale = false
	rl.wasSaved = true
	return rl, nil
}

func (a *app) resolveMovedRefs(ctx context.Context, p *project.Project, r *resolve.Resolver) error {
	modpacks := p.Manifest.Modpacks()
	for i, l := range r.Packs {
		mp := modpacks[l.Name]
		pinned, locked := p.Lock.Modpacks[l.Name]
		if !locked || pinned.Source != mp.Source || (pinned.Ref == mp.Ref && pinned.Path == mp.Path) {
			continue
		}
		store, err := a.packStore(p)
		if err != nil {
			return err
		}
		loaded, err := store.Resolve(ctx, l.Name, mp)
		if err != nil {
			return err
		}
		r.Packs[i] = loaded
	}
	return nil
}

func (a *app) outdatedCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "outdated [mod...]",
		Annotations: reads(),
		Short:       "Show mods with a newer compatible version (dry run of update)",
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
			local, rest := splitLocalFiles(p.Manifest, args)
			res := []resolve.Outdated{}
			if len(local) == 0 || len(rest) > 0 {
				if res, err = r.Outdated(cmd.Context(), rest); err != nil {
					return err
				}
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				printLocalFiles(l, local)
				if len(res) == 0 {
					printUpToDate(l, "all mods are up to date", local, args)
					return
				}
				var items []out.Item
				for _, o := range res {
					it := out.Item{Kind: out.Change, Name: o.ID, From: o.Current, To: o.Latest}
					if o.Modpack {
						it.Aside = append(it.Aside, "modpack")
					}
					if o.Pinned {
						it.Aside = append(it.Aside, "pinned")
					}
					items = append(items, it)
				}
				l.Items(items...)
				l.Nudge("Move the lock to these versions", "shulker update")
			})
		},
	}
}

// splitLocalFiles takes the local files out of the named entries, since no provider has a newer
// version of one.
func splitLocalFiles(m *manifest.Manifest, args []string) (local, rest []string) {
	for _, arg := range args {
		if m.IsLocalFile(arg) {
			local = append(local, arg)
			continue
		}
		rest = append(rest, arg)
	}
	return local, rest
}

func printLocalFiles(l *out.Lines, local []string) {
	for _, key := range local {
		l.Info(key + " is a local file; nothing to check")
	}
}

// printUpToDate says the named entries are up to date, leaving out the local files, which have no
// newer version to be behind.
func printUpToDate(l *out.Lines, text string, local, named []string) {
	switch {
	case len(local) == 0:
		l.OK(text, "")
	case len(local) < len(named):
		l.OK("the rest are up to date", "")
	}
}

func printChanges(l *out.Lines, c *resolve.Changes, suggestions []resolve.Suggestion, sides []string, placements map[string]build.Placement) {
	var items []out.Item
	for _, p := range c.Platform {
		if p.From == "" {
			items = append(items, out.Item{Kind: out.Add, Name: p.ID, Version: p.To})
			continue
		}
		items = append(items, out.Item{Kind: out.Change, Name: p.ID, From: p.From, To: p.To})
	}
	for _, p := range c.Modpacks {
		switch {
		case p.From == "":
			items = append(items, out.Item{Kind: out.Add, Name: p.Name, Version: p.To, Aside: []string{"modpack"}})
		case p.To == "":
			items = append(items, out.Item{Kind: out.Drop, Name: p.Name, Aside: []string{"modpack"}})
		default:
			items = append(items, out.Item{Kind: out.Change, Name: p.Name, From: p.From, To: p.To, Aside: []string{"modpack"}})
		}
	}
	for _, m := range c.Added {
		place := placements[m.ID]
		it := out.Item{Kind: out.Add, Name: m.ID, Version: m.VersionNumber, Sides: place.Sides, OfSides: len(sides)}
		if m.Side != "" && m.Side != "both" {
			it.Aside = append(it.Aside, m.Side+" only")
		}
		if text := conditionText("os", place.OS); text != "" {
			it.Aside = append(it.Aside, text)
		}
		if text := conditionText("feature", place.Feature); text != "" {
			if len(place.Sides) == 0 && isSideDeclared(sides, m.Side) {
				text += ", off on every side"
			}
			it.Aside = append(it.Aside, text)
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
		if s.Kind != "optional" && s.InstalledAs == "" {
			items = append(items, out.Item{Kind: out.Note, Name: s.Mod, Aside: []string{s.Kind + " " + s.On + ", not installed"}})
		}
	}
	l.Items(items...)
}

func unshippedWarnings(c *resolve.Changes, sides []string, mods map[string]lock.Mod, placements map[string]build.Placement) []string {
	var warnings []string
	for _, m := range c.Added {
		if _, isMod := mods[m.ID]; !isMod || len(m.RequiredBy) > 0 || len(sides) == 0 || isSideDeclared(sides, m.Side) {
			continue
		}
		if place := placements[m.ID]; len(place.OS) > 0 || len(place.Feature) > 0 {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("%s is %s only, so no side of this project ships it; shulker set requires.%s.side both ships it anyway", m.ID, m.Side, m.ID))
	}
	return warnings
}

func isSideDeclared(sides []string, side string) bool {
	if side == "" || side == "both" {
		return len(sides) > 0
	}
	return slices.Contains(sides, side)
}

// conditionText reads a condition list back as the manifest means it: any of
// the plain names, and none of the negated ones.
func conditionText(kind string, list manifest.StringList) string {
	var wanted, parts []string
	for _, item := range list {
		if name, negated := strings.CutPrefix(item, "!"); negated {
			parts = append(parts, "not "+name)
		} else {
			wanted = append(wanted, item)
		}
	}
	if len(wanted) > 0 {
		parts = append([]string{strings.Join(wanted, " or ")}, parts...)
	}
	if len(parts) == 0 {
		return ""
	}
	return kind + ": " + strings.Join(parts, " and ")
}

func sideText(side string) string {
	if side == "" || side == "both" {
		return "client and server"
	}
	return side + " only"
}
