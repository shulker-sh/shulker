package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
)

type prismReport struct {
	Launcher    string      `json:"launcher"`
	LauncherDir string      `json:"launcherDir"`
	Instance    string      `json:"instance"`
	InstanceDir string      `json:"instanceDir"`
	Name        string      `json:"name"`
	GameDir     string      `json:"gameDir"`
	Command     string      `json:"command,omitempty"`
	Created     bool        `json:"created"`
	Source      string      `json:"source"`
	Ref         string      `json:"ref,omitempty"`
	Modpack     string      `json:"modpack"`
	Sync        *syncResult `json:"sync"`
}

func (a *app) linkPrismCmd() *cobra.Command { return a.linkPrismLikeCmd(false) }

func (a *app) linkMultiMCCmd() *cobra.Command { return a.linkPrismLikeCmd(true) }

// linkPrismLikeCmd is `link prism` and `link multimc`: one implementation, since MultiMC is the
// layout Prism grew from, told apart by the launcher name, the instance.cfg dialect and MultiMC
// having no default directory to find.
func (a *app) linkPrismLikeCmd(multimc bool) *cobra.Command {
	var launcherDir, instanceName, ref, as string
	var force bool
	var ff featureFlags
	var ls linkSettings
	launcherName, use, short := "prism", "prism", "Create a Prism Launcher instance that syncs the client build before each launch"
	if multimc {
		launcherName, use, short = "multimc", "multimc", "Create a MultiMC instance that syncs the client build before each launch"
	}
	cmd := &cobra.Command{
		Use:   use + " [project-dir | git-url | manifest-url]",
		Short: short,
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ls.check(); err != nil {
				return err
			}
			if multimc && launcherDir == "" {
				return out.Errorf("launcher-dir-required", "MultiMC is portable; pass --launcher-dir with the folder that holds multimc.cfg")
			}
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
			if !p.Manifest.HasSide("client") {
				a.printer.Warn("%s", noClientPack)
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
				if launcherDir, err = launcher.DefaultPrismDir(); err != nil {
					return err
				}
			}
			if launcherDir, err = filepath.Abs(launcherDir); err != nil {
				return err
			}
			l := &launcher.Prism{Dir: launcherDir, MultiMC: multimc}
			if err := l.Check(); errors.Is(err, launcher.ErrNotFound) {
				return out.Errorf("launcher-not-found", "no launcher directory at %s; run the launcher once or pass --launcher-dir", launcherDir)
			} else if err != nil {
				return err
			}
			display := p.Manifest.DisplayName("client")
			if instanceName != "" {
				display = instanceName
			}
			if !force {
				if err := checkAdopt(l.GameDir(profileKey(display)), src.name, "instance", display, "--name"); err != nil {
					return err
				}
			}
			res, err := l.WriteInstance(launcher.Instance{
				ID:            profileKey(display),
				Name:          display,
				Minecraft:     p.Lock.Minecraft,
				LoaderType:    p.Lock.Loader.Type,
				LoaderVersion: p.Lock.Loader.Version,
			})
			if err != nil {
				return err
			}
			if hasFeatures {
				if err := a.saveInstanceFeatures(res.GameDir, ff); err != nil {
					return err
				}
			}
			row := config.Instance{Launcher: launcherName, LauncherDir: launcherDir, Name: display, Dir: res.GameDir, Source: src.name}
			inst, synced, err := a.linkInstance(cmd, row, as, ref, src, ls)
			if err != nil {
				return err
			}
			rep := prismReport{
				Launcher:    launcherName,
				LauncherDir: launcherDir,
				Instance:    filepath.Base(res.Dir),
				InstanceDir: res.Dir,
				Name:        display,
				GameDir:     res.GameDir,
				Command:     launcher.SlotCommand(launcherName, res.GameDir, launcher.HookPreLaunch),
				Created:     res.Created,
				Source:      src.name,
				Ref:         ref,
				Modpack:     modpackKey(inst.Manifest, src.name),
				Sync:        &synced,
			}
			return a.printer.Emit(rep, func(l *out.Lines) {
				verb := "created"
				if !res.Created {
					verb = "updated"
				}
				l.OKInto(verb+" instance "+display, res.Dir, "")
				rows := []out.Row{
					{Text: "follows " + rep.Modpack + " from " + rep.Source},
					{Text: "the launcher syncs this instance before each launch"},
				}
				if hasFeatures {
					rows = append(rows, out.Row{Text: "feature choices saved; change them with `shulker feature on|off <feature> --into " + launcher.CommandArg(res.GameDir) + "`"})
				}
				if !res.Created {
					rows = append(rows, out.Row{Text: "restart the launcher if it is open so the change is picked up"})
				}
				l.Tree(rows...)
				synced.print(l)
			})
		},
	}
	if multimc {
		cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "the MultiMC folder, the one that holds multimc.cfg (required)")
	} else {
		cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher data directory (default: Prism Launcher's)")
	}
	cmd.Flags().StringVar(&instanceName, "name", "", "instance name (default: the side's display name)")
	cmd.Flags().StringVar(&as, "as", "", "id for this instance, for -i (default: from its name)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().BoolVar(&force, "force", false, "repoint the modpack an instance already follows")
	ff.register(cmd, "for this instance")
	ls.register(cmd)
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
