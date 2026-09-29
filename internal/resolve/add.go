// Package resolve keeps a project's manifest and lock in step: adding, updating, pinning and removing
// mods and modpacks, and resolving the platform they run on.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/manual"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/provider"
)

// Resolver changes a project's manifest and lock together: it picks versions from the providers,
// downloads them into the cache and records them in the lock.
type Resolver struct {
	Dir       string
	Manifest  *manifest.Manifest
	Lock      *lock.Lock
	Providers provider.Providers
	Cache     *cache.Cache
	Fetch     *fetch.Client
	Packs     []*modpack.Loaded
	Meta      *Meta
	Log       func(format string, args ...any)
	// Note prints a list row about one entry, such as a dependency kept at the version the pack has.
	Note func(it out.Item)
	// Warnings are raised while resolving, for the command to print when it finishes.
	Warnings []string
	// FailFast stops Install at the first download that fails, rather than trying every file and
	// failing with them all.
	FailFast bool
	// EveryFetch keeps a step for each fetch of a group; see startGroup.
	EveryFetch bool
	// Progress starts a download bar for the named files.
	Progress func(verb string, files []out.Download) *out.Progress
	// AskMove is shown the deps-held refusal before an add without --with-deps gives up;
	// yes carries on as --with-deps would.
	AskMove func(held *out.Error) (bool, error)
	// AskUnlock is asked whether to unlock a modpack built for another Minecraft than the
	// project's, so its mods resolve here; nil keeps the modpack-mismatch refusal.
	AskUnlock func(key, minecraft string) (bool, error)
	// LockModpack locks a hosted modpack entry and puts it in the manifest under key, in place of
	// the modpack already there.
	LockModpack func(ctx context.Context, key string, entry manifest.Require) error

	// keepNewest has a mod another project already locks under the same jar id replace it when
	// its jar is newer, the way FML picks among files sharing a mod id. Import sets it: a pack's
	// file order is arbitrary.
	keepNewest bool
	// groupCached are the manual downloads the open group took from the cache, by file name.
	groupCached map[string]bool
	// advanced is set when the open group counted the fetch the next obtain makes.
	advanced bool
	// group is the live line a run of fetches draws on in place of a step each; see startGroup.
	group *out.Progress
	// held are the notes made while a group's live line was drawn, printed once it settles.
	held []out.Item
	// listings is the listing index, read once the first add asks it.
	listings *cache.ListingIndex
	// droppedSums are the sha512s of the files the last sweep found in the downloads folder, nil
	// before one runs.
	droppedSums map[string]bool
	// adopted are the pending mods install filled from downloads/.
	adopted []string
	// locked are the mods this resolver locked, which keeping one again doesn't report.
	locked map[string]bool
	// SkipPending goes on without the files waiting for a manual download rather than naming them
	// as missing: install leaves out the mods pending in the lock, and an import locks the mods it
	// can't download pending. A build leaves pending mods out.
	SkipPending bool
	// SkipManual is SkipPending for every locked file a provider won't serve that the cache lacks,
	// pending or not, once a download wait was skipped. A build leaves those out too.
	SkipManual bool
	// DownloadsIn is where manual downloads are taken from when it isn't Dir's downloads/: a new
	// project staged elsewhere reads the ones dropped into the folder it is for.
	DownloadsIn string
	// builds are the sides an import judges a pack's sides against, where they aren't the
	// manifest's own: a modpack's manifest declares none, and the project requiring it builds.
	builds []string
}

// readJar reads a jar's metadata the way the locked loader would.
func (r *Resolver) readJar(path, name string) (*jarmeta.Info, error) {
	return jarmeta.Read(path, name, loader.Running(r.Lock))
}

// jarKey is the requires key a mod jar takes: its mod id, or fallback when the id is missing or
// can't be a key, as Forge before 1.13 allows capitals and more in ids.
func jarKey(id, fallback string) string {
	if manifest.IsValidKey(id) {
		return id
	}
	return fallback
}

type AddOptions struct {
	Side     string
	Channel  string
	Pin      string
	Provider string
	As       string
	Type     string
	WithDeps bool
	// ResourcePack also places a datapack in resourcepacks/.
	ResourcePack bool
	// KeepFilename places a pack zip under its own file name when that isn't <key>.zip.
	KeepFilename bool
	// IsFromURL is set when the project was named by a Modrinth or CurseForge URL, which a --type
	// can then disagree with.
	IsFromURL bool
}

// groupFetch is one file a group may fetch: its name, and the kind of entry it is, empty for a
// file that is no kind shulker locks.
type groupFetch struct {
	name, kind string
}

// fetchAt is the file at a path in the game directory, its kind read from the folder it sits in.
func fetchAt(p string) groupFetch {
	f := groupFetch{name: path.Base(p)}
	switch {
	case strings.HasPrefix(p, "mods/"):
		f.kind = manifest.TypeMod
	case strings.HasPrefix(p, "resourcepacks/"):
		f.kind = manifest.TypeResourcePack
	case strings.HasPrefix(p, "shaderpacks/"):
		f.kind = manifest.TypeShader
	case packarchive.IsDatapackZip(p):
		f.kind = manifest.TypeDatapack
	}
	return f
}

// startGroup puts a loop's fetches on one live line that settles into a count of them, "✔ Fetched
// 60 mods", rather than a step each. The count names the kind every fetch shares, and files when
// they share none. end settles the line, or clears it for the error that follows when failed.
// Without a bar, as under --json, off a terminal for the steps or with --verbose, each fetch keeps
// its own step.
func (r *Resolver) startGroup(fetches []groupFetch) (end func(failed bool)) {
	if r.Progress == nil || r.EveryFetch || r.group != nil || len(fetches) == 0 {
		return func(bool) {}
	}
	downloads := make([]out.Download, len(fetches))
	for i, f := range fetches {
		downloads[i] = out.Download{Name: f.name}
	}
	one, many := "file", "files"
	if kind := fetches[0].kind; kind != "" && !slices.ContainsFunc(fetches, func(f groupFetch) bool { return f.kind != kind }) {
		one, many = manifest.TypeNouns(kind)
	}
	g := r.Progress("fetching", downloads).Counts(one, many)
	if g == nil {
		return func(bool) {}
	}
	r.group = g
	if r.Fetch != nil {
		r.Fetch.Progress = g.Bytes
	}
	return func(failed bool) {
		r.group = nil
		if r.Fetch != nil {
			r.Fetch.Progress = nil
		}
		defer r.afterGroup()
		if failed {
			g.Abort()
			return
		}
		g.Finish()
	}
}

// afterGroup prints what a group's live line held back: its notes and the files taken from the cache.
func (r *Resolver) afterGroup() {
	for _, it := range r.held {
		r.Note(it)
	}
	r.held = nil
	r.reportCached(len(r.groupCached))
	r.groupCached = nil
}

// tookFromCache notes a manual download taken from the cache: a step of its own, or, taken off the
// group's count of fetches when the group counted it, a count the group's line is followed by.
func (r *Resolver) tookFromCache(filename string, advanced bool) {
	if r.group != nil {
		if advanced {
			r.group.Retract()
		}
		if r.groupCached == nil {
			r.groupCached = map[string]bool{}
		}
		r.groupCached[filename] = true
		return
	}
	r.log("taking %s from the cache", filename)
}

func (r *Resolver) reportCached(n int) {
	if n > 0 {
		r.log("took %s from the cache", out.Count(n, "manual download", "manual downloads"))
	}
}

// fetching is one fetch of slug at version: a count on the group's line when one is open, else a
// step of its own.
func (r *Resolver) fetching(slug, version string) {
	if r.group != nil {
		r.group.File(slug)
		r.group.Advance()
		r.advanced = true
		return
	}
	r.log("fetching %s %s", slug, version)
}

func (r *Resolver) log(format string, args ...any) {
	// A step would draw over the group's live line, which stands for everything its loop does.
	if r.Log != nil && r.group == nil {
		r.Log(format, args...)
	}
}

func (r *Resolver) provider(name string) (provider.Provider, error) {
	if name != "" {
		return r.Providers.Get(name)
	}
	var reasons []*out.Error
	for _, n := range r.Manifest.ProviderOrder() {
		p, err := r.Providers.Get(n)
		if err == nil {
			return p, nil
		}
		reasons = append(reasons, out.AsError(err))
	}
	return nil, NoneAvailable("no manifest provider is available", reasons)
}

// NoneAvailable is the error for when every provider is unavailable: each reason is an item, and
// the first reason with a hint gives its help.
func NoneAvailable(message string, reasons []*out.Error) *out.Error {
	e := out.Errorf("provider-unavailable", "%s", message)
	for _, reason := range reasons {
		e.Items = append(e.Items, reason.Message)
		if e.Help == "" {
			e.Help = reason.Help
		}
	}
	return e
}

func (r *Resolver) lookup(ctx context.Context, slug, providerName, kind string) (provider.Provider, *provider.Project, error) {
	if providerName != "" {
		p, err := r.provider(providerName)
		if err != nil {
			return nil, nil, err
		}
		proj, err := p.Project(ctx, slug, kind)
		if errors.Is(err, provider.ErrNotFound) {
			return nil, nil, notFound(slug, []provider.Provider{p}, nil)
		}
		return p, proj, err
	}
	var missed []provider.Provider
	var skipped []*out.Error
	for _, n := range r.Manifest.ProviderOrder() {
		p, err := r.Providers.Get(n)
		if err != nil {
			skipped = append(skipped, out.AsError(err))
			continue
		}
		proj, err := p.Project(ctx, slug, kind)
		if errors.Is(err, provider.ErrNotFound) {
			missed = append(missed, p)
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		return p, proj, nil
	}
	if len(missed) == 0 {
		_, err := r.provider("")
		return nil, nil, err
	}
	return nil, nil, notFound(slug, missed, skipped)
}

// Missed reports whether an add that failed with err failed only because its name isn't there to
// add, before it changed anything since before: not found on every provider it asked, or with no
// version that fits. Anything else, a provider it couldn't reach included, may hide a name that
// exists, and fails the whole command.
func (r *Resolver) Missed(before Snapshot, err error) bool {
	e, ok := errors.AsType[*out.Error](err)
	if !ok || !r.Changes(before).IsEmpty() {
		return false
	}
	switch e.Code {
	case "mod-not-found":
		return len(e.Items) == 0
	case "no-compatible-version":
		return true
	}
	return false
}

// notFound names the providers that missed slug, with the first help a provider offers for a
// slug its lookup can miss.
func notFound(slug string, missed []provider.Provider, skipped []*out.Error) *out.Error {
	var names []string
	for _, p := range missed {
		names = append(names, p.Title())
	}
	e := out.Errorf("mod-not-found", "%s was not found on %s", slug, strings.Join(names, " or "))
	for _, reason := range skipped {
		e.Items = append(e.Items, "skipped: "+reason.Message)
	}
	if _, err := strconv.Atoi(slug); err != nil {
		for _, p := range missed {
			if e.Help = p.NotFoundHelp(); e.Help != "" {
				break
			}
		}
	}
	return e
}

func (r *Resolver) Add(ctx context.Context, slug string, opts AddOptions) error {
	if IsLocalPath(slug) || IsLocalFolder(slug) {
		return r.addFile(ctx, slug, opts)
	}
	explicit := opts.Provider != ""
	listed := opts.As
	if listed == "" {
		listed = slug
	}
	if prev, ok := r.Manifest.Mods()[listed]; !explicit && ok && prev.Provider != "" {
		opts.Provider = prev.Provider
	}
	p, proj, err := r.lookup(ctx, slug, opts.Provider, opts.Type)
	if err != nil {
		return err
	}
	kind, err := addKind(opts.Type, proj, slug)
	if err != nil && opts.IsFromURL {
		return out.Errorf("usage", "--type %s disagrees with the URL's %s", opts.Type, proj.Type)
	}
	if err != nil {
		return err
	}
	switch kind {
	case manifest.TypeMod:
	case manifest.TypeModpack:
		return r.addModpack(ctx, p, proj, opts)
	case manifest.TypeResourcePack, manifest.TypeShader, manifest.TypeDatapack:
		return r.addPack(ctx, p, proj, kind, opts)
	default:
		return out.Errorf("requires-unsupported", "%s is a %s, which shulker can't add yet", slug, kind)
	}
	// The lock, not the manifest: an instance following a modpack sets no loader of its own and
	// inherits the pack's into its lock, which is what its mods are resolved against.
	if r.Lock.Loader.Type == "" {
		return loaderRequired()
	}
	held := holdVersions(r.Lock)
	v, err := pickVersion(ctx, p, proj, r.queryFor(manifest.TypeMod, p.Name()), opts.Pin, opts.Channel)
	if err != nil {
		return err
	}
	from, id, prior, err := r.placeAnywhere(ctx, hosted{p, proj, v}, opts.As, "", opts.Side, opts.Channel, explicit, func(w string) { r.Warnings = append(r.Warnings, w) })
	p, proj, v = from.p, from.proj, from.v
	if err != nil {
		return err
	}
	previous := r.Manifest.Requires[id]
	switched := explicit && prior != nil && prior.Provider != p.Name()
	if switched {
		r.dropRequiredBy(id)
		if opts.Side == "" {
			opts.Side = previous.Side
		}
		if opts.Channel == "" {
			opts.Channel = previous.Channel
		}
	}
	if opts.Pin != "" {
		opts.Channel = r.pinnedChannel(id, v, opts.Channel)
	}
	r.settle(id, opts.Side, opts.Channel)
	visited := map[string]bool{proj.ID: true}
	if err := r.addDeps(ctx, p, v, id, opts.Channel, visited); err != nil {
		return err
	}
	if err := r.settleHeld(ctx, held, p, v, id, opts.Channel, opts.WithDeps); err != nil {
		return err
	}
	entry := manifest.Require{Side: opts.Side}
	if opts.Channel != "" && opts.Channel != "release" {
		entry.Channel = opts.Channel
	}
	if opts.Pin != "" {
		entry.Pin = opts.Pin
	}
	r.setSource(&entry, id, p, proj)
	r.Manifest.Requires[id] = entry
	if switched {
		r.pruneOrphans()
	}
	return nil
}

func loaderRequired() *out.Error {
	e := out.Errorf("loader-required", "mods need a loader")
	e.Help = fmt.Sprintf("pick one with `shulker set loader.type <%s>`", strings.Join(loader.Names(), "|"))
	return e
}

// versionQuery is how one kind asks a provider for its versions: the game and loader tags to
// filter by and the loader Newest prefers.
type versionQuery struct {
	kind   string
	game   string
	tags   []string
	loader string
	// untagged has a version that names no loader at all fit too, as a CurseForge modpack file
	// often does: RLCraft 2.9.3 is tagged 1.12.2 alone.
	untagged bool
}

func (r *Resolver) queryFor(kind, providerName string) versionQuery {
	switch {
	case kind == manifest.TypeModpack:
		game, loaderName := r.modpackPlatform()
		return versionQuery{kind: kind, game: game, tags: modpackLoaders(loaderName), loader: loaderName, untagged: true}
	case manifest.IsPackKind(kind):
		q := versionQuery{kind: kind, game: r.Lock.Minecraft}
		if p, ok := r.Providers[providerName]; ok {
			q.tags = p.PackTags(kind)
		}
		return q
	}
	return versionQuery{kind: manifest.TypeMod, game: r.Lock.Minecraft, tags: loader.ProviderLoaders(r.Lock.Loader.Type), loader: r.Lock.Loader.Type}
}

// pickVersion is the version pin names, or the newest one q finds on channel.
func pickVersion(ctx context.Context, p provider.Provider, proj *provider.Project, q versionQuery, pin, channel string) (*provider.Version, error) {
	if pin != "" {
		return pinnedVersion(ctx, p, proj, q.kind, pin)
	}
	tags := q.tags
	if q.untagged {
		tags = nil
	}
	versions, err := p.Versions(ctx, proj.ID, q.game, tags)
	if err != nil {
		return nil, err
	}
	if q.untagged && len(q.tags) > 0 {
		versions = slices.DeleteFunc(versions, func(v provider.Version) bool {
			return len(v.Loaders) > 0 && !slices.ContainsFunc(v.Loaders, func(l string) bool { return slices.Contains(q.tags, l) })
		})
	}
	v, ok := provider.Newest(versions, channel, q.loader)
	if !ok {
		e := out.Errorf("no-compatible-version", "%s has no %s version%s", proj.Slug, channelLabel(channel), platformSuffix(q.game, q.loader))
		e.Candidates, e.Pass = otherChannels(versions)
		e.Flag = "--channel"
		return nil, e
	}
	return &v, nil
}

// channelSetting names key's channel in shulker.json as what a no-compatible-version pick sets, in
// place of --channel, which would widen the channel of the command's own argument instead.
func channelSetting(err error, key string) error {
	var e *out.Error
	if errors.As(err, &e) && e.Code == "no-compatible-version" {
		e.Setting, e.Flag = "requires."+key+".channel", ""
	}
	return err
}

// newerThan is the newest version q finds on channel, and whether it is not the locked one.
func newerThan(ctx context.Context, p provider.Provider, projectID string, q versionQuery, channel string, locked string) (*provider.Version, bool, error) {
	versions, err := p.Versions(ctx, projectID, q.game, q.tags)
	if err != nil {
		return nil, false, err
	}
	newest, ok := provider.Newest(versions, channel, q.loader)
	if !ok || newest.ID == locked {
		return nil, false, nil
	}
	return &newest, true, nil
}

// pinnedVersion is the provider version pin names, refused when it belongs to another project.
func pinnedVersion(ctx context.Context, p provider.Provider, proj *provider.Project, kind, pin string) (*provider.Version, error) {
	v, err := p.Version(ctx, pin)
	if errors.Is(err, provider.ErrNotFound) {
		e := out.Errorf("version-not-found", "%s has no version %s for %s", p.Title(), pin, proj.Slug)
		e.Help = "list versions at " + p.VersionsPage(kind, proj.Slug)
		return nil, e
	}
	if err != nil {
		return nil, err
	}
	if v.ProjectID != proj.ID {
		return nil, out.Errorf("pin-mismatch", "version %s belongs to project %s, not %s", pin, v.ProjectID, proj.Slug)
	}
	return v, nil
}

// pinnedChannel is the channel a mod, pack or modpack pinned to v accepts: channel, widened to v's
// own when v is less stable, since pinning it accepts it.
func (r *Resolver) pinnedChannel(id string, v *provider.Version, channel string) string {
	if provider.ChannelAllows(channel, v.Channel) {
		return channel
	}
	r.Warnings = append(r.Warnings, fmt.Sprintf("%s %s is a %s; accepting %s for it", id, v.Number, v.Channel, v.Channel))
	return v.Channel
}

// relistedChannel is the channel entry accepts once v is locked for it: widened when entry is pinned
// to a less stable v, and then written back to key's entry in shulker.json, when it has one there.
func (r *Resolver) relistedChannel(key string, entry manifest.Require, v *provider.Version) string {
	if entry.Pin == "" {
		return entry.Channel
	}
	channel := r.pinnedChannel(key, v, entry.Channel)
	if listed, ok := r.Manifest.Requires[key]; ok && channel != entry.Channel {
		listed.Channel = channel
		r.Manifest.Requires[key] = listed
	}
	return channel
}

func channelLabel(channel string) string {
	if channel == "" {
		return "release"
	}
	return channel
}

func otherChannels(versions []provider.Version) (shown, channels []string) {
	seen := map[string]bool{}
	for _, v := range versions {
		key := v.Number + " (" + v.Channel + ")"
		if !seen[key] {
			seen[key] = true
			shown = append(shown, key)
			channels = append(channels, v.Channel)
		}
	}
	return shown, channels
}

type obtained struct {
	path   string
	sha512 string
	url    *string
	page   string
}

func (r *Resolver) obtain(ctx context.Context, proj *provider.Project, v *provider.Version) (obtained, error) {
	advanced := r.advanced
	r.advanced = false
	if v.File.Sha512 != "" {
		path, err := r.Cache.Ensure(ctx, r.Fetch, v.File.URL, v.File.Sha512)
		url := v.File.URL
		return obtained{path: path, sha512: v.File.Sha512, url: &url}, err
	}
	if sha, ok := r.Cache.BySha1(v.File.Sha1); ok && v.File.URL != "" && !r.Cache.IsManual(sha) {
		isDropped, err := r.isDropped(sha)
		if err != nil {
			return obtained{}, err
		}
		if !isDropped {
			url := v.File.URL
			return obtained{path: r.Cache.Object(sha), sha512: sha, url: &url}, nil
		}
	}
	if v.File.URL != "" {
		sha, err := r.Cache.Fetch(ctx, r.Fetch, v.File.URL)
		if err == nil {
			got, err := fsutil.SHA1(r.Cache.Object(sha))
			if err != nil {
				return obtained{}, err
			}
			if got != v.File.Sha1 {
				e := out.Errorf("checksum-mismatch", "the download from %s doesn't match the sha1 its provider gives", v.File.URL)
				e.Rows = []out.Detail{{Label: "want", Text: v.File.Sha1}, {Label: "got", Text: got}}
				return obtained{}, e
			}
			url := v.File.URL
			return obtained{path: r.Cache.Object(sha), sha512: sha, url: &url}, nil
		}
		if !errors.Is(err, fetch.ErrForbidden) {
			return obtained{}, err
		}
		r.log("treating %s %s as distribution-disabled: download forbidden", proj.Slug, v.Number)
	}
	files, err := r.sweepDownloads()
	if err != nil {
		return obtained{}, err
	}
	for _, f := range files {
		if f.Sha1 == v.File.Sha1 {
			return obtained{path: r.Cache.Object(f.Sha512), sha512: f.Sha512, page: v.Page}, r.Cache.MarkManual(f.Sha512)
		}
	}
	if sha, ok := r.Cache.BySha1(v.File.Sha1); ok {
		r.tookFromCache(v.File.Filename, advanced)
		return obtained{path: r.Cache.Object(sha), sha512: sha, page: v.Page}, nil
	}
	e := out.Errorf("manual-download", "%s can't be downloaded automatically", v.File.Filename)
	e.Items = []string{v.Page}
	e.Rows = []out.Detail{{Text: fmt.Sprintf("Download it into %s/ and run the command again:", DownloadsDir), Children: []out.Detail{{Text: v.Page}}}}
	manual.Attach(e, []manual.File{{Name: v.File.Filename, Page: v.Page, Sha1: v.File.Sha1, Sha512: v.File.Sha512}})
	return obtained{}, e
}

// obtainFrom is obtain, naming the file and p in the error when p fails to serve it.
func (r *Resolver) obtainFrom(ctx context.Context, p provider.Provider, proj *provider.Project, v *provider.Version) (obtained, error) {
	got, err := r.obtain(ctx, proj, v)
	if code := out.CodeOf(err); err != nil && v.File.URL != "" && (code == "" || code == "checksum-mismatch") {
		url := v.File.URL
		err = downloadable{id: proj.Slug, filename: v.File.Filename, host: p.Title(), url: &url}.downloadError(err)
	}
	return got, err
}

func (r *Resolver) settle(id, side, channel string) {
	m, ok := r.Lock.Mods[id]
	if !ok {
		return
	}
	m.Channel = channelLabel(channel)
	if side != "" {
		m.Side, m.SideFrom = side, sideFromRequires
	}
	r.Lock.Mods[id] = m
}

// setSource names where entry under key comes from: proj's id, unless key is its slug and p finds
// a slug as surely as an id, and p, unless it is the manifest's first provider.
func (r *Resolver) setSource(entry *manifest.Require, key string, p provider.Provider, proj *provider.Project) {
	if proj.Slug != key || !p.KeysBySlug() {
		entry.Project = proj.ID
	}
	if p.Name() != r.Manifest.ProviderOrder()[0] {
		entry.Provider = p.Name()
	}
}

func (r *Resolver) place(ctx context.Context, p provider.Provider, proj *provider.Project, v *provider.Version, key, requiredBy, sideOverride, channel string, replace bool) (string, *lock.Mod, error) {
	if !replace {
		id, indexed, err := r.indexedMod(p.Name(), proj.ID)
		if err != nil {
			return "", nil, err
		}
		if indexed && (key == "" || key == id) {
			prior, err := r.keepIndexed(id, requiredBy, p, proj)
			return id, prior, err
		}
	}
	r.fetching(proj.Slug, v.Number)
	got, err := r.obtainFrom(ctx, p, proj, v)
	if err != nil {
		return "", nil, err
	}
	info, err := r.readJar(got.path, v.File.Filename)
	if err != nil {
		return "", nil, prefixed("mod "+proj.Slug, err)
	}
	id := key
	if id == "" {
		id = jarKey(info.ID, proj.Slug)
	}
	if info.ID == "" {
		info.ID = id
	}
	if held, ok := r.Manifest.Requires[id]; ok && held.Kind() != manifest.TypeMod {
		return "", nil, manifest.KeyTaken(id, held.Kind(), manifest.TypeMod)
	}
	if err := r.modIDFree(info.ID, id); err != nil {
		return "", nil, err
	}
	var prior *lock.Mod
	replacesProject := false
	if existing, ok := r.Lock.Mods[id]; ok {
		if !isSameMod(existing, r.Lock.JarID(id), p.Name(), proj.ID, info.ID) {
			e := out.Errorf("requires-taken", "requires already has %s as %s", id, r.Lock.JarID(id))
			e.Help = fmt.Sprintf("pass `--as <key>` to give %s another key", info.ID)
			return "", nil, e
		}
		otherProject := existing.Provider == p.Name() && existing.Project != proj.ID
		if existing.Provider == p.Name() && existing.Slug == "" && proj.Slug != id && !otherProject {
			existing.Slug = proj.Slug
			r.Lock.Mods[id] = existing
		}
		prior = &existing
		if requiredBy != "" {
			r.Lock.AddRequiredBy(id, requiredBy)
		}
		switch {
		case otherProject && r.keepNewest:
			newer, err := r.newerThanLocked(existing, info)
			if err != nil {
				return "", nil, err
			}
			if !newer {
				r.noteKept(id, requiredBy, existing)
				return id, prior, nil
			}
			replacesProject = true
		case existing.Provider != p.Name() && replace:
			r.log("switching %s from %s to %s", id, existing.Provider, p.Name())
		case existing.Provider != p.Name():
			r.keepAlias(id, p, proj)
			if r.Lock.JarID(id) != info.ID {
				return id, prior, nil
			}
			return id, prior, r.recordJarID(existing, p, proj.ID, info.ID)
		case existing.Version != v.ID:
			r.noteKept(id, requiredBy, existing)
			return id, prior, nil
		default:
			return id, prior, nil
		}
	}
	side, sideFrom := proj.Side, sideFromProvider
	if side == "" {
		side, sideFrom = jarSide(info)
	}
	if sideOverride != "" {
		side, sideFrom = sideOverride, sideFromRequires
	}
	entry := lock.Mod{
		Provider:      p.Name(),
		Project:       proj.ID,
		Version:       v.ID,
		VersionNumber: versionNumber(p, v.Number, info),
		Filename:      v.File.Filename,
		URL:           got.url,
		Page:          got.page,
		Sha512:        got.sha512,
		Size:          v.File.Size,
		Side:          side,
		SideFrom:      sideFrom,
		Channel:       channelLabel(channel),
		RequiredBy:    []string{},
	}
	if requiredBy != "" {
		entry.RequiredBy = []string{requiredBy}
	}
	if prior != nil {
		entry.RequiredBy = prior.RequiredBy
	}
	if prior != nil && !replacesProject {
		entry.Aliases = maps.Clone(prior.Aliases)
		if sideOverride == "" {
			entry.Side, entry.SideFrom = prior.Side, prior.SideFrom
		}
		setAlias(&entry, prior.Provider, prior.Project)
		clearAlias(&entry, p.Name())
	}
	if info.ID != id {
		entry.ModID = info.ID
	}
	if proj.Slug != id {
		entry.Slug = proj.Slug
	}
	was := r.Lock.JarID(id)
	if prior != nil && was != info.ID {
		r.log("%s %s now identifies itself as %s", id, v.Number, info.ID)
	}
	if prior != nil && !replacesProject && prior.Provider != p.Name() && was == info.ID {
		if err := r.recordJarID(*prior, p, proj.ID, info.ID); err != nil {
			return "", nil, err
		}
	}
	r.Lock.Mods[id] = entry
	if r.locked == nil {
		r.locked = map[string]bool{}
	}
	r.locked[id] = true
	return id, prior, nil
}

// noteKept notes keeping a mod's locked file over the one requiredBy asked for, unless this
// resolver locked it: that is the same command settling on one file, not an entry the user had.
func (r *Resolver) noteKept(id, requiredBy string, existing lock.Mod) {
	if r.locked[id] || r.Note == nil {
		return
	}
	it := out.Item{Kind: out.Note, Name: id, Version: existing.VersionNumber, Text: "already in the pack"}
	if requiredBy != "" {
		it.Aside = []string{"required by " + requiredBy}
	}
	if r.group != nil {
		r.held = append(r.held, it)
		return
	}
	r.Note(it)
}

// newerThanLocked reports whether info's jar is newer than the one the entry locks. An entry whose
// jar isn't cached is kept: only a file this import locked is sure to be there.
func (r *Resolver) newerThanLocked(existing lock.Mod, info *jarmeta.Info) (bool, error) {
	if !r.Cache.Has(existing.Sha512) {
		return false, nil
	}
	locked, err := r.readJar(r.Cache.Object(existing.Sha512), existing.Filename)
	if err != nil {
		return false, err
	}
	return compareVersions(candidate{version: info.Version, maven: info.UsesMavenRanges}, candidate{version: locked.Version, maven: locked.UsesMavenRanges}) > 0, nil
}

// isSameMod reports whether a lock entry and a freshly resolved jar are the same
// mod: the provider's own project id where it can be compared, an alias where
// the entry came from another provider, and the jar's id otherwise.
func isSameMod(existing lock.Mod, jarID, providerName, projectID, resolvedJarID string) bool {
	if jarID == resolvedJarID {
		return true
	}
	if existing.Provider == providerName {
		return existing.Project == projectID
	}
	alias := aliasFor(existing, providerName)
	return alias != "" && alias == projectID
}

func clearAlias(m *lock.Mod, providerName string) {
	delete(m.Aliases, providerName)
}

func setAlias(m *lock.Mod, providerName, projectID string) {
	if m.Aliases == nil {
		m.Aliases = lock.Aliases{}
	}
	m.Aliases[providerName] = projectID
}

func (r *Resolver) addDeps(ctx context.Context, p provider.Provider, v *provider.Version, parentID, channel string, visited map[string]bool) error {
	for _, d := range v.Dependencies {
		if d.Type != "required" {
			continue
		}
		var dv *provider.Version
		var err error
		if d.VersionID != "" {
			dv, err = p.Version(ctx, d.VersionID)
			if err != nil {
				return err
			}
			d.ProjectID = dv.ProjectID
		}
		if d.ProjectID == "" || visited[d.ProjectID] {
			continue
		}
		visited[d.ProjectID] = true
		if locked, ok := r.lockedProject(p.Name(), d.ProjectID); ok && dv == nil {
			r.Lock.AddRequiredBy(locked, parentID)
			continue
		}
		dproj, err := p.Project(ctx, d.ProjectID, "")
		if err != nil {
			return err
		}
		if dv == nil {
			dv, err = pickVersion(ctx, p, dproj, r.queryFor(manifest.TypeMod, p.Name()), "", channel)
			if err != nil {
				return prefixed("dependency of "+parentID, err)
			}
		}
		id, _, err := r.place(ctx, p, dproj, dv, "", parentID, "", channel, false)
		if err != nil {
			return err
		}
		if err := r.addDeps(ctx, p, dv, id, channel, visited); err != nil {
			return err
		}
	}
	return nil
}

// lockedProject finds the lock's mod that providerName's project projectID resolved to, by its
// project or its alias for that provider.
func (r *Resolver) lockedProject(providerName, projectID string) (string, bool) {
	for _, id := range sortedKeys(r.Lock.Mods) {
		m := r.Lock.Mods[id]
		if m.Provider == providerName && m.Project == projectID || aliasFor(m, providerName) == projectID {
			return id, true
		}
	}
	return "", false
}

func aliasFor(m lock.Mod, providerName string) string {
	return m.Aliases[providerName]
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// Install downloads every locked mod the cache lacks, taking manual downloads from DownloadsDir. It
// returns the mods it fetched and warnings for files there that match no locked mod. With sides
// given, it leaves out files that none of them use. A download the host fails goes on to the next
// file unless FailFast, and the error joins the failed downloads' and the missing files'.
func (r *Resolver) Install(ctx context.Context, sides ...string) ([]string, []string, error) {
	return r.install(ctx, r.lockFiles, sides)
}

// InstallMods is Install for the locked mods alone, the jars Validate reads.
func (r *Resolver) InstallMods(ctx context.Context) ([]string, []string, error) {
	return r.install(ctx, r.modFiles, nil)
}

// Adopted are the pending mods an install filled from downloads/, which changed the lock.
func (r *Resolver) Adopted() []string { return r.adopted }

func (r *Resolver) install(ctx context.Context, lockedFiles func() []downloadable, sides []string) ([]string, []string, error) {
	files, err := r.sweepDownloads()
	if err != nil {
		return nil, nil, err
	}
	if err := r.adoptPending(files); err != nil {
		return nil, nil, err
	}
	locked := lockedFiles()
	var warnings []string
	for _, f := range files {
		if !r.lockHas(f.Sha512) {
			warnings = append(warnings, fmt.Sprintf("%s/%s matches no mod in the lock", DownloadsDir, f.Name))
		} else if err := r.Cache.MarkManual(f.Sha512); err != nil {
			return nil, nil, err
		}
	}
	var fetched []string
	var missing []string
	var rows []out.Detail
	var byHand []manual.File
	var wanted []string
	var downloads []out.Download
	byID := map[string]downloadable{}
	for _, f := range locked {
		if r.Cache.Has(f.sha512) || !usedBy(f.side, sides) {
			continue
		}
		if f.file != "" {
			if problem := r.restoreLocal(f); problem != "" {
				missing = append(missing, problem)
			}
			continue
		}
		if f.url == nil && (r.SkipManual || f.sha512 == "" && r.SkipPending) {
			continue
		}
		if f.url == nil {
			missing = append(missing, fmt.Sprintf("%s: download %s from %s and place it in %s/", f.id, f.filename, f.page, DownloadsDir))
			rows, byHand = append(rows, ManualRow(f.filename, f.page)), append(byHand, f.byHand())
			continue
		}
		byID[f.id] = f
		wanted, downloads = append(wanted, f.id), append(downloads, out.Download{Name: f.filename, Size: f.size})
	}
	var progress *out.Progress
	if r.Progress != nil && !r.EveryFetch && len(wanted) > 0 {
		progress = r.Progress("fetching", downloads)
		if r.Fetch != nil {
			r.Fetch.Progress = progress.Bytes
			defer func() { r.Fetch.Progress = nil }()
		}
	}
	var failed []*out.Error
	for i, id := range wanted {
		f := byID[id]
		if progress == nil {
			r.log("fetching %s", downloads[i].Name)
		}
		progress.File(downloads[i].Name)
		_, err := r.Cache.Ensure(ctx, r.Fetch, *f.url, f.sha512)
		if errors.Is(err, fetch.ErrForbidden) {
			missing = append(missing, fmt.Sprintf("%s: download forbidden; download %s from %s and place it in %s/", id, f.filename, f.page, DownloadsDir))
			rows, byHand = append(rows, ManualRow(f.filename, f.page)), append(byHand, f.byHand())
			continue
		}
		if err != nil {
			err = f.downloadError(err)
			code := out.CodeOf(err)
			if r.FailFast || (code != "download-failed" && code != "checksum-mismatch") {
				progress.Abort()
				return fetched, warnings, err
			}
			failed = append(failed, out.AsError(err))
			continue
		}
		progress.Advance()
		fetched = append(fetched, id)
	}
	if len(failed) > 0 && len(fetched) == 0 {
		progress.Abort()
	} else {
		progress.Finish()
	}
	var errs []error
	if len(failed) > 0 {
		errs = append(errs, downloadsFailed(failed))
	}
	if len(missing) > 0 {
		e := out.Errorf("missing-files", "%s a manual download", out.Count(len(missing), "file needs", "files need"))
		e.Items = missing
		if len(rows) == len(missing) {
			e.Rows = rows
			manual.Attach(e, byHand)
		}
		errs = append(errs, e)
	}
	return fetched, warnings, errors.Join(errs...)
}

// byHand is the file to download by hand in place of f.
func (f downloadable) byHand() manual.File {
	return manual.File{Name: f.filename, Page: f.page, Sha1: f.sha1, Sha512: f.sha512}
}

// downloadsFailed is the error for the locked files whose downloads failed: the one file's own
// error, or one download-failed listing each file's.
func downloadsFailed(failed []*out.Error) *out.Error {
	if len(failed) == 1 {
		return failed[0]
	}
	e := out.Errorf("download-failed", "couldn't download %d files", len(failed))
	e.Help = failed[0].Help
	for _, f := range failed {
		if f.Help != e.Help {
			e.Help = ""
		}
		e.Items = append(e.Items, f.Message)
		row := out.Detail{Text: f.Message, Children: f.Rows}
		if f.Code != e.Code {
			row.Text += " (" + f.Code + ")"
		}
		e.Rows = append(e.Rows, row)
	}
	return e
}

// downloadError names the locked file whose download failed when the failure is its host's, as a CDN
// cutting the file short, leaving one of shulker's own, such as the cache's disk, as it is.
func (f downloadable) downloadError(err error) error {
	host := f.host
	if host == "" {
		host = urlHost(*f.url)
	}
	var e *out.Error
	if errors.As(err, &e) && e.Code == "checksum-mismatch" {
		e.Rows = append([]out.Detail{{Label: "file", Text: f.id + " (" + f.filename + ")"}}, e.Rows...)
		return err
	}
	fault, ok := downloadFailure(err, host)
	if !ok {
		return err
	}
	e = out.Errorf("download-failed", "couldn't download %s (%s) from %s", f.id, f.filename, host)
	e.Help = fault.help
	e.Wrapped = err
	e.Rows = []out.Detail{{Label: "url", Text: *f.url}, {Label: "cause", Text: downloadCause(err, *f.url)}}
	return e
}

// downloadCause is what went wrong with err, without the URL the error rows already show.
func downloadCause(err error, rawURL string) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return strings.TrimPrefix(err.Error(), rawURL+": ")
}

func urlHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

func usedBy(side string, sides []string) bool {
	return len(sides) == 0 || side == "both" || slices.Contains(sides, side)
}

func (r *Resolver) lockHas(sha512 string) bool {
	for _, f := range r.lockFiles() {
		if f.sha512 == sha512 {
			return true
		}
	}
	return false
}

// pageFor is where a mod's file can be fetched by hand: its own page, else its project's, else
// its download URL.
func (r *Resolver) pageFor(m lock.Mod) string {
	switch {
	case m.Page != "":
		return m.Page
	case m.URL == nil && m.Provider != "":
		if p, ok := r.Providers[m.Provider]; ok {
			return p.ProjectPage("", m.Project)
		}
		return ""
	case m.URL == nil:
		return ""
	}
	return *m.URL
}

// versionNumber is the version a mod is shown at: the one its jar declares where the provider's
// number is only a name, unless the jar declares none.
func versionNumber(p provider.Provider, number string, info *jarmeta.Info) string {
	if !p.NamesVersions() || info.Version == "" || info.Version == "0.0NONE" || strings.Contains(info.Version, "${") {
		return number
	}
	return info.Version
}

// adoptPending fills each pending mod a file in downloads/ matches by sha1: its sha512 and size
// from the file, and its mod id and version from the jar.
func (r *Resolver) adoptPending(files []dropped) error {
	cached := 0
	defer func() { r.reportCached(cached) }()
	for _, id := range r.lockIDs() {
		m := r.Lock.Mods[id]
		if !m.IsPending() {
			continue
		}
		var sha string
		if i := slices.IndexFunc(files, func(f dropped) bool { return f.Sha1 == m.Sha1 }); i >= 0 {
			sha = files[i].Sha512
			if err := r.Cache.MarkManual(sha); err != nil {
				return err
			}
		} else if found, ok := r.Cache.BySha1(m.Sha1); ok {
			sha = found
			cached++
		} else {
			continue
		}
		path := r.Cache.Object(sha)
		info, err := r.readJar(path, m.Filename)
		if err != nil {
			return prefixed("mod "+id, err)
		}
		st, err := os.Stat(path)
		if err != nil {
			return err
		}
		m.Sha512, m.Sha1, m.Size = sha, "", st.Size()
		if info.ID != "" && info.ID != id {
			m.ModID = info.ID
		}
		if p, err := r.provider(m.Provider); err == nil {
			m.VersionNumber = versionNumber(p, m.VersionNumber, info)
		}
		r.Lock.Mods[id] = m
		r.adopted = append(r.adopted, id)
	}
	return nil
}

// ManualRow is how a file to download by hand shows under a missing-files error: its name, and
// the page it comes from on the line beneath.
func ManualRow(filename, page string) out.Detail {
	return out.Detail{Text: filename, Children: []out.Detail{{Text: page}}}
}
