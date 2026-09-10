package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/andrewmast/shulker/internal/launcher"
	"github.com/andrewmast/shulker/internal/manifest"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/spf13/cobra"
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
	cmd.AddCommand(a.linkMojangCmd())
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
			name, t, err := sideTarget(p.Manifest, target, "client", "link")
			if err != nil {
				return err
			}
			if launcherDir == "" {
				if launcherDir, err = launcher.DefaultMojangDir(); err != nil {
					return err
				}
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
			profile, err := d.meta.Fabric.LoaderProfile(context.Background(), p.Lock.Minecraft, p.Lock.Loader.Version)
			if err != nil {
				return err
			}
			versionID, err := v.InstallVersion(profile)
			if err != nil {
				return err
			}
			gameDir, err := filepath.Abs(filepath.Join(p.Dir, t.Build))
			if err != nil {
				return err
			}
			rep := linkReport{
				Launcher:    "mojang",
				LauncherDir: launcherDir,
				Profile:     profileKey(p.Manifest.Name),
				VersionID:   versionID,
				Target:      name,
				GameDir:     gameDir,
			}
			if err := v.WriteProfile(launcher.Profile{Key: rep.Profile, Name: p.Manifest.Name, VersionID: versionID, GameDir: gameDir}); err != nil {
				return err
			}
			return a.printer.Emit(rep, func(w io.Writer) {
				fmt.Fprintf(w, "Installed %s into %s\n", versionID, filepath.Join(launcherDir, "versions"))
				fmt.Fprintf(w, "Linked launcher profile %q to %s\n", p.Manifest.Name, gameDir)
				fmt.Fprintln(w, "Run `shulker install` before launching.")
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher directory (default: the official launcher's .minecraft folder)")
	cmd.Flags().StringVar(&target, "target", "", "client target to link (default: the only client target)")
	return cmd
}

func sideTarget(m *manifest.Manifest, want, side, verb string) (string, manifest.Target, error) {
	if want != "" {
		t, ok := m.Targets[want]
		if !ok {
			e := out.Errorf("target-not-found", "no target %q in shulker.json", want)
			e.Candidates = targetNames(m.Targets)
			return "", t, e
		}
		if t.Side != side {
			return "", t, out.Errorf("not-"+side+"-target", "target %q is a %s target; %s needs a %s target", want, t.Side, verb, side)
		}
		return want, t, nil
	}
	var matches []string
	for name, t := range m.Targets {
		if t.Side == side {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return "", manifest.Target{}, out.Errorf("no-"+side+"-target", "shulker.json has no %s target to %s", side, verb)
	case 1:
		return matches[0], m.Targets[matches[0]], nil
	}
	e := out.Errorf("ambiguous-target", "shulker.json has several %s targets; pass --target", side)
	e.Candidates = matches
	return "", manifest.Target{}, e
}

var unsafeKeyChars = regexp.MustCompile(`[^a-z0-9]+`)

func profileKey(name string) string {
	slug := strings.Trim(unsafeKeyChars.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		slug = "project"
	}
	return "shulker-" + slug
}
