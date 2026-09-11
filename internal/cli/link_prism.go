package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shulker-sh/shulker/internal/build"
	"github.com/shulker-sh/shulker/internal/launcher"
	"github.com/shulker-sh/shulker/internal/out"
	"github.com/spf13/cobra"
)

type prismReport struct {
	Launcher    string `json:"launcher"`
	LauncherDir string `json:"launcherDir"`
	Instance    string `json:"instance"`
	InstanceDir string `json:"instanceDir"`
	Name        string `json:"name"`
	Mode        string `json:"mode"`
	Target      string `json:"target"`
	GameDir     string `json:"gameDir"`
	Command     string `json:"command,omitempty"`
	Created     bool   `json:"created"`
}

func (a *app) linkPrismCmd() *cobra.Command {
	var launcherDir, target, mode string
	var ff featureFlags
	cmd := &cobra.Command{
		Use:     "prism",
		Aliases: []string{"multimc"},
		Short:   "Create a Prism Launcher or MultiMC instance that syncs the client build before each launch",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if mode != "sync" && mode != "symlink" {
				return out.Errorf("invalid-mode", "--mode must be sync or symlink, not %q", mode)
			}
			if mode == "symlink" && runtime.GOOS == "windows" {
				return out.Errorf("unsupported-mode", "symlink mode is not supported on Windows yet; use --mode sync")
			}
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if _, ok := launcher.LoaderUID(p.Lock.Loader.Type); !ok {
				return out.Errorf("unsupported-loader", "link prism does not know the %s loader", p.Lock.Loader.Type)
			}
			name, err := sideTarget(p.Manifest, target, "client", "link")
			if err != nil {
				return err
			}
			if len(ff.args()) > 0 {
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
			l := &launcher.Prism{Dir: launcherDir, MultiMC: cmd.CalledAs() == "multimc"}
			if err := l.Check(); errors.Is(err, launcher.ErrNotFound) {
				return out.Errorf("launcher-not-found", "no launcher directory at %s; run the launcher once or pass --launcher-dir", launcherDir)
			} else if err != nil {
				return err
			}
			projectDir, err := filepath.Abs(p.Dir)
			if err != nil {
				return err
			}
			buildDir := filepath.Join(projectDir, p.Manifest.BuildDir(name))
			display := p.Manifest.DisplayName(name)
			inst := launcher.Instance{
				ID:            profileKey(display),
				Name:          display,
				Minecraft:     p.Lock.Minecraft,
				LoaderType:    p.Lock.Loader.Type,
				LoaderVersion: p.Lock.Loader.Version,
			}
			if mode == "sync" {
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				inst.PreLaunch = strings.Join(append([]string{launcher.CommandArg(exe), "sync", launcher.CommandArg(projectDir), "--target", name, "--into", `"$INST_MC_DIR"`}, ff.args()...), " ")
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
			rep := prismReport{
				Launcher:    "prism",
				LauncherDir: launcherDir,
				Instance:    inst.ID,
				InstanceDir: res.Dir,
				Name:        display,
				Mode:        mode,
				Target:      name,
				GameDir:     res.GameDir,
				Command:     inst.PreLaunch,
				Created:     res.Created,
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
				if !res.Created {
					fmt.Fprintln(w, "Restart the launcher if it is open so the change is picked up.")
				}
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher data directory (default: Prism Launcher's; required for MultiMC)")
	cmd.Flags().StringVar(&target, "target", "", "client target to link (default: the only client target)")
	cmd.Flags().StringVar(&mode, "mode", "sync", "sync: build into the instance before each launch; symlink: point the instance at the build directory")
	ff.register(cmd, "in every pre-launch sync of this instance")
	return cmd
}
