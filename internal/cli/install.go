package cli

import (
	"context"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
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
		Use:         "install",
		Annotations: acts(),
		Short:       "Download everything in the lock and build every side",
		Args:        noArgs,
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
			fetched, err := a.fetchLocked(cmd.Context(), p, nil, p.Manifest.HasSide("server"))
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
			lf, err := a.loadLocal(p.Dir)
			if err != nil {
				return err
			}
			overrides, err := featureOverrides(b, lf.Features, ff)
			if err != nil {
				return err
			}
			sides := p.Manifest.Sides()
			res := installResult{Fetched: fetched}
			for _, side := range sides {
				rep, err := b.Build(side, build.Options{Force: force, OS: osName, Features: overrides})
				if err != nil {
					return err
				}
				a.warnBuild(side, len(sides) > 1, rep.Warnings, rep.State, takeOver(cmd, nil, force))
				if err := a.installServerLoader(cmd.Context(), p, rep); err != nil {
					return err
				}
				res.Builds = append(res.Builds, rep)
			}
			a.refreshLocal(lf, true, false)
			nudge := a.awaitsALauncher(p)
			return a.printer.Emit(res, func(l *out.Lines) {
				for _, rep := range res.Builds {
					printReport(l, rep)
				}
				if nudge {
					l.Nudge("Play it in a launcher", "shulker link <launcher>")
				}
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the build directory")
	cmd.Flags().StringVar(&osName, "os", "", "build for this os instead of the detected one: macos, windows, or linux")
	ff.register(cmd, "for this run only")
	return cmd
}

// awaitsALauncher reports a client project that builds out of place and has nothing linked or
// synced from it yet.
func (a *app) awaitsALauncher(p *project.Project) bool {
	if !p.Manifest.HasSide("client") {
		return false
	}
	if _, ok := p.Manifest.InPlaceSide(); ok {
		return false
	}
	synced, err := a.hasSyncedInstances(p)
	return err == nil && !synced
}

// fetchLocked puts the locked files the given sides use in the cache, every locked file with no
// sides, and the server jar and its Java runtime too when wantServer is set.
func (a *app) fetchLocked(ctx context.Context, p *project.Project, sides []string, wantServer bool) ([]string, error) {
	r, err := a.resolver(ctx, p)
	if err != nil {
		return nil, err
	}
	fetched, dropWarnings, err := r.Install(ctx, sides...)
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
		if jar.WasFetched && p.Lock.Loader.Type == "" {
			fetched = append(fetched, "minecraft-server")
		} else if jar.WasFetched {
			fetched = append(fetched, p.Lock.Loader.Type+"-server-launcher")
		}
		if jar.ChangedLock {
			if err := p.Lock.Save(p.LockPath()); err != nil {
				return nil, err
			}
		}
		if p.Manifest.Java == "" {
			rt, err := a.freshestJava(ctx, p, serverJavaFix)
			if err != nil && out.CodeOf(err) != "runtime-unavailable" {
				return nil, err
			}
			if err != nil {
				a.printer.Warn("%s", runtimeWarning(err))
			} else if rt.Fetched {
				fetched = append(fetched, rt.Component+" "+rt.Version)
			}
		}
	}
	v, err := r.Validate(sides...)
	if err != nil {
		return nil, err
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	a.warn(v.Warnings)
	return fetched, nil
}
