package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
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
	Side        string      `json:"side"`
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
	cmd.AddCommand(a.linkMojangCmd(), a.linkPrismCmd(), a.linkMultiMCCmd(), a.linkATLauncherCmd(), a.linkGDLauncherCmd())
	return cmd
}

func (a *app) linkMojangCmd() *cobra.Command {
	var launcherDir, instanceName, ref, as string
	var force bool
	var assumeClient bool
	var ls linkSettings
	cmd := &cobra.Command{
		Use:     "mojang [project-dir | git-url | manifest-url]",
		Aliases: []string{"vanilla"},
		Short:   "Add a profile for the client build to the official launcher, installing its loader if it has one",
		Args:    maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ls.check(); err != nil {
				return err
			}
			src, err := a.linkSource(cmd.Context(), args, ref)
			if err != nil {
				return err
			}
			p := src.project
			var l loader.Loader
			if p.Lock.Loader.Type != "" {
				if l, err = loader.Require(p.Lock.Loader.Type); err != nil {
					return err
				}
			}
			side, err := a.clientSide(p.Manifest, assumeClient)
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
			display := p.Manifest.DisplayName(side)
			if instanceName != "" {
				display = instanceName
			}
			key := profileKey(display)
			if prev, ok := a.findLauncherInstance("mojang", launcherDir, display); ok && prev.Source != src.name && !force {
				return out.Errorf("instance-exists", "profile %q already syncs from %s; pass --name to create a second profile, or --force to repoint this one", display, prev.Source)
			}
			gameDir := filepath.Join(src.Dir, p.Manifest.BuildDir(side))
			if src.remote() {
				gameDir = filepath.Join(launcherDir, "shulker", strings.TrimPrefix(key, "shulker-"))
			}
			if gameDir, err = filepath.Abs(gameDir); err != nil {
				return err
			}
			versionID := p.Lock.Minecraft
			switch {
			case p.Lock.Loader.Type == "":
			case l.InstallClientFlag != "":
				if versionID, err = a.installClientLoader(cmd.Context(), p, v, l); err != nil {
					return err
				}
			default:
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
				Side:        side,
				GameDir:     gameDir,
				Source:      src.name,
				Ref:         ref,
			}
			if err := a.checkID(as, gameDir); err != nil {
				return err
			}
			if err := v.WriteProfile(launcher.Profile{Key: key, Name: display, VersionID: versionID, GameDir: gameDir}); err != nil {
				return err
			}
			if err := ls.save(gameDir, src.name, ref, side, assumeClient, p.Manifest); err != nil {
				return err
			}
			row := config.Instance{ID: as, Launcher: "mojang", LauncherDir: launcherDir, Name: display, Dir: gameDir, Source: src.name}
			a.registerInstance(row)
			if !src.remote() {
				if _, err := a.recordClientRuntime(cmd.Context(), p, side, gameDir); err != nil {
					return err
				}
				// The shim records the Java it falls back to, which the runtime step just resolved.
				a.reconcileOrWarn(row)
			}
			if src.remote() {
				r, err := a.sync(cmd.Context(), src, syncRequest{ref: ref, side: side, into: gameDir, assumeClient: assumeClient})
				if err != nil {
					return err
				}
				rep.Sync = &r
			}
			return a.printer.Emit(rep, func(l *out.Lines) {
				if p.Lock.Loader.Type != "" {
					l.OKInto("installed "+versionID, filepath.Join(launcherDir, "versions"), "")
				}
				l.OKInto("linked launcher profile "+display, gameDir, "")
				if rep.Sync != nil {
					rep.Sync.print(l)
					return
				}
				if _, err := os.Stat(build.StatePath(gameDir)); err != nil {
					l.Nudge("Download and build before launching", "shulker install")
				}
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher directory (default: the official launcher's .minecraft folder)")
	cmd.Flags().BoolVar(&assumeClient, "assume-client", false, "link a client even when the source declares none, built from the mods and overrides both sides share")
	cmd.Flags().StringVar(&instanceName, "name", "", "profile name (default: the side's display name)")
	cmd.Flags().StringVar(&as, "as", "", "id for this instance, for -i (default: from its name)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().BoolVar(&force, "force", false, "repoint a profile that syncs from a different source")
	ls.register(cmd)
	return cmd
}

// findLauncherInstance is the registry row a launcher already has under a name,
// when its directory is still there.
func (a *app) findLauncherInstance(launcherName, launcherDir, name string) (config.Instance, bool) {
	instances, err := a.loadInstances()
	if err != nil {
		return config.Instance{}, false
	}
	for _, in := range instances {
		if in.Launcher != launcherName || in.Name != name || !sameDir(in.LauncherDir, launcherDir) {
			continue
		}
		if _, err := os.Stat(in.Dir); err == nil {
			return in, true
		}
	}
	return config.Instance{}, false
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

var unsafeKeyChars = regexp.MustCompile(`[^a-z0-9]+`)

func profileKey(name string) string {
	slug := strings.Trim(unsafeKeyChars.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		slug = "project"
	}
	return "shulker-" + slug
}

// linkSettings are the settings a `link` seeds an instance with: the manifest's defaults on a new
// instance, then a flag's value over them. On a relink only the flags land, because the settings
// block belongs to whoever edited it once it exists, and no sync rewrites it.
type linkSettings struct {
	noHooks     bool
	noPreLaunch bool
	noPostExit  bool
	noMarker    bool
	java        string
	wrapper     string
}

func (ls *linkSettings) register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&ls.noHooks, "no-hooks", false, "install neither hook: don't sync before a launch, don't record how a run ended")
	cmd.Flags().BoolVar(&ls.noPreLaunch, "no-pre-launch", false, "don't sync this instance before each launch")
	cmd.Flags().BoolVar(&ls.noPostExit, "no-post-exit", false, "don't record how each run ended")
	cmd.Flags().BoolVar(&ls.noMarker, "no-marker", false, "leave the marker mod out of this instance's builds")
	cmd.Flags().StringVar(&ls.java, "java", "", "absolute path to the Java this machine launches the instance with (default: shulker's managed runtime)")
	cmd.Flags().StringVar(&ls.wrapper, "wrapper", "", "command prefix for the launch command, such as gamemoderun; split on whitespace")
}

func (ls linkSettings) check() error {
	if ls.java != "" && !filepath.IsAbs(ls.java) {
		return out.Errorf("usage", "--java needs an absolute path: a launcher runs the instance with almost no environment, and nothing searches PATH for it")
	}
	return nil
}

// set reports whether this link asks for any setting at all, which is what a mode with no instance
// file to record them in has to refuse.
func (ls linkSettings) set() bool {
	return ls.noHooks || ls.noPreLaunch || ls.noPostExit || ls.noMarker || ls.java != "" || ls.wrapper != ""
}

// save writes what a directory syncs from, and the settings this link decided.
func (ls linkSettings) save(dir, source, ref, side string, assumeClient bool, m *manifest.Manifest) error {
	f, fresh, err := loadIntent(dir, source, ref, side, assumeClient)
	if err != nil {
		return err
	}
	if fresh {
		h := m.ClientHooks()
		if h.PreLaunch != nil {
			f.Settings.Hooks.PreLaunch = h.PreLaunch
		}
		if h.PostExit != nil {
			f.Settings.Hooks.PostExit = h.PostExit
		}
		if m.Marker != nil {
			f.Settings.Marker = m.Marker
		}
	}
	if ls.noHooks || ls.noPreLaunch {
		f.Settings.Hooks.PreLaunch = instance.Off()
	}
	if ls.noHooks || ls.noPostExit {
		f.Settings.Hooks.PostExit = instance.Off()
	}
	if ls.noMarker {
		f.Settings.Marker = instance.Off()
	}
	if ls.java != "" {
		f.Settings.Java = ls.java
	}
	if w := strings.Fields(ls.wrapper); len(w) > 0 {
		f.Settings.Wrapper = w
	}
	return f.Save(dir)
}
