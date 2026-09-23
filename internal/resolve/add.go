// Package resolve keeps a project's manifest and lock in step: adding, updating, pinning and removing
// mods and modpacks, and resolving the platform they run on.
package resolve

import (
	"context"
	"errors"
	"fmt"
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
	e := out.Errorf("mod-not-found", "%s was not found on %s", slug, strings.Join(missed, " or "))
	for _, reason := range skipped {
		e.Items = append(e.Items, "skipped: "+reason.Message)
	}
	return nil, nil, e
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
	v, err := r.pick(ctx, p, proj, opts.Pin, opts.Channel)
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
		entry.Pin = lockID(p.Name(), opts.Pin)
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

func (r *Resolver) pick(ctx context.Context, p provider.Provider, proj *provider.Project, pin, channel string) (*provider.Version, error) {
	if pin != "" {
		v, err := p.Version(ctx, pin)
		if errors.Is(err, provider.ErrNotFound) {
			page := "https://modrinth.com/mod/" + proj.Slug + "/versions"
			if p.Name() == "curseforge" {
				page = "https://www.curseforge.com/minecraft/mc-mods/" + proj.Slug + "/files"
			}
			e := out.Errorf("version-not-found", "%s has no version %s for %s", p.Name(), pin, proj.Slug)
			e.Help = "list versions at " + page
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
	versions, err := p.Versions(ctx, proj.ID, r.Lock.Minecraft, loader.ProviderLoaders(r.Lock.Loader.Type))
	if err != nil {
		return nil, err
	}
	v, ok := provider.Newest(versions, channel, r.Lock.Loader.Type)
	if !ok {
		e := out.Errorf("no-compatible-version", "%s has no %s version for Minecraft %s with %s", proj.Slug, channelLabel(channel), r.Lock.Minecraft, r.Lock.Loader.Type)
		e.Candidates, e.Pass = otherChannels(versions)
		e.Flag = "--channel"
		return nil, e
	}
	return &v, nil
}

// pinnedChannel is the channel a mod pinned to v accepts: channel, widened to v's own when v is less
// stable, since pinning it accepts it.
func (r *Resolver) pinnedChannel(id string, v *provider.Version, channel string) string {
	if provider.ChannelAllows(channel, v.Channel) {
		return channel
	}
	r.Warnings = append(r.Warnings, fmt.Sprintf("%s %s is a %s; accepting %s for it", id, v.Number, v.Channel, v.Channel))
	return v.Channel
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
		entry.Project = lockID(p.Name(), proj.ID)
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
		case fmt.Sprint(existing.Version) != v.ID:
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
		Project:       lockID(p.Name(), proj.ID),
		Version:       lockID(p.Name(), v.ID),
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
		setAlias(&entry, prior.Provider, fmt.Sprint(prior.Project))
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
		return fmt.Sprint(existing.Project) == projectID
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
			dv, err = r.pick(ctx, p, dproj, "", channel)
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
		if m.Provider == providerName && fmt.Sprint(m.Project) == projectID || aliasFor(m, providerName) == projectID {
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
// given, it leaves out files that none of them use.
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
	for i, id := range wanted {
		f := byID[id]
		progress.File(downloads[i].Name)
		_, err := r.Cache.Ensure(ctx, r.Fetch, *f.url, f.sha512)
		if errors.Is(err, fetch.ErrForbidden) {
			missing = append(missing, fmt.Sprintf("%s: download forbidden; download %s from %s and place it in %s/", id, f.filename, f.page, DownloadsDir))
			continue
		}
		if err != nil {
			progress.Abort()
			return fetched, warnings, err
		}
		progress.Advance()
		fetched = append(fetched, id)
	}
	progress.Finish()
	if len(missing) > 0 {
		e := out.Errorf("missing-files", "%d file(s) need a manual download", len(missing))
		e.Items = missing
		return fetched, warnings, e
	}
	return fetched, warnings, nil
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
		return curseforge.ProjectPage(fmt.Sprint(m.Project))
	case m.URL == nil:
		return ""
	}
	return *m.URL
}
