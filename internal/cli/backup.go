package cli

import (
	"fmt"
	"slices"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/saves"
)

type backupResult struct {
	savesTarget
	saves.Backup
	elapsed time.Duration
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
	start := time.Now()
	taken, err := saves.Take(src, target.Home(), "backup", a.zipping("zipping"))
	if err != nil {
		return backupResult{}, err
	}
	if taken.Path == "" {
		if target.World != "" {
			return backupResult{}, out.Errorf("no-worlds", "no world %s in %s to back up", target.World, target.WorldsDir)
		}
		return backupResult{}, out.Errorf("no-worlds", "no worlds in %s to back up", target.WorldsDir)
	}
	return backupResult{savesTarget: target, Backup: taken, elapsed: time.Since(start)}, nil
}

func (b backupResult) print(l *out.Lines) {
	l.OKInto("backed up "+plural(b.Worlds, "world", "worlds"), b.Path, fmt.Sprintf("%s in %.1fs", out.HumanBytes(b.Size), b.elapsed.Seconds()))
}

func distinct(names []string) []string {
	names = slices.Clone(names)
	slices.Sort(names)
	return slices.Compact(names)
}

// backupSource is where target's worlds are, and what the zip comment records about them: the
// instance registered at its directory and the platform its last build installed, when there are
// any. A build backs up before it records its own platform, so this is what the worlds were played on.
func (a *app) backupSource(target savesTarget) saves.Source {
	src := saves.Source{Dir: target.WorldsDir}
	if target.World != "" {
		src.Only = []string{target.World}
	}
	if target.Dir == "" {
		return src
	}
	if in, ok := a.registeredInstance(target.Dir); ok {
		src.Instance = in.ID
	}
	state := build.LoadState(target.Dir)
	src.Minecraft, src.Loader, src.LoaderVersion = state.Minecraft, state.Loader, state.LoaderVersion
	return src
}

// zipping is the step line saves.Take shows for each world, under a warning when a running game
// has the world open.
func (a *app) zipping(verb string) func(world string, open bool) {
	return func(world string, open bool) {
		if open {
			a.printer.Warn("%s is open in a running game; its backup may be torn", world)
		}
		a.printer.Step("%s %s", verb, world)
	}
}

// beforeModChange is what a build runs before it changes dir's mod set: the automatic backup of
// its worlds, found through the saves target the directory belongs to and kept to
// play.saveBackups. A target that can't be found is a warning, not a failed build.
func (a *app) beforeModChange(reason, dir string) func() error {
	if reason == "" {
		return nil
	}
	return func() error {
		skip := func(err error) error {
			a.printer.Warn("couldn't back up the worlds in %s before the mods changed: %v", dir, err)
			return nil
		}
		if _, err := a.loadInstances(); err != nil {
			return skip(err)
		}
		target, err := a.targetOfDir(dir)
		if err != nil {
			return skip(err)
		}
		keep, err := a.saveBackups()
		if err != nil {
			a.printer.Warn("couldn't read play.saveBackups, keeping %d automatic backups: %v", keep, err)
		}
		if a.backedUp == nil {
			a.backedUp = map[saves.Home]bool{}
		}
		_, warning, err := saves.Auto(a.backupSource(target), target.Home(), reason, keep, a.backedUp, a.zipping("backing up"))
		if warning != "" {
			a.printer.Warn("%s", warning)
		}
		return err
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
