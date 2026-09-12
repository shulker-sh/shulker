package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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
	Target      string      `json:"target"`
	GameDir     string      `json:"gameDir"`
	Command     string      `json:"command,omitempty"`
	Created     bool        `json:"created"`
	Source      string      `json:"source"`
	Sync        *syncResult `json:"sync,omitempty"`
}

func (a *app) linkPrismCmd() *cobra.Command {
	var launcherDir, target, mode, instanceName, ref string
	var force bool
	var ff featureFlags
	cmd := &cobra.Command{
		Use:     "prism [project-dir | git-url | manifest-url]",
		Aliases: []string{"multimc"},
		Short:   "Create a Prism Launcher or MultiMC instance that syncs the client build before each launch",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mode != "sync" && mode != "symlink" {
				return out.Errorf("usage", "--mode must be sync or symlink, not %q", mode)
			}
			if mode == "symlink" && runtime.GOOS == "windows" {
				return out.Errorf("unsupported-mode", "symlink mode is not supported on Windows yet; use --mode sync")
			}
			var src *syncSource
			var err error
			if len(args) == 1 {
				src, err = a.openSource(cmd.Context(), args[0], ref)
			} else if ref != "" {
				err = out.Errorf("usage", "--ref needs a git source argument")
			} else {
				src, err = a.projectSource()
			}
			if err != nil {
				return err
			}
			if mode == "symlink" && src.remote() {
				return out.Errorf("usage", "--mode symlink needs a local project; a remote source can only be synced")
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
				if cmd.CalledAs() == "multimc" {
					return out.Errorf("launcher-dir-required", "MultiMC is portable; pass --launcher-dir with the folder that holds multimc.cfg")
				}
				if launcherDir, err = launcher.DefaultPrismDir(); err != nil {
					return err
				}
			}
			if launcherDir, err = filepath.Abs(launcherDir); err != nil {
				return err
			}
			l := &launcher.Prism{Dir: launcherDir, MultiMC: cmd.CalledAs() == "multimc"}
			if err := l.Check(); errors.Is(err, launcher.ErrNotFound) {
				return out.Errorf("launcher-not-found", "no launcher directory at %s; run the launcher once or pass --launcher-dir", launcherDir)
			} else if err != nil {
				return err
			}
			buildDir := filepath.Join(src.Dir, p.Manifest.BuildDir(name))
			display := p.Manifest.DisplayName(name)
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
				if prev := build.LoadState(l.GameDir(inst.ID)).Source; prev != "" && prev != src.name && !force {
					return out.Errorf("instance-exists", "instance %q already syncs from %s; pass --name to create a second instance, or --force to repoint this one", display, prev)
				}
				exe, err := shulkerPath()
				if err != nil {
					return err
				}
				command := []string{launcher.CommandArg(exe), "sync", launcher.CommandArg(src.name)}
				if ref != "" {
					command = append(command, "--ref", launcher.CommandArg(ref))
				}
				inst.PreLaunch = strings.Join(append(command, "--target", name, "--into", `"$INST_MC_DIR"`), " ")
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
			launcherName := "prism"
			if l.MultiMC {
				launcherName = "multimc"
			}
			a.registerLink(config.Link{Launcher: launcherName, LauncherDir: launcherDir, Side: "client", Name: display, Dir: res.GameDir, Source: src.name, Target: name, Ref: ref})
			var synced *syncResult
			if len(args) == 1 && mode == "sync" {
				r, err := a.sync(cmd.Context(), src, syncRequest{ref: ref, target: name, into: res.GameDir})
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
				Target:      name,
				GameDir:     res.GameDir,
				Command:     inst.PreLaunch,
				Created:     res.Created,
				Source:      src.name,
				Sync:        synced,
			}
			return a.printer.Emit(rep, func(w io.Writer) {
				verb := "Created"
				if !res.Created {
					verb = "Updated"
				}
				fmt.Fprintf(w, "%s instance %q in %s\n", verb, display, res.Dir)
				if mode == "sync" {
					fmt.Fprintf(w, "The launcher runs `shulker sync` for target %s before each launch.\n", name)
				} else {
					fmt.Fprintf(w, "Linked the instance game directory to %s\n", buildDir)
					if _, err := os.Stat(filepath.Join(buildDir, build.StateFile)); err != nil {
						fmt.Fprintln(w, "Run `shulker install` before launching.")
					}
				}
				if hasFeatures {
					fmt.Fprintf(w, "Saved the feature choices for this instance; change them with `shulker feature on|off <feature> --into %s`.\n", launcher.CommandArg(res.GameDir))
				}
				if !res.Created {
					fmt.Fprintln(w, "Restart the launcher if it is open so the change is picked up.")
				}
				if synced != nil {
					synced.print(w)
				}
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher data directory (default: Prism Launcher's; required for MultiMC)")
	cmd.Flags().StringVar(&target, "target", "", "client target to link (default: the only client target)")
	cmd.Flags().StringVar(&mode, "mode", "sync", "sync: build into the instance before each launch; symlink: point the instance at the build directory")
	cmd.Flags().StringVar(&instanceName, "name", "", "instance name (default: the target's display name)")
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

// shulkerPath is the path to write into a pre-launch command: the one shulker
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
