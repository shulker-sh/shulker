package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/meta"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

func (a *app) linkATLauncherCmd() *cobra.Command {
	var k launcherLink
	cmd := &cobra.Command{
		Use:         "atlauncher [project-dir | git-url | manifest-url]",
		Annotations: acts(),
		Short:       "Create an ATLauncher instance that syncs the client build before each launch",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, l, err := a.startLauncherLink(cmd, args, &k, launcher.DefaultATLauncherDir)
			if err != nil {
				return err
			}
			p := src.project
			atl := &launcher.ATLauncher{Dir: k.launcherDir}
			if err := atl.Check(); errors.Is(err, launcher.ErrNotFound) {
				e := out.Errorf("launcher-not-found", "no ATLauncher directory at %s", k.launcherDir)
				e.Help = "run ATLauncher once or pass --launcher-dir"
				return e
			} else if err != nil {
				return err
			}
			display := k.display(p)
			if launcher.ATLauncherFolder(display) == "" {
				e := out.Errorf("usage", "ATLauncher names an instance's folder after the letters and digits in its name, and %q has none", display)
				e.Help = "pass --name"
				return e
			}
			gameDir := atl.InstanceDir(display)
			if err := checkAdopt(gameDir, src, "instance", display, "--name", k.force); err != nil {
				return err
			}
			if err := a.refuseForeignInstance(&k, gameDir, "ATLauncher", display, func() (string, bool, error) {
				return launcher.ATLauncherPreLaunch(gameDir)
			}); err != nil {
				return err
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
			return a.finishLauncherLink(cmd, &k, "atlauncher", display, src, res, out.Row{Text: "restart ATLauncher if it is open so the instance shows up"})
		},
	}
	k.register(cmd, "launcher data directory (default: ATLauncher's)", "repoint the modpack an instance already follows, or link over one shulker didn't link")
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
