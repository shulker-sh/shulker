package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

type linkReport struct {
	Launcher    string `json:"launcher"`
	LauncherDir string `json:"launcherDir"`
	Profile     string `json:"profile"`
	VersionID   string `json:"versionId"`
	Target      string `json:"target"`
	GameDir     string `json:"gameDir"`
}

func (a *app) linkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Point a launcher at this project's client build",
	}
	cmd.AddCommand(a.linkMojangCmd(), a.linkPrismCmd())
	return cmd
}

func (a *app) linkMojangCmd() *cobra.Command {
	var launcherDir, target string
	cmd := &cobra.Command{
		Use:     "mojang",
		Aliases: []string{"vanilla"},
		Short:   "Install the loader into the official launcher and add a profile for the client build",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if p.Lock.Loader.Type != "fabric" {
				return out.Errorf("unsupported-loader", "link mojang supports only the fabric loader for now, not %s", p.Lock.Loader.Type)
			}
			name, err := sideTarget(p.Manifest, target, "client", "link")
			if err != nil {
				return err
			}
			if launcherDir == "" {
				if launcherDir, err = launcher.DefaultMojangDir(); err != nil {
					return err
				}
			}
			if launcherDir, err = filepath.Abs(launcherDir); err != nil {
				return err
			}
			v := &launcher.Mojang{Dir: launcherDir}
			if err := v.Check(); errors.Is(err, launcher.ErrNotFound) {
				return out.Errorf("launcher-not-found", "no Minecraft launcher directory at %s; run the launcher once or pass --launcher-dir", launcherDir)
			} else if err != nil {
				return err
			}
			d, err := a.deps()
			if err != nil {
				return err
			}
			a.progress("Fetching Fabric loader %s profile for %s", p.Lock.Loader.Version, p.Lock.Minecraft)
			profile, err := d.meta.Fabric.LoaderProfile(cmd.Context(), p.Lock.Minecraft, p.Lock.Loader.Version)
			if err != nil {
				return err
			}
			versionID, err := v.InstallVersion(profile)
			if err != nil {
				return err
			}
			gameDir, err := filepath.Abs(filepath.Join(p.Dir, p.Manifest.BuildDir(name)))
			if err != nil {
				return err
			}
			display := p.Manifest.DisplayName(name)
			rep := linkReport{
				Launcher:    "mojang",
				LauncherDir: launcherDir,
				Profile:     profileKey(display),
				VersionID:   versionID,
				Target:      name,
				GameDir:     gameDir,
			}
			if err := v.WriteProfile(launcher.Profile{Key: rep.Profile, Name: display, VersionID: versionID, GameDir: gameDir}); err != nil {
				return err
			}
			projectDir, err := filepath.Abs(p.Dir)
			if err != nil {
				return err
			}
			a.registerLink(config.Link{Launcher: "mojang", LauncherDir: launcherDir, Side: "client", Name: display, Dir: gameDir, Source: projectDir, Target: name})
			return a.printer.Emit(rep, func(w io.Writer) {
				fmt.Fprintf(w, "Installed %s into %s\n", versionID, filepath.Join(launcherDir, "versions"))
				fmt.Fprintf(w, "Linked launcher profile %q to %s\n", display, gameDir)
				if _, err := os.Stat(filepath.Join(gameDir, build.StateFile)); err != nil {
					fmt.Fprintln(w, "Run `shulker install` before launching.")
				}
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher directory (default: the official launcher's .minecraft folder)")
	cmd.Flags().StringVar(&target, "target", "", "client target to link (default: the only client target)")
	return cmd
}

func sideTarget(m *manifest.Manifest, want, side, verb string) (string, error) {
	if want != "" {
		t, err := m.Target(want)
		if err != nil {
			return "", err
		}
		if t.Side != side {
			e := out.Errorf("wrong-side-target", "target %q is a %s target; %s needs a %s target", want, t.Side, verb, side)
			e.Candidates = sideTargets(m, side)
			return "", e
		}
		return want, nil
	}
	matches := sideTargets(m, side)
	switch len(matches) {
	case 0:
		return "", out.Errorf("no-target", "shulker.json has no %s target to %s", side, verb)
	case 1:
		return matches[0], nil
	}
	e := out.Errorf("ambiguous-target", "shulker.json has several %s targets; pass --target", side)
	e.Candidates = matches
	return "", e
}

var unsafeKeyChars = regexp.MustCompile(`[^a-z0-9]+`)

func profileKey(name string) string {
	slug := strings.Trim(unsafeKeyChars.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		slug = "project"
	}
	return "shulker-" + slug
}

func sideTargets(m *manifest.Manifest, side string) []string {
	var names []string
	for name, t := range m.Targets {
		if t.Side == side {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
