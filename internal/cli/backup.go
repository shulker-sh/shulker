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
}

func (a *app) backupCmd() *cobra.Command {
	var group string
	var only []string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Zip an instance's or save group's worlds into its backups",
		Long:  "Zip the worlds of the current project or instance, of -i or -C, or of the save group --group names, into that target's backups, or only the worlds --world names. A save group's backups are in the data folder's backups/<group>/; any other directory keeps its own in .shulker/backups/.",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := a.savesTargetOf(group)
			if err != nil {
				return err
			}
			src := a.backupSource(target)
			if len(only) > 0 {
				if src.Only, err = heldWorlds(target, only); err != nil {
					return err
				}
			}
			start := time.Now()
			taken, err := saves.Take(src, target.home(), "backup", a.zipping("zipping"))
			if err != nil {
				return err
			}
			if taken.Path == "" {
				if target.World != "" {
					return out.Errorf("no-worlds", "no world %s in %s to back up", target.World, target.WorldsDir)
				}
				return out.Errorf("no-worlds", "no worlds in %s to back up", target.WorldsDir)
			}
			elapsed := time.Since(start)
			return a.printer.Emit(backupResult{savesTarget: target, Backup: taken}, func(l *out.Lines) {
				l.OKInto("backed up "+plural(taken.Worlds, "world", "worlds"), taken.Path, fmt.Sprintf("%s in %.1fs", out.HumanBytes(taken.Size), elapsed.Seconds()))
			})
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "back up this save group rather than an instance")
	cmd.Flags().StringArrayVar(&only, "world", nil, "back up only this world, by its folder name; repeatable")
	return cmd
}

// heldWorlds is names, each checked against the worlds target holds: for a server, only the one its
// level-name loads.
func heldWorlds(target savesTarget, names []string) ([]string, error) {
	held, err := saves.Worlds(target.WorldsDir)
	if err != nil {
		return nil, err
	}
	names = distinct(names)
	for _, name := range names {
		switch {
		case target.World != "" && name != target.World:
			return nil, out.Errorf("world-not-found", "this server loads only %s, its level-name, not %s", target.World, name)
		case !slices.Contains(held, name):
			return nil, out.Errorf("world-not-found", "no world %s in %s", name, target.WorldsDir)
		}
	}
	return names, nil
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

func (t savesTarget) home() saves.Home {
	return saves.Home{Dir: t.backups, Shared: t.Group != ""}
}

// autoBackup is the backup a build takes of dir's worlds before it changes the mod set, then trims
// that target's automatic backups to play.saveBackups. Only a zip that can't be written stops the
// build: a target with no worlds backs up as nothing, and one whose worlds can't be found, or
// whose trim fails, is a warning. A target is backed up once a run, so a build retried after
// failing part way through its mods doesn't copy the same worlds again.
func (a *app) autoBackup(reason, dir string) func() error {
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
		target, err := a.savesTargetAt(dir)
		if err != nil {
			return skip(err)
		}
		key := savesTarget{WorldsDir: target.WorldsDir, World: target.World}
		if a.backedUp[key] {
			return nil
		}
		worlds, err := saves.Worlds(target.WorldsDir)
		if err != nil {
			return skip(err)
		}
		if len(worlds) == 0 {
			return nil
		}
		keep, err := a.saveBackups()
		if err != nil {
			a.printer.Warn("couldn't read play.saveBackups, keeping %d automatic backups: %v", keep, err)
		}
		if keep == 0 {
			return nil
		}
		taken, err := saves.Take(a.backupSource(target), target.home(), reason, a.zipping("backing up"))
		if err != nil || taken.Path == "" {
			return err
		}
		if a.backedUp == nil {
			a.backedUp = map[savesTarget]bool{}
		}
		a.backedUp[key] = true
		if err := saves.TrimAutomatic(target.backups, keep); err != nil {
			a.printer.Warn("couldn't trim the automatic backups in %s: %v", target.backups, err)
		}
		return nil
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
