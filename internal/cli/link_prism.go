package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
)

func (a *app) linkPrismCmd() *cobra.Command {
	var k launcherLink
	cmd := &cobra.Command{
		Use:   "prism [project-dir | git-url | manifest-url]",
		Short: "Create a Prism Launcher instance that syncs the client build before each launch",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, _, err := a.startLauncherLink(cmd, args, &k, launcher.DefaultPrismDir)
			if err != nil {
				return err
			}
			p := src.project
			l := &launcher.Prism{Dir: k.launcherDir}
			if err := l.Check(); errors.Is(err, launcher.ErrNotFound) {
				return out.Errorf("launcher-not-found", "no Prism Launcher directory at %s; run Prism Launcher once or pass --launcher-dir", k.launcherDir)
			} else if err != nil {
				return err
			}
			display := k.display(p)
			if err := checkAdopt(l.GameDir(instanceKey(display)), src, "instance", display, "--name", k.force); err != nil {
				return err
			}
			res, err := l.WriteInstance(launcher.PrismInstance{
				ID:            instanceKey(display),
				Name:          display,
				Minecraft:     p.Lock.Minecraft,
				LoaderType:    p.Lock.Loader.Type,
				LoaderVersion: p.Lock.Loader.Version,
			})
			if err != nil {
				return err
			}
			var extra []out.Row
			if !res.Created {
				extra = append(extra, out.Row{Text: "restart Prism Launcher if it is open so the change is picked up"})
			}
			return a.finishLauncherLink(cmd, &k, "prism", display, src, res, extra...)
		},
	}
	k.register(cmd, "launcher data directory (default: Prism Launcher's)", "repoint the modpack an instance already follows")
	return cmd
}

func (a *app) projectSource() (*syncSource, error) {
	p, err := a.openProject()
	if err != nil {
		return nil, err
	}
	if err := p.RequireLock(); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(p.Dir)
	if err != nil {
		return nil, err
	}
	return &syncSource{Checkout: &pack.Checkout{Source: dir, Kind: pack.Local, Dir: dir}, name: dir, project: p}, nil
}

func (a *app) saveInstanceFeatures(gameDir string, ff featureFlags) error {
	lf, err := local.Load(gameDir)
	if err != nil {
		return err
	}
	for _, name := range ff.with {
		lf.SetFeature(name, true)
	}
	for _, name := range ff.without {
		lf.SetFeature(name, false)
	}
	return a.saveLocal(lf, false)
}

// shulkerPath is the path reconcile records in settings.shulker for the generated scripts to call:
// the one shulker
// is installed under on PATH when that is this binary, else this binary's own
// path. A bare "shulker" would not do, because launchers opened from the Dock
// never read the shell rc files installers add PATH through.
func shulkerPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	onPath, err := exec.LookPath("shulker")
	if err != nil {
		return exe, nil
	}
	if onPath, err = filepath.Abs(onPath); err != nil {
		return exe, nil
	}
	a, err := os.Stat(onPath)
	if err != nil {
		return exe, nil
	}
	b, err := os.Stat(exe)
	if err != nil || !os.SameFile(a, b) {
		return exe, nil
	}
	return onPath, nil
}
