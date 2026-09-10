package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/andrewmast/shulker/internal/build"
	"github.com/andrewmast/shulker/internal/manifest"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/andrewmast/shulker/internal/player"
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
			r, err := a.resolver(cmd.Context(), p)
			if err != nil {
				return err
			}
			fetched, dropWarnings, err := r.Install(cmd.Context())
			if err != nil {
				return err
			}
			if fetched == nil {
				fetched = []string{}
			}
			var runtimeWarning string
			if hasServerTarget(p.Manifest.Targets) {
				d, err := a.deps()
				if err != nil {
					return err
				}
				jar, err := r.EnsureServerJar(cmd.Context(), d.meta.Fabric)
				if err != nil {
					return err
				}
				if jar.Fetched {
					fetched = append(fetched, "fabric-server-launcher")
				}
				if jar.Locked {
					if err := p.Lock.Save(p.LockPath()); err != nil {
						return err
					}
				}
				if p.Manifest.Java == "" {
					rt, err := a.managedJava(cmd.Context(), p, true)
					if err != nil && out.CodeOf(err) != "runtime-unavailable" {
						return err
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
				return err
			}
			if err := v.Err(); err != nil {
				return err
			}
			v.Warnings = append(dropWarnings, v.Warnings...)
			if runtimeWarning != "" {
				v.Warnings = append(v.Warnings, runtimeWarning)
			}
			a.warn(v.Warnings)
			if err := a.syncPlayers(cmd.Context(), p, player.MissingOnly, false); err != nil {
				return err
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			names := targetNames(p.Manifest.Targets)
			res := installResult{Fetched: fetched, Warnings: v.Warnings}
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
