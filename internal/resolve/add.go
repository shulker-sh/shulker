// Package resolve keeps a project's manifest and lock in step: adding, updating, pinning and removing
// mods and modpacks, and resolving the platform they run on.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/curseforge"
)

// Resolver changes a project's manifest and lock together: it picks versions from the providers,
// downloads them into the cache and records them in the lock.
type Resolver struct {
	Dir       string
	Manifest  *manifest.Manifest
	Lock      *lock.Lock
	Providers map[string]provider.Provider
	Cache     *cache.Cache
	Fetch     *fetch.Client
	Packs     []*pack.Loaded
	Meta      *Meta
	Log       func(format string, args ...any)
	// Warnings are raised while resolving, for the command to print when it finishes.
	Warnings []string
	// FailFast stops Install at the first download that fails, rather than trying every file and
	// failing with them all.
	FailFast bool
	// Progress starts a download bar for the named files.
	Progress func(verb string, files []out.Download) *out.Progress
	// AskMove is shown the deps-held refusal before an add without --with-deps gives up;
	// yes carries on as --with-deps would.
	AskMove func(held *out.Error) (bool, error)
	// LockModpack locks a hosted modpack entry and puts it in the manifest under key, in place of
	// the modpack already there.
	LockModpack func(ctx context.Context, key string, entry manifest.Require) error
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

func (r *Resolver) log(format string, args ...any) {
	if r.Log != nil {
		r.Log(format, args...)
	}
}

func (r *Resolver) provider(name string) (provider.Provider, error) {
	if name != "" {
		p, ok := r.Providers[name]
		if !ok {
			return nil, Unavailable(name)
		}
		return p, nil
	}
	var reasons []*out.Error
	for _, n := range r.Manifest.ProviderOrder() {
		if p, ok := r.Providers[n]; ok {
			return p, nil
		}
		reasons = append(reasons, Unavailable(n))
	}
	return nil, NoneAvailable("no manifest provider is available", reasons)
}

// Unavailable says why a provider can't be used.
func Unavailable(name string) *out.Error {
	if name == "curseforge" {
		e := out.Errorf("provider-unavailable", "curseforge needs an API key")
		e.Help = "set " + curseforge.KeyEnv + " or run `shulker config set curseforge.key <key>`"
		return e
	}
	return out.Errorf("provider-unavailable", "%s is not a known provider", name)
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
			return nil, nil, notFound(slug, []string{providerName}, nil)
		}
		return p, proj, err
	}
	var missed []string
	var skipped []*out.Error
	for _, n := range r.Manifest.ProviderOrder() {
		p, ok := r.Providers[n]
		if !ok {
			skipped = append(skipped, Unavailable(n))
			continue
		}
		proj, err := p.Project(ctx, slug, kind)
		if errors.Is(err, provider.ErrNotFound) {
			missed = append(missed, n)
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

func notFound(slug string, missed []string, skipped []*out.Error) *out.Error {
	e := out.Errorf("mod-not-found", "%s was not found on %s", slug, strings.Join(missed, " or "))
	for _, reason := range skipped {
		e.Items = append(e.Items, "skipped: "+reason.Message)
	}
	if _, err := strconv.Atoi(slug); err != nil && slices.Contains(missed, "curseforge") {
		e.Help = "CurseForge's search doesn't list every project; add one it misses by a file URL (https://www.curseforge.com/minecraft/mc-mods/<slug>/files/<file id>), by https://www.curseforge.com/projects/<project id>, or by its project id, shown on its CurseForge page under About Project, with `--provider curseforge`"
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
	id, prior, err := r.place(ctx, p, proj, v, opts.As, "", opts.Side, opts.Channel, explicit)
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
		entry.Pin = manifest.NewID(p.Name(), opts.Pin)
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
}

func (r *Resolver) queryFor(kind, providerName string) versionQuery {
	switch {
	case kind == manifest.TypeModpack:
		game, loaderName := r.modpackPlatform()
		return versionQuery{kind: kind, game: game, tags: modpackLoaders(loaderName), loader: loaderName}
	case manifest.IsPackKind(kind):
		return versionQuery{kind: kind, game: r.Lock.Minecraft, tags: packTags(providerName, kind)}
	}
	return versionQuery{kind: manifest.TypeMod, game: r.Lock.Minecraft, tags: loader.ProviderLoaders(r.Lock.Loader.Type), loader: r.Lock.Loader.Type}
}

// pickVersion is the version pin names, or the newest one q finds on channel.
func pickVersion(ctx context.Context, p provider.Provider, proj *provider.Project, q versionQuery, pin, channel string) (*provider.Version, error) {
	if pin != "" {
		return pinnedVersion(ctx, p, proj, q.kind, pin)
	}
	versions, err := p.Versions(ctx, proj.ID, q.game, q.tags)
	if err != nil {
		return nil, err
	}
	v, ok := provider.Newest(versions, channel, q.loader)
	if !ok {
		e := out.Errorf("no-compatible-version", "%s has no %s version%s", proj.Slug, channelLabel(channel), platformLabel(q.game, q.loader))
		e.Candidates, e.Pass = otherChannels(versions)
		e.Flag = "--channel"
		return nil, e
	}
	return &v, nil
}

// newerThan is the newest version q finds on channel, and whether it is not the locked one.
func newerThan(ctx context.Context, p provider.Provider, projectID manifest.ID, q versionQuery, channel string, locked manifest.ID) (*provider.Version, bool, error) {
	versions, err := p.Versions(ctx, projectID.String(), q.game, q.tags)
	if err != nil {
		return nil, false, err
	}
	newest, ok := provider.Newest(versions, channel, q.loader)
	if !ok || newest.ID == locked.String() {
		return nil, false, nil
	}
	return &newest, true, nil
}

// pinnedVersion is the provider version pin names, refused when it belongs to another project.
func pinnedVersion(ctx context.Context, p provider.Provider, proj *provider.Project, kind, pin string) (*provider.Version, error) {
	v, err := p.Version(ctx, pin)
	if errors.Is(err, provider.ErrNotFound) {
		e := out.Errorf("version-not-found", "%s has no version %s for %s", p.Name(), pin, proj.Slug)
		e.Help = "list versions at " + versionsPage(p.Name(), kind, proj.Slug)
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

func versionsPage(providerName, kind, slug string) string {
	if providerName == "curseforge" {
		return "https://www.curseforge.com/minecraft/" + curseforgeSections[kind] + "/" + slug + "/files"
	}
	return "https://modrinth.com/" + kind + "/" + slug + "/versions"
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
	if entry.Pin.IsZero() {
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
	if v.File.Sha512 != "" {
		path, err := r.Cache.Ensure(ctx, r.Fetch, v.File.URL, v.File.Sha512)
		url := v.File.URL
		return obtained{path: path, sha512: v.File.Sha512, url: &url}, err
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
			return obtained{path: r.Cache.Object(f.Sha512), sha512: f.Sha512, page: v.Page}, nil
		}
	}
	e := out.Errorf("manual-download", "%s %s is not distributed by its provider", proj.Slug, v.Number)
	e.Help = fmt.Sprintf("download %s from %s into %s/ and run the command again", v.File.Filename, v.Page, DownloadsDir)
	return obtained{}, e
}

func (r *Resolver) settle(id, side, channel string) {
	m, ok := r.Lock.Mods[id]
	if !ok {
		return
	}
	m.Channel = channelLabel(channel)
	if side != "" {
		m.Side = side
	}
	r.Lock.Mods[id] = m
}

// setSource names where entry under key comes from: proj's id, unless key is its Modrinth slug,
// and p, unless it is the manifest's first provider.
func (r *Resolver) setSource(entry *manifest.Require, key string, p provider.Provider, proj *provider.Project) {
	if proj.Slug != key || p.Name() != "modrinth" {
		entry.Project = manifest.NewID(p.Name(), proj.ID)
	}
	if p.Name() != r.Manifest.ProviderOrder()[0] {
		entry.Provider = p.Name()
	}
}

func (r *Resolver) place(ctx context.Context, p provider.Provider, proj *provider.Project, v *provider.Version, key, requiredBy, sideOverride, channel string, replace bool) (string, *lock.Mod, error) {
	r.log("fetching %s %s", proj.Slug, v.Number)
	got, err := r.obtain(ctx, proj, v)
	if err != nil {
		return "", nil, err
	}
	info, err := jarmeta.Read(got.path, v.File.Filename, r.Lock.Loader.Type)
	if err != nil {
		return "", nil, prefixed("mod "+proj.Slug, err)
	}
	id := key
	if id == "" {
		id = info.ID
	}
	if held, ok := r.Manifest.Requires[id]; ok && held.Kind() != manifest.TypeMod {
		return "", nil, manifest.KeyTaken(id, held.Kind(), manifest.TypeMod)
	}
	if err := r.modIDFree(info.ID, id); err != nil {
		return "", nil, err
	}
	var prior *lock.Mod
	if existing, ok := r.Lock.Mods[id]; ok {
		if !isSameMod(existing, r.Lock.JarID(id), p.Name(), proj.ID, info.ID) {
			e := out.Errorf("requires-taken", "requires already has %s as %s", id, r.Lock.JarID(id))
			e.Help = fmt.Sprintf("pass `--as <key>` to give %s another key", info.ID)
			return "", nil, e
		}
		if existing.Provider == p.Name() && existing.Slug == "" && proj.Slug != id {
			existing.Slug = proj.Slug
			r.Lock.Mods[id] = existing
		}
		prior = &existing
		if requiredBy != "" {
			r.Lock.AddRequiredBy(id, requiredBy)
		}
		switch {
		case existing.Provider != p.Name() && replace:
			r.log("switching %s from %s to %s", id, existing.Provider, p.Name())
		case existing.Provider != p.Name():
			aliased := r.Lock.Mods[id]
			setAlias(&aliased, p.Name(), proj.ID)
			r.Lock.Mods[id] = aliased
			r.log("keeping %s %s from %s (%s project %s recorded as an alias)", id, existing.VersionNumber, existing.Provider, p.Name(), proj.ID)
			return id, prior, nil
		case existing.Version.String() != v.ID:
			r.log("keeping %s %s already in lock", id, existing.VersionNumber)
			return id, prior, nil
		default:
			return id, prior, nil
		}
	}
	side := proj.Side
	if side == "" {
		side = info.Side
	}
	if sideOverride != "" {
		side = sideOverride
	}
	entry := lock.Mod{
		Provider:      p.Name(),
		Project:       manifest.NewID(p.Name(), proj.ID),
		Version:       manifest.NewID(p.Name(), v.ID),
		VersionNumber: v.Number,
		Filename:      v.File.Filename,
		URL:           got.url,
		Page:          got.page,
		Sha512:        got.sha512,
		Size:          v.File.Size,
		Side:          side,
		Channel:       channelLabel(channel),
		RequiredBy:    []string{},
	}
	if requiredBy != "" {
		entry.RequiredBy = []string{requiredBy}
	}
	if prior != nil {
		entry.RequiredBy = prior.RequiredBy
		entry.Aliases = prior.Aliases
		if sideOverride == "" {
			entry.Side = prior.Side
		}
		setAlias(&entry, prior.Provider, prior.Project.String())
		clearAlias(&entry, p.Name())
	}
	if info.ID != id {
		entry.ModID = info.ID
	}
	if proj.Slug != id {
		entry.Slug = proj.Slug
	}
	if was := r.Lock.JarID(id); prior != nil && was != info.ID {
		r.log("%s %s now identifies itself as %s", id, v.Number, info.ID)
	}
	r.Lock.Mods[id] = entry
	return id, prior, nil
}

// isSameMod reports whether a lock entry and a freshly resolved jar are the same
// mod: the provider's own project id where it can be compared, an alias where
// the entry came from another provider, and the jar's id otherwise.
func isSameMod(existing lock.Mod, jarID, providerName, projectID, resolvedJarID string) bool {
	if jarID == resolvedJarID {
		return true
	}
	if existing.Provider == providerName {
		return existing.Project.String() == projectID
	}
	alias := aliasFor(existing, providerName)
	return alias != "" && alias == projectID
}

func clearAlias(m *lock.Mod, providerName string) {
	switch providerName {
	case "modrinth":
		m.Aliases.Modrinth = ""
	case "curseforge":
		m.Aliases.CurseForge = 0
	}
}

func setAlias(m *lock.Mod, providerName, projectID string) {
	switch providerName {
	case "modrinth":
		m.Aliases.Modrinth = projectID
	case "curseforge":
		m.Aliases.CurseForge, _ = strconv.Atoi(projectID)
	}
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
		if m.Provider == providerName && m.Project.String() == projectID || aliasFor(m, providerName) == projectID {
			return id, true
		}
	}
	return "", false
}

func aliasFor(m lock.Mod, providerName string) string {
	switch providerName {
	case "modrinth":
		return m.Aliases.Modrinth
	case "curseforge":
		if m.Aliases.CurseForge != 0 {
			return strconv.Itoa(m.Aliases.CurseForge)
		}
	}
	return ""
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
	files, err := r.sweepDownloads()
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	for _, f := range files {
		if !r.lockHas(f.Sha512) {
			warnings = append(warnings, fmt.Sprintf("%s/%s matches no mod in the lock", DownloadsDir, f.Name))
		}
	}
	var fetched []string
	var missing []string
	var wanted []string
	var downloads []out.Download
	byID := map[string]downloadable{}
	for _, f := range r.lockFiles() {
		if r.Cache.Has(f.sha512) || !usedBy(f.side, sides) {
			continue
		}
		if f.file != "" {
			if problem := r.restoreLocal(f); problem != "" {
				missing = append(missing, problem)
			}
			continue
		}
		if f.url == nil {
			missing = append(missing, fmt.Sprintf("%s: download %s from %s and place it in %s/", f.id, f.filename, f.page, DownloadsDir))
			continue
		}
		byID[f.id] = f
		wanted, downloads = append(wanted, f.id), append(downloads, out.Download{Name: f.filename, Size: f.size})
	}
	var progress *out.Progress
	if r.Progress != nil && len(wanted) > 0 {
		progress = r.Progress("fetching", downloads)
		if r.Fetch != nil {
			r.Fetch.Progress = progress.Bytes
			defer func() { r.Fetch.Progress = nil }()
		}
	}
	var failed []*out.Error
	for i, id := range wanted {
		f := byID[id]
		progress.File(downloads[i].Name)
		_, err := r.Cache.Ensure(ctx, r.Fetch, *f.url, f.sha512)
		if errors.Is(err, fetch.ErrForbidden) {
			missing = append(missing, fmt.Sprintf("%s: download forbidden; download %s from %s and place it in %s/", id, f.filename, f.page, DownloadsDir))
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
		e := out.Errorf("missing-files", "%d file(s) need a manual download", len(missing))
		e.Items = missing
		errs = append(errs, e)
	}
	return fetched, warnings, errors.Join(errs...)
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
	host := provider.Title(f.provider)
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

func pageFor(m lock.Mod) string {
	switch {
	case m.Page != "":
		return m.Page
	case m.Provider == "curseforge":
		return curseforge.ProjectPage(m.Project.String())
	case m.URL == nil:
		return ""
	}
	return *m.URL
}
