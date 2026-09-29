package cli

import (
	"context"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/sync"
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
			overrides, err := ff.overrides(b, lf.Features)
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
				rebuild := takeOver(cmd, nil, force)
				a.warnBuild(side, len(sides) > 1, rep.Warnings, a.securityWarnings(rep, rebuild), rep.State, rebuild)
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
	a.scopeFlags(cmd)
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the build directory, seeded files included.")
	cmd.Flags().StringVar(&osName, "os", "", "build for this os instead of the detected one: macos, windows, or linux.")
	ff.register(cmd, "for this run only")
	a.registerFailFast(cmd)
	a.registerEveryFetch(cmd)
	return cmd
}

func (a *app) awaitsALauncher(p *project.Project) bool {
	synced, err := a.syncedFrom(p)
	return err == nil && project.AwaitsLauncher(p, synced)
}

// fetchLocked puts the locked files the given sides use in the cache, every locked file with no
// sides, and the server jar and its Java runtime too when wantServer is set.
func (a *app) fetchLocked(ctx context.Context, p *project.Project, sides []string, wantServer bool) ([]string, error) {
	se, err := a.syncEnv()
	if err != nil {
		return nil, err
	}
	fetched, err := sync.FetchLocked(ctx, se, p, sides, wantServer)
	if err != nil {
		return nil, a.lastOf(err)
	}
	return fetched, nil
}
