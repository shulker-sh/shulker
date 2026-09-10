package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/andrewmast/shulker/internal/build"
	"github.com/andrewmast/shulker/internal/manifest"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/andrewmast/shulker/internal/player"
	"github.com/andrewmast/shulker/internal/project"
	"github.com/spf13/cobra"
)

type syncResult struct {
	Source   string        `json:"source"`
	Target   string        `json:"target"`
	Dir      string        `json:"dir"`
	Fetched  []string      `json:"fetched"`
	Warnings []string      `json:"warnings"`
	Build    *build.Report `json:"build"`
}

func (a *app) syncCmd() *cobra.Command {
	var target, into string
	var force bool
	cmd := &cobra.Command{
		Use:   "sync <project-dir>",
		Short: "Download and build one target of a project straight into a directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			p, err := a.openProjectAt(source)
			if errors.Is(err, project.ErrNoManifest) {
				return out.Errorf("project-not-found", "no shulker.json in %s", source)
			}
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if a.printer.LockStale {
				a.progress("warning: shulker.lock is out of date with shulker.json; run `shulker add`, `remove`, or `update` to refresh it")
			}
			name, t, err := singleTarget(p, target)
			if err != nil {
				return err
			}
			if into == "" {
				into = filepath.Join(source, t.Build)
			}
			if into, err = filepath.Abs(into); err != nil {
				return err
			}
			fetched, warnings, err := a.fetchLocked(cmd.Context(), p, t.Side == "server")
			if err != nil {
				return err
			}
			if err := a.syncPlayers(cmd.Context(), p, player.MissingOnly, false); err != nil {
				return err
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			rep, err := b.Build(name, build.Options{Force: force, Dir: into})
			if err != nil {
				return err
			}
			a.warn(rep.Warnings)
			res := syncResult{Source: source, Target: name, Dir: into, Fetched: fetched, Warnings: warnings, Build: rep}
			return a.printer.Emit(res, func(w io.Writer) {
				fmt.Fprintf(w, "fetched %d file(s)\n", len(res.Fetched))
				fmt.Fprintf(w, "%s into %s\n", rep.Summary(), into)
				for _, m := range rep.Moved {
					fmt.Fprintf(w, "  moved %s into %s\n", m, filepath.Join(build.DataDir, rep.Target, m))
				}
			})
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "target to build (default: the only target)")
	cmd.Flags().StringVar(&into, "into", "", "output directory (default: the target's build directory)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the output directory")
	return cmd
}

func singleTarget(p *project.Project, want string) (string, manifest.Target, error) {
	if want != "" {
		t, ok := p.Manifest.Targets[want]
		if !ok {
			e := out.Errorf("target-not-found", "no target %q in shulker.json", want)
			e.Candidates = targetNames(p.Manifest.Targets)
			return "", t, e
		}
		return want, t, nil
	}
	names := targetNames(p.Manifest.Targets)
	if len(names) == 1 {
		return names[0], p.Manifest.Targets[names[0]], nil
	}
	e := out.Errorf("ambiguous-target", "shulker.json has several targets; pass --target")
	e.Candidates = names
	return "", manifest.Target{}, e
}
