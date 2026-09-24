package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/project"
)

// linkReport is what every link reports: the launcher and where it keeps the instance, the
// instance's id and name, what it follows, and the build that left it ready to play.
type linkReport struct {
	Launcher    string      `json:"launcher"`
	LauncherDir string      `json:"launcherDir,omitempty"`
	ID          string      `json:"id"`
	Instance    string      `json:"instance"`
	InstanceDir string      `json:"instanceDir"`
	Name        string      `json:"name"`
	VersionID   string      `json:"versionId,omitempty"`
	GameDir     string      `json:"gameDir"`
	Command     string      `json:"command,omitempty"`
	Created     bool        `json:"created"`
	Source      string      `json:"source"`
	Ref         string      `json:"ref,omitempty"`
	Path        string      `json:"path,omitempty"`
	Modpack     string      `json:"modpack"`
	Sync        *syncResult `json:"sync"`
	noun        string
	shown       string
	versionDir  string
	rows        []out.Row
}

func (r *linkReport) print(l *out.Lines) {
	if r.VersionID != "" {
		l.OKInto("installed "+r.VersionID, r.versionDir, "")
	}
	verb := "created"
	if !r.Created {
		verb = "updated"
	}
	l.OKInto(verb+" "+r.noun+" "+r.shown, r.InstanceDir, "")
	l.Tree(r.rows...)
	r.Sync.print(l)
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
	for _, e := range launcher.All {
		cmd.AddCommand(a.launcherLinkCmd(e))
	}
	return cmd
}

// launcherLink is the flags a link takes, the same for every launcher but for the ones its usage
// block leaves out.
type launcherLink struct {
	launcherDir, instanceName, as string
	at                            pack.At
	force                         bool
	ff                            featureFlags
	ls                            linkSettings
}

func (k *launcherLink) register(cmd *cobra.Command, e *launcher.Entry) {
	if e.HasDir() {
		cmd.Flags().StringVar(&k.launcherDir, "launcher-dir", "", e.Usage.Dir)
	}
	if e.Usage.Names {
		cmd.Flags().StringVar(&k.instanceName, "name", "", e.Usage.Noun+" name (default: the side's display name)")
	}
	as := e.Usage.As
	if as == "" {
		as = "id for this instance, for -i (default: from its name)"
	}
	cmd.Flags().StringVar(&k.as, "as", "", as)
	cmd.Flags().StringVar(&k.at.Ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().StringVar(&k.at.Path, "path", "", "folder of a git source's repository that holds its shulker.json (default: the root)")
	cmd.Flags().BoolVar(&k.force, "force", false, e.Usage.Force)
	k.ff.register(cmd, "for this instance")
	k.ls.register(cmd)
}

func (k *launcherLink) hasFeatures() bool { return len(k.ff.with)+len(k.ff.without) > 0 }

func (k *launcherLink) display(p *project.Project) string {
	if k.instanceName != "" {
		return k.instanceName
	}
	return p.Manifest.DisplayName("client")
}

// launcherLinkCmd is `link <launcher>` for one entry: the shared flags and flow around the
// launcher's own placement and link step.
func (a *app) launcherLinkCmd(e *launcher.Entry) *cobra.Command {
	var k launcherLink
	cmd := &cobra.Command{
		Use:         e.Name + " [project-dir | git-url | manifest-url]",
		Annotations: acts(),
		Aliases:     e.Usage.Aliases,
		Short:       e.Usage.Short,
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := a.linkInto(cmd, args, e, &k)
			if err != nil {
				return err
			}
			return a.printer.Emit(rep, rep.print)
		},
	}
	k.register(cmd, e)
	return cmd
}

// linkInto links the source into one launcher: the launcher's own placement, checks and link step
// around the project, registry row and build every link shares.
func (a *app) linkInto(cmd *cobra.Command, args []string, e *launcher.Entry, k *launcherLink) (*linkReport, error) {
	if e.HasDir() && e.DefaultDir == nil && k.launcherDir == "" {
		if err := k.ls.check(); err != nil {
			return nil, err
		}
		dir, err := a.askLauncherDir(e)
		if err != nil {
			return nil, err
		}
		k.launcherDir = dir
	}
	src, l, err := a.startLauncherLink(cmd, args, k, e)
	if err != nil {
		return nil, err
	}
	p := src.project
	dir := k.launcherDir
	if e.HasDir() {
		if dir, err = e.Locate(dir); err != nil {
			return nil, err
		}
	}
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	instances, err := a.loadInstances()
	if err != nil {
		return nil, err
	}
	display := k.display(p)
	req := &launcher.Link{
		LauncherDir:   dir,
		Name:          display,
		ID:            k.as,
		Minecraft:     p.Lock.Minecraft,
		LoaderType:    p.Lock.Loader.Type,
		LoaderVersion: p.Lock.Loader.Version,
		Force:         k.force,
		Registry:      instances,
		Cache:         d.cache,
		Fetch:         d.fetch,
		MetaURL:       a.metaURL(d, e),
		Versions:      clientVersions{a: a, p: p, l: l},
		Log:           a.progress,
		Warn:          a.printer.Warn,
	}
	place, err := e.Place(req)
	if err != nil {
		return nil, err
	}
	second := "--name"
	if !e.Usage.Names {
		second = "--as"
	}
	if err := checkAdopt(place.GameDir, src, e.Usage.Noun, display, second, k.force); err != nil {
		return nil, err
	}
	if err := a.checkID(k.as, place.GameDir); err != nil {
		return nil, err
	}
	if err := a.refuseForeignInstance(k, e, dir, place.GameDir, display); err != nil {
		return nil, err
	}
	res, err := e.Link(cmd.Context(), req, place)
	if err != nil {
		return nil, err
	}
	if k.hasFeatures() {
		if err := a.saveInstanceFeatures(res.GameDir, k.ff); err != nil {
			return nil, err
		}
	}
	row := config.Instance{Launcher: e.Name, Name: display, Dir: res.GameDir, Source: src.name}
	if e.HasDir() {
		row.LauncherDir = dir
	}
	inst, synced, err := a.linkInstance(cmd, row, place.ID, src, k.ls)
	if err != nil {
		return nil, err
	}
	if e.Slot != nil && e.Slot.UsesShim {
		// The shim records the Java it falls back to, which the build just resolved.
		row.ID = inst.Manifest.Name
		a.reconcileOrWarn(row)
	}
	rep := &linkReport{
		Launcher:    e.Name,
		LauncherDir: row.LauncherDir,
		ID:          inst.Manifest.Name,
		Instance:    res.Key,
		InstanceDir: res.Dir,
		Name:        display,
		VersionID:   res.Version,
		GameDir:     res.GameDir,
		Created:     res.Created,
		Source:      src.name,
		Ref:         src.Ref,
		Path:        src.Path,
		Modpack:     project.ModpackKey(inst.Manifest, src.name),
		Sync:        &synced,
		noun:        e.Usage.Noun,
		shown:       display,
		versionDir:  res.VersionDir,
	}
	// A launcher that shows no name of its own is addressed by the id, so that is what the line
	// names.
	if !e.Usage.Names {
		rep.shown = rep.ID
	}
	rep.rows = follows(rep.Modpack, rep.Source, rep.Path)
	if e.Slot != nil {
		rep.Command = launcher.SlotCommand(e.Name, res.GameDir, launcher.HookPreLaunch)
		rep.rows = append(rep.rows, out.Row{Text: "the launcher syncs this instance before each launch"})
	}
	if k.hasFeatures() {
		rep.rows = append(rep.rows, out.Row{Text: "feature choices saved; change them with `shulker feature on|off <feature> --into " + launcher.CommandArg(res.GameDir) + "`"})
	}
	if note := e.AfterNote(res); note != "" {
		rep.rows = append(rep.rows, out.Row{Text: note})
	}
	return rep, nil
}

// metaURL is where a link reads a launcher's own metadata: the entry's service, unless the run
// points that service at a fake.
func (a *app) metaURL(d *deps, e *launcher.Entry) string {
	if url, ok := d.metaURLs[e.MetaURL]; ok {
		return url
	}
	return e.MetaURL
}

// openLinkSource is what every link does first: check the settings, fetch the source, refuse a loader
// this build doesn't know, and warn when the pack declares no client.
func (a *app) openLinkSource(cmd *cobra.Command, args []string, at pack.At, ls linkSettings) (*syncSource, loader.Loader, error) {
	if err := ls.check(); err != nil {
		return nil, loader.Loader{}, err
	}
	src, err := a.linkFrom(cmd, args, at)
	if err != nil {
		return nil, loader.Loader{}, err
	}
	p := src.project
	var l loader.Loader
	if p.Lock.Loader.Type != "" {
		if l, err = loader.Require(p.Lock.Loader.Type); err != nil {
			return nil, loader.Loader{}, err
		}
	}
	if !p.Manifest.HasSide("client") {
		a.printer.Warn("%s", noClientPack)
	}
	return src, l, nil
}

// startLauncherLink is openLinkSource plus the feature choices checked against the build, and
// --launcher-dir made absolute, filled from the launcher's default when it is empty. A launcher with
// no directory of its own works in shulker's instances root.
func (a *app) startLauncherLink(cmd *cobra.Command, args []string, k *launcherLink, e *launcher.Entry) (*syncSource, loader.Loader, error) {
	src, l, err := a.openLinkSource(cmd, args, k.at, k.ls)
	if err != nil {
		return nil, l, err
	}
	if k.hasFeatures() {
		b, err := a.builder(cmd.Context(), src.project)
		if err != nil {
			return nil, l, err
		}
		if err := k.ff.check(b); err != nil {
			return nil, l, err
		}
	}
	switch {
	case !e.HasDir():
		r, err := a.roots()
		if err != nil {
			return nil, l, err
		}
		k.launcherDir = r.Instances
	case k.launcherDir == "":
		if k.launcherDir, err = e.DefaultDir(); err != nil {
			return nil, l, err
		}
	}
	if k.launcherDir, err = filepath.Abs(k.launcherDir); err != nil {
		return nil, l, err
	}
	return src, l, nil
}

// askLauncherDir asks where a launcher with no default directory is, and off a terminal requires
// the flag.
func (a *app) askLauncherDir(e *launcher.Entry) (string, error) {
	required := out.Errorf("launcher-dir-required", "%s", e.Usage.NoDefault)
	required.Help = "pass --launcher-dir with " + e.Usage.DirHint
	if !a.canPick() {
		return "", required
	}
	dir, err := a.askText("Where is "+e.Title+" installed?", e.Usage.DirHint, "")
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", required
	}
	// A shell expands ~ in --launcher-dir, so the answer to the same question does too.
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, rest)
	}
	return dir, nil
}

// refuseForeignInstance refuses to link over an instance shulker didn't link, unless --force. An
// instance shulker linked is a project in its own game directory, and stays one after an unlink;
// anything else in a folder the launcher names after the instance is the player's own.
func (a *app) refuseForeignInstance(k *launcherLink, e *launcher.Entry, launcherDir, gameDir, display string) error {
	if k.force || !e.NamesFolder {
		return nil
	}
	_, _, inPlace, err := a.inPlaceProject(gameDir)
	if err != nil || inPlace {
		return err
	}
	slots, found, err := launcher.ReadSlots(e, config.Instance{Dir: gameDir, LauncherDir: launcherDir})
	if err != nil {
		return err
	}
	if found && !launcher.IsShulkerSlot(slots.PreLaunch) {
		err := out.Errorf("instance-exists", "%s already has an %s %q that shulker didn't link", e.Title, e.Usage.Noun, display)
		err.Help = "pass --name to create a second " + e.Usage.Noun + ", or --force to link this one"
		return err
	}
	return nil
}

// clientVersions is what a link hands a launcher to install the locked platform: the versions the
// project's resolver and loader row can fetch or build.
type clientVersions struct {
	a *app
	p *project.Project
	l loader.Loader
}

func (v clientVersions) Vanilla(ctx context.Context) (json.RawMessage, error) {
	d, err := v.a.deps()
	if err != nil {
		return nil, err
	}
	return d.meta.Piston.Version(ctx, v.p.Lock.Minecraft)
}

func (v clientVersions) HasInstaller() bool { return v.l.HasInstaller() }

func (v clientVersions) LoaderProfile(ctx context.Context) (json.RawMessage, error) {
	d, err := v.a.deps()
	if err != nil {
		return nil, err
	}
	return d.meta.LoaderProfile(ctx, v.p.Lock.Loader, v.p.Lock.Minecraft)
}

func (v clientVersions) InstallClient(ctx context.Context, launcherDir string) (string, error) {
	src, err := v.a.storeSources(v.p)
	if err != nil {
		return "", err
	}
	id, err := game.InstallLoader(ctx, launcherDir, v.p.Lock, src)
	if err != nil {
		return "", v.a.keepInstallerOutput(err)
	}
	return id, nil
}

func (v clientVersions) InstallerVersion(ctx context.Context) (json.RawMessage, error) {
	d, err := v.a.deps()
	if err != nil {
		return nil, err
	}
	raw, changed, err := v.l.InstallerVersion(ctx, d.loaders, v.p.Lock)
	return raw, v.p.SaveIfChanged(changed, err)
}

// linkAsked is a bare link at a terminal: it asks which launcher, then runs that launcher's own
// link as if it had been named, so everything after the question is the named command's.
func (a *app) linkAsked(cmd *cobra.Command) error {
	var choices []out.Choice
	for _, e := range launcher.All {
		choices = append(choices, out.Choice{Label: e.Title, Value: e.Name})
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

// linkInstance is the half of a link every instanced launcher shares: the project its game
// directory becomes, the settings this link seeds it with, the registry row that finds it again,
// and the build that leaves it ready to play.
func (a *app) linkInstance(cmd *cobra.Command, row config.Instance, as string, src *syncSource, ls linkSettings) (*project.Project, syncResult, error) {
	if err := a.checkID(as, row.Dir); err != nil {
		return nil, syncResult{}, err
	}
	instances, err := a.loadInstances()
	if err != nil {
		return nil, syncResult{}, err
	}
	id := config.InstanceID(instances, as, row.Name, row.Dir)
	p, linked, err := a.linkProject(row.Dir, id, row.Name, src)
	if err != nil {
		return nil, syncResult{}, err
	}
	if err := ls.save(row.Dir, src.name, src.At, "client", false, src.project.Manifest); err != nil {
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
func (a *app) linkProject(gameDir, id, display string, src *syncSource) (p *project.Project, linked string, err error) {
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
		p, err := newInstance(gameDir, id, display, src)
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
	key := project.ModpackKey(p.Manifest, src.name)
	if key == "" {
		key = src.project.Manifest.Name
	}
	entry, held := p.Manifest.Requires[key]
	if held && entry.Kind() != manifest.TypeModpack {
		return nil, "", manifest.KeyTaken(key, entry.Kind(), manifest.TypeModpack)
	}
	if entry.Source != src.name || entry.Ref != src.Ref || entry.Path != src.Path {
		entry.Source, entry.Ref, entry.Path = src.name, src.Ref, src.Path
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
func newInstance(gameDir, id, display string, src *syncSource) (*project.Project, error) {
	pack := src.project.Manifest
	m := &manifest.Manifest{
		Schema:   manifest.SchemaURL,
		Name:     id,
		Requires: map[string]manifest.Require{pack.Name: {Source: src.name, Ref: src.Ref, Path: src.Path}},
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
	key := project.ModpackKey(m, source)
	if key == "" || (m.Requires[key].Source == source && m.Requires[key].Path == src.Path) {
		return nil
	}
	e := out.Errorf("instance-exists", "%s %q already follows %s from %s", noun, name, key, m.Requires[key].Source)
	e.Help = fmt.Sprintf("pass %s to create a second %s, or --force to repoint the modpack it follows", second, noun)
	return e
}

// linkSource is the project a link command works from: the argument when there
// is one, else the project in the current directory.
func (a *app) linkSource(ctx context.Context, args []string, at pack.At) (*syncSource, error) {
	if len(args) == 1 {
		return a.openSource(ctx, args[0], at)
	}
	if at.Ref != "" {
		return nil, out.Errorf("usage", "--ref needs a git source argument")
	}
	if at.Path != "" {
		return nil, out.Errorf("usage", "--path needs a git source argument")
	}
	return a.projectSource()
}

// linkFrom is linkSource for a link command: at a terminal, with nothing to follow, the link
// authors the instance itself.
func (a *app) linkFrom(cmd *cobra.Command, args []string, at pack.At) (*syncSource, error) {
	src, err := a.linkSource(cmd.Context(), args, at)
	if len(args) > 0 || !errors.Is(err, project.ErrNoManifest) || !a.canPick() {
		return src, err
	}
	return a.authorSource(cmd)
}

// follows is the row naming what an instance follows, which an authored instance has none of.
func follows(modpack, source, path string) []out.Row {
	if modpack == "" {
		return nil
	}
	if path != "" {
		source += ", path " + path
	}
	return []out.Row{{Text: "follows " + modpack + " from " + source}}
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
func (ls linkSettings) save(dir, source string, at pack.At, side string, assumeClient bool, m *manifest.Manifest) error {
	f, fresh, err := loadIntent(dir, source, at, side, assumeClient)
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
	lf, err := a.loadLocal(gameDir)
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
