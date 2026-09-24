package resolve

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// A hosted modpack is a modpack on a provider: it picks its version like a mod, and that
// version's archive is consumed the way a local one is.

// addModpack writes a hosted modpack's entry, provider and project both named, and has LockModpack
// lock it.
func (r *Resolver) addModpack(ctx context.Context, p provider.Provider, proj *provider.Project, opts AddOptions) error {
	if opts.Side != "" {
		return out.Errorf("usage", "--side doesn't apply to a modpack: its mods carry their own sides")
	}
	key := opts.As
	if key == "" {
		key = proj.Slug
	}
	if !manifest.IsValidKey(key) {
		e := out.Errorf("usage", "%s can't be a requires key", key)
		e.Help = "pass `--as <key>` to give this modpack one"
		return e
	}
	if held, taken := r.Manifest.Requires[key]; taken {
		return manifest.KeyTaken(key, held.Kind(), manifest.TypeModpack)
	}
	project := proj.ID
	for _, name := range sortedKeys(r.Manifest.Modpacks()) {
		existing := r.Manifest.Requires[name]
		if pinned, ok := r.Lock.Modpacks[name]; ok && existing.IsHosted() && pinned.Provider == p.Name() && pinned.Project == project {
			return out.Errorf("modpack-exists", "modpack %s is already in the manifest as %s", proj.Slug, name)
		}
	}
	entry := manifest.Require{Type: manifest.TypeModpack, Provider: p.Name(), Project: project}
	if opts.Channel != "" && opts.Channel != "release" {
		entry.Channel = opts.Channel
	}
	if opts.Pin != "" {
		entry.Pin = opts.Pin
	}
	return r.lockModpack(ctx, key, entry)
}

func (r *Resolver) lockModpack(ctx context.Context, key string, entry manifest.Require) error {
	if r.LockModpack == nil {
		return out.Errorf("requires-unsupported", "modpack %s is on a provider, which this command can't lock", key)
	}
	return r.LockModpack(ctx, key, entry)
}

// pinModpack holds a hosted modpack at version, the locked one when version is empty, and locks it
// there.
func (r *Resolver) pinModpack(ctx context.Context, key, version string) (string, error) {
	entry := r.Manifest.Requires[key]
	locked := r.Lock.Modpacks[key]
	if version == "" {
		if locked.Version == "" {
			return "", unlockedModpack(key)
		}
		version = locked.Version
	}
	entry.Pin = version
	return version, r.lockModpack(ctx, key, entry)
}

// unpinModpack lets a pinned hosted modpack move again, and locks it at its newest version.
func (r *Resolver) unpinModpack(ctx context.Context, key string) error {
	entry := r.Manifest.Requires[key]
	if entry.Pin == "" {
		return out.Errorf("not-pinned", "%s is not pinned", key)
	}
	entry.Pin = ""
	return r.lockModpack(ctx, key, entry)
}

func (r *Resolver) modpackProvider(entry manifest.Require, locked lock.Modpack) string {
	switch {
	case locked.Provider != "":
		return locked.Provider
	case entry.Provider != "":
		return entry.Provider
	}
	return r.Manifest.ProviderOrder()[0]
}

func unlockedModpack(key string) *out.Error {
	e := out.Errorf("modpack-unlocked", "modpack %s has no version in the lock", key)
	e.Help = "run `shulker lock`"
	return e
}

// ObtainModpack picks the hosted modpack entry's version and puts its archive in the cache,
// returning the lock entry that names both. A CurseForge archive's sha512 is only known once it is
// downloaded, as a CurseForge mod's is.
func (r *Resolver) ObtainModpack(ctx context.Context, name string, entry manifest.Require) (lock.Modpack, error) {
	slug := name
	if entry.Project != "" {
		slug = entry.Project
	}
	p, proj, err := r.lookup(ctx, slug, entry.Provider, manifest.TypeModpack)
	if err != nil {
		return lock.Modpack{}, err
	}
	if proj.Type != "" && proj.Type != manifest.TypeModpack {
		e := out.Errorf("type-mismatch", "requires.%s is a modpack, and %s on %s is a %s", name, proj.Slug, p.Name(), proj.Type)
		e.Help = fmt.Sprintf("set requires.%s.type to %s", name, proj.Type)
		return lock.Modpack{}, e
	}
	v, err := pickVersion(ctx, p, proj, r.queryFor(manifest.TypeModpack, p.Name()), entry.Pin, entry.Channel)
	if err != nil {
		return lock.Modpack{}, err
	}
	channel := r.relistedChannel(name, entry, v)
	r.log("fetching modpack %s %s", proj.Slug, v.Number)
	got, err := r.obtain(ctx, proj, v)
	if err != nil {
		return lock.Modpack{}, prefixed("modpack "+name, err)
	}
	size := v.File.Size
	if size == 0 {
		st, err := os.Stat(r.Cache.Object(got.sha512))
		if err != nil {
			return lock.Modpack{}, err
		}
		size = st.Size()
	}
	return lock.Modpack{
		Provider:      p.Name(),
		Project:       proj.ID,
		Version:       v.ID,
		VersionNumber: v.Number,
		Channel:       channelLabel(channel),
		URL:           got.url,
		Page:          got.page,
		Filename:      v.File.Filename,
		Sha512:        got.sha512,
		Size:          size,
	}, nil
}

// modpackPlatform is what a hosted modpack's version has to fit: the Minecraft version and loader
// the project sets, each left open when the project sets none, since the pack then supplies it.
func (r *Resolver) modpackPlatform() (game, loaderName string) {
	if r.Lock == nil {
		return "", ""
	}
	if r.Manifest.Minecraft != "" {
		game = r.Lock.Minecraft
	}
	if r.Manifest.Loader.Type != "" {
		loaderName = r.Lock.Loader.Type
	}
	return game, loaderName
}

func modpackLoaders(name string) []string {
	if name == "" {
		return nil
	}
	return loader.ProviderLoaders(name)
}

func platformLabel(game, loaderName string) string {
	var parts []string
	if game != "" {
		parts = append(parts, " for Minecraft "+game)
	}
	if loaderName != "" {
		parts = append(parts, " with "+loaderName)
	}
	return strings.Join(parts, "")
}

// outdatedModpacks reports the hosted modpacks a newer version is published for, without changing
// the lock.
func (r *Resolver) outdatedModpacks(ctx context.Context, ids []string) ([]Outdated, error) {
	var res []Outdated
	for _, key := range sortedKeys(r.Manifest.Modpacks()) {
		entry := r.Manifest.Requires[key]
		locked, ok := r.Lock.Modpacks[key]
		if !entry.IsHosted() || !ok || locked.Provider == "" || (len(ids) > 0 && !slices.Contains(ids, key)) {
			continue
		}
		p, err := r.provider(locked.Provider)
		if err != nil {
			return nil, err
		}
		newest, newer, err := newerThan(ctx, p, locked.Project, r.queryFor(manifest.TypeModpack, p.Name()), entry.Channel, locked.Version)
		if err != nil {
			return nil, err
		}
		if !newer {
			continue
		}
		res = append(res, Outdated{ID: key, Current: locked.VersionNumber, Latest: newest.Number, Pinned: entry.Pin != "", Modpack: true})
	}
	return res, nil
}

// splitHosted takes the hosted modpacks out of a command's arguments.
func (r *Resolver) splitHosted(ids []string) (rest, hosted []string) {
	for _, id := range ids {
		if entry, ok := r.Manifest.Requires[id]; ok && entry.IsHosted() {
			hosted = append(hosted, id)
			continue
		}
		rest = append(rest, id)
	}
	return rest, hosted
}
