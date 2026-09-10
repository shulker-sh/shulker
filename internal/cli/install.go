package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/andrewmast/shulker/internal/build"
	"github.com/andrewmast/shulker/internal/manifest"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/andrewmast/shulker/internal/player"
	"github.com/andrewmast/shulker/internal/project"
	"github.com/spf13/cobra"
)

type installResult struct {
	Fetched  []string        `json:"fetched"`
	Warnings []string        `json:"warnings"`
	Builds   []*build.Report `json:"builds"`
}

func (a *app) installCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Download everything in the lock and build all targets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			fetched, warnings, err := a.fetchLocked(cmd.Context(), p, hasServerTarget(p.Manifest.Targets))
			if err != nil {
				return err
			}
			if err := a.syncPlayers(cmd.Context(), p, player.MissingOnly, false, true); err != nil {
				return err
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			names := targetNames(p.Manifest.Targets)
			res := installResult{Fetched: fetched, Warnings: warnings}
			for _, name := range names {
				rep, err := b.Build(name, build.Options{Force: force})
				if err != nil {
					return err
				}
				a.warn(rep.Warnings)
				res.Builds = append(res.Builds, rep)
			}
			return a.printer.Emit(res, func(w io.Writer) {
				fmt.Fprintf(w, "fetched %d file(s)\n", len(res.Fetched))
				for _, rep := range res.Builds {
					fmt.Fprintln(w, rep.Summary())
					for _, m := range rep.Moved {
						fmt.Fprintf(w, "  moved %s into %s\n", m, filepath.Join(build.DataDir, rep.Target, m))
					}
				}
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the build directory")
	return cmd
}

func (a *app) fetchLocked(ctx context.Context, p *project.Project, wantServer bool) ([]string, []string, error) {
	r, err := a.resolver(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	fetched, dropWarnings, err := r.Install(ctx)
	if err != nil {
		return nil, nil, err
	}
	if fetched == nil {
		fetched = []string{}
	}
	var runtimeWarning string
	if wantServer {
		d, err := a.deps()
		if err != nil {
			return nil, nil, err
		}
		jar, err := r.EnsureServerJar(ctx, d.meta.Fabric)
		if err != nil {
			return nil, nil, err
		}
		if jar.Fetched {
			fetched = append(fetched, "fabric-server-launcher")
		}
		if jar.Locked {
			if err := p.Lock.Save(p.LockPath()); err != nil {
				return nil, nil, err
			}
		}
		if p.Manifest.Java == "" {
			rt, err := a.managedJava(ctx, p, true)
			if err != nil && out.CodeOf(err) != "runtime-unavailable" {
				return nil, nil, err
			}
			if err != nil {
				runtimeWarning = err.Error()
			} else if rt.Fetched {
				fetched = append(fetched, rt.Component+" "+rt.Version)
			}
		}
	}
	v, err := r.Validate()
	if err != nil {
		return nil, nil, err
	}
	if err := v.Err(); err != nil {
		return nil, nil, err
	}
	warnings := append(dropWarnings, v.Warnings...)
	if runtimeWarning != "" {
		warnings = append(warnings, runtimeWarning)
	}
	a.warn(warnings)
	return fetched, warnings, nil
}

func hasServerTarget(targets map[string]manifest.Target) bool {
	for _, t := range targets {
		if t.Side == "server" {
			return true
		}
	}
	return false
}

func targetNames[T any](targets map[string]T) []string {
	names := make([]string, 0, len(targets))
	for n := range targets {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
