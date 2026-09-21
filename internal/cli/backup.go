package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/saves"
)

type backupResult struct {
	savesTarget
	saves.Taken
}

func (a *app) backupCmd() *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Zip an instance's or save group's worlds into its backups",
		Long:  "Zip the worlds of the current project or instance, of -i or -C, or of the save group --group names, into that target's backups. A save group's backups are in the data folder's backups/<group>/; any other directory keeps its own in .shulker/backups/.",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := a.savesTargetOf(group)
			if err != nil {
				return err
			}
			start := time.Now()
			taken, err := saves.Take(a.backupSource(target), target.home(), "backup", func(world string) {
				a.printer.Step("zipping %s", world)
			})
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
			return a.printer.Emit(backupResult{savesTarget: target, Taken: taken}, func(l *out.Lines) {
				l.OKInto("backed up "+plural(taken.Worlds, "world", "worlds"), taken.Path, fmt.Sprintf("%s in %.1fs", out.HumanBytes(taken.Size), elapsed.Seconds()))
			})
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "back up this save group rather than an instance")
	return cmd
}

// backupSource is where target's worlds are, and what the zip comment records about them: the
// instance registered at its directory and the platform its lock pins, when there are any.
func (a *app) backupSource(target savesTarget) saves.Source {
	src := saves.Source{Dir: target.WorldsDir, Only: target.World}
	if target.Dir == "" {
		return src
	}
	if in, ok := a.registeredInstance(target.Dir); ok {
		src.Instance = in.ID
	}
	if lk, err := lock.Load(filepath.Join(target.Dir, lock.FileName)); err == nil {
		src.Minecraft, src.Loader = lk.Minecraft, lk.Loader.Type
	}
	return src
}

func (t savesTarget) home() saves.Home {
	return saves.Home{Dir: t.backups, Shared: t.Group != ""}
}
