package cli

import (
	"context"
	"errors"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/mavenver"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

func (a *app) linkGDLauncherCmd() *cobra.Command {
	var launcherDir, instanceName, ref, as string
	var force bool
	var assumeClient bool
	var ff featureFlags
	cmd := &cobra.Command{
		Use:   "gdlauncher [project-dir | git-url | manifest-url]",
		Short: "Create a GDLauncher instance that syncs the client build before each launch",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := a.linkSource(cmd.Context(), args, ref)
			if err != nil {
				return err
			}
			p := src.project
			if p.Lock.Loader.Type != "" {
				if _, err := loader.Require(p.Lock.Loader.Type); err != nil {
					return err
				}
			}
			side, err := a.clientSide(p.Manifest, assumeClient)
			if err != nil {
				return err
			}
			hasFeatures := len(ff.with)+len(ff.without) > 0
			if hasFeatures {
				b, err := a.builder(cmd.Context(), p)
				if err != nil {
					return err
				}
				if err := ff.check(b); err != nil {
					return err
				}
			}
			if launcherDir == "" {
				if launcherDir, err = launcher.DefaultGDLauncherDir(); err != nil {
					return err
				}
			}
			if launcherDir, err = filepath.Abs(launcherDir); err != nil {
				return err
			}
			gdl := &launcher.GDLauncher{Dir: launcherDir}
			if err := gdl.Check(); errors.Is(err, launcher.ErrNotFound) {
				return out.Errorf("launcher-not-found", "no GDLauncher directory at %s; run GDLauncher once or pass --launcher-dir", launcherDir)
			} else if err != nil {
				return err
			}
			// GDLauncher resolves its runtime path and starts hooks there, so the sync a hook runs only
			// matches this link under the resolved path.
			if launcherDir, err = filepath.EvalSymlinks(launcherDir); err != nil {
				return err
			}
			gdl.Dir = launcherDir
			display := p.Manifest.DisplayName(side)
			if instanceName != "" {
				display = instanceName
			}
			if launcher.GDLauncherFolder(display) == "" {
				return out.Errorf("usage", "GDLauncher needs an instance name that isn't blank; pass --name")
			}
			gameDir := gdl.GameDir(display)
			prevState, stateErr := build.ReadState(gameDir)
			if stateErr != nil {
				a.printer.Warn("%v", stateErr)
			}
			if prev := prevState.Source; prev != "" && prev != src.name && !force {
				return out.Errorf("instance-exists", "instance %q already syncs from %s; pass --name to create a second instance, or --force to repoint this one", display, prev)
			}
			if prevState.Source == "" && !force {
				hook, found, err := launcher.GDLauncherPreLaunch(gdl.InstanceDir(display))
				if err != nil {
					return err
				}
				if found && !launcher.IsSyncCommand(hook) {
					return out.Errorf("instance-exists", "GDLauncher already has an instance %q that shulker didn't link; pass --name to create a second instance, or --force to link this one", display)
				}
			}
			loaderVersion, err := a.gdlauncherLoaderVersion(cmd.Context(), p, force)
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
			if hasFeatures {
				if err := a.saveInstanceFeatures(res.GameDir, ff); err != nil {
					return err
				}
			}
			if err := a.checkID(as, res.GameDir); err != nil {
				return err
			}
			if err := saveIntent(res.GameDir, src.name, ref, side, assumeClient); err != nil {
				return err
			}
			a.registerInstance(config.Instance{ID: as, Launcher: "gdlauncher", LauncherDir: launcherDir, Name: display, Dir: res.GameDir, Source: src.name})
			var synced *syncResult
			if len(args) == 1 {
				r, err := a.sync(cmd.Context(), src, syncRequest{ref: ref, side: side, into: res.GameDir, assumeClient: assumeClient})
				if err != nil {
					return err
				}
				synced = &r
			}
			rep := prismReport{
				Launcher:    "gdlauncher",
				LauncherDir: launcherDir,
				Instance:    filepath.Base(res.Dir),
				InstanceDir: res.Dir,
				Name:        display,
				Mode:        "sync",
				Side:        side,
				GameDir:     res.GameDir,
				Command:     launcher.SlotCommand("gdlauncher", res.GameDir, launcher.HookPreLaunch),
				Created:     res.Created,
				Source:      src.name,
				Sync:        synced,
			}
			return a.printer.Emit(rep, func(l *out.Lines) {
				verb := "created"
				if !res.Created {
					verb = "updated"
				}
				l.OKInto(verb+" instance "+display, res.Dir, "")
				rows := []out.Row{{Text: "the launcher runs `shulker sync` for the " + side + " side before each launch"}}
				if hasFeatures {
					rows = append(rows, out.Row{Text: "feature choices saved; change them with `shulker feature on|off <feature> --into " + launcher.CommandArg(res.GameDir) + "`"})
				}
				if !detectable {
					rows = append(rows, out.Row{Text: "restart GDLauncher if it is open so the instance shows up"})
				}
				l.Tree(rows...)
				if synced != nil {
					synced.print(l)
				}
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher runtime directory (default: GDLauncher's)")
	cmd.Flags().BoolVar(&assumeClient, "assume-client", false, "link a client even when the source declares none, built from the mods and overrides both sides share")
	cmd.Flags().StringVar(&as, "as", "", "id for this instance, for -i (default: from its name)")
	cmd.Flags().StringVar(&instanceName, "name", "", "instance name (default: the side's display name)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().BoolVar(&force, "force", false, "link over an instance that syncs from a different source or that shulker didn't link, and use the locked loader version even if GDLauncher can't install it yet")
	ff.register(cmd, "for this instance")
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
