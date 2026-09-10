package cli

import (
	"fmt"
	"github.com/shulker-sh/shulker/internal/out"
	"io"
	"path/filepath"

	"github.com/shulker-sh/shulker/internal/build"
	"github.com/spf13/cobra"
)

func (a *app) diffCmd() *cobra.Command {
	var into string
	cmd := &cobra.Command{
		Use:   "diff [target]",
		Short: "Show build files that differ from what build would write",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if a.printer.LockStale {
				a.progress("warning: shulker.lock is out of date with shulker.json; run `shulker add`, `remove`, or `update` to refresh it")
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			names := targetNames(p.Manifest.Targets)
			if len(args) == 1 {
				names = args
			}
			if into != "" {
				if len(names) != 1 {
					return out.Errorf("into-target", "--into applies to one target; name it")
				}
				if into, err = filepath.Abs(into); err != nil {
					return err
				}
			}
			var reports []*build.DiffReport
			for _, name := range names {
				rep, err := b.Diff(name, build.Options{Dir: into})
				if err != nil {
					return err
				}
				a.warn(rep.Warnings)
				reports = append(reports, rep)
			}
			return a.printer.Emit(reports, func(w io.Writer) {
				for _, rep := range reports {
					if len(rep.Files) == 0 {
						fmt.Fprintf(w, "%s: no changes in the build directory\n", rep.Target)
						continue
					}
					fmt.Fprintf(w, "%s: %d file(s) changed in the build directory\n", rep.Target, len(rep.Files))
					for _, f := range rep.Files {
						fmt.Fprintf(w, "%s %s\n", f.State, f.Path)
						fmt.Fprint(w, f.Diff)
					}
				}
			})
		},
	}
	cmd.Flags().StringVar(&into, "into", "", "directory the target was synced into (default: the target's build directory)")
	return cmd
}

func (a *app) pullCmd() *cobra.Command {
	var target, into string
	cmd := &cobra.Command{
		Use:   "pull [file...]",
		Short: "Copy edits made in a build directory back into their source",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if a.printer.LockStale {
				a.progress("warning: shulker.lock is out of date with shulker.json; run `shulker add`, `remove`, or `update` to refresh it")
			}
			name, _, err := singleTarget(p, target)
			if err != nil {
				return err
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			if into != "" {
				if into, err = filepath.Abs(into); err != nil {
					return err
				}
			}
			rep, err := b.Pull(name, args, build.Options{Dir: into})
			if err != nil {
				return err
			}
			a.warn(rep.Warnings)
			if rep.ManifestChanged {
				if err := p.SaveManifest(); err != nil {
					return err
				}
			}
			return a.printer.Emit(rep, func(w io.Writer) {
				fmt.Fprintf(w, "%s: %d file(s) pulled, %d key(s) written to shulker.json, %d skipped\n", rep.Target, len(rep.Pulled), len(rep.Keys), len(rep.Skipped))
				for _, f := range rep.Pulled {
					fmt.Fprintf(w, "  pulled %s\n", f)
				}
				for _, k := range rep.Keys {
					fmt.Fprintf(w, "  set %s\n", k)
				}
				for _, s := range rep.Skipped {
					fmt.Fprintf(w, "  skipped %s\n", s)
				}
			})
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "target whose build directory to pull from (default: the only target)")
	cmd.Flags().StringVar(&into, "into", "", "directory the target was synced into (default: the target's build directory)")
	return cmd
}
