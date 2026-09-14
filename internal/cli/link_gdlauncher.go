package cli

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
)

func (a *app) linkGDLauncherCmd() *cobra.Command {
	var launcherDir, target, instanceName, ref string
	var force bool
	var ff featureFlags
	cmd := &cobra.Command{
		Use:   "gdlauncher [project-dir | git-url | manifest-url]",
		Short: "Create a GDLauncher instance that syncs the client build before each launch",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := a.linkSource(cmd.Context(), args, ref)
			if err != nil {
				return err
			}
			p := src.project
			if _, err := loader.Require(p.Lock.Loader.Type); err != nil {
				return err
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
			display := p.Manifest.DisplayName(name)
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
			exe, err := shulkerPath()
			if err != nil {
				return err
			}
			command := []string{launcher.GDLauncherHookArg(exe), "sync", launcher.GDLauncherHookArg(src.name)}
			if ref != "" {
				command = append(command, "--ref", launcher.GDLauncherHookArg(ref))
			}
			preLaunch := strings.Join(append(command, "--target", name, "--into", "."), " ")
			res, err := gdl.WriteInstance(launcher.GDLauncherInstance{
				Name:          display,
				Minecraft:     p.Lock.Minecraft,
				LoaderType:    p.Lock.Loader.Type,
				LoaderVersion: p.Lock.Loader.Version,
				PreLaunch:     preLaunch,
			})
			if err != nil {
				return err
			}
			if hasFeatures {
				if err := a.saveInstanceFeatures(res.GameDir, ff); err != nil {
					return err
				}
			}
			a.registerLink(config.Link{Launcher: "gdlauncher", LauncherDir: launcherDir, Side: "client", Name: display, Dir: res.GameDir, Source: src.name, Target: name, Ref: ref})
			var synced *syncResult
			if len(args) == 1 {
				r, err := a.sync(cmd.Context(), src, syncRequest{ref: ref, target: name, into: res.GameDir})
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
				Target:      name,
				GameDir:     res.GameDir,
				Command:     preLaunch,
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
				rows = append(rows, out.Row{Text: "restart GDLauncher if it is open so the instance shows up"})
				l.Tree(rows...)
				if synced != nil {
					synced.print(l)
				}
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher runtime directory (default: GDLauncher's)")
	cmd.Flags().StringVar(&target, "target", "", "client target to link (default: the only client target)")
	cmd.Flags().StringVar(&instanceName, "name", "", "instance name (default: the target's display name)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().BoolVar(&force, "force", false, "repoint an instance that syncs from a different source")
	ff.register(cmd, "for this instance")
	return cmd
}
