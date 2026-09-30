package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/security"
	"shulker.sh/shulker/internal/sync"
)

func (a *app) updateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "update [mod...]",
		Annotations: acts(),
		Aliases:     []string{"upgrade"},
		Short:       "Re-resolve mods to the newest compatible versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			plan := relockPlan{
				buildsInPlace: true,
				local: func(m *manifest.Manifest) []string {
					local, _ := resolve.SplitLocalFiles(m, args)
					return local
				},
			}
			return a.relock(cmd, plan, func(p *project.Project, r *resolve.Resolver) (string, error) {
				local, args := resolve.SplitLocalFiles(p.Manifest, args)
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
					store, err := a.packStore(p)
					if err != nil {
						return "", err
					}
					loaded, err := r.RefreshModpacks(cmd.Context(), store, p, func(manifest.Require) bool { return true }, resolve.FailUnreachable)
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
	a.scopeFlags(cmd)
	a.registerEveryFetch(cmd)
	return cmd
}

func (a *app) pinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "pin <mod> [version|url]",
		Annotations: acts(),
		Short:       "Pin a mod to a provider version id or URL, or to its locked version",
		Args:        rangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			version := ""
			if len(args) == 2 {
				version = args[1]
			}
			d, err := a.deps()
			if err != nil {
				return err
			}
			u, isURL, err := d.Providers.ParseURL(version)
			if err != nil {
				return err
			}
			pinned := func(l *out.Lines, res lockChanges) { l.OK("Pinned "+args[0], res.Pin) }
			return a.relock(cmd, relockPlan{ok: pinned}, func(_ *project.Project, r *resolve.Resolver) (string, error) {
				if isURL {
					return r.PinURL(cmd.Context(), args[0], u)
				}
				return r.Pin(cmd.Context(), args[0], version)
			})
		},
	}
	a.scopeFlags(cmd)
	return cmd
}

func (a *app) unpinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "unpin <mod>",
		Annotations: acts(),
		Short:       "Remove a mod's pin and re-resolve it",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			unpinned := func(l *out.Lines, _ lockChanges) { l.OK("Unpinned "+args[0], "") }
			return a.relock(cmd, relockPlan{ok: unpinned}, func(_ *project.Project, r *resolve.Resolver) (string, error) {
				return "", r.Unpin(cmd.Context(), args[0])
			})
		},
	}
	a.scopeFlags(cmd)
	return cmd
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
	dropped    error
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
	// isFetched marks a command that already fetched what it added, so no install nudge follows.
	isFetched bool
	// dropsFailing keeps what validates of what the command added, and fails with the rest.
	dropsFailing bool
	// upToDate says why nothing changed; nil says the project is already up to date.
	upToDate func(l *out.Lines)
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
	_, statErr := os.Stat(p.LockPath())
	created := errors.Is(statErr, os.ErrNotExist)
	rl, err := a.relockOpened(cmd, p, relockOptions{dropsFailing: plan.dropsFailing}, run)
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
		synced, err := a.buildInPlace(cmd.Context(), p.Dir, syncRequest{Request: sync.Request{Side: side, Backup: "update"}})
		if err != nil {
			return err
		}
		rl.Synced = &synced
		// The nudge is only a hint: a registry it can't read mustn't fail an update that is done.
		children, _ := a.syncedFrom(p)
		hasChildren = len(children) > 0
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
	if rl.dropped != nil && a.printer.JSON {
		e := out.AsError(rl.dropped)
		e.Data = res
		return e
	}
	err = a.printer.Emit(res, func(l *out.Lines) {
		if plan.ok != nil {
			plan.ok(l, res)
		}
		switch {
		case created:
			l.OK("Created shulker.lock", "")
		case len(res.Reresolved) > 0:
			l.Info("Re-resolved every mod: " + strings.Join(res.Reresolved, "; ") + ".")
		}
		printLocalFiles(l, local)
		res.printItems(l)
		if optional > 0 {
			optionalNudge(l, optional)
		}
		if key := unchosenShader(p.Manifest, res.Added); key != "" {
			l.Nudge("Turn it on in the game with", "shulker set client.shader "+key)
		}
		if !created && res.IsEmpty() {
			if plan.upToDate != nil {
				plan.upToDate(l)
			} else {
				printUpToDate(l, "Already up to date", local, cmd.Flags().Args())
			}
		}
		if res.Synced != nil {
			res.Synced.print(l)
			if hasChildren {
				l.Nudge("Build the instances synced from here", "shulker sync")
			}
			return
		}
		if !res.IsEmpty() && !plan.isFetched {
			l.Nudge("Download and build what changed", "shulker install")
		}
	})
	if err != nil {
		return err
	}
	return rl.dropped
}

// relockOptions shape a relock. With keepUnchanged, a relock that changes nothing writes nothing,
// so a sync on every launch doesn't fill the history with copies. linked is the modpack a link just
// pointed the project at.
type relockOptions struct {
	keepUnchanged bool
	linked        string
	dropsFailing  bool
}

// relockOpened relocks the project a command has already opened, prints what the relock warned
// of, and shapes its changes for the command's result.
func (a *app) relockOpened(cmd *cobra.Command, p *project.Project, opts relockOptions, run func(*project.Project, *resolve.Resolver) (pin string, err error)) (relocked, error) {
	if err := p.RequireLock(); err != nil {
		return relocked{}, err
	}
	r, err := a.resolverFor(cmd.Context(), p, resolve.PackMode{IsRelocking: true, Linked: opts.linked})
	if err != nil {
		return relocked{}, err
	}
	store, err := a.packStore(p)
	if err != nil {
		return relocked{}, err
	}
	res, err := r.Relock(cmd.Context(), store, p, run, resolve.RelockOptions{KeepUnchanged: opts.keepUnchanged, Reason: cmd.Name(), DropsFailing: opts.dropsFailing})
	if err != nil {
		return relocked{}, err
	}
	a.warn(res.Warnings)
	for _, w := range res.SecurityWarnings {
		a.printer.WarnSecurity(w)
	}
	rl := relocked{lockChanges: *a.lockChangesOf(p, &res), validation: res.Validation, wasSaved: res.WasSaved, dropped: res.Dropped}
	if res.WasSaved {
		a.printer.LockStale = false
	}
	return rl, nil
}

// lockChangesOf shapes a relock's changes for a command's result and its printing.
func (a *app) lockChangesOf(p *project.Project, res *resolve.Relocked) *lockChanges {
	c := &lockChanges{Changes: res.Changes, Reresolved: res.Reresolved, Pin: res.Pin, Suggestions: res.Validation.Recommended()}
	followed := a.followedModpack(p)
	c.printItems = func(l *out.Lines) {
		shown := *c.Changes
		shown.Modpacks = slices.DeleteFunc(slices.Clone(shown.Modpacks), func(m resolve.ModpackChange) bool { return m.Name == followed })
		printChanges(l, &shown, res.Validation.Suggestions, p.Manifest.Sides(), res.Placements)
	}
	return c
}

// followedModpack is the modpack a linked instance follows, whose pin moves with every change to
// its source: the mods that moved with it are the change worth reading, not the pin.
func (a *app) followedModpack(p *project.Project) string {
	instances, err := a.loadInstances()
	if err != nil {
		return ""
	}
	i, ok := config.FindInstance(instances, p.Dir)
	if !ok || instances[i].Source == "" {
		return ""
	}
	return project.ModpackKey(p.Manifest, instances[i].Source)
}

func (a *app) outdatedCmd() *cobra.Command {
	cmd := &cobra.Command{
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
			local, rest := resolve.SplitLocalFiles(p.Manifest, args)
			res := []resolve.Outdated{}
			if len(local) == 0 || len(rest) > 0 {
				if res, err = r.Outdated(cmd.Context(), rest); err != nil {
					return err
				}
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				printLocalFiles(l, local)
				if len(res) == 0 {
					printUpToDate(l, "Everything is up to date", local, args)
					return
				}
				var items []out.Item
				var held []*resolve.Held
				for _, o := range res {
					if o.Held != nil {
						held = append(held, o.Held)
					}
				}
				moves := false
				for _, o := range res {
					it := out.Item{Kind: out.Change, Name: o.ID, From: o.Current, To: o.Latest}
					if o.Modpack {
						it.Aside = append(it.Aside, "modpack")
					}
					if o.Pinned {
						it.Aside = append(it.Aside, "pinned")
					}
					if o.Held != nil {
						age := o.Held.Text()
						if len(held) > 1 {
							age = "id " + o.Held.SkippedID + ", " + age
						}
						if o.Latest == o.Current {
							it.To = o.Held.Skipped
							it.Aside = append(it.Aside, "held back: "+age)
						} else {
							it.Aside = append(it.Aside, o.Held.Skipped+" held back: "+age)
						}
					}
					moves = moves || o.Latest != o.Current
					items = append(items, it)
				}
				l.Items(items...)
				if moves {
					l.Nudge("Move the lock to these versions", "shulker update")
				}
				switch len(held) {
				case 0:
					return
				case 1:
					n := held[0].PinNudge()
					l.Nudge(n.Lead, n.Command)
				default:
					l.Nudge("Take a held-back version now", "shulker pin <mod> <id>")
				}
				l.Nudge(security.Nudge.Lead, security.Nudge.Command)
			})
		},
	}
	a.scopeFlags(cmd)
	return cmd
}

func printLocalFiles(l *out.Lines, local []string) {
	for _, key := range local {
		l.Info(key + " is a local file; nothing to check.")
	}
}

// printUpToDate says the named entries are up to date, leaving out the local files, which have no
// newer version to be behind.
func printUpToDate(l *out.Lines, text string, local, named []string) {
	switch {
	case len(local) == 0:
		l.OK(text, "")
	case len(local) < len(named):
		l.OK("The rest are up to date", "")
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
		if text := conditionText("os", place.OS); text != "" {
			it.Aside = append(it.Aside, text)
		}
		if text := conditionText("feature", place.Feature); text != "" {
			if len(place.Sides) == 0 && resolve.IsSideDeclared(sides, m.Side) {
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
		if s.Kind != "optional" && s.InstalledAs == "" && slices.ContainsFunc(c.Added, func(m resolve.AddedMod) bool { return m.ID == s.Mod }) {
			items = append(items, out.Item{Kind: out.Note, Name: s.Mod, Aside: []string{s.Kind + " " + s.On + ", not installed"}})
		}
	}
	l.Items(items...)
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

// unchosenShader is the first shader a relock added while the manifest names none to turn on, since
// a build only enables the one client.shader names.
func unchosenShader(m *manifest.Manifest, added []resolve.AddedMod) string {
	if m.Client != nil && m.Client.Shader != nil {
		return ""
	}
	shaders := m.Shaders()
	for _, a := range added {
		if _, ok := shaders[a.ID]; ok {
			return a.ID
		}
	}
	return ""
}
