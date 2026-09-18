package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
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
	Mode        string      `json:"mode"`
	Side        string      `json:"side"`
	GameDir     string      `json:"gameDir"`
	Command     string      `json:"command,omitempty"`
	Created     bool        `json:"created"`
	Source      string      `json:"source"`
	Sync        *syncResult `json:"sync,omitempty"`
}

func (a *app) linkPrismCmd() *cobra.Command { return a.linkPrismLikeCmd(false) }

func (a *app) linkMultiMCCmd() *cobra.Command { return a.linkPrismLikeCmd(true) }

// linkPrismLikeCmd is `link prism` and `link multimc`: one implementation, since MultiMC is the
// layout Prism grew from, told apart by the launcher name, the instance.cfg dialect and MultiMC
// having no default directory to find.
func (a *app) linkPrismLikeCmd(multimc bool) *cobra.Command {
	var launcherDir, mode, instanceName, ref, as string
	var force bool
	var assumeClient bool
	var ff featureFlags
	launcherName, use, short := "prism", "prism", "Create a Prism Launcher instance that syncs the client build before each launch"
	if multimc {
		launcherName, use, short = "multimc", "multimc", "Create a MultiMC instance that syncs the client build before each launch"
	}
	cmd := &cobra.Command{
		Use:   use + " [project-dir | git-url | manifest-url]",
		Short: short,
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if multimc && launcherDir == "" {
				return out.Errorf("launcher-dir-required", "MultiMC is portable; pass --launcher-dir with the folder that holds multimc.cfg")
			}
			if mode != "sync" && mode != "symlink" {
				return out.Errorf("usage", "--mode must be sync or symlink, not %q", mode)
			}
			if mode == "symlink" && runtime.GOOS == "windows" {
				return out.Errorf("unsupported-mode", "symlink mode is not supported on Windows yet; use --mode sync")
			}
			src, err := a.linkSource(cmd.Context(), args, ref)
			if err != nil {
				return err
			}
			if mode == "symlink" && src.remote() {
				return out.Errorf("usage", "--mode symlink needs a local project; a remote source can only be synced")
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
				if mode != "sync" {
					return out.Errorf("usage", "--with and --without need --mode sync; a symlinked instance uses the build directory as built")
				}
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
			buildDir := filepath.Join(src.Dir, p.Manifest.BuildDir(side))
			display := p.Manifest.DisplayName(side)
			if instanceName != "" {
				display = instanceName
			}
			inst := launcher.Instance{
				ID:            profileKey(display),
				Name:          display,
				Minecraft:     p.Lock.Minecraft,
				LoaderType:    p.Lock.Loader.Type,
				LoaderVersion: p.Lock.Loader.Version,
			}
			if mode == "sync" {
				prevState, stateErr := build.ReadState(l.GameDir(inst.ID))
				if stateErr != nil {
					a.printer.Warn("%v", stateErr)
				}
				if prev := prevState.Source; prev != "" && prev != src.name && !force {
					return out.Errorf("instance-exists", "instance %q already syncs from %s; pass --name to create a second instance, or --force to repoint this one", display, prev)
				}
			} else {
				inst.GameDirLink = buildDir
			}
			res, err := l.WriteInstance(inst)
			if errors.Is(err, launcher.ErrGameDirNotEmpty) {
				return out.Errorf("instance-dir-not-empty", "%s already has files; move them away (or keep --mode sync) before linking in symlink mode", res.GameDir)
			}
			if err != nil {
				return err
			}
			if hasFeatures {
				if err := a.saveInstanceFeatures(res.GameDir, ff); err != nil {
					return err
				}
			}
			// A symlink-mode instance gets no instance file and so no hooks; if it had them before,
			// this is where it lets go of them.
			if mode != "sync" {
				if e := launcher.Find(launcherName); e != nil {
					if _, _, err := launcher.ReleaseSlots(e, config.Instance{Launcher: launcherName, LauncherDir: launcherDir, Dir: res.GameDir}); err != nil {
						return err
					}
				}
			}
			if err := a.checkID(as, res.GameDir); err != nil {
				return err
			}
			// In symlink mode the game directory is the project's own build directory, so the
			// project holds the intent and nothing is written into its output.
			if mode == "sync" {
				if err := saveIntent(res.GameDir, src.name, ref, side, assumeClient); err != nil {
					return err
				}
			}
			a.registerInstance(config.Instance{ID: as, Launcher: launcherName, LauncherDir: launcherDir, Name: display, Dir: res.GameDir, Source: src.name})
			var synced *syncResult
			if len(args) == 1 && mode == "sync" {
				r, err := a.sync(cmd.Context(), src, syncRequest{ref: ref, side: side, into: res.GameDir, assumeClient: assumeClient})
				if err != nil {
					return err
				}
				synced = &r
			}
			rep := prismReport{
				Launcher:    launcherName,
				LauncherDir: launcherDir,
				Instance:    inst.ID,
				InstanceDir: res.Dir,
				Name:        display,
				Mode:        mode,
				Side:        side,
				GameDir:     res.GameDir,
				Command:     linkedSlotCommand(launcherName, mode, res.GameDir),
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
				var rows []out.Row
				if mode == "sync" {
					rows = append(rows, out.Row{Text: "the launcher runs `shulker sync` for the " + side + " side before each launch"})
				} else {
					rows = append(rows, out.Row{Text: "the instance game directory links to " + buildDir})
				}
				if hasFeatures {
					rows = append(rows, out.Row{Text: "feature choices saved; change them with `shulker feature on|off <feature> --into " + launcher.CommandArg(res.GameDir) + "`"})
				}
				if !res.Created {
					rows = append(rows, out.Row{Text: "restart the launcher if it is open so the change is picked up"})
				}
				l.Tree(rows...)
				if mode != "sync" {
					if _, err := os.Stat(build.StatePath(buildDir)); err != nil {
						l.Nudge("Download and build before launching", "shulker install")
					}
				}
				if synced != nil {
					synced.print(l)
				}
			})
		},
	}
	if multimc {
		cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "the MultiMC folder, the one that holds multimc.cfg (required)")
	} else {
		cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher data directory (default: Prism Launcher's)")
	}
	cmd.Flags().BoolVar(&assumeClient, "assume-client", false, "link a client even when the source declares none, built from the mods and overrides both sides share")
	cmd.Flags().StringVar(&mode, "mode", "sync", "sync: build into the instance before each launch; symlink: point the instance at the build directory")
	cmd.Flags().StringVar(&instanceName, "name", "", "instance name (default: the side's display name)")
	cmd.Flags().StringVar(&as, "as", "", "id for this instance, for -i (default: from its name)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().BoolVar(&force, "force", false, "repoint an instance that syncs from a different source")
	ff.register(cmd, "for this instance")
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

// linkedSlotCommand is what the launcher's slot holds once reconcile has written it. An instance
// linked in symlink mode has no instance file and so no hooks, and nothing in its slot.
func linkedSlotCommand(launcherName, mode, gameDir string) string {
	if mode != "sync" {
		return ""
	}
	return launcher.SlotCommand(launcherName, gameDir, launcher.HookPreLaunch)
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
