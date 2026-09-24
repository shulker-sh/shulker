package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/saves"
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
type savesTarget struct {
	Group     string `json:"group,omitempty"`
	Dir       string `json:"dir,omitempty"`
	WorldsDir string `json:"worldsDir"`
	World     string `json:"world,omitempty"`
	backups   string
	via       string
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
			return onSaves(a, where, func(n, of int) *out.Error {
				return out.Errorf("saves-failed", "%d of %d targets failed to read their saves", n, of)
			}, a.savesView, a.printSavesView)
		},
	}
	where.register(cmd, "show this save group rather than an instance", "show every instance's worlds and backups")
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
	where.register(cmd, "prune this save group's backups rather than an instance's", "prune every instance's backups")
	cmd.Flags().IntVar(&keep, "keep", 0, "how many of the newest backups to keep (required)")
	return cmd
}

func pruneBackups(target savesTarget, keep int) (savesPruned, error) {
	pruned, err := saves.Prune(target.backups, keep)
	if err != nil {
		return savesPruned{}, err
	}
	left, err := saves.Backups(target.backups)
	if err != nil {
		return savesPruned{}, err
	}
	return savesPruned{savesTarget: target, Pruned: pruned, Kept: len(left)}, nil
}

func (s savesPruned) print(l *out.Lines) {
	kept := plural(s.Kept, "backup", "backups") + " left"
	if len(s.Pruned) == 0 {
		l.Info("nothing to prune; " + kept)
		return
	}
	items := make([]out.Item, 0, len(s.Pruned))
	for _, b := range s.Pruned {
		items = append(items, out.Item{Kind: out.Drop, Name: b.ID})
	}
	l.Items(items...)
	l.OK("pruned "+plural(len(s.Pruned), "backup", "backups"), kept)
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
			l.Info("no save groups yet; `shulker link shulker` makes an instance that joins group " + saves.Default)
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
	backups, err := saves.Backups(target.backups)
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
		l.Info("no world " + view.World + " in " + view.WorldsDir)
	default:
		l.Info("no worlds in " + view.WorldsDir)
	}
	for _, w := range view.Worlds {
		l.Plain(t.Grey(t.GlyphDot()) + " " + t.Bold(w))
	}
	l.Blank()
	l.Heading("Backups")
	if len(view.Backups) == 0 {
		l.Info("no backups yet; `" + a.savesCommand(view.savesTarget, "backup") + "` takes one")
		return
	}
	width := len(strconv.Itoa(len(view.Backups)))
	for _, b := range view.Backups {
		label := fmt.Sprintf("%*d)", width, b.N)
		l.Plain(t.Cyan(label) + " " + t.Bold(b.ID) + " " + t.Grey(backupAside(b.Backup)))
	}
	l.Nudge("Restore one", a.savesCommand(view.savesTarget, "restore <n>"))
}

// savesCommand is command, with any arguments, run on the target saves shows, selected the way
// saves was.
func (a *app) savesCommand(target savesTarget, command string) string {
	switch {
	case target.via != "":
		return "shulker -i " + shellWord(target.via) + " " + command
	case target.Dir == "":
		return "shulker " + command + " --group " + target.Group
	case a.instance != "":
		return "shulker -i " + shellWord(a.instance) + " " + command
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

func backupAside(b saves.Backup) string {
	parts := []string{"taken " + b.Taken.Format("2006-01-02 15:04")}
	switch b.Reason {
	case "backup":
		parts = append(parts, "on request")
	case "restore":
		parts = append(parts, "before a restore")
	case "":
	default:
		parts = append(parts, "before "+b.Reason)
	}
	if b.Worlds > 0 {
		parts = append(parts, plural(b.Worlds, "world", "worlds"))
	}
	parts = append(parts, out.HumanBytes(b.Size))
	if b.Minecraft != "" {
		parts = append(parts, "Minecraft "+b.Minecraft)
	}
	if b.Loader != "" {
		parts = append(parts, strings.TrimSpace(b.Loader+" "+b.LoaderVersion))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func worldCount(n int) string {
	if n == 0 {
		return "no worlds"
	}
	return plural(n, "world", "worlds")
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
		t := groupTarget(r, group)
		if info, err := os.Stat(t.WorldsDir); err != nil || !info.IsDir() || !saves.IsValidGroup(group) {
			return savesTarget{}, out.Errorf("group-not-found", "no save group %q in %s", group, r.Saves)
		}
		return t, nil
	}
	dir, err := a.scopeDir()
	if err != nil {
		return savesTarget{}, err
	}
	return a.savesTargetAt(dir)
}

// savesTargetAt is the target a directory's worlds belong to: its save group when it is a shulker
// instance in one, and the directory itself otherwise.
func (a *app) savesTargetAt(dir string) (savesTarget, error) {
	r, err := a.roots()
	if err != nil {
		return savesTarget{}, err
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return savesTarget{}, err
	}
	g, _, err := a.saveGroupOf(dir)
	if err != nil {
		return savesTarget{}, err
	}
	if g != saves.None {
		t := groupTarget(r, g)
		t.Dir = dir
		return t, nil
	}
	m, _, inPlace, err := inPlaceManifest(dir)
	if err != nil {
		return savesTarget{}, err
	}
	if !inPlace {
		m = nil
	}
	w, err := build.WorldsOf(dir, m)
	if err != nil {
		return savesTarget{}, err
	}
	return savesTarget{Dir: dir, WorldsDir: w.Dir, World: w.Level, backups: filepath.Join(dir, instance.Dir, "backups")}, nil
}

func groupTarget(r rootDirs, group string) savesTarget {
	return savesTarget{Group: group, WorldsDir: filepath.Join(r.Saves, group), backups: filepath.Join(r.Backups, group)}
}

// saveGroupOf is the group a registered shulker instance joins, and None for every other directory,
// since only instances shulker launches itself share worlds. owned says whether dir is one.
func (a *app) saveGroupOf(dir string) (group string, owned bool, err error) {
	in, ok := a.registeredInstance(dir)
	if !ok || in.Launcher != launcher.Shulker.Name {
		return saves.None, false, nil
	}
	f, err := instance.Load(dir)
	if errors.Is(err, instance.ErrNotFound) {
		return saves.Default, true, nil
	}
	if err != nil {
		return "", true, err
	}
	if f.Settings.SavesGroup == "" {
		return saves.Default, true, nil
	}
	return f.Settings.SavesGroup, true, nil
}

// linkSaves points a shulker instance's saves/ at its save group. Any other directory keeps its
// own worlds, and gets nil.
func (a *app) linkSaves(dir string) (*saves.Result, error) {
	group, owned, err := a.saveGroupOf(dir)
	if err != nil || !owned {
		return nil, err
	}
	r, err := a.roots()
	if err != nil {
		return nil, err
	}
	res, err := saves.Link(dir, r.Saves, group)
	if err != nil {
		return nil, err
	}
	if res.Conflict != "" {
		a.printer.Drop()
		a.printer.Warn("%s", res.Conflict)
	}
	return &res, nil
}

// savesRow is the tree row a sync prints when it relinked saves/, naming the worlds the game now
// lists.
func savesRow(res *saves.Result) (out.Row, bool) {
	if res == nil || !res.Changed {
		return out.Row{}, false
	}
	text := "group " + res.Group
	if res.Group == saves.None {
		text = "kept in the instance"
	}
	if res.Moved {
		text += ", moved the instance's worlds in"
	}
	if len(res.Worlds) == 0 {
		return out.Row{Label: "saves", Text: text + ", no worlds yet"}, true
	}
	return out.Row{Label: "saves", Text: text + ", showing " + strings.Join(res.Worlds, ", ")}, true
}
