package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/saves"
)

type restoreResult struct {
	savesTarget
	From     saves.Backup     `json:"from"`
	Snapshot *saves.Backup    `json:"snapshot,omitempty"`
	Worlds   []saves.Restored `json:"worlds"`
}

func (a *app) restoreCmd() *cobra.Command {
	var where savesWhere
	var named string
	cmd := &cobra.Command{
		Use:   "restore [n]",
		Short: "Put a backup's worlds back, taking a backup of the worlds there first",
		Long:  "Put the worlds of backup n, newest first and 1 by default, back into the current project or instance, -i or -C, or the save group --group names; with --all, each registered instance's newest backup into it, each save group once. --backup names a backup in that target's backups, or any zip of world folders by its path. Each world in the zip replaces the one there whole; worlds the zip doesn't hold are left alone.",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if named != "" && len(args) > 0 {
				return out.Errorf("usage", "pass an index or --backup, not both: each names the backup to restore")
			}
			if where.sel.all && (named != "" || len(args) > 0) {
				return out.Errorf("usage", "--all restores each target's newest backup; to pick one, name its target with -i, -C or --group")
			}
			return onSaves(a, where, func(n, of int) *out.Error {
				return out.Errorf("restore-failed", "%d of %d targets failed to restore", n, of)
			}, func(target savesTarget) (restoreResult, error) { return a.restore(target, args, named) }, restoreResult.print, "backups-empty")
		},
	}
	where.register(cmd, "restore into this save group rather than an instance", "restore every instance's newest backup")
	cmd.Flags().StringVar(&named, "backup", "", "restore this backup, by its name in the target's backups or a zip's path")
	return cmd
}

func (a *app) restore(target savesTarget, args []string, named string) (restoreResult, error) {
	from, err := a.pickBackup(target, args, named)
	if err != nil {
		return restoreResult{}, err
	}
	archive, err := saves.OpenArchive(from.Path)
	if err != nil {
		return restoreResult{}, err
	}
	defer archive.Close()
	worlds := archive.Worlds
	if target.World != "" {
		if !slices.Contains(worlds, target.World) {
			return restoreResult{}, out.Errorf("world-not-found", "%s holds no world %s, the level-name this server loads", from.Path, target.World)
		}
		worlds = []string{target.World}
	}
	open, err := saves.OpenWorlds(target.WorldsDir, worlds)
	if err != nil {
		return restoreResult{}, err
	}
	if len(open) > 0 {
		e := out.Errorf("world-in-use", "%s open in a running game", plural(len(open), "world is", "worlds are"))
		e.Items = open
		e.Help = "save and quit to the title screen, or stop the server, then restore again"
		return restoreResult{}, e
	}
	res := restoreResult{savesTarget: target, From: from}
	snapshot, err := saves.Take(a.backupSource(target), target.home(), "restore", a.zipping("zipping"))
	if err != nil {
		return restoreResult{}, err
	}
	if snapshot.Path != "" {
		res.Snapshot = &snapshot
	}
	res.Worlds, err = archive.Restore(target.WorldsDir, worlds, func(world string) {
		a.printer.Step("unzipping %s", world)
	})
	if err != nil {
		return restoreResult{}, err
	}
	return res, nil
}

func (res restoreResult) print(l *out.Lines) {
	if res.Snapshot != nil {
		l.Done("kept the worlds there as backup " + res.Snapshot.ID)
	}
	l.OKInto("restored "+plural(len(res.Worlds), "world", "worlds")+" from "+res.From.ID, res.WorldsDir, "")
	items := make([]out.Item, 0, len(res.Worlds))
	for _, w := range res.Worlds {
		kind := out.Add
		if w.Replaced {
			kind = out.Change
		}
		items = append(items, out.Item{Kind: kind, Name: w.Name})
	}
	l.Items(items...)
}

// pickBackup is the backup restore puts back: the one --backup names, else the nth of target's,
// newest first. A --backup value with a path separator, or naming something on disk, is a path, as
// -i's is; any other is a name in target's backups.
func (a *app) pickBackup(target savesTarget, args []string, named string) (saves.Backup, error) {
	if named != "" {
		path := filepath.Join(target.backups, strings.TrimSuffix(named, ".zip")+".zip")
		if _, err := os.Stat(named); err == nil || strings.ContainsRune(named, '/') || strings.ContainsRune(named, filepath.Separator) {
			if path, err = filepath.Abs(named); err != nil {
				return saves.Backup{}, err
			}
		}
		b, err := saves.ReadBackup(path)
		if errors.Is(err, fs.ErrNotExist) {
			return saves.Backup{}, out.Errorf("backup-missing", "there is no backup %s", path)
		}
		return b, err
	}
	n := 1
	if len(args) > 0 {
		var err error
		if n, err = strconv.Atoi(args[0]); err != nil || n < 1 {
			return saves.Backup{}, out.Errorf("usage", "backups are numbered from 1, newest first, not %q", args[0])
		}
	}
	backups, err := saves.Backups(target.backups)
	if err != nil {
		return saves.Backup{}, err
	}
	if len(backups) == 0 {
		return saves.Backup{}, out.Errorf("backups-empty", "%s has no backups yet; `%s` takes one", target.WorldsDir, a.savesCommand(target, "backup"))
	}
	if n > len(backups) {
		return saves.Backup{}, out.Errorf("backup-missing", "there is no backup %d; %s has %s", n, target.WorldsDir, plural(len(backups), "backup", "backups"))
	}
	return backups[n-1], nil
}
