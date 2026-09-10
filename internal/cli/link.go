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
	cmd.AddCommand(a.linkVanillaCmd())
	return cmd
}

func (a *app) linkVanillaCmd() *cobra.Command {
	var launcherDir, target string
	cmd := &cobra.Command{
		Use:     "vanilla",
		Aliases: []string{"mojang"},
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
				return out.Errorf("unsupported-loader", "link vanilla supports only the fabric loader for now, not %s", p.Lock.Loader.Type)
			}
			name, t, err := clientTarget(p.Manifest, target)
			if err != nil {
				return err
			}
			if launcherDir == "" {
				if launcherDir, err = launcher.DefaultVanillaDir(); err != nil {
					return err
				}
			}
			v := &launcher.Vanilla{Dir: launcherDir}
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
				Launcher:    "vanilla",
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

func clientTarget(m *manifest.Manifest, want string) (string, manifest.Target, error) {
	if want != "" {
		t, ok := m.Targets[want]
		if !ok {
			e := out.Errorf("target-not-found", "no target %q in shulker.json", want)
			e.Candidates = targetNames(m.Targets)
			return "", t, e
		}
		if t.Side != "client" {
			return "", t, out.Errorf("not-client-target", "target %q is a %s target; link needs a client target", want, t.Side)
		}
		return want, t, nil
	}
	var clients []string
	for name, t := range m.Targets {
		if t.Side == "client" {
			clients = append(clients, name)
		}
	}
	sort.Strings(clients)
	switch len(clients) {
	case 0:
		return "", manifest.Target{}, out.Errorf("no-client-target", "shulker.json has no client target to link")
	case 1:
		return clients[0], m.Targets[clients[0]], nil
	}
	e := out.Errorf("ambiguous-target", "shulker.json has several client targets; pass --target")
	e.Candidates = clients
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
