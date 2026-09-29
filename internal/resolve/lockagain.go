package resolve

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// LockAgain looks each named entry up again from its provider at its locked version and rewrites
// the file it locks, leaving every other entry as it is. A hosted modpack is locked again at its
// version instead, which lays the mods it brings again too.
func (r *Resolver) LockAgain(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := r.lockAgain(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) lockAgain(ctx context.Context, key string) error {
	if r.Manifest.IsLocalFile(key) {
		return refuseLocalFile(key)
	}
	if m, ok := r.Lock.Mods[key]; ok {
		return r.lockModAgain(ctx, key, m)
	}
	if kind, ok := r.packKind(key); ok {
		if pk, locked := r.Lock.Packs(kind)[key]; locked {
			return r.lockPackAgain(ctx, key, kind, pk)
		}
	}
	if pinned, ok := r.Lock.Modpacks[key]; ok {
		return r.lockModpackAgain(ctx, key, pinned)
	}
	e := out.Errorf("mod-not-found", "%s is not in the lock", key)
	e.Candidates, e.Given = r.lockedKeys(), key
	return e
}

func refuseLocalFile(key string) error {
	return out.Errorf("local-file", "%s is a local file; there is no provider version to look up", key)
}

// refuseProvided turns away an entry the modpack pack brings, since the pack lays it again from its
// own archive or lock and would undo a rewrite.
func (r *Resolver) refuseProvided(key, pack string) error {
	if r.Manifest.Requires[pack].IsHosted() {
		return providedBy(key, pack, fmt.Sprintf("run `shulker lock %s` to lock the modpack again", pack))
	}
	return providedBy(key, pack, fmt.Sprintf("%s comes from modpack %s's own lock, which its author has to fix", key, pack))
}

func (r *Resolver) lockModAgain(ctx context.Context, key string, m lock.Mod) error {
	if m.File != "" {
		return refuseLocalFile(key)
	}
	if m.Modpack != "" {
		return r.refuseProvided(key, m.Modpack)
	}
	slug := m.Slug
	if slug == "" {
		slug = key
	}
	p, proj, v, err := r.lockedVersion(ctx, key, manifest.TypeMod, m.Provider, m.Project, slug, m.Version)
	if err != nil {
		return err
	}
	got, err := r.fetchFrom(ctx, p, proj, v)
	if err != nil {
		return err
	}
	m.Filename, m.URL, m.Page, m.Sha512, m.Sha1, m.Size = v.File.Filename, got.url, got.page, got.sha512, "", v.File.Size
	r.Lock.Mods[key] = m
	return nil
}

func (r *Resolver) lockPackAgain(ctx context.Context, key, kind string, pk lock.Pack) error {
	if pk.File != "" {
		return refuseLocalFile(key)
	}
	if pk.Modpack != "" {
		return r.refuseProvided(key, pk.Modpack)
	}
	p, proj, v, err := r.lockedVersion(ctx, key, kind, pk.Provider, pk.Project, key, pk.Version)
	if err != nil {
		return err
	}
	got, err := r.fetchFrom(ctx, p, proj, v)
	if err != nil {
		return err
	}
	sha1 := v.File.Sha1
	if sha1 == "" {
		if sha1, err = fsutil.SHA1(got.path); err != nil {
			return err
		}
	}
	pk.ProviderFilename, pk.URL, pk.Page, pk.Sha512, pk.Sha1, pk.Size = v.File.Filename, got.url, got.page, got.sha512, sha1, v.File.Size
	r.Lock.Packs(kind)[key] = pk
	return nil
}

// lockModpackAgain locks a hosted modpack at its locked version as a pin would, and keeps the
// manifest's own pin and channel, since the pin is only how the version is asked for.
func (r *Resolver) lockModpackAgain(ctx context.Context, key string, pinned lock.Modpack) error {
	entry, listed := r.Manifest.Requires[key]
	if listed && entry.File != "" {
		return refuseLocalFile(key)
	}
	if !listed || !entry.IsHosted() {
		return out.Errorf("not-on-provider", "modpack %s comes from its source, not a provider; there is no provider version to look up", key)
	}
	if pinned.Version == "" {
		return unlockedModpack(key)
	}
	again := entry
	again.Pin = pinned.Version
	err := r.lockModpack(ctx, key, again)
	if relisted, ok := r.Manifest.Requires[key]; ok {
		relisted.Pin, relisted.Channel = entry.Pin, entry.Channel
		r.Manifest.Requires[key] = relisted
	}
	return versionGone(err, key)
}

// lockedVersion is the version a lock entry names, read from its provider by id.
func (r *Resolver) lockedVersion(ctx context.Context, key, kind, providerName, project, slug, version string) (provider.Provider, *provider.Project, *provider.Version, error) {
	p, err := r.provider(providerName)
	if err != nil {
		return nil, nil, nil, err
	}
	proj := &provider.Project{ID: project, Slug: slug}
	v, err := pinnedVersion(ctx, p, proj, kind, version)
	if err != nil {
		return nil, nil, nil, versionGone(err, key)
	}
	return p, proj, v, nil
}

// versionGone points a locked version the provider no longer has at update, which moves key to one
// it still has.
func versionGone(err error, key string) error {
	var e *out.Error
	if errors.As(err, &e) && e.Code == "version-not-found" {
		e.Help = fmt.Sprintf("run `shulker update %s` to move to a version it still has", key)
	}
	return err
}

func (r *Resolver) lockedKeys() []string {
	keys := slices.Collect(maps.Keys(r.Lock.Mods))
	keys = slices.AppendSeq(keys, maps.Keys(r.lockedPacks()))
	keys = slices.AppendSeq(keys, maps.Keys(r.Lock.Modpacks))
	slices.Sort(keys)
	return keys
}
