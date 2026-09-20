package cli

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

type linkReport struct {
	Launcher    string      `json:"launcher"`
	LauncherDir string      `json:"launcherDir"`
	Profile     string      `json:"profile"`
	Name        string      `json:"name"`
	VersionID   string      `json:"versionId"`
	GameDir     string      `json:"gameDir"`
	Source      string      `json:"source"`
	Ref         string      `json:"ref,omitempty"`
	Modpack     string      `json:"modpack"`
	Sync        *syncResult `json:"sync"`
}

func (a *app) linkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Point a launcher at this project's client build",
	}
	cmd.AddCommand(a.linkShulkerCmd(), a.linkMojangCmd(), a.linkPrismCmd(), a.linkMultiMCCmd(), a.linkATLauncherCmd(), a.linkGDLauncherCmd())
	return cmd
}

func (a *app) linkMojangCmd() *cobra.Command {
	var launcherDir, instanceName, ref, as string
	var force bool
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
			if !p.Manifest.HasSide("client") {
				a.printer.Warn("%s", noClientPack)
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
			display := p.Manifest.DisplayName("client")
			if instanceName != "" {
				display = instanceName
			}
			key := profileKey(display)
			if prev, ok := a.findLauncherInstance("mojang", launcherDir, display); ok && prev.Source != src.name && !force {
				return out.Errorf("instance-exists", "profile %q already syncs from %s; pass --name to create a second profile, or --force to repoint this one", display, prev.Source)
			}
			gameDir, err := filepath.Abs(filepath.Join(launcherDir, "shulker", strings.TrimPrefix(key, "shulker-")))
			if err != nil {
				return err
			}
			if err := a.checkID(as, gameDir); err != nil {
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
			id, err := a.linkID(as, display, gameDir)
			if err != nil {
				return err
			}
			inst, err := a.linkProject(gameDir, id, display, ref, src)
			if err != nil {
				return err
			}
			if err := v.WriteProfile(launcher.Profile{Key: key, Name: display, VersionID: versionID, GameDir: gameDir}); err != nil {
				return err
			}
			if err := ls.save(gameDir, src.name, ref, "client", false, p.Manifest); err != nil {
				return err
			}
			row := config.Instance{ID: id, Launcher: "mojang", LauncherDir: launcherDir, Name: display, Dir: gameDir, Source: src.name}
			a.registerInstance(row)
			synced, err := a.syncInPlace(cmd, inst, "client", syncRequest{})
			if err != nil {
				return err
			}
			// The shim records the Java it falls back to, which the build just resolved.
			a.reconcileOrWarn(row)
			rep := linkReport{
				Launcher:    "mojang",
				LauncherDir: launcherDir,
				Profile:     key,
				Name:        display,
				VersionID:   versionID,
				GameDir:     gameDir,
				Source:      src.name,
				Ref:         ref,
				Modpack:     modpackKey(inst.Manifest, src.name),
				Sync:        &synced,
			}
			return a.printer.Emit(rep, func(l *out.Lines) {
				if p.Lock.Loader.Type != "" {
					l.OKInto("installed "+versionID, filepath.Join(launcherDir, "versions"), "")
				}
				l.OKInto("linked launcher profile "+display, gameDir, "")
				l.Tree(out.Row{Text: "follows " + rep.Modpack + " from " + rep.Source})
				synced.print(l)
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher directory (default: the official launcher's .minecraft folder)")
	cmd.Flags().StringVar(&instanceName, "name", "", "profile name (default: the side's display name)")
	cmd.Flags().StringVar(&as, "as", "", "id for this instance, for -i (default: from its name)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().BoolVar(&force, "force", false, "repoint a profile that syncs from a different source")
	ls.register(cmd)
	return cmd
}

// noClientPack is what a link says when the pack it is about to follow declares no client. The
// instance has a client block of its own, so the build goes ahead on the pack's shared mods and
// overrides; without the line a server-only pack would just give a near-empty instance.
const noClientPack = "the source declares no client; building one from its shared mods and overrides"

// linkID is the id a link gives a game directory: the one its registry row already holds, else
// --as or a slug of the display name, made unique across the registry. A new instance takes it as
// its manifest's name too, so the id a player types and the project they play are the same thing.
func (a *app) linkID(as, display, dir string) (string, error) {
	instances, err := a.loadInstances()
	if err != nil {
		return "", err
	}
	if as == "" {
		if i, ok := config.FindInstance(instances, dir); ok && instances[i].ID != "" {
			return instances[i].ID, nil
		}
	}
	return uniqueID(instances, as, display, dir), nil
}

// linkInstance is the half of a link every instanced launcher shares: the project its game
// directory becomes, the settings this link seeds it with, the registry row that finds it again,
// and the build that leaves it ready to play.
func (a *app) linkInstance(cmd *cobra.Command, row config.Instance, as, ref string, src *syncSource, ls linkSettings) (*project.Project, syncResult, error) {
	if err := a.checkID(as, row.Dir); err != nil {
		return nil, syncResult{}, err
	}
	id, err := a.linkID(as, row.Name, row.Dir)
	if err != nil {
		return nil, syncResult{}, err
	}
	p, err := a.linkProject(row.Dir, id, row.Name, ref, src)
	if err != nil {
		return nil, syncResult{}, err
	}
	if err := ls.save(row.Dir, src.name, ref, "client", false, src.project.Manifest); err != nil {
		return nil, syncResult{}, err
	}
	row.ID = id
	a.registerInstance(row)
	synced, err := a.syncInPlace(cmd, p, "client", syncRequest{})
	return p, synced, err
}

// linkProject is the project a link leaves in the game directory: the minimal manifest ADR 0001
// calls an instance, following the link's source as a modpack and building where it stands. A
// project already there is adopted, never replaced, so a relink keeps whatever the player added
// on top of the pack.
func (a *app) linkProject(gameDir, id, display, ref string, src *syncSource) (*project.Project, error) {
	p, err := a.openProjectAt(gameDir)
	if errors.Is(err, project.ErrNoManifest) {
		return newInstance(gameDir, id, display, ref, src)
	}
	if err != nil {
		return nil, err
	}
	if p.Lock == nil {
		p.Lock = lock.New()
	}
	changed := false
	// A project that builds elsewhere is not yet an instance; linking it here is what makes it one.
	if !p.Manifest.InPlace("client") {
		if p.Manifest.Client == nil {
			p.Manifest.Client = &manifest.Client{Name: display}
		}
		p.Manifest.Client.Build, changed = ".", true
	}
	// Only --force reaches here with a source the instance doesn't follow yet. Repointing that one
	// modpack entry leaves the player's own requires, and the lock holding them, where they are.
	key := modpackKey(p.Manifest, src.name)
	if key == "" {
		key = src.project.Manifest.Name
	}
	entry, held := p.Manifest.Requires[key]
	if held && entry.Kind() != manifest.TypeModpack {
		return nil, manifest.KeyTaken(key, entry.Kind(), manifest.TypeModpack)
	}
	if entry.Source != src.name || entry.Ref != ref {
		entry.Source, entry.Ref = src.name, ref
		p.Manifest.Requires[key], changed = entry, true
	}
	if !changed {
		return p, nil
	}
	return p, p.SaveManifest()
}

// newInstance writes the instance manifest. It pins no platform and lists no feature: the pack is
// locked, so the relock inherits all of that, and a pack that moves platform is followed rather
// than fought. What it does copy is the two preferences only the pack's author can weigh, its
// history retention and whether builds carry the marker mod; from then on both are the player's.
func newInstance(gameDir, id, display, ref string, src *syncSource) (*project.Project, error) {
	pack := src.project.Manifest
	m := &manifest.Manifest{
		Schema:   manifest.SchemaURL,
		Name:     id,
		Requires: map[string]manifest.Require{pack.Name: {Source: src.name, Ref: ref}},
		Client:   &manifest.Client{Name: display, Build: "."},
	}
	if pack.History != nil {
		keep := *pack.History
		m.History = &keep
	}
	if pack.Marker != nil {
		marker := *pack.Marker
		m.Marker = &marker
	}
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		return nil, err
	}
	p := &project.Project{Dir: gameDir, Manifest: m, Lock: lock.New()}
	return p, p.SaveManifest()
}

// modpackKey is the key the instance follows the link's source under: the source manifest's name
// when this link wrote the entry, and whatever an earlier link or a hand edit chose when it didn't.
// Empty where no single entry is the link's: with several packs required, none of them is the one.
func modpackKey(m *manifest.Manifest, source string) string {
	keys := slices.Sorted(maps.Keys(m.Modpacks()))
	for _, key := range keys {
		if m.Requires[key].Source == source {
			return key
		}
	}
	if len(keys) == 1 {
		return keys[0]
	}
	return ""
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

// linkSettings are the settings a `link` seeds an instance with: the manifest's hook defaults on a
// new instance, then a flag's value over them. On a relink only the flags land, because the
// settings block belongs to whoever edited it once it exists, and no sync rewrites it. The marker
// is never seeded: an absent settings.marker defers to the manifest, so only --no-marker and
// --with-marker write one.
type linkSettings struct {
	noHooks     bool
	noPreLaunch bool
	noPostExit  bool
	noMarker    bool
	withMarker  bool
	java        string
	wrapper     string
}

func (ls *linkSettings) register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&ls.noHooks, "no-hooks", false, "install neither hook: don't sync before a launch, don't record how a run ended")
	cmd.Flags().BoolVar(&ls.noPreLaunch, "no-pre-launch", false, "don't sync this instance before each launch")
	cmd.Flags().BoolVar(&ls.noPostExit, "no-post-exit", false, "don't record how each run ended")
	cmd.Flags().BoolVar(&ls.noMarker, "no-marker", false, "leave the marker mod out of this instance's builds")
	cmd.Flags().BoolVar(&ls.withMarker, "with-marker", false, "include the marker mod in this instance's builds, over a manifest that leaves it out")
	cmd.Flags().StringVar(&ls.java, "java", "", "absolute path to the Java this machine launches the instance with (default: shulker's managed runtime)")
	cmd.Flags().StringVar(&ls.wrapper, "wrapper", "", "command prefix for the launch command, such as gamemoderun; split on whitespace")
}

func (ls linkSettings) check() error {
	if ls.noMarker && ls.withMarker {
		return out.Errorf("usage", "--no-marker and --with-marker ask for opposite things")
	}
	if ls.java != "" && !filepath.IsAbs(ls.java) {
		return out.Errorf("usage", "--java needs an absolute path: a launcher runs the instance with almost no environment, and nothing searches PATH for it")
	}
	return nil
}

// set reports whether this link asks for any setting at all, which is what a mode with no instance
// file to record them in has to refuse.
func (ls linkSettings) set() bool {
	return ls.noHooks || ls.noPreLaunch || ls.noPostExit || ls.noMarker || ls.withMarker || ls.java != "" || ls.wrapper != ""
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
	if ls.withMarker {
		f.Settings.Marker = instance.On()
	}
	if ls.java != "" {
		f.Settings.Java = ls.java
	}
	if w := strings.Fields(ls.wrapper); len(w) > 0 {
		f.Settings.Wrapper = w
	}
	return f.Save(dir)
}
