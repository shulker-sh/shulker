package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/saves"
	"shulker.sh/shulker/internal/sync"
)

type savesGroupRow struct {
	saves.Group
	LastBackup string `json:"lastBackup,omitempty"`
}

type backupRow struct {
	N int `json:"n"`
	saves.Backup
}

// savesTarget is whose worlds and backups one command looks at: a save group, or a game
// directory keeping its own. World is set for a server, whose only world is that folder in
// WorldsDir. via is the -i the commands saves prints name it by, when --all reached it.
// savesTarget is a saves.Target with the -i id it was reached through, for the command a nudge
// names.
type savesTarget struct {
	saves.Target
	via string
}

type savesView struct {
	savesTarget
	Worlds  []string    `json:"worlds"`
	Backups []backupRow `json:"backups"`
}

type savesPruned struct {
	savesTarget
	Pruned []saves.Backup `json:"pruned"`
	Kept   int            `json:"kept"`
}

func (a *app) savesCmd() *cobra.Command {
	var where savesWhere
	cmd := &cobra.Command{
		Use:         "saves",
		Annotations: reads(),
		Short:       "Show save groups, or one group's or instance's worlds and backups",
		Long:        "Show the save groups shulker's own instances share worlds through. With -i, -C or --group, show that target's worlds and its backups instead, newest backup first; with --all, every registered instance's, each save group once.",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if where.group == "" && a.instance == "" && !cmd.Flags().Changed("dir") && !where.sel.all && !where.sel.narrows() {
				return a.listSaveGroups()
			}
			if where.sel.all {
				return a.savesAll(where)
			}
			target, err := a.savesTargetFor(where)
			if err != nil {
				return err
			}
			view, err := a.savesView(target)
			if err != nil {
				return err
			}
			return a.printer.Emit(view, func(l *out.Lines) { a.printSavesView(view, l) })
		},
	}
	a.scopeFlags(cmd)
	where.register(cmd, "show this save group rather than an instance.", "show every instance's worlds and backups")
	cmd.AddCommand(a.savesPruneCmd())
	return cmd
}

func (a *app) savesPruneCmd() *cobra.Command {
	var where savesWhere
	var keep int
	cmd := &cobra.Command{
		Use:         "prune --keep <n>",
		Annotations: acts(),
		Short:       "Delete all but the newest backups of a save group or instance",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("keep") {
				return out.Errorf("usage", "saves prune needs --keep <n>, the number of backups to keep, so it never guesses how many to delete")
			}
			if keep < 0 {
				return out.Errorf("usage", "--keep takes 0 or more, not %d", keep)
			}
			return onSaves(a, where, func(n, of int) *out.Error {
				return out.Errorf("saves-failed", "%d of %d targets failed to prune", n, of)
			}, func(target savesTarget) (savesPruned, error) { return pruneBackups(target, keep) }, savesPruned.print)
		},
	}
	a.scopeFlags(cmd)
	where.register(cmd, "prune this save group's backups rather than an instance's.", "prune every instance's backups")
	cmd.Flags().IntVar(&keep, "keep", 0, "how many of the newest backups to keep (required)")
	return cmd
}

func pruneBackups(target savesTarget, keep int) (savesPruned, error) {
	pruned, err := saves.Prune(target.Backups, keep)
	if err != nil {
		return savesPruned{}, err
	}
	left, err := saves.Backups(target.Backups)
	if err != nil {
		return savesPruned{}, err
	}
	return savesPruned{savesTarget: target, Pruned: pruned, Kept: len(left)}, nil
}

func (s savesPruned) print(l *out.Lines) {
	kept := out.Count(s.Kept, "backup", "backups") + " left"
	if len(s.Pruned) == 0 {
		l.Info("Nothing to prune; " + kept)
		return
	}
	items := make([]out.Item, 0, len(s.Pruned))
	for _, b := range s.Pruned {
		items = append(items, out.Item{Kind: out.Drop, Name: b.ID})
	}
	l.Items(items...)
	l.OK("Pruned "+out.Count(len(s.Pruned), "backup", "backups"), kept)
}

func (a *app) listSaveGroups() error {
	r, err := a.roots()
	if err != nil {
		return err
	}
	groups, err := saves.Groups(r.Saves)
	if err != nil {
		return err
	}
	rows := make([]savesGroupRow, 0, len(groups))
	for _, g := range groups {
		row := savesGroupRow{Group: g}
		backups, err := saves.Backups(filepath.Join(r.Backups, g.Name))
		if err != nil {
			return err
		}
		if len(backups) > 0 {
			row.LastBackup = backups[0].Taken.Format("2006-01-02 15:04")
		}
		rows = append(rows, row)
	}
	return a.printer.Emit(rows, func(l *out.Lines) {
		if len(rows) == 0 {
			l.Info("No save groups yet; `shulker link shulker` makes an instance that joins group " + saves.Default + ".")
			return
		}
		t := l.T
		for _, row := range rows {
			aside := []string{worldCount(row.Worlds)}
			if row.Size > 0 {
				aside = append(aside, out.HumanBytes(row.Size))
			}
			if row.LastBackup != "" {
				aside = append(aside, "last backup "+row.LastBackup)
			}
			l.Plain(t.Grey(t.GlyphDot()) + " " + t.Bold(row.Name) + t.Aside(strings.Join(aside, ", ")))
		}
	})
}

func (a *app) savesView(target savesTarget) (savesView, error) {
	worlds, err := saves.Worlds(target.WorldsDir)
	if err != nil {
		return savesView{}, err
	}
	if target.World != "" {
		worlds = slices.DeleteFunc(worlds, func(w string) bool { return w != target.World })
	}
	backups, err := saves.Backups(target.Backups)
	if err != nil {
		return savesView{}, err
	}
	view := savesView{savesTarget: target, Worlds: worlds, Backups: make([]backupRow, 0, len(backups))}
	for i, b := range backups {
		view.Backups = append(view.Backups, backupRow{N: i + 1, Backup: b})
	}
	return view, nil
}

func (a *app) printSavesView(view savesView, l *out.Lines) {
	t := l.T
	l.Heading("Worlds")
	switch {
	case len(view.Worlds) > 0:
	case view.World != "":
		l.Info("No world " + view.World + " in " + view.WorldsDir)
	default:
		l.Info("No worlds in " + view.WorldsDir)
	}
	for _, w := range view.Worlds {
		l.Plain(t.Grey(t.GlyphDot()) + " " + t.Bold(w))
	}
	l.Blank()
	l.Heading("Backups")
	if len(view.Backups) == 0 {
		l.Info("No backups yet; `" + a.savesCommand(view.savesTarget, "backup") + "` takes one.")
		return
	}
	printBackupsTable(view, l)
	l.Nudge("Restore one", a.savesCommand(view.savesTarget, "restore <n>"))
}

// savesAll shows every target --all reaches as one section of worlds and backups, and names the
// targets with neither on one line after them.
func (a *app) savesAll(w savesWhere) error {
	picks, err := a.savesPicks(w)
	if err != nil {
		return err
	}
	runs := []savesRun[savesView]{}
	failures := 0
	for _, p := range picks {
		r := savesRun[savesView]{savesTarget: p.savesTarget, Instances: instanceIDs(p.rows), OK: true}
		err := p.err
		if err == nil {
			var view savesView
			if view, err = a.savesView(p.savesTarget); err == nil {
				r.Result = &view
			}
		}
		if err != nil {
			failures++
			r.OK, r.Error = false, out.AsError(err)
			a.printer.Report(r.Error)
		}
		runs = append(runs, r)
	}
	if failures > 0 {
		e := out.Errorf("saves-failed", "%d of %d targets failed to read their saves", failures, len(picks))
		e.Data = runs
		return e
	}
	return a.printer.Emit(runs, func(l *out.Lines) {
		var empty []string
		first := true
		for i, r := range runs {
			view := r.Result
			if len(view.Worlds) == 0 && len(view.Backups) == 0 {
				empty = append(empty, pickLabel(picks[i]))
				continue
			}
			if !first {
				l.Blank()
			}
			first = false
			l.Heading(savesHeading(l.T, picks[i]))
			a.printSavesSection(*view, l)
		}
		if len(empty) > 0 {
			if !first {
				l.Blank()
			}
			l.Info("No worlds or backups: " + strings.Join(empty, ", "))
		}
	})
}

// savesHeading names a save group by how many instances share it, and one instance by its label
// and launcher.
func savesHeading(t out.Theme, p savesPick) string {
	if len(p.rows) == 1 {
		return t.Bold(p.rows[0].Label()) + instanceAside(t, p.rows[0])
	}
	return t.Bold(p.Group) + " " + t.Grey("save group") + t.Aside(out.Count(len(p.rows), "instance", "instances"))
}

// printSavesSection is one target's worlds and backups under its saves --all heading.
func (a *app) printSavesSection(view savesView, l *out.Lines) {
	t := l.T
	if len(view.Worlds) == 0 {
		l.Info("No worlds")
	}
	for _, w := range view.Worlds {
		l.Plain(t.Grey(t.GlyphDot()) + " " + t.Bold(w))
	}
	l.Blank()
	if len(view.Backups) == 0 {
		l.Info("No backups yet")
		return
	}
	printBackupsTable(view, l)
	l.Nudge("Restore one", a.savesCommand(view.savesTarget, "restore <n>"))
}

func printBackupsTable(view savesView, l *out.Lines) {
	t := l.T
	rows := make([][]string, len(view.Backups))
	for i, b := range view.Backups {
		worlds := ""
		if b.Worlds > 0 {
			worlds = strconv.Itoa(b.Worlds)
		}
		rows[i] = []string{strconv.Itoa(b.N), b.ID, b.Taken.Format("2006-01-02 15:04"), backupReason(b.Backup), worlds, out.HumanBytes(b.Size), backupGame(b.Backup)}
	}
	l.Table([]string{"#", "Backup", "Taken", "Reason", "Worlds", "Size", "Game"}, rows, out.Columns(t.StyleCyan(), t.StyleBold(), t.StyleGrey(), t.Style(), t.StyleGrey()))
}

// savesCommand is command, with any arguments, run on the target saves shows, selected the way
// saves was.
func (a *app) savesCommand(target savesTarget, command string) string {
	switch {
	case target.via != "":
		return "shulker " + command + " -i " + shellWord(target.via)
	case target.Dir == "":
		return "shulker " + command + " --group " + target.Group
	case a.instance != "":
		return "shulker " + command + " -i " + shellWord(a.instance)
	case a.dir != "":
		return "shulker " + command + " -C " + shellWord(target.Dir)
	}
	return "shulker " + command
}

func shellWord(s string) string {
	if strings.ContainsAny(s, " \t'\"$`\\") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}

func backupReason(b saves.Backup) string {
	switch b.Reason {
	case "backup":
		return "on request"
	case "restore":
		return "before a restore"
	case "":
		return ""
	}
	return "before " + b.Reason
}

// backupGame is the Minecraft version, then the loader with its version when the backup recorded them:
// "26.3, Fabric 0.19.5".
func backupGame(b saves.Backup) string {
	parts := []string{}
	if b.Minecraft != "" {
		parts = append(parts, b.Minecraft)
	}
	if b.Loader != "" {
		parts = append(parts, strings.TrimSpace(loader.Title(b.Loader)+" "+b.LoaderVersion))
	}
	return strings.Join(parts, ", ")
}

func worldCount(n int) string {
	if n == 0 {
		return "no worlds"
	}
	return out.Count(n, "world", "worlds")
}

// savesTargetOf resolves --group, or else the directory -i, -C or the current directory names. A
// shulker instance in a group is that group; any other directory keeps its own backups, and its
// worlds wherever build.WorldsOf finds them.
func (a *app) savesTargetOf(group string) (savesTarget, error) {
	r, err := a.roots()
	if err != nil {
		return savesTarget{}, err
	}
	if group != "" {
		if a.instance != "" {
			return savesTarget{}, out.Errorf("usage", "pass --group or -i, not both: each names whose saves to act on")
		}
		t := savesTarget{Target: saves.GroupTarget(r.saves(), group)}
		if info, err := os.Stat(t.WorldsDir); err != nil || !info.IsDir() || !saves.IsValidGroup(group) {
			return savesTarget{}, out.Errorf("group-not-found", "no save group %q in %s", group, r.Saves)
		}
		return t, nil
	}
	dir, err := a.scopeDir()
	if err != nil {
		return savesTarget{}, err
	}
	return a.targetOfDir(dir)
}

func (a *app) targetOfDir(dir string) (savesTarget, error) {
	se, err := a.syncEnv()
	if err != nil {
		return savesTarget{}, err
	}
	t, err := sync.TargetOfDir(se, dir)
	if err != nil {
		return savesTarget{}, err
	}
	return savesTarget{Target: t}, nil
}

// savesRow is the tree row a sync prints when it relinked saves/, naming the worlds the game now
// lists.
func savesRow(res *saves.Result) (out.Row, bool) {
	if res == nil || !res.Changed {
		return out.Row{}, false
	}
	into := "the shared saves"
	if res.Group != saves.Default {
		into = "save group " + strconv.Quote(res.Group)
	}
	switch {
	case res.Group == saves.None:
		return out.Row{Text: "Worlds kept in the instance"}, true
	case res.Moved:
		return out.Row{Text: "Moved " + out.Count(len(res.Worlds), "world", "worlds") + " into " + into}, true
	case res.Group != saves.Default:
		return out.Row{Text: "Worlds from " + into}, true
	}
	return out.Row{}, false
}
