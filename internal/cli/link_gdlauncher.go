package cli

import (
	"context"
	"errors"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/mavenver"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

func (a *app) linkGDLauncherCmd() *cobra.Command {
	var k launcherLink
	cmd := &cobra.Command{
		Use:         "gdlauncher [project-dir | git-url | manifest-url]",
		Annotations: acts(),
		Short:       "Create a GDLauncher instance that syncs the client build before each launch",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, _, err := a.startLauncherLink(cmd, args, &k, launcher.DefaultGDLauncherDir)
			if err != nil {
				return err
			}
			p := src.project
			gdl := &launcher.GDLauncher{Dir: k.launcherDir}
			if err := gdl.Check(); errors.Is(err, launcher.ErrNotFound) {
				e := out.Errorf("launcher-not-found", "no GDLauncher directory at %s", k.launcherDir)
				e.Help = "run GDLauncher once or pass --launcher-dir"
				return e
			} else if err != nil {
				return err
			}
			// GDLauncher resolves its runtime path and starts hooks there, so the sync a hook runs only
			// matches this link under the resolved path.
			if k.launcherDir, err = filepath.EvalSymlinks(k.launcherDir); err != nil {
				return err
			}
			gdl.Dir = k.launcherDir
			display := k.display(p)
			if launcher.GDLauncherFolder(display) == "" {
				e := out.Errorf("usage", "GDLauncher needs an instance name that isn't blank")
				e.Help = "pass --name"
				return e
			}
			gameDir := gdl.GameDir(display)
			if err := checkAdopt(gameDir, src, "instance", display, "--name", k.force); err != nil {
				return err
			}
			if err := a.refuseForeignInstance(&k, gameDir, "GDLauncher", display, func() (string, bool, error) {
				return launcher.GDLauncherPreLaunch(gdl.InstanceDir(display))
			}); err != nil {
				return err
			}
			loaderVersion, err := a.gdlauncherLoaderVersion(cmd.Context(), p, k.force)
			if err != nil {
				return err
			}
			running, detectable := launcher.GDLauncherRunning()
			if running {
				a.printer.Warn("GDLauncher is open; it may overwrite this instance's changes. Quit it and run this link again")
			}
			res, err := gdl.WriteInstance(launcher.GDLauncherInstance{
				Name:          display,
				Minecraft:     p.Lock.Minecraft,
				LoaderType:    p.Lock.Loader.Type,
				LoaderVersion: loaderVersion,
			})
			if err != nil {
				return err
			}
			var extra []out.Row
			if !detectable {
				extra = append(extra, out.Row{Text: "restart GDLauncher if it is open so the instance shows up"})
			}
			return a.finishLauncherLink(cmd, &k, "gdlauncher", display, src, res, extra...)
		},
	}
	k.register(cmd, "launcher runtime directory (default: GDLauncher's)", "repoint the modpack an instance already follows, link over one shulker didn't link, and use the locked loader version even if GDLauncher can't install it yet")
	return cmd
}

// gdlauncherLoaderVersion is the loader version the instance asks GDLauncher for. GDLauncher installs
// loaders only from its own meta, which lags behind new releases, so a locked version it doesn't list
// yet gives way to the newest one it has, unless force.
func (a *app) gdlauncherLoaderVersion(ctx context.Context, p *project.Project, force bool) (string, error) {
	locked := p.Lock.Loader
	if locked.Type == "" {
		return "", nil
	}
	want := launcher.GDLauncherLoaderVersion(p.Lock.Minecraft, locked.Type, locked.Version)
	d, err := a.deps()
	if err != nil {
		return "", err
	}
	a.progress("checking which %s versions GDLauncher can install", locked.Type)
	listed, err := d.gdlauncher.LoaderVersions(ctx, locked.Type, p.Lock.Minecraft)
	switch {
	case err != nil:
		a.printer.Warn("couldn't check whether GDLauncher can install %s %s (%v); the instance asks for it anyway", locked.Type, want, err)
		return want, nil
	case slices.Contains(listed, want):
		return want, nil
	case len(listed) == 0:
		a.printer.Warn("GDLauncher can't install %s for Minecraft %s yet, so the instance won't start until it can", locked.Type, p.Lock.Minecraft)
		return want, nil
	}
	newest := slices.MaxFunc(listed, func(x, y string) int {
		return mavenver.Compare(mavenver.Parse(x), mavenver.Parse(y))
	})
	if force {
		a.printer.Warn("GDLauncher can't install %s %s yet, so the instance won't start until it can; without --force it would use %s", locked.Type, want, newest)
		return want, nil
	}
	a.printer.Warn("GDLauncher can't install %s %s yet, so the instance uses %s, the newest it has; run this link again once GDLauncher adds %s, or pass --force to use it anyway", locked.Type, want, newest, want)
	return newest, nil
}
