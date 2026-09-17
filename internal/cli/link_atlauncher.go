package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/meta"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

func (a *app) linkATLauncherCmd() *cobra.Command {
	var launcherDir, target, instanceName, ref, as string
	var force bool
	var ff featureFlags
	cmd := &cobra.Command{
		Use:   "atlauncher [project-dir | git-url | manifest-url]",
		Short: "Create an ATLauncher instance that syncs the client build before each launch",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := a.linkSource(cmd.Context(), args, ref)
			if err != nil {
				return err
			}
			p := src.project
			var l loader.Loader
			if p.Lock.Loader.Type != "" {
				if l, err = loader.Require(p.Lock.Loader.Type); err != nil {
					return err
				}
			}
			name, err := sideTarget(p.Manifest, target, "client", "link")
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
				if launcherDir, err = launcher.DefaultATLauncherDir(); err != nil {
					return err
				}
			}
			if launcherDir, err = filepath.Abs(launcherDir); err != nil {
				return err
			}
			atl := &launcher.ATLauncher{Dir: launcherDir}
			if err := atl.Check(); errors.Is(err, launcher.ErrNotFound) {
				return out.Errorf("launcher-not-found", "no ATLauncher directory at %s; run ATLauncher once or pass --launcher-dir", launcherDir)
			} else if err != nil {
				return err
			}
			display := p.Manifest.DisplayName(name)
			if instanceName != "" {
				display = instanceName
			}
			if launcher.ATLauncherFolder(display) == "" {
				return out.Errorf("usage", "ATLauncher names an instance's folder after the letters and digits in its name, and %q has none; pass --name", display)
			}
			gameDir := atl.InstanceDir(display)
			prevState, stateErr := build.ReadState(gameDir)
			if stateErr != nil {
				a.printer.Warn("%v", stateErr)
			}
			if prev := prevState.Source; prev != "" && prev != src.name && !force {
				return out.Errorf("instance-exists", "instance %q already syncs from %s; pass --name to create a second instance, or --force to repoint this one", display, prev)
			}
			if prevState.Source == "" && !force {
				command, found, err := launcher.ATLauncherPreLaunch(gameDir)
				if err != nil {
					return err
				}
				if found && !launcher.IsSyncCommand(command) {
					return out.Errorf("instance-exists", "ATLauncher already has an instance %q that shulker didn't link; pass --name to create a second instance, or --force to link this one", display)
				}
			}
			version, err := a.atlauncherVersion(cmd.Context(), p, l, atl)
			if err != nil {
				return err
			}
			res, err := atl.WriteInstance(launcher.ATLauncherInstance{
				Name:          display,
				Minecraft:     p.Lock.Minecraft,
				LoaderType:    p.Lock.Loader.Type,
				LoaderVersion: p.Lock.Loader.Version,
				Version:       version,
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
			if err := saveIntent(res.GameDir, src.name, ref, name, "client"); err != nil {
				return err
			}
			a.registerInstance(config.Instance{ID: as, Launcher: "atlauncher", LauncherDir: launcherDir, Name: display, Dir: res.GameDir, Source: src.name})
			var synced *syncResult
			if len(args) == 1 {
				r, err := a.sync(cmd.Context(), src, syncRequest{ref: ref, target: name, into: res.GameDir})
				if err != nil {
					return err
				}
				synced = &r
			}
			rep := prismReport{
				Launcher:    "atlauncher",
				LauncherDir: launcherDir,
				Instance:    filepath.Base(res.Dir),
				InstanceDir: res.Dir,
				Name:        display,
				Mode:        "sync",
				Target:      name,
				GameDir:     res.GameDir,
				Command:     launcher.SlotCommand("atlauncher", res.GameDir, launcher.HookPreLaunch),
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
				rows := []out.Row{{Text: "the launcher runs `shulker sync` for target " + name + " before each launch"}}
				if hasFeatures {
					rows = append(rows, out.Row{Text: "feature choices saved; change them with `shulker feature on|off <feature> --into " + launcher.CommandArg(res.GameDir) + "`"})
				}
				rows = append(rows, out.Row{Text: "restart ATLauncher if it is open so the instance shows up"})
				l.Tree(rows...)
				if synced != nil {
					synced.print(l)
				}
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher data directory (default: ATLauncher's)")
	cmd.Flags().StringVar(&target, "target", "", "client target to link (default: the only client target)")
	cmd.Flags().StringVar(&instanceName, "name", "", "instance name (default: the target's display name)")
	cmd.Flags().StringVar(&as, "as", "", "id for this instance, for -i (default: from its name)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().BoolVar(&force, "force", false, "link over an instance that syncs from a different source or that shulker didn't link")
	ff.register(cmd, "for this instance")
	return cmd
}

// atlauncherVersion is the version JSON an ATLauncher instance starts the game from. NeoForge and
// Forge build part of the client with their installer, so it runs once per loader version into a
// scratch launcher directory in the cache, and what it put in libraries/ is copied into ATLauncher's.
func (a *app) atlauncherVersion(ctx context.Context, p *project.Project, l loader.Loader, atl *launcher.ATLauncher) (json.RawMessage, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	a.progress("fetching Minecraft %s", p.Lock.Minecraft)
	vanilla, err := d.meta.Piston.Version(ctx, p.Lock.Minecraft)
	if err != nil {
		return nil, err
	}
	if p.Lock.Loader.Type == "" {
		return vanilla, nil
	}
	var loaderVersion json.RawMessage
	if l.InstallClientFlag == "" {
		a.progress("fetching %s loader %s for %s", p.Lock.Loader.Type, p.Lock.Loader.Version, p.Lock.Minecraft)
		if loaderVersion, err = d.meta.LoaderProfile(ctx, p.Lock.Loader, p.Lock.Minecraft); err != nil {
			return nil, err
		}
		return launcher.MergeVersion(vanilla, loaderVersion)
	}
	scratch := d.cache.ATLauncherInstall(l.Name, p.Lock.Loader.Version)
	installed := filepath.Join(scratch, ".installed")
	if _, err := os.Stat(installed); err != nil {
		if err := os.MkdirAll(scratch, 0o755); err != nil {
			return nil, err
		}
		if _, err := a.installClientLoader(ctx, p, &launcher.Mojang{Dir: scratch}, l); err != nil {
			return nil, err
		}
		if err := fsutil.Write(installed, nil); err != nil {
			return nil, err
		}
	}
	jar, err := a.clientInstaller(ctx, p)
	if err != nil {
		return nil, err
	}
	if _, err := launcher.CopyLibraries(filepath.Join(scratch, "libraries"), atl.LibrariesDir()); err != nil {
		return nil, err
	}
	if loaderVersion, err = meta.InstallerVersion(jar); err != nil {
		return nil, err
	}
	return launcher.MergeVersion(vanilla, loaderVersion)
}
