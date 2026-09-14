package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

type linkReport struct {
	Launcher    string      `json:"launcher"`
	LauncherDir string      `json:"launcherDir"`
	Profile     string      `json:"profile"`
	Name        string      `json:"name"`
	VersionID   string      `json:"versionId"`
	Target      string      `json:"target"`
	GameDir     string      `json:"gameDir"`
	Source      string      `json:"source"`
	Ref         string      `json:"ref,omitempty"`
	Sync        *syncResult `json:"sync,omitempty"`
}

func (a *app) linkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Point a launcher at this project's client build",
	}
	cmd.AddCommand(a.linkMojangCmd(), a.linkPrismCmd(), a.linkATLauncherCmd(), a.linkGDLauncherCmd())
	return cmd
}

func (a *app) linkMojangCmd() *cobra.Command {
	var launcherDir, target, instanceName, ref string
	var force bool
	cmd := &cobra.Command{
		Use:     "mojang [project-dir | git-url | manifest-url]",
		Aliases: []string{"vanilla"},
		Short:   "Install the loader into the official launcher and add a profile for the client build",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := a.linkSource(cmd.Context(), args, ref)
			if err != nil {
				return err
			}
			p := src.project
			l, err := loader.Require(p.Lock.Loader.Type)
			if err != nil {
				return err
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
			display := p.Manifest.DisplayName(name)
			if instanceName != "" {
				display = instanceName
			}
			key := profileKey(display)
			if prev, ok := a.findLauncherLink("mojang", launcherDir, display); ok && prev.Source != src.name && !force {
				return out.Errorf("instance-exists", "profile %q already syncs from %s; pass --name to create a second profile, or --force to repoint this one", display, prev.Source)
			}
			gameDir := filepath.Join(src.Dir, p.Manifest.BuildDir(name))
			if src.remote() {
				gameDir = filepath.Join(launcherDir, "shulker", strings.TrimPrefix(key, "shulker-"))
			}
			if gameDir, err = filepath.Abs(gameDir); err != nil {
				return err
			}
			var versionID string
			if l.InstallClientFlag != "" {
				if versionID, err = a.installClientLoader(cmd.Context(), p, v, l); err != nil {
					return err
				}
			} else {
				d, err := a.deps()
				if err != nil {
					return err
				}
				a.progress("fetching %s loader %s for %s", p.Lock.Loader.Type, p.Lock.Loader.Version, p.Lock.Minecraft)
				profile, err := d.meta.LoaderProfile(cmd.Context(), p.Lock.Loader, p.Lock.Minecraft)
				if err != nil {
					return err
				}
				if versionID, err = v.InstallVersion(profile); err != nil {
					return err
				}
			}
			rep := linkReport{
				Launcher:    "mojang",
				LauncherDir: launcherDir,
				Profile:     key,
				Name:        display,
				VersionID:   versionID,
				Target:      name,
				GameDir:     gameDir,
				Source:      src.name,
				Ref:         ref,
			}
			if err := v.WriteProfile(launcher.Profile{Key: key, Name: display, VersionID: versionID, GameDir: gameDir}); err != nil {
				return err
			}
			a.registerLink(config.Link{Launcher: "mojang", LauncherDir: launcherDir, Side: "client", Name: display, Dir: gameDir, Source: src.name, Target: name, Ref: ref})
			if src.remote() {
				r, err := a.sync(cmd.Context(), src, syncRequest{ref: ref, target: name, into: gameDir})
				if err != nil {
					return err
				}
				rep.Sync = &r
			}
			return a.printer.Emit(rep, func(l *out.Lines) {
				l.OKInto("installed "+versionID, filepath.Join(launcherDir, "versions"), "")
				l.OKInto("linked launcher profile "+display, gameDir, "")
				if rep.Sync != nil {
					rep.Sync.print(l)
					return
				}
				if _, err := os.Stat(filepath.Join(gameDir, build.StateFile)); err != nil {
					l.Nudge("Download and build before launching", "shulker install")
				}
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher directory (default: the official launcher's .minecraft folder)")
	cmd.Flags().StringVar(&target, "target", "", "client target to link (default: the only client target)")
	cmd.Flags().StringVar(&instanceName, "name", "", "profile name (default: the target's display name)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().BoolVar(&force, "force", false, "repoint a profile that syncs from a different source")
	return cmd
}

// findLauncherLink is the registry entry a launcher already has under a name,
// when its directory is still there.
func (a *app) findLauncherLink(launcherName, launcherDir, name string) (config.Link, bool) {
	links, err := a.loadLinks()
	if err != nil {
		return config.Link{}, false
	}
	for _, l := range links {
		if l.Launcher != launcherName || l.Name != name || !sameDir(l.LauncherDir, launcherDir) {
			continue
		}
		if _, err := os.Stat(l.Dir); err == nil {
			return l, true
		}
	}
	return config.Link{}, false
}

// linkSource is the project a link command works from: the argument when there
// is one, else the project in the current directory.
func (a *app) linkSource(ctx context.Context, args []string, ref string) (*syncSource, error) {
	if len(args) == 1 {
		return a.openSource(ctx, args[0], ref)
	}
	if ref != "" {
		return nil, out.Errorf("usage", "--ref needs a git source argument")
	}
	return a.projectSource()
}

func sideTarget(m *manifest.Manifest, want, side, verb string) (string, error) {
	if want != "" {
		t, err := m.Target(want)
		if err != nil {
			return "", err
		}
		if t.Side != side {
			e := out.Errorf("wrong-side-target", "target %q is a %s target; %s needs a %s target", want, t.Side, verb, side)
			e.Candidates, e.Flag = sideTargets(m, side), "--target"
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
	e.Candidates, e.Flag = matches, "--target"
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
