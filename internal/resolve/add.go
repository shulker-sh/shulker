package resolve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/curseforge"
)

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
}

type AddOptions struct {
	Side     string
	Channel  string
	Pin      string
	Provider string
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
			return nil, out.Errorf("provider-unavailable", "provider %q is not available", name)
		}
		return p, nil
	}
	for _, n := range r.Manifest.ProviderOrder() {
		if p, ok := r.Providers[n]; ok {
			return p, nil
		}
	}
	return nil, out.Errorf("provider-unavailable", "none of the manifest providers %v are available", r.Manifest.ProviderOrder())
}

func (r *Resolver) lookup(ctx context.Context, slug, providerName string) (provider.Provider, *provider.Project, error) {
	if providerName != "" {
		p, err := r.provider(providerName)
		if err != nil {
			return nil, nil, err
		}
		proj, err := p.Project(ctx, slug)
		return p, proj, err
	}
	var missed []string
	for _, n := range r.Manifest.ProviderOrder() {
		p, ok := r.Providers[n]
		if !ok {
			continue
		}
		proj, err := p.Project(ctx, slug)
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
	return nil, nil, out.Errorf("mod-not-found", "%s was not found on %s", slug, strings.Join(missed, " or "))
}

func (r *Resolver) Add(ctx context.Context, slug string, opts AddOptions) error {
	explicit := opts.Provider != ""
	if prev, ok := r.Manifest.Mods[slug]; !explicit && ok && prev.Provider != "" {
		opts.Provider = prev.Provider
	}
	p, proj, err := r.lookup(ctx, slug, opts.Provider)
	if err != nil {
		return err
	}
	v, err := r.pick(ctx, p, proj, opts.Pin, opts.Channel)
	if err != nil {
		return err
	}
	id, prior, err := r.place(ctx, p, proj, v, "", opts.Side, opts.Channel, explicit)
	if err != nil {
		return err
	}
	previous := r.Manifest.Mods[id]
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
	r.settle(id, opts.Side, opts.Channel)
	visited := map[string]bool{proj.ID: true}
	if err := r.addDeps(ctx, p, v, id, opts.Channel, visited); err != nil {
		return err
	}
	entry := manifest.Mod{Side: opts.Side}
	if opts.Channel != "" && opts.Channel != "release" {
		entry.Channel = opts.Channel
	}
	if opts.Pin != "" {
		entry.Pin = lockID(p.Name(), opts.Pin)
	}
	if proj.Slug != id || p.Name() != "modrinth" {
		entry.Project = lockID(p.Name(), proj.ID)
	}
	if p.Name() != r.Manifest.ProviderOrder()[0] {
		entry.Provider = p.Name()
	}
	r.Manifest.Mods[id] = entry
	if switched {
		r.pruneOrphans()
	}
	return nil
}

func (r *Resolver) pick(ctx context.Context, p provider.Provider, proj *provider.Project, pin, channel string) (*provider.Version, error) {
	if pin != "" {
		v, err := p.Version(ctx, pin)
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
		e.Candidates = otherChannels(versions)
		return nil, e
	}
	return &v, nil
}

func channelLabel(channel string) string {
	if channel == "" {
		return "release"
	}
	return channel
}

func otherChannels(versions []provider.Version) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range versions {
		key := v.Number + " (" + v.Channel + ")"
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
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
			got, err := sha1Of(r.Cache.Object(sha))
			if err != nil {
				return obtained{}, err
			}
			if got != v.File.Sha1 {
				return obtained{}, fmt.Errorf("%s: sha1 mismatch (expected %s…, got %s…)", v.File.URL, v.File.Sha1[:12], got[:12])
			}
			url := v.File.URL
			return obtained{path: r.Cache.Object(sha), sha512: sha, url: &url}, nil
		}
		if !errors.Is(err, fetch.ErrForbidden) {
			return obtained{}, err
		}
		r.log("%s %s: download forbidden, treating it as distribution-disabled", proj.Slug, v.Number)
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
	return obtained{}, out.Errorf("manual-download", "%s %s is not distributed by its provider: download %s from %s into %s/ and run the command again", proj.Slug, v.Number, v.File.Filename, v.Page, DownloadsDir)
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

func (r *Resolver) place(ctx context.Context, p provider.Provider, proj *provider.Project, v *provider.Version, requiredBy, sideOverride, channel string, replace bool) (string, *lock.Mod, error) {
	r.log("fetching %s %s", proj.Slug, v.Number)
	got, err := r.obtain(ctx, proj, v)
	if err != nil {
		return "", nil, err
	}
	info, err := jarmeta.Read(got.path, r.Lock.Loader.Type)
	if err != nil {
		return "", nil, err
	}
	var prior *lock.Mod
	if existing, ok := r.Lock.Mods[info.ID]; ok {
		prior = &existing
		if requiredBy != "" {
			r.Lock.AddRequiredBy(info.ID, requiredBy)
		}
		switch {
		case existing.Provider != p.Name() && replace:
			r.log("switching %s from %s to %s", info.ID, existing.Provider, p.Name())
		case existing.Provider != p.Name():
			aliased := r.Lock.Mods[info.ID]
			setAlias(&aliased, p.Name(), proj.ID)
			r.Lock.Mods[info.ID] = aliased
			r.log("keeping %s %s from %s (%s project %s recorded as an alias)", info.ID, existing.VersionNumber, existing.Provider, p.Name(), proj.ID)
			return info.ID, prior, nil
		case fmt.Sprint(existing.Version) != v.ID:
			r.log("keeping %s %s already in lock", info.ID, existing.VersionNumber)
			return info.ID, prior, nil
		default:
			return info.ID, prior, nil
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
	r.Lock.Mods[info.ID] = entry
	return info.ID, prior, nil
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
		dproj, err := p.Project(ctx, d.ProjectID)
		if err != nil {
			return err
		}
		if dv == nil {
			dv, err = r.pick(ctx, p, dproj, "", channel)
			if err != nil {
				return fmt.Errorf("dependency of %s: %w", parentID, err)
			}
		}
		id, _, err := r.place(ctx, p, dproj, dv, parentID, "", channel, false)
		if err != nil {
			return err
		}
		if err := r.addDeps(ctx, p, dv, id, channel, visited); err != nil {
			return err
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func (r *Resolver) Install(ctx context.Context) ([]string, []string, error) {
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
	ids := make([]string, 0, len(r.Lock.Mods))
	for id := range r.Lock.Mods {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var fetched []string
	var missing []string
	for _, id := range ids {
		m := r.Lock.Mods[id]
		if r.Cache.Has(m.Sha512) {
			continue
		}
		if m.URL == nil {
			missing = append(missing, fmt.Sprintf("%s: download %s from %s and place it in %s/", id, m.Filename, m.Page, DownloadsDir))
			continue
		}
		r.log("downloading %s %s", id, m.VersionNumber)
		_, err := r.Cache.Ensure(ctx, r.Fetch, *m.URL, m.Sha512)
		if errors.Is(err, fetch.ErrForbidden) {
			missing = append(missing, fmt.Sprintf("%s: download forbidden; download %s from %s and place it in %s/", id, m.Filename, pageFor(m), DownloadsDir))
			continue
		}
		if err != nil {
			return fetched, warnings, err
		}
		fetched = append(fetched, id)
	}
	if len(missing) > 0 {
		e := out.Errorf("missing-files", "%d mod(s) need a manual download:\n  %s", len(missing), strings.Join(missing, "\n  "))
		e.Items = missing
		return fetched, warnings, e
	}
	return fetched, warnings, nil
}

func (r *Resolver) lockHas(sha512 string) bool {
	for _, m := range r.Lock.Mods {
		if m.Sha512 == sha512 {
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
	}
	return *m.URL
}
