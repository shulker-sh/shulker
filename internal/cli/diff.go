package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

func buildDirs(p *project.Project, lf *local.File, name string) (string, []string, error) {
	buildDir, err := filepath.Abs(filepath.Join(p.Dir, p.Manifest.BuildDir(name)))
	if err != nil {
		return "", nil, err
	}
	var dirs []string
	if _, err := os.Stat(buildDir); !errors.Is(err, os.ErrNotExist) {
		dirs = append(dirs, buildDir)
	}
	for _, d := range lf.ExistingSyncDirs(name) {
		if d != buildDir {
			dirs = append(dirs, d)
		}
	}
	return buildDir, dirs, nil
}

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
			lf, err := local.Load(p.Dir)
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
			where := map[*build.DiffReport]string{}
			for _, name := range names {
				buildDir, dirs := "", []string{into}
				if into == "" {
					if buildDir, dirs, err = buildDirs(p, lf, name); err != nil {
						return err
					}
					if len(dirs) == 0 {
						dirs = []string{""}
					}
				}
				for _, dir := range dirs {
					rep, err := b.Diff(name, build.Options{Dir: dir, Features: lf.Features})
					if err != nil && dir != "" && dir != buildDir && into == "" {
						a.progress("warning: skipped %s: %v", dir, err)
						continue
					}
					if err != nil {
						return err
					}
					a.warn(rep.Warnings)
					reports = append(reports, rep)
					where[rep] = "the build directory"
					if dir != "" && dir != buildDir {
						where[rep] = dir
					}
				}
			}
			return a.printer.Emit(reports, func(w io.Writer) {
				for _, rep := range reports {
					if len(rep.Files) == 0 {
						fmt.Fprintf(w, "%s: no changes in %s\n", rep.Target, where[rep])
						continue
					}
					fmt.Fprintf(w, "%s: %d file(s) changed in %s\n", rep.Target, len(rep.Files), where[rep])
					for _, f := range rep.Files {
						fmt.Fprintf(w, "%s %s\n", f.State, f.Path)
						fmt.Fprint(w, f.Diff)
					}
				}
			})
		},
	}
	cmd.Flags().StringVar(&into, "into", "", "directory the target was synced into (default: the build directory and every directory it was synced into)")
	return cmd
}

func (a *app) pullCmd() *cobra.Command {
	var target, into string
	var keys []string
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
			lf, err := local.Load(p.Dir)
			if err != nil {
				return err
			}
			if into != "" {
				if into, err = filepath.Abs(into); err != nil {
					return err
				}
			} else if into, err = a.pullSource(b, p, lf, name); err != nil {
				return err
			}
			rep, err := b.Pull(name, args, keys, build.Options{Dir: into, Features: lf.Features})
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
				for _, k := range rep.Adopted {
					fmt.Fprintf(w, "  adopted %s\n", k)
				}
				for _, s := range rep.Skipped {
					fmt.Fprintf(w, "  skipped %s\n", s)
				}
			})
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "target whose build directory to pull from (default: the only target)")
	cmd.Flags().StringVar(&into, "into", "", "directory the target was synced into (default: whichever of the build directory and its sync directories has edits)")
	cmd.Flags().StringArrayVar(&keys, "key", nil, "start managing this key of the named .properties file; repeat for more")
	return cmd
}

func (a *app) pullSource(b *build.Builder, p *project.Project, lf *local.File, name string) (string, error) {
	buildDir, dirs, err := buildDirs(p, lf, name)
	if err != nil || len(dirs) == 0 {
		return "", err
	}
	if len(dirs) == 1 {
		return dirs[0], nil
	}
	var drifted []string
	for _, dir := range dirs {
		rep, err := b.Diff(name, build.Options{Dir: dir, Features: lf.Features})
		if err != nil {
			if dir == buildDir {
				return "", err
			}
			a.progress("warning: skipped %s: %v", dir, err)
			continue
		}
		if len(rep.Files) > 0 {
			drifted = append(drifted, dir)
		}
	}
	switch len(drifted) {
	case 0:
		return dirs[0], nil
	case 1:
		return drifted[0], nil
	}
	e := out.Errorf("ambiguous-into", "target %s has edits in several directories; pass --into", name)
	e.Candidates = drifted
	return "", e
}
