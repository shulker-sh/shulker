package cli

import (
	"context"
	"sort"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
)

type installResult struct {
	Fetched []string        `json:"fetched"`
	Builds  []*build.Report `json:"builds"`
}

func (a *app) installCmd() *cobra.Command {
	var force bool
	var osName string
	var ff featureFlags
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Download everything in the lock and build all targets",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := checkOS(osName); err != nil {
				return err
			}
			if err := a.requireLock(p); err != nil {
				return err
			}
			fetched, err := a.fetchLocked(cmd.Context(), p, hasServerTarget(p.Manifest.Targets))
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
			lf, err := local.Load(p.Dir)
			if err != nil {
				return err
			}
			overrides, err := featureOverrides(b, lf.Features, ff)
			if err != nil {
				return err
			}
			names := targetNames(p.Manifest.Targets)
			res := installResult{Fetched: fetched}
			for _, name := range names {
				rep, err := b.Build(name, build.Options{Force: force, OS: osName, Features: overrides})
				if err != nil {
					return err
				}
				a.warnFor(name, len(names) > 1, rep.Warnings)
				if err := a.installServerLoader(cmd.Context(), p, rep); err != nil {
					return err
				}
				res.Builds = append(res.Builds, rep)
			}
			a.refreshLocal(lf, true, false)
			return a.printer.Emit(res, func(l *out.Lines) {
				for _, rep := range res.Builds {
					printReport(l, rep)
				}
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the build directory")
	cmd.Flags().StringVar(&osName, "os", "", "build for this os instead of the detected one: macos, windows, or linux")
	ff.register(cmd, "for this run only")
	return cmd
}

func (a *app) fetchLocked(ctx context.Context, p *project.Project, wantServer bool) ([]string, error) {
	r, err := a.resolver(ctx, p)
	if err != nil {
		return nil, err
	}
	fetched, dropWarnings, err := r.Install(ctx)
	a.warn(dropWarnings)
	if err != nil {
		return nil, err
	}
	if fetched == nil {
		fetched = []string{}
	}
	if wantServer {
		d, err := a.deps()
		if err != nil {
			return nil, err
		}
		jar, err := r.EnsureServerJar(ctx, d.meta)
		if err != nil {
			return nil, err
		}
		if jar.Fetched && p.Lock.Loader.Type == "" {
			fetched = append(fetched, "minecraft-server")
		} else if jar.Fetched {
			fetched = append(fetched, p.Lock.Loader.Type+"-server-launcher")
		}
		if jar.Locked {
			if err := p.Lock.Save(p.LockPath()); err != nil {
				return nil, err
			}
		}
		if p.Manifest.Java == "" {
			rt, err := a.managedJava(ctx, p, true)
			if err != nil && fetch.IsNetwork(err) {
				if kept, keptErr := a.managedJava(ctx, p, false); keptErr == nil {
					rt, err = kept, nil
					a.printer.Warn("offline, keeping the installed Java runtime %s %s", kept.Component, kept.Version)
				}
			}
			if err != nil && out.CodeOf(err) != "runtime-unavailable" {
				return nil, err
			}
			if err != nil {
				a.printer.Warn("%s", err)
			} else if rt.Fetched {
				fetched = append(fetched, rt.Component+" "+rt.Version)
			}
		}
	}
	v, err := r.Validate()
	if err != nil {
		return nil, err
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	a.warn(v.Warnings)
	return fetched, nil
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
