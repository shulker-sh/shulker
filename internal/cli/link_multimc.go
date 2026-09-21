package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
)

func (a *app) linkMultiMCCmd() *cobra.Command {
	var k launcherLink
	cmd := &cobra.Command{
		Use:   "multimc [project-dir | git-url | manifest-url]",
		Short: "Create a MultiMC instance that syncs the client build before each launch",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if k.launcherDir == "" {
				if err := k.ls.check(); err != nil {
					return err
				}
				dir, err := a.askMultiMCDir()
				if err != nil {
					return err
				}
				k.launcherDir = dir
			}
			src, _, err := a.startLauncherLink(cmd, args, &k, nil)
			if err != nil {
				return err
			}
			p := src.project
			l := &launcher.MultiMC{Dir: k.launcherDir}
			if err := l.Check(); errors.Is(err, launcher.ErrNotFound) {
				return out.Errorf("launcher-not-found", "no MultiMC directory at %s; run MultiMC once or pass --launcher-dir", k.launcherDir)
			} else if err != nil {
				return err
			}
			display := k.display(p)
			if err := checkAdopt(l.GameDir(instanceKey(display)), src, "instance", display, "--name", k.force); err != nil {
				return err
			}
			res, err := l.WriteInstance(launcher.MultiMCInstance{
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
				extra = append(extra, out.Row{Text: "restart MultiMC if it is open so the change is picked up"})
			}
			return a.finishLauncherLink(cmd, &k, "multimc", display, src, res, extra...)
		},
	}
	k.register(cmd, "the MultiMC folder, the one that holds multimc.cfg (required)", "repoint the modpack an instance already follows")
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
