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
// WorldsDir.
type savesTarget struct {
	Group     string `json:"group,omitempty"`
	Dir       string `json:"dir,omitempty"`
	WorldsDir string `json:"worldsDir"`
	World     string `json:"world,omitempty"`
	backups   string
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
	var group string
	cmd := &cobra.Command{
		Use:   "saves",
		Short: "Show save groups, or one group's or instance's worlds and backups",
		Long:  "Show the save groups shulker's own instances share worlds through. With -i, -C or --group, show that target's worlds and its backups instead, newest backup first.",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if group == "" && a.instance == "" && !cmd.Flags().Changed("dir") {
				return a.listSaveGroups()
			}
			target, err := a.savesTargetOf(group)
			if err != nil {
				return err
			}
			return a.showSaves(target)
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "show this save group rather than an instance")
	cmd.AddCommand(a.savesPruneCmd())
	return cmd
}

func (a *app) savesPruneCmd() *cobra.Command {
	var group string
	var keep int
	cmd := &cobra.Command{
		Use:   "prune --keep <n>",
		Short: "Delete all but the newest backups of a save group or instance",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("keep") {
				return out.Errorf("usage", "saves prune needs --keep <n>, the number of backups to keep, so it never guesses how many to delete")
			}
			if keep < 0 {
				return out.Errorf("usage", "--keep takes 0 or more, not %d", keep)
			}
			target, err := a.savesTargetOf(group)
			if err != nil {
				return err
			}
			pruned, err := saves.Prune(target.backups, keep)
			if err != nil {
				return err
			}
			left, err := saves.Backups(target.backups)
			if err != nil {
				return err
			}
			res := savesPruned{savesTarget: target, Pruned: pruned, Kept: len(left)}
			return a.printer.Emit(res, func(l *out.Lines) {
				kept := plural(len(left), "backup", "backups") + " left"
				if len(pruned) == 0 {
					l.Info("nothing to prune; " + kept)
					return
				}
				items := make([]out.Item, 0, len(pruned))
				for _, b := range pruned {
					items = append(items, out.Item{Kind: out.Drop, Name: b.ID})
				}
				l.Items(items...)
				l.OK("pruned "+plural(len(pruned), "backup", "backups"), kept)
			})
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "prune this save group's backups rather than an instance's")
	cmd.Flags().IntVar(&keep, "keep", 0, "how many of the newest backups to keep (required)")
	return cmd
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

func (a *app) showSaves(target savesTarget) error {
	worlds, err := saves.Worlds(target.WorldsDir)
	if err != nil {
		return err
	}
	if target.World != "" {
		worlds = slices.DeleteFunc(worlds, func(w string) bool { return w != target.World })
	}
	backups, err := saves.Backups(target.backups)
	if err != nil {
		return err
	}
	view := savesView{savesTarget: target, Worlds: worlds, Backups: make([]backupRow, 0, len(backups))}
	for i, b := range backups {
		view.Backups = append(view.Backups, backupRow{N: i + 1, Backup: b})
	}
	return a.printer.Emit(view, func(l *out.Lines) {
		t := l.T
		l.Heading("Worlds")
		switch {
		case len(worlds) > 0:
		case target.World != "":
			l.Info("no world " + target.World + " in " + target.WorldsDir)
		default:
			l.Info("no worlds in " + target.WorldsDir)
		}
		for _, w := range worlds {
			l.Plain(t.Grey(t.GlyphDot()) + " " + t.Bold(w))
		}
		l.Blank()
		l.Heading("Backups")
		if len(view.Backups) == 0 {
			l.Info("no backups yet; one is taken before update or sync changes the mods")
			return
		}
		width := len(strconv.Itoa(len(view.Backups)))
		for _, b := range view.Backups {
			label := fmt.Sprintf("%*d)", width, b.N)
			l.Plain(t.Cyan(label) + " " + t.Bold(b.ID) + " " + t.Grey(backupAside(b.Backup)))
		}
	})
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
	parts = append(parts, out.HumanBytes(b.Size))
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
		if info, err := os.Stat(t.WorldsDir); err != nil || !info.IsDir() || !saves.ValidGroup(group) {
			return savesTarget{}, out.Errorf("group-not-found", "no save group %q in %s", group, r.Saves)
		}
		return t, nil
	}
	dir, err := a.scopeDir()
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
	if !ok || in.Launcher != "shulker" {
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
