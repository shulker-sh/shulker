package link

import (
	"context"
	"os"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/sync"
)

// Request is how one link differs from the next: where the launcher is, what the instance is
// called and known as, whether an instance already there is taken over, the feature choices and
// the settings the instance starts with.
type Request struct {
	// LauncherDir is the launcher directory as given or asked for; empty takes the launcher's
	// default, and shulker's own instances root for shulker.
	LauncherDir string
	// Name is the display name; empty takes the side's.
	Name string
	// ID is the id asked for; empty derives one from the name.
	ID    string
	Force bool
	// With and Without are the features turned on and off for this instance.
	With, Without []string
	Settings      Settings
	// Reason names the command for the build's history entry.
	Reason string
}

// Report is what every link reports: the launcher and where it keeps the instance, the instance's
// id and name, what it follows, and the build that left it ready to play. Sync is the CLI's to
// finish printing, so it is left out of the JSON here.
type Report struct {
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
	Sync        sync.Result `json:"-"`
	// Noun is what the launcher calls what a link makes, and Shown the name the headline uses:
	// the display name, or the id for a launcher that shows no name of its own.
	Noun  string `json:"-"`
	Shown string `json:"-"`
	// VersionDir is where the launcher installed VersionID.
	VersionDir string `json:"-"`
	// Note is the launcher's own closing line, empty for none.
	Note string `json:"-"`
	// FeaturesSaved reports that this link wrote feature choices into the instance.
	FeaturesSaved bool `json:"-"`
}

// NoClientPack is what a link says when the pack it is about to follow declares no client. The
// instance has a client block of its own, so the build goes ahead on the pack's shared mods and
// overrides; without the line a server-only pack would just give a near-empty instance.
const NoClientPack = "The source declares no client; building one from its shared mods and overrides"

// Into links the source into one launcher: the launcher's own placement, checks and link step
// around the project, registry row and build every link shares.
func Into(ctx context.Context, e *Env, entry *launcher.Entry, src *sync.Source, req Request) (*Report, error) {
	p := src.Project
	row, err := sync.LockedLoader(p)
	if err != nil {
		return nil, err
	}
	if !p.Manifest.HasSide("client") {
		e.Warn("%s", NoClientPack)
	}
	if len(req.With)+len(req.Without) > 0 {
		if err := checkFeatures(ctx, e, p, req); err != nil {
			return nil, err
		}
	}
	dir, err := entry.LinkDir(req.LauncherDir, e.Instances)
	if err != nil {
		return nil, err
	}
	if entry.HasDir() {
		if dir, err = entry.Locate(dir); err != nil {
			return nil, err
		}
	}
	instances, err := config.LoadInstances(e.Registry)
	if err != nil {
		return nil, err
	}
	display := req.Name
	if display == "" {
		display = p.Manifest.DisplayName("client")
	}
	lreq := &launcher.Link{
		LauncherDir:   dir,
		Name:          display,
		ID:            req.ID,
		Minecraft:     p.Lock.Minecraft,
		LoaderType:    p.Lock.Loader.Type,
		LoaderVersion: p.Lock.Loader.Version,
		Force:         req.Force,
		Registry:      instances,
		Cache:         e.Cache,
		Fetch:         e.Fetch,
		MetaURL:       e.metaURL(entry),
		Versions:      versions{e: e, p: p, row: row},
		Log:           e.Log,
		Warn:          e.Warn,
	}
	place, err := entry.Place(lreq)
	if err != nil {
		return nil, err
	}
	second := "--name"
	if !entry.Usage.Names {
		second = "--as"
	}
	if err := project.CheckAdopt(place.GameDir, src.ForLink(), entry.Usage.Noun, display, second, req.Force); err != nil {
		return nil, err
	}
	if err := checkID(instances, req.ID, place.GameDir); err != nil {
		return nil, err
	}
	if err := refuseForeign(entry, req.Force, dir, place.GameDir, display); err != nil {
		return nil, err
	}
	res, err := entry.Link(ctx, lreq, place)
	if err != nil {
		return nil, err
	}
	rep := &Report{
		Launcher:    entry.Name,
		Instance:    res.Key,
		InstanceDir: res.Dir,
		Name:        display,
		VersionID:   res.Version,
		GameDir:     res.GameDir,
		Created:     res.Created,
		Ref:         src.Ref,
		Path:        src.Path,
		Noun:        entry.Usage.Noun,
		Shown:       display,
		VersionDir:  res.VersionDir,
		Note:        entry.AfterNote(res),
	}
	if len(req.With)+len(req.Without) > 0 {
		if err := saveFeatures(e, res.GameDir, req.With, req.Without); err != nil {
			return nil, err
		}
		rep.FeaturesSaved = true
	}
	in := config.Instance{Launcher: entry.Name, Name: display, Dir: res.GameDir, Source: src.Name}
	if entry.HasDir() {
		in.LauncherDir = dir
	}
	inst, synced, err := instanceOf(ctx, e, in, place.ID, src, req)
	if err != nil {
		return nil, err
	}
	in.ID = inst.Manifest.Name
	if entry.Slot != nil && entry.Slot.UsesShim {
		// The shim records the Java it falls back to, which the build just resolved.
		sync.Reconcile(e.Env, in)
	}
	rep.LauncherDir, rep.ID, rep.Source, rep.Sync = in.LauncherDir, in.ID, src.Name, synced
	rep.Modpack = project.ModpackKey(inst.Manifest, src.Name)
	// A launcher that shows no name of its own is addressed by the id, so that is what the line
	// names.
	if !entry.Usage.Names {
		rep.Shown = rep.ID
	}
	if entry.Slot != nil {
		rep.Command = launcher.SlotCommand(entry.Name, res.GameDir, launcher.HookPreLaunch)
	}
	return rep, nil
}

// checkFeatures refuses a feature choice the build doesn't know before anything is written.
func checkFeatures(ctx context.Context, e *Env, p *project.Project, req Request) error {
	packs, err := resolve.Packs(ctx, e.Env.Env, p)
	if err != nil {
		return err
	}
	_, err = sync.FeatureOverrides(build.New(e.Env.Env, p, packs), req.With, req.Without, nil)
	return err
}

// checkID refuses an id that can't be a manifest name or that another instance already holds.
func checkID(instances []config.Instance, as, dir string) error {
	if as == "" {
		return nil
	}
	if !manifest.IsValidKey(as) {
		return out.Errorf("usage", "--as must be lowercase letters, digits, dots, dashes or underscores, up to 64 characters, not %q", as)
	}
	if heldBy, taken := config.IDTaken(instances, as, dir); taken {
		e := out.Errorf("instance-id-taken", "another instance is already called %s (%s)", as, heldBy)
		e.Help = "pass a different --as"
		return e
	}
	return nil
}

// refuseForeign refuses to link over an instance shulker didn't link, unless forced. An instance
// shulker linked is a project in its own game directory, and stays one after an unlink; anything
// else in a folder the launcher names after the instance is the player's own.
func refuseForeign(entry *launcher.Entry, force bool, launcherDir, gameDir, display string) error {
	if force {
		return nil
	}
	_, _, inPlace, err := sync.InPlaceProject(gameDir)
	if err != nil || inPlace {
		return err
	}
	ok, err := entry.LinkedByShulker(launcherDir, gameDir)
	if err != nil || ok {
		return err
	}
	foreign := out.Errorf("instance-exists", "%s already has an %s %q that shulker didn't link", entry.Title, entry.Usage.Noun, display)
	foreign.Help = "pass --name to create a second " + entry.Usage.Noun + ", or --force to link this one"
	return foreign
}

// saveFeatures writes the feature choices into the instance's local file. A launcher whose link
// step writes nothing has no game directory yet, so the file's is made here.
func saveFeatures(e *Env, gameDir string, with, without []string) error {
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		return err
	}
	lf, err := sync.LoadLocal(e.Env, gameDir)
	if err != nil {
		return err
	}
	for _, name := range with {
		lf.SetFeature(name, true)
	}
	for _, name := range without {
		lf.SetFeature(name, false)
	}
	return sync.SaveLocal(e.Env, lf, false)
}

// instanceOf is the half of a link every launcher shares: the project its game directory becomes,
// the settings this link seeds it with, the registry row that finds it again, and the build that
// leaves it ready to play.
func instanceOf(ctx context.Context, e *Env, row config.Instance, as string, src *sync.Source, req Request) (*project.Project, sync.Result, error) {
	instances, err := config.LoadInstances(e.Registry)
	if err != nil {
		return nil, sync.Result{}, err
	}
	id := config.InstanceID(instances, as, row.Name, row.Dir)
	from := src.ForLink()
	p, linked, err := project.LinkInstance(row.Dir, id, row.Name, from)
	if err != nil {
		return nil, sync.Result{}, err
	}
	src.Name = from.Name
	if err := req.Settings.save(row.Dir, src.Name, src.At, src.Project.Manifest); err != nil {
		return nil, sync.Result{}, err
	}
	row.ID, row.Source = id, src.Name
	register(e, row)
	res, err := sync.InPlace(ctx, e.Env, p, "client", sync.Request{Linked: linked, Reason: req.Reason})
	return p, res, err
}

// register puts the row in the registry, replacing the row for its directory when there is one,
// and sets the launcher's slots up for it. A registry that can't be written costs a warning, never
// the link: the instance is built and the launcher knows it, so what is lost is only how -i finds
// it.
func register(e *Env, in config.Instance) {
	_, err := config.UpdateInstances(e.Registry, func(instances []config.Instance) []config.Instance {
		if i, ok := config.FindInstance(instances, in.Dir); ok {
			if in.ID == "" {
				in.ID = instances[i].ID
			}
			instances[i] = in
			return instances
		}
		in.ID = config.InstanceID(instances, in.ID, in.Name, in.Dir)
		return append(instances, in)
	})
	if err != nil {
		e.Warn("registry not updated: %v", err)
	}
	sync.Reconcile(e.Env, in)
}
