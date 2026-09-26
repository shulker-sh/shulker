package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

type historyRow struct {
	N int `json:"n"`
	build.HistoryEntry
}

type historyShown struct {
	N       int                   `json:"n"`
	Entry   build.HistoryEntry    `json:"entry"`
	Changes []build.HistoryChange `json:"changes"`
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
	e := out.Errorf("not-in-place", "no side builds in place, so this project keeps no history")
	e.Help = "point a side's build directory at \".\" to make it an instance"
	return nil, "", e
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
		Use:         "list",
		Annotations: reads(),
		Aliases:     []string{"ls"},
		Short:       "List history entries, newest first",
		Args:        noArgs,
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
					l.Info("No history entries yet; one is taken before an in-place build changes anything")
					return
				}
				t := l.T
				cells := make([][]string, len(rows))
				for i, r := range rows {
					cells[i] = []string{strconv.Itoa(r.N), r.ID, historyWhen(r.HistoryEntry), historyBefore(r.HistoryEntry), historyContents(r.HistoryEntry)}
				}
				l.Table([]string{"#", "Entry", "Taken", "Before", "Contents"}, cells, out.Columns(t.StyleCyan(), t.StyleBold(), t.StyleGrey()))
				if keep := p.Manifest.HistoryKeep(); keep >= 0 && len(rows) > keep {
					l.Nudge("Trim them", "shulker history prune")
				}
			})
		},
	}
}

func (a *app) historyShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "show [n]",
		Annotations: reads(),
		Short:       "Show a history entry and what restoring it would change",
		Args:        maximumArgs(1),
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
			changes, err := build.HistoryChanges(p.Dir, p.Lock, e)
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
				if e.Datapacks > 0 {
					rows = append(rows, out.Row{Label: "datapacks", Text: strconv.Itoa(e.Datapacks)})
				}
				l.Tree(rows...)
				l.Blank()
				if len(changes) == 0 {
					l.Info("Restoring it would change nothing")
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

func changeItem(c build.HistoryChange) out.Item {
	switch {
	case c.From == "":
		return out.Item{Kind: out.Add, Name: c.Mod, Version: c.To}
	case c.To == "":
		return out.Item{Kind: out.Drop, Name: c.Mod, Version: c.From}
	}
	return out.Item{Kind: out.Change, Name: c.Mod, From: c.From, To: c.To}
}

func (a *app) historyPruneCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "prune",
		Annotations: acts(),
		Short:       "Remove history entries beyond the number the manifest keeps",
		Args:        noArgs,
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
					l.Info(fmt.Sprintf("Nothing to prune; %s", historyKept(len(left), keep)))
					return
				}
				items := make([]out.Item, 0, len(pruned))
				for _, e := range pruned {
					items = append(items, out.Item{Kind: out.Drop, Name: e.ID})
				}
				l.Items(items...)
				l.OK(fmt.Sprintf("Pruned %s", out.Count(len(pruned), "history entry", "history entries")), historyKept(len(left), keep))
			})
		},
	}
}

func (a *app) rollbackCmd() *cobra.Command {
	var prune bool
	cmd := &cobra.Command{
		Use:         "rollback [n]",
		Annotations: acts(),
		Short:       "Restore a history entry and build it in place",
		Args:        maximumArgs(1),
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
			lf, err := a.loadLocal(p.Dir)
			if err != nil {
				return err
			}
			overrides, err := featureFlags{}.overrides(b, lf.Features)
			if err != nil {
				return err
			}
			rep, err := b.Build(side, build.Options{Features: overrides, NoHistory: true})
			if err != nil {
				return err
			}
			a.warnBuild(side, false, rep.Warnings, rep.State, a.forceCommand(p, side, rep.Dir))
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
					l.Done("Kept this state as history entry " + res.Snapshot)
				}
				for _, e := range res.Pruned {
					l.Done("Pruned history entry " + e.ID)
				}
				l.OKInto("Rolled back to "+e.ID, p.Dir, "")
				printReport(l, rep)
			})
		},
	}
	cmd.Flags().BoolVar(&prune, "prune", false, "also trim history to the number the manifest keeps")
	return cmd
}

func historyContents(e build.HistoryEntry) string {
	parts := []string{out.Count(e.Mods, "mod", "mods")}
	if e.ResourcePacks > 0 {
		parts = append(parts, out.Count(e.ResourcePacks, "resource pack", "resource packs"))
	}
	if e.Shaders > 0 {
		parts = append(parts, out.Count(e.Shaders, "shader", "shaders"))
	}
	if e.Datapacks > 0 {
		parts = append(parts, out.Count(e.Datapacks, "datapack", "datapacks"))
	}
	return strings.Join(parts, ", ")
}

// historyTaken is when the entry was taken and what it preceded, for the show heading's row.
func historyTaken(e build.HistoryEntry) string {
	if before := historyBefore(e); before != "" {
		return historyWhen(e) + ", before " + before
	}
	return historyWhen(e)
}

func historyWhen(e build.HistoryEntry) string {
	if t, err := time.Parse(time.RFC3339, e.TakenAt); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return e.TakenAt
}

// historyBefore is what the entry was taken ahead of: a rollback, a side's build, or the command.
func historyBefore(e build.HistoryEntry) string {
	switch {
	case e.Reason == "rollback":
		return "a rollback"
	case e.Reason == "build" && e.Side != "":
		return "build " + e.Side
	}
	return e.Reason
}

func historyPlatform(e build.HistoryEntry) string {
	if e.Loader == "" {
		return "Minecraft " + e.Minecraft
	}
	return "Minecraft " + e.Minecraft + ", " + e.Loader
}

func historyKept(left, keep int) string {
	if keep < 0 {
		return out.Count(left, "entry", "entries") + " kept, every one"
	}
	return out.Count(left, "entry", "entries") + " kept"
}
