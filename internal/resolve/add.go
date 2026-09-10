package resolve

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/andrewmast/shulker/internal/cache"
	"github.com/andrewmast/shulker/internal/fetch"
	"github.com/andrewmast/shulker/internal/jarmeta"
	"github.com/andrewmast/shulker/internal/lock"
	"github.com/andrewmast/shulker/internal/manifest"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/andrewmast/shulker/internal/provider"
)

type Resolver struct {
	Manifest  *manifest.Manifest
	Lock      *lock.Lock
	Providers map[string]provider.Provider
	Cache     *cache.Cache
	Fetch     *fetch.Client
	Log       func(format string, args ...any)
}

type AddOptions struct {
	Side     string
	Channel  string
	Pin      string
	Provider string
}

type Added struct {
	ID            string   `json:"id"`
	Provider      string   `json:"provider"`
	Project       string   `json:"project"`
	VersionNumber string   `json:"versionNumber"`
	Filename      string   `json:"filename"`
	Side          string   `json:"side"`
	Dependencies  []string `json:"dependencies"`
	AlreadyLocked bool     `json:"alreadyLocked"`
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

func (r *Resolver) Add(ctx context.Context, slug string, opts AddOptions) (*Added, error) {
	p, err := r.provider(opts.Provider)
	if err != nil {
		return nil, err
	}
	proj, err := p.Project(ctx, slug)
	if err != nil {
		return nil, err
	}
	v, err := r.pick(ctx, p, proj, opts.Pin, opts.Channel)
	if err != nil {
		return nil, err
	}
	id, existed, err := r.place(ctx, p, proj, v, "", opts.Side)
	if err != nil {
		return nil, err
	}
	added := &Added{ID: id, Provider: p.Name(), Project: proj.ID, VersionNumber: v.Number, Filename: v.File.Filename, Side: r.Lock.Mods[id].Side, Dependencies: []string{}, AlreadyLocked: existed}
	visited := map[string]bool{proj.ID: true}
	if err := r.addDeps(ctx, p, v, id, opts.Channel, added, visited); err != nil {
		return nil, err
	}
	sort.Strings(added.Dependencies)
	entry := manifest.Mod{Side: opts.Side}
	if opts.Channel != "" && opts.Channel != "release" {
		entry.Channel = opts.Channel
	}
	if opts.Pin != "" {
		entry.Pin = opts.Pin
	}
	if proj.Slug != id || p.Name() != "modrinth" {
		entry.Project = proj.ID
	}
	if opts.Provider != "" && opts.Provider != r.Manifest.ProviderOrder()[0] {
		entry.Provider = opts.Provider
	}
	r.Manifest.Mods[id] = entry
	return added, nil
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
	versions, err := p.Versions(ctx, proj.ID, r.Lock.Minecraft, r.Lock.Loader.Type)
	if err != nil {
		return nil, err
	}
	v, ok := provider.Newest(versions, channel)
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

func (r *Resolver) place(ctx context.Context, p provider.Provider, proj *provider.Project, v *provider.Version, requiredBy, sideOverride string) (string, bool, error) {
	r.log("fetching %s %s", proj.Slug, v.Number)
	path, err := r.Cache.Ensure(ctx, r.Fetch, v.File.URL, v.File.Sha512)
	if err != nil {
		return "", false, err
	}
	info, err := jarmeta.Read(path)
	if err != nil {
		return "", false, err
	}
	if existing, ok := r.Lock.Mods[info.ID]; ok {
		if requiredBy != "" {
			r.Lock.AddRequiredBy(info.ID, requiredBy)
		}
		if existing.Sha512 != v.File.Sha512 {
			r.log("keeping %s %s already in lock", info.ID, existing.VersionNumber)
		}
		return info.ID, true, nil
	}
	side := proj.Side
	if sideOverride != "" {
		side = sideOverride
	}
	url := v.File.URL
	entry := lock.Mod{
		Provider:      p.Name(),
		Project:       proj.ID,
		Version:       v.ID,
		VersionNumber: v.Number,
		Filename:      v.File.Filename,
		URL:           &url,
		Sha512:        v.File.Sha512,
		Side:          side,
		RequiredBy:    []string{},
	}
	if requiredBy != "" {
		entry.RequiredBy = []string{requiredBy}
	}
	r.Lock.Mods[info.ID] = entry
	return info.ID, false, nil
}

func (r *Resolver) addDeps(ctx context.Context, p provider.Provider, v *provider.Version, parentID, channel string, added *Added, visited map[string]bool) error {
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
		id, _, err := r.place(ctx, p, dproj, dv, parentID, "")
		if err != nil {
			return err
		}
		if !contains(added.Dependencies, id) && id != added.ID {
			added.Dependencies = append(added.Dependencies, id)
		}
		if err := r.addDeps(ctx, p, dv, id, channel, added, visited); err != nil {
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

func (r *Resolver) Install(ctx context.Context) ([]string, error) {
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
			missing = append(missing, fmt.Sprintf("%s: download %s from %s and place it in downloads/", id, m.Filename, m.Page))
			continue
		}
		r.log("downloading %s %s", id, m.VersionNumber)
		if _, err := r.Cache.Ensure(ctx, r.Fetch, *m.URL, m.Sha512); err != nil {
			return fetched, err
		}
		fetched = append(fetched, id)
	}
	if len(missing) > 0 {
		e := out.Errorf("missing-files", "%d mod(s) need a manual download:\n  %s", len(missing), strings.Join(missing, "\n  "))
		e.Candidates = missing
		return fetched, e
	}
	return fetched, nil
}
