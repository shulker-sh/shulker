package cli

import (
	"context"
	"errors"
	"fmt"
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
		Use:         "link",
		Annotations: decides(),
		Short:       "Point a launcher at this project's client build",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return unknownSubcommand(cmd, args[0])
			}
			if !a.canPick() {
				return cmd.Help()
			}
			a.logActing()
			return a.linkAsked(cmd)
		},
	}
	cmd.AddCommand(a.linkShulkerCmd(), a.linkMojangCmd(), a.linkPrismCmd(), a.linkMultiMCCmd(), a.linkATLauncherCmd(), a.linkGDLauncherCmd())
	return cmd
}

func (a *app) linkMojangCmd() *cobra.Command {
	var launcherDir, instanceName, ref, as string
	var force bool
	var ls linkSettings
	cmd := &cobra.Command{
		Use:         "mojang [project-dir | git-url | manifest-url]",
		Annotations: acts(),
		Aliases:     []string{"vanilla"},
		Short:       "Add a profile for the client build to the official launcher, installing its loader if it has one",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, l, err := a.openLinkSource(cmd, args, ref, ls)
			if err != nil {
				return err
			}
			p := src.project
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
				e := out.Errorf("launcher-not-found", "no Minecraft launcher directory at %s", launcherDir)
				e.Help = "run the launcher once or pass --launcher-dir"
				return e
			} else if err != nil {
				return err
			}
			display := p.Manifest.DisplayName("client")
			if instanceName != "" {
				display = instanceName
			}
			key := instanceKey(display)
			gameDir, err := filepath.Abs(filepath.Join(launcherDir, "shulker", strings.TrimPrefix(key, "shulker-")))
			if err != nil {
				return err
			}
			if err := checkAdopt(gameDir, src, "profile", display, "--name", force); err != nil {
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
			inst, linked, err := a.linkProject(gameDir, id, display, ref, src)
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
			synced, err := a.syncInPlace(cmd, inst, "client", syncRequest{linked: linked})
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
				l.Tree(follows(rep.Modpack, rep.Source)...)
				synced.print(l)
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "launcher directory (default: the official launcher's .minecraft folder)")
	cmd.Flags().StringVar(&instanceName, "name", "", "profile name (default: the side's display name)")
	cmd.Flags().StringVar(&as, "as", "", "id for this instance, for -i (default: from its name)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().BoolVar(&force, "force", false, "repoint the modpack a profile already follows")
	ls.register(cmd)
	return cmd
}

// linkAsked is a bare link at a terminal: it asks which launcher, then runs that launcher's own
// link as if it had been named, so everything after the question is the named command's.
func (a *app) linkAsked(cmd *cobra.Command) error {
	var choices []out.Choice
	for _, name := range []string{"mojang", "prism", "multimc", "atlauncher", "gdlauncher"} {
		choices = append(choices, out.Choice{Label: launcher.Title(name), Value: name})
	}
	name, err := a.ask("Which launcher?", choices)
	if err != nil {
		return err
	}
	sub, _, err := cmd.Find([]string{name})
	if err != nil {
		return err
	}
	sub.SetContext(cmd.Context())
	a.printer.Command = strings.TrimPrefix(sub.CommandPath(), "shulker ")
	return sub.RunE(sub, nil)
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
	p, linked, err := a.linkProject(row.Dir, id, row.Name, ref, src)
	if err != nil {
		return nil, syncResult{}, err
	}
	if err := ls.save(row.Dir, src.name, ref, "client", false, src.project.Manifest); err != nil {
		return nil, syncResult{}, err
	}
	row.ID, row.Source = id, src.name
	a.registerInstance(row)
	synced, err := a.syncInPlace(cmd, p, "client", syncRequest{linked: linked})
	return p, synced, err
}

// linkProject is the project a link leaves in the game directory: the minimal manifest ADR 0001
// calls an instance, following the link's source as a modpack and building where it stands. A
// project already there is adopted, never replaced, so a relink keeps whatever the player added
// on top of the pack. Its name is set to the id either way, since repair reads the id back from it.
func (a *app) linkProject(gameDir, id, display, ref string, src *syncSource) (p *project.Project, linked string, err error) {
	p, err = a.openProjectAt(gameDir)
	if src.isAuthor {
		if err == nil {
			return nil, "", authoredOver(gameDir)
		}
		if !errors.Is(err, project.ErrNoManifest) {
			return nil, "", err
		}
		p, err := authorInstance(gameDir, id, display, src)
		return p, "", err
	}
	if errors.Is(err, project.ErrNoManifest) {
		p, err := newInstance(gameDir, id, display, ref, src)
		return p, src.project.Manifest.Name, err
	}
	if err != nil {
		return nil, "", err
	}
	if p.Lock == nil {
		p.Lock = lock.New()
	}
	changed := false
	if p.Manifest.Name != id {
		p.Manifest.Name, changed = id, true
	}
	// A project that builds elsewhere is not yet an instance; linking it here is what makes it one.
	if !p.Manifest.BuildsInPlace("client") {
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
		return nil, "", manifest.KeyTaken(key, entry.Kind(), manifest.TypeModpack)
	}
	if entry.Source != src.name || entry.Ref != ref {
		entry.Source, entry.Ref = src.name, ref
		p.Manifest.Requires[key], changed = entry, true
		linked = key
	}
	if !changed {
		return p, linked, nil
	}
	return p, linked, p.SaveManifest()
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

// authorInstance writes the project the link's answers describe into the game directory, which
// from then on is both the instance and the project it builds. It follows nothing, so it syncs from
// itself, and that is the source its registry row records.
func authorInstance(gameDir, id, display string, src *syncSource) (*project.Project, error) {
	m := *src.project.Manifest
	client := *m.Client
	m.Name, client.Name, client.Build = id, display, "."
	m.Client = &client
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		return nil, err
	}
	p := &project.Project{Dir: gameDir, Manifest: &m, Lock: src.project.Lock}
	if err := p.SaveManifest(); err != nil {
		return nil, err
	}
	if err := p.SaveLock(); err != nil {
		return nil, err
	}
	src.name, src.Source, src.Dir = gameDir, gameDir, gameDir
	return p, nil
}

// authoredOver refuses to author an instance where a project already stands: there is no pack to
// repoint, so --force has nothing to do either, and the answers would only overwrite a player's
// own instance.
func authoredOver(gameDir string) error {
	e := out.Errorf("instance-exists", "%s already holds a project", gameDir)
	e.Help = "give the new instance another name"
	return e
}

// checkAdopt guards the project a link is about to adopt. A game directory holding an in-place
// project keeps the pack it follows, so the source a link names has to agree with the manifest
// before the link may take it over, and --force is what repoints that one entry. The manifest is
// what decides, not the registry row: an unlink deletes the row and leaves the project whole.
func checkAdopt(gameDir string, src *syncSource, noun, name, second string, force bool) error {
	m, _, inPlace, err := inPlaceManifest(gameDir)
	if err != nil || !inPlace {
		return err
	}
	if src.isAuthor {
		return authoredOver(gameDir)
	}
	if force {
		return nil
	}
	source := src.name
	key := modpackKey(m, source)
	if key == "" || m.Requires[key].Source == source {
		return nil
	}
	e := out.Errorf("instance-exists", "%s %q already follows %s from %s", noun, name, key, m.Requires[key].Source)
	e.Help = fmt.Sprintf("pass %s to create a second %s, or --force to repoint the modpack it follows", second, noun)
	return e
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

// linkFrom is linkSource for a link command: at a terminal, with nothing to follow, the link
// authors the instance itself.
func (a *app) linkFrom(cmd *cobra.Command, args []string, ref string) (*syncSource, error) {
	src, err := a.linkSource(cmd.Context(), args, ref)
	if len(args) > 0 || !errors.Is(err, project.ErrNoManifest) || !a.canPick() {
		return src, err
	}
	return a.authorSource(cmd)
}

// follows is the row naming what an instance follows, which an authored instance has none of.
func follows(modpack, source string) []out.Row {
	if modpack == "" {
		return nil
	}
	return []out.Row{{Text: "follows " + modpack + " from " + source}}
}

var unsafeKeyChars = regexp.MustCompile(`[^a-z0-9]+`)

func instanceKey(name string) string {
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

// isSet reports whether this link asks for any setting at all, which is what a mode with no instance
// file to record them in has to refuse.
func (ls linkSettings) isSet() bool {
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
