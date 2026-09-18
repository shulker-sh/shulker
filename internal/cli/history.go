package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

type historyRow struct {
	N int `json:"n"`
	build.HistoryEntry
}

type historyChange struct {
	Mod  string `json:"mod"`
	Kind string `json:"kind,omitempty"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

type historyShown struct {
	N       int                `json:"n"`
	Entry   build.HistoryEntry `json:"entry"`
	Changes []historyChange    `json:"changes"`
}

type historyPruned struct {
	Pruned []build.HistoryEntry `json:"pruned"`
	Kept   int                  `json:"kept"`
}

type rollbackResult struct {
	Entry    build.HistoryEntry   `json:"entry"`
	Snapshot string               `json:"snapshot,omitempty"`
	Pruned   []build.HistoryEntry `json:"pruned,omitempty"`
	Builds   []*build.Report      `json:"builds"`
}

func (a *app) historyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Inspect and trim the states kept before in-place builds",
	}
	cmd.AddCommand(a.historyListCmd(), a.historyShowCmd(), a.historyPruneCmd())
	return cmd
}

// instanceProject names the side that builds in place, the only kind of build
// that takes history.
func (a *app) instanceProject() (*project.Project, string, error) {
	p, err := a.openProject()
	if err != nil {
		return nil, "", err
	}
	if side, ok := p.Manifest.InPlaceSide(); ok {
		return p, side, nil
	}
	return nil, "", out.Errorf("not-in-place", "no side builds in place, so this project keeps no history; point a side's build directory at \".\" to make it an instance")
}

func historyIndex(args []string) (int, error) {
	if len(args) == 0 {
		return 1, nil
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		return 0, out.Errorf("usage", "history entries are numbered from 1, newest first, not %q", args[0])
	}
	return n, nil
}

func (a *app) historyListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List history entries, newest first",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _, err := a.instanceProject()
			if err != nil {
				return err
			}
			entries, err := build.History(p.Dir)
			if err != nil {
				return err
			}
			rows := make([]historyRow, 0, len(entries))
			for i, e := range entries {
				rows = append(rows, historyRow{N: i + 1, HistoryEntry: e})
			}
			return a.printer.Emit(rows, func(l *out.Lines) {
				if len(rows) == 0 {
					l.Info("no history entries yet; one is taken before an in-place build changes anything")
					return
				}
				width := len(strconv.Itoa(len(rows)))
				for _, r := range rows {
					label := fmt.Sprintf("%*d)", width, r.N)
					l.Plain(l.T.Cyan(label) + " " + l.T.Bold(r.ID) + " " + l.T.Grey(historyAside(r.HistoryEntry)))
				}
				if keep := p.Manifest.HistoryKeep(); keep >= 0 && len(rows) > keep {
					l.Nudge("Trim them", "shulker history prune")
				}
			})
		},
	}
}

func (a *app) historyShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [n]",
		Short: "Show a history entry and what restoring it would change",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _, err := a.instanceProject()
			if err != nil {
				return err
			}
			n, err := historyIndex(args)
			if err != nil {
				return err
			}
			e, err := build.PickHistory(p.Dir, n)
			if err != nil {
				return err
			}
			changes, err := historyChanges(p, e)
			if err != nil {
				return err
			}
			return a.printer.Emit(historyShown{N: n, Entry: e, Changes: changes}, func(l *out.Lines) {
				l.Heading(fmt.Sprintf("History entry %d  %s", n, e.ID))
				rows := []out.Row{{Label: "taken", Text: historyTaken(e)}}
				if e.Minecraft != "" {
					rows = append(rows, out.Row{Label: "platform", Text: historyPlatform(e)})
				}
				rows = append(rows, out.Row{Label: "mods", Text: strconv.Itoa(e.Mods)})
				if e.ResourcePacks > 0 {
					rows = append(rows, out.Row{Label: "resource packs", Text: strconv.Itoa(e.ResourcePacks)})
				}
				if e.Shaders > 0 {
					rows = append(rows, out.Row{Label: "shaders", Text: strconv.Itoa(e.Shaders)})
				}
				l.Tree(rows...)
				l.Blank()
				if len(changes) == 0 {
					l.Info("restoring it would change nothing")
				} else {
					l.Text("Restoring it would:")
					items := make([]out.Item, 0, len(changes))
					for _, c := range changes {
						items = append(items, changeItem(c))
					}
					l.Items(items...)
				}
				l.Nudge("Restore it", "shulker rollback "+strconv.Itoa(n))
			})
		},
	}
}

func changeItem(c historyChange) out.Item {
	switch {
	case c.From == "":
		return out.Item{Kind: out.Add, Name: c.Mod, Version: c.To}
	case c.To == "":
		return out.Item{Kind: out.Drop, Name: c.Mod, Version: c.From}
	}
	return out.Item{Kind: out.Change, Name: c.Mod, From: c.From, To: c.To}
}

// historyChanges compares what an entry's lock holds with what is locked now,
// across mods, resource packs and shaders, so From is what is installed and To
// is what restoring would put back.
func historyChanges(p *project.Project, e build.HistoryEntry) ([]historyChange, error) {
	was, err := lock.Load(filepath.Join(build.HistoryPath(p.Dir), e.ID, lock.FileName))
	if err != nil {
		return nil, err
	}
	sections := []struct {
		kind string
		now  map[string]string
		was  map[string]string
	}{
		{"", modVersions(p.Lock.Mods), modVersions(was.Mods)},
		{manifest.TypeResourcePack, packVersions(p.Lock.ResourcePacks), packVersions(was.ResourcePacks)},
		{manifest.TypeShader, packVersions(p.Lock.Shaders), packVersions(was.Shaders)},
	}
	changes := []historyChange{}
	for _, s := range sections {
		keys := []string{}
		for key := range s.now {
			keys = append(keys, key)
		}
		for key := range s.was {
			if _, both := s.now[key]; !both {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		for _, key := range keys {
			now, installed := s.now[key]
			then, kept := s.was[key]
			switch {
			case installed && !kept:
				changes = append(changes, historyChange{Mod: key, Kind: s.kind, From: now})
			case !installed && kept:
				changes = append(changes, historyChange{Mod: key, Kind: s.kind, To: then})
			case now != then:
				changes = append(changes, historyChange{Mod: key, Kind: s.kind, From: now, To: then})
			}
		}
	}
	return changes, nil
}

func modVersions(mods map[string]lock.Mod) map[string]string {
	versions := make(map[string]string, len(mods))
	for key, m := range mods {
		versions[key] = m.VersionNumber
	}
	return versions
}

func packVersions(packs map[string]lock.Pack) map[string]string {
	versions := make(map[string]string, len(packs))
	for key, p := range packs {
		versions[key] = p.VersionNumber
	}
	return versions
}

func (a *app) historyPruneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prune",
		Short: "Remove history entries beyond the number the manifest keeps",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _, err := a.instanceProject()
			if err != nil {
				return err
			}
			keep := p.Manifest.HistoryKeep()
			pruned, err := build.PruneHistory(p.Dir, keep)
			if err != nil {
				return err
			}
			left, err := build.History(p.Dir)
			if err != nil {
				return err
			}
			if pruned == nil {
				pruned = []build.HistoryEntry{}
			}
			return a.printer.Emit(historyPruned{Pruned: pruned, Kept: len(left)}, func(l *out.Lines) {
				if len(pruned) == 0 {
					l.Info(fmt.Sprintf("nothing to prune; %s", historyKept(len(left), keep)))
					return
				}
				items := make([]out.Item, 0, len(pruned))
				for _, e := range pruned {
					items = append(items, out.Item{Kind: out.Drop, Name: e.ID})
				}
				l.Items(items...)
				l.OK(fmt.Sprintf("pruned %s", plural(len(pruned), "history entry", "history entries")), historyKept(len(left), keep))
			})
		},
	}
}

func (a *app) rollbackCmd() *cobra.Command {
	var prune bool
	cmd := &cobra.Command{
		Use:   "rollback [n]",
		Short: "Restore a history entry and build it in place",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, side, err := a.instanceProject()
			if err != nil {
				return err
			}
			n, err := historyIndex(args)
			if err != nil {
				return err
			}
			e, err := build.PickHistory(p.Dir, n)
			if err != nil {
				return err
			}
			// The current state becomes an entry of its own first, so a rollback
			// is itself undoable.
			snapshot, err := build.TakeHistory(p.Dir, p.Manifest.HistoryKeep(), build.HistoryEntry{
				Side:   side,
				Reason: "rollback",
			})
			if err != nil {
				return err
			}
			if err := build.RestoreHistory(p.Dir, e); err != nil {
				return err
			}
			p, err = a.openProject()
			if err != nil {
				return err
			}
			if err := a.requireLock(p); err != nil {
				return err
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			lf, err := local.Load(p.Dir)
			if err != nil {
				return err
			}
			overrides, err := featureOverrides(b, lf.Features, featureFlags{})
			if err != nil {
				return err
			}
			rep, err := b.Build(side, build.Options{Dir: buildDir(p, side), Features: overrides, NoHistory: true})
			if err != nil {
				return err
			}
			a.warnFor(side, false, rep.Warnings)
			res := rollbackResult{Entry: e, Snapshot: snapshot.ID, Builds: []*build.Report{rep}}
			if prune {
				dropped, err := build.PruneHistory(p.Dir, p.Manifest.HistoryKeep())
				if err != nil {
					return err
				}
				res.Pruned = dropped
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				if res.Snapshot != "" {
					l.Done("kept this state as history entry " + res.Snapshot)
				}
				for _, e := range res.Pruned {
					l.Done("pruned history entry " + e.ID)
				}
				l.OKInto("rolled back to "+e.ID, p.Dir, "")
				printReport(l, rep)
			})
		},
	}
	cmd.Flags().BoolVar(&prune, "prune", false, "also trim history to the number the manifest keeps")
	return cmd
}

func historyAside(e build.HistoryEntry) string {
	parts := []string{"taken " + historyTaken(e)}
	parts = append(parts, plural(e.Mods, "mod", "mods"))
	if e.ResourcePacks > 0 {
		parts = append(parts, plural(e.ResourcePacks, "resource pack", "resource packs"))
	}
	if e.Shaders > 0 {
		parts = append(parts, plural(e.Shaders, "shader", "shaders"))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func historyTaken(e build.HistoryEntry) string {
	when := e.TakenAt
	if t, err := time.Parse(time.RFC3339, e.TakenAt); err == nil {
		when = t.Local().Format("2006-01-02 15:04")
	}
	switch {
	case e.Reason == "rollback":
		return when + ", before a rollback"
	case e.Reason == "build" && e.Side != "":
		return when + ", before build " + e.Side
	case e.Reason != "":
		return when + ", before " + e.Reason
	}
	return when
}

func historyPlatform(e build.HistoryEntry) string {
	if e.Loader == "" {
		return "Minecraft " + e.Minecraft
	}
	return "Minecraft " + e.Minecraft + ", " + e.Loader
}

func historyKept(left, keep int) string {
	if keep < 0 {
		return plural(left, "entry", "entries") + " kept, every one"
	}
	return plural(left, "entry", "entries") + " kept"
}
