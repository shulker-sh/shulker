package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
)

func (a *app) linkPrismCmd() *cobra.Command { return a.linkPrismLikeCmd(false) }

func (a *app) linkMultiMCCmd() *cobra.Command { return a.linkPrismLikeCmd(true) }

// linkPrismLikeCmd is `link prism` and `link multimc`: one implementation, since MultiMC is the
// layout Prism grew from, told apart by the launcher name, the instance.cfg dialect and MultiMC
// having no default directory to find.
func (a *app) linkPrismLikeCmd(multimc bool) *cobra.Command {
	var k launcherLink
	launcherName, use, short := "prism", "prism", "Create a Prism Launcher instance that syncs the client build before each launch"
	if multimc {
		launcherName, use, short = "multimc", "multimc", "Create a MultiMC instance that syncs the client build before each launch"
	}
	cmd := &cobra.Command{
		Use:   use + " [project-dir | git-url | manifest-url]",
		Short: short,
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if multimc && k.launcherDir == "" {
				if err := k.ls.check(); err != nil {
					return err
				}
				dir, err := a.askMultiMCDir()
				if err != nil {
					return err
				}
				k.launcherDir = dir
			}
			src, _, err := a.startLauncherLink(cmd, args, &k, launcher.DefaultPrismDir)
			if err != nil {
				return err
			}
			p := src.project
			l := &launcher.Prism{Dir: k.launcherDir, MultiMC: multimc}
			if err := l.Check(); errors.Is(err, launcher.ErrNotFound) {
				return out.Errorf("launcher-not-found", "no launcher directory at %s; run the launcher once or pass --launcher-dir", k.launcherDir)
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
				extra = append(extra, out.Row{Text: "restart the launcher if it is open so the change is picked up"})
			}
			return a.finishLauncherLink(cmd, &k, launcherName, display, src, res, extra...)
		},
	}
	dirUsage := "launcher data directory (default: Prism Launcher's)"
	if multimc {
		dirUsage = "the MultiMC folder, the one that holds multimc.cfg (required)"
	}
	k.register(cmd, dirUsage, "repoint the modpack an instance already follows")
	return cmd
}

// askMultiMCDir asks where MultiMC is: it is portable, so unlike every other launcher there is no
// default to fall back on, and off a terminal the flag is required.
func (a *app) askMultiMCDir() (string, error) {
	required := out.Errorf("launcher-dir-required", "MultiMC is portable; pass --launcher-dir with the folder that holds multimc.cfg")
	if !a.canPick() {
		return "", required
	}
	dir, err := a.askText("Where is MultiMC installed?", "the folder that holds multimc.cfg", "")
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", required
	}
	// A shell expands ~ in --launcher-dir, so the answer to the same question does too.
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, rest)
	}
	return dir, nil
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
