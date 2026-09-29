package cli

import (
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/saves"
	"shulker.sh/shulker/internal/sync"
)

type backupResult struct {
	savesTarget
	saves.Backup
}

func (a *app) backupCmd() *cobra.Command {
	var where savesWhere
	var only []string
	cmd := &cobra.Command{
		Use:         "backup",
		Annotations: acts(),
		Short:       "Zip an instance's or save group's worlds into its backups",
		Long:        "Zip the worlds of the current project or instance, of -i or -C, or of the save group --group names, into that target's backups, or only the worlds --world names; with --all, of every registered instance, each save group once. A save group's backups are in the data folder's backups/<group>/; any other directory keeps its own in .shulker/backups/.",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if where.sel.all && len(only) > 0 {
				e := out.Errorf("usage", "--all backs up every world of each target")
				e.Help = "to pick worlds, name the target with -i, -C or --group"
				return e
			}
			return onSaves(a, where, func(n, of int) *out.Error {
				return out.Errorf("backup-failed", "%d of %d targets failed to back up", n, of)
			}, func(target savesTarget) (backupResult, error) { return a.backup(target, only) }, backupResult.print, "no-worlds")
		},
	}
	a.scopeFlags(cmd)
	where.register(cmd, "back up this save group rather than an instance", "back up every instance")
	cmd.Flags().StringArrayVar(&only, "world", nil, "back up only this world, by its folder name; repeatable")
	return cmd
}

func (a *app) backup(target savesTarget, only []string) (backupResult, error) {
	src := a.backupSource(target)
	if len(only) > 0 {
		var err error
		if src.Only, err = saves.HeldWorlds(target.WorldsDir, target.World, only); err != nil {
			return backupResult{}, err
		}
	}
	taken, err := saves.Take(src, target.Home(), "backup", a.zipping(a.printer.Working))
	if err != nil {
		return backupResult{}, err
	}
	if taken.Path == "" {
		if target.World != "" {
			return backupResult{}, out.Errorf("no-worlds", "no world %s in %s to back up", target.World, target.WorldsDir)
		}
		return backupResult{}, out.Errorf("no-worlds", "no worlds in %s to back up", target.WorldsDir)
	}
	return backupResult{savesTarget: target, Backup: taken}, nil
}

func (b backupResult) print(l *out.Lines) {
	what := out.Count(b.Worlds, "world", "worlds")
	if b.Worlds == 1 && len(b.Names) == 1 {
		what = b.Names[0]
	}
	l.OKInto("Backed up "+what, b.Path, out.HumanBytes(b.Size))
}

func (a *app) backupSource(target savesTarget) saves.Source {
	se, err := a.syncEnv()
	if err != nil {
		return saves.Source{Dir: target.WorldsDir}
	}
	return sync.BackupSource(se, target.Target)
}

// zipping is the line saves.Take shows for each world through step, under a warning when a
// running game has the world open.
func (a *app) zipping(step func(format string, args ...any)) func(world string, open bool) {
	return func(world string, open bool) {
		if open {
			a.printer.Warn("%s is open in a running game; its backup may be torn.", world)
		}
		step("zipping %s", world)
	}
}

func (a *app) saveBackups() (int, error) {
	path, err := a.configFile()
	if err != nil {
		return config.DefaultSaveBackups, err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return config.DefaultSaveBackups, err
	}
	return cfg.Play.Backups(), nil
}
