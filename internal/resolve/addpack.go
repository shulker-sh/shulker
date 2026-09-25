package resolve

import (
	"context"
	"os"
	"slices"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

// AddPackSource resolves one modpack source through store and puts it in the manifest under the
// key as names, or the name the pack's own manifest carries. A source already in the manifest, or
// a key another entry holds, is refused.
func (r *Resolver) AddPackSource(ctx context.Context, store *modpack.Store, source, as string, entry manifest.Require) error {
	for _, existing := range r.Manifest.Modpacks() {
		if existing.Source == source && existing.Path == entry.Path {
			return out.Errorf("modpack-exists", "modpack %s is already in the manifest", source)
		}
	}
	loaded, err := store.Resolve(ctx, source, entry)
	if err != nil {
		return err
	}
	key := as
	if key == "" {
		key = loaded.Manifest.Name
	}
	if held, taken := r.Manifest.Requires[key]; taken {
		return manifest.KeyTaken(key, held.Kind(), manifest.TypeModpack)
	}
	loaded.Name = key
	return r.addLoadedPack(ctx, store, source, key, loaded, entry)
}

// AddArchive adds the modpack archive at path, under the key as names or the file's stem. An
// archive outside the project, or in a folder whose files something else owns, is copied into
// manifest.FilesDir, and adding the same file again refreshes that copy and relocks it, keeping
// its locked and auto-update flags. A second key for the same file is refused.
func (r *Resolver) AddArchive(ctx context.Context, store *modpack.Store, path, as string, entry manifest.Require) error {
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return out.Errorf("file-not-found", "%s is not a file", path)
	}
	rel, copied, err := r.ProjectPath(path)
	if err != nil {
		return err
	}
	key := as
	if key == "" {
		key = StemKey(path)
	}
	if !manifest.IsValidKey(key) {
		e := out.Errorf("usage", "%s can't be a requires key", key)
		e.Help = "pass `--as <key>` to give this modpack one"
		return e
	}
	held, taken := r.Manifest.Requires[key]
	isReadded := taken && held.Kind() == manifest.TypeModpack && held.File == rel
	if taken && !isReadded {
		return manifest.KeyTaken(key, held.Kind(), manifest.TypeModpack)
	}
	for name, existing := range r.Manifest.Modpacks() {
		if existing.File == rel && name != key {
			return out.Errorf("modpack-exists", "modpack %s is already in the manifest as %s", rel, name)
		}
	}
	if err := modpack.CheckArchive(key, path); err != nil {
		return err
	}
	entry.Source, entry.Type, entry.File = "", manifest.TypeModpack, rel
	if isReadded {
		if entry.Locked == nil {
			entry.Locked = held.Locked
		}
		if entry.AutoUpdate == nil {
			entry.AutoUpdate = held.AutoUpdate
		}
	}
	if copied {
		if err := r.CopyIn(path, rel, isReadded); err != nil {
			return err
		}
	}
	loaded, err := store.Resolve(ctx, key, entry)
	if err != nil {
		return err
	}
	if isReadded {
		if err := r.RemovePack(key); err != nil {
			return err
		}
	}
	return r.addLoadedPack(ctx, store, key, key, loaded, entry)
}

// LockHosted locks the hosted modpack entry through store and puts it in the manifest under key,
// in place of the modpack already loaded there. The channel its version came from is recorded
// unless it is release.
func (r *Resolver) LockHosted(ctx context.Context, store *modpack.Store, p *project.Project, key string, entry manifest.Require) error {
	loaded, err := store.Resolve(ctx, key, entry)
	if err != nil {
		return err
	}
	if channel := loaded.Pin.Channel; channel != "release" {
		entry.Channel = channel
	}
	i := slices.IndexFunc(r.Packs, func(l *modpack.Loaded) bool { return l.Name == key })
	if i < 0 {
		return r.addLoadedPack(ctx, store, key, key, loaded, entry)
	}
	packs := slices.Clone(r.Packs)
	packs[i] = loaded
	if err := r.RefreshPacks(packs); err != nil {
		return err
	}
	p.ReplacePacks(packs)
	r.Manifest.Requires[key] = entry
	return nil
}

// addLoadedPack adds a resolved modpack under key. One built for another Minecraft than the
// project's is refused as modpack-mismatch, unless AskUnlock says to unlock it, which resolves it
// again as name with its mods resolved here.
func (r *Resolver) addLoadedPack(ctx context.Context, store *modpack.Store, name, key string, loaded *modpack.Loaded, entry manifest.Require) error {
	r.Warnings = append(r.Warnings, scoped(key, loaded.Warnings)...)
	err := r.AddPack(ctx, loaded)
	if r.AskUnlock != nil && unlockAnswers(err, loaded, r.Lock.Minecraft) {
		unlock, askErr := r.AskUnlock(key, r.Lock.Minecraft)
		if askErr != nil {
			return askErr
		}
		if unlock {
			no := false
			entry.Locked = &no
			if loaded, err = store.Resolve(ctx, name, entry); err != nil {
				return err
			}
			loaded.Name = key
			err = r.AddPack(ctx, loaded)
		}
	}
	if err != nil {
		return err
	}
	r.Manifest.Requires[key] = entry
	return nil
}

// unlockAnswers reports whether a modpack refused for its platform is the one refusal unlocking
// answers: locked, and built for another Minecraft than the project's.
func unlockAnswers(err error, l *modpack.Loaded, minecraft string) bool {
	return out.CodeOf(err) == "modpack-mismatch" && l.Kind != modpack.Hosted && l.UsesLock && l.Lock != nil && minecraft != "" && l.Lock.Minecraft != minecraft
}
