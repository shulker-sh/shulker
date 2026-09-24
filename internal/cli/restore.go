package cli

import (
	"errors"
	"fmt"
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

// restoreRequest is what restore puts back into each target: the backup args or named picks, and
// the worlds only and as choose from it.
type restoreRequest struct {
	args      []string
	named, as string
	only      []string
}

func (a *app) restoreCmd() *cobra.Command {
	var where savesWhere
	var req restoreRequest
	cmd := &cobra.Command{
		Use:         "restore [n]",
		Annotations: acts(),
		Short:       "Put a backup's worlds back, taking a backup of the worlds there first",
		Long:        "Put the worlds of backup n, newest first and 1 by default, back into the current project or instance, -i or -C, or the save group --group names; with --all, each registered instance's newest backup into it, each save group once. --backup names a backup in that target's backups, or any zip of world folders by its path. Each world in the zip, or each --world names, replaces the one there whole; worlds left out are left alone. --as puts a single world back under another name.",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.args = args
			if req.named != "" && len(args) > 0 {
				return out.Errorf("usage", "pass an index or --backup, not both: each names the backup to restore")
			}
			if req.as != "" && (req.as == "." || !filepath.IsLocal(req.as) || strings.ContainsAny(req.as, `/\`)) {
				return out.Errorf("usage", "--as takes a world's folder name, not %q", req.as)
			}
			if where.sel.all && (req.named != "" || len(args) > 0 || len(req.only) > 0 || req.as != "") {
				e := out.Errorf("usage", "--all restores every world of each target's newest backup")
				e.Help = "to pick a backup or a world, name its target with -i, -C or --group"
				return e
			}
			return onSaves(a, where, func(n, of int) *out.Error {
				return out.Errorf("restore-failed", "%d of %d targets failed to restore", n, of)
			}, func(target savesTarget) (restoreResult, error) { return a.restore(target, req) }, restoreResult.print, "backups-empty")
		},
	}
	where.register(cmd, "restore into this save group rather than an instance", "restore every instance's newest backup")
	cmd.Flags().StringVar(&req.named, "backup", "", "restore this backup, by its name in the target's backups or a zip's path")
	cmd.Flags().StringArrayVar(&req.only, "world", nil, "restore only this world from the backup, by its folder name; repeatable")
	cmd.Flags().StringVar(&req.as, "as", "", "restore the backup's one world under this folder name")
	return cmd
}

func (a *app) restore(target savesTarget, req restoreRequest) (restoreResult, error) {
	from, err := a.pickBackup(target, req.args, req.named)
	if err != nil {
		return restoreResult{}, err
	}
	archive, err := saves.OpenArchive(from.Path)
	if err != nil {
		return restoreResult{}, err
	}
	defer archive.Close()
	worlds, err := restoreScope(archive, target, req.only, req.as)
	if err != nil {
		return restoreResult{}, err
	}
	replacing := worlds
	if req.as != "" {
		replacing = []string{req.as}
	}
	open, err := saves.OpenWorlds(target.WorldsDir, replacing)
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
	snapshot, err := saves.Take(a.backupSource(target), target.Home(), "restore", a.zipping("zipping"))
	if err != nil {
		return restoreResult{}, err
	}
	if snapshot.Path != "" {
		res.Snapshot = &snapshot
	}
	unzipping := func(world string) { a.printer.Step("unzipping %s", world) }
	if req.as != "" {
		w, err := archive.RestoreAs(target.WorldsDir, worlds[0], req.as, unzipping)
		if err != nil {
			return restoreResult{}, err
		}
		res.Worlds = []saves.Restored{w}
	} else if res.Worlds, err = archive.Restore(target.WorldsDir, worlds, unzipping); err != nil {
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
		item := out.Item{Kind: kind, Name: w.Name}
		if w.From != "" {
			item.Aside = []string{"from " + w.From}
		}
		items = append(items, item)
	}
	l.Items(items...)
}

// restoreScope is the worlds a restore takes from archive: those only names, else all it holds. A
// server takes only its level-name world, or with as, the one world renamed to it.
func restoreScope(archive *saves.Archive, target savesTarget, only []string, as string) ([]string, error) {
	worlds := archive.Worlds
	if len(only) > 0 {
		worlds = distinct(only)
		for _, w := range worlds {
			if !slices.Contains(archive.Worlds, w) {
				return nil, out.Errorf("world-not-found", "%s holds no world %s", archive.Path, w)
			}
		}
	}
	switch {
	case as != "":
		if len(worlds) != 1 {
			e := out.Errorf("usage", "--as renames one world, and this restore takes %d: %s", len(worlds), strings.Join(worlds, ", "))
			e.Help = "pick one with --world"
			return nil, e
		}
		if target.World != "" && as != target.World {
			return nil, out.Errorf("usage", "this server loads %s, its level-name, so --as must name %s, not %s", target.World, target.World, as)
		}
	case target.World != "" && !slices.Contains(worlds, target.World):
		if len(only) > 0 {
			e := out.Errorf("world-not-found", "this server loads only %s, its level-name, which --world leaves out", target.World)
			e.Help = fmt.Sprintf("--as %s restores another world under that name", target.World)
			return nil, e
		}
		e := out.Errorf("world-not-found", "%s holds no world %s, the level-name this server loads", archive.Path, target.World)
		e.Help = fmt.Sprintf("--as %s restores another world under that name", target.World)
		return nil, e
	case target.World != "":
		worlds = []string{target.World}
	}
	return worlds, nil
}

// pickBackup is the backup restore puts back: the one --backup names, else the nth of target's,
// newest first. A --backup value with a path separator, or naming something on disk, is a path, as
// -i's is; any other is a name in target's backups.
func (a *app) pickBackup(target savesTarget, args []string, named string) (saves.Backup, error) {
	if named != "" {
		path := filepath.Join(target.Backups, strings.TrimSuffix(named, ".zip")+".zip")
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
	backups, err := saves.Backups(target.Backups)
	if err != nil {
		return saves.Backup{}, err
	}
	if len(backups) == 0 {
		e := out.Errorf("backups-empty", "%s has no backups yet", target.WorldsDir)
		e.Help = fmt.Sprintf("`%s` takes one", a.savesCommand(target, "backup"))
		return saves.Backup{}, e
	}
	if n > len(backups) {
		return saves.Backup{}, out.Errorf("backup-missing", "there is no backup %d; %s has %s", n, target.WorldsDir, plural(len(backups), "backup", "backups"))
	}
	return backups[n-1], nil
}
