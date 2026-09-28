package resolve

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
)

// Resource packs, shaders and datapacks resolve like mods with the jar taken away: there is
// no metadata to read, no dependencies to follow, and nothing else can require
// them. Each is keyed by its requires key, which is also the name the build
// places it under, so a pack enabled in game survives its own updates.

// packSide is where a datapack is placed: its entry's side, or both. The other
// kinds are client-only and record none.
func packSide(kind string, e manifest.Require) string {
	switch {
	case kind != manifest.TypeDatapack:
		return ""
	case e.Side != "":
		return e.Side
	}
	return "both"
}

func (r *Resolver) addPack(ctx context.Context, p provider.Provider, proj *provider.Project, kind string, opts AddOptions) error {
	key := opts.As
	if key == "" {
		key = proj.Slug
	}
	if !manifest.IsValidKey(key) {
		e := out.Errorf("usage", "%s can't be a requires key", proj.Slug)
		e.Help = fmt.Sprintf("pass `--as <key>` to give this %s one", kind)
		return e
	}
	if err := r.packKeyFree(key, kind); err != nil {
		return err
	}
	channel, err := r.lockPack(ctx, p, proj, key, kind, opts.Pin, opts.Channel)
	if err != nil {
		return err
	}
	listed := manifest.Require{Type: kind}
	if channel != "" && channel != "release" {
		listed.Channel = channel
	}
	if opts.Pin != "" {
		listed.Pin = opts.Pin
	}
	r.setSource(&listed, key, p, proj)
	listed.Filename = r.Manifest.Requires[key].Filename
	if listed.Filename == "" {
		listed.Filename = providerPackName(key, kind, r.Lock.Packs(kind)[key].ProviderFilename)
	}
	locked := r.placePack(key, kind, listed)
	if kind == manifest.TypeDatapack {
		listed.Side, listed.ResourcePack = opts.Side, opts.ResourcePack
		locked.Side, locked.ResourcePack = packSide(kind, listed), listed.ResourcePack
	}
	r.Lock.Packs(kind)[key] = locked
	r.Manifest.Requires[key] = listed
	return nil
}

// providerPackName is the provider's file name when a pack can be placed under it and it isn't
// the key's own, since other packs' options.txt and load orders name a pack by that name.
func providerPackName(key, kind, name string) string {
	if ext := manifest.FileExtension(kind); strings.HasSuffix(name, ext) && name != key+ext {
		return name
	}
	return ""
}

// placePack records the name listed places key under in its lock entry, and returns the entry.
func (r *Resolver) placePack(key, kind string, listed manifest.Require) lock.Pack {
	locked := r.Lock.Packs(kind)[key]
	locked.Filename = manifest.PackFilename(key, listed)
	r.Lock.Packs(kind)[key] = locked
	return locked
}

// packKeyFree refuses a key another entry already holds, whatever kind it is:
// requires keys are one namespace, and the key is also a file name.
func (r *Resolver) packKeyFree(key, kind string) error {
	if held, ok := r.Manifest.Requires[key]; ok && held.Kind() != kind {
		return manifest.KeyTaken(key, held.Kind(), kind)
	}
	if _, ok := r.Lock.Mods[key]; ok {
		return manifest.KeyTaken(key, manifest.TypeMod, kind)
	}
	for _, other := range manifest.PackKinds {
		if _, ok := r.Lock.Packs(other)[key]; ok && other != kind {
			return manifest.KeyTaken(key, other, kind)
		}
	}
	return nil
}

// lockedPacks is every pack in the lock, by key. Keys are one namespace, so the
// sections never collide.
func (r *Resolver) lockedPacks() map[string]lock.Pack {
	all := map[string]lock.Pack{}
	for _, kind := range manifest.PackKinds {
		maps.Copy(all, r.Lock.Packs(kind))
	}
	return all
}

// packKind reports which section holds a key, when a pack does. The manifest decides it where the key is listed, so a hand-written entry
// resolves before it has ever been locked.
func (r *Resolver) packKind(key string) (string, bool) {
	if e, listed := r.Manifest.Requires[key]; listed {
		return e.Kind(), manifest.IsPackKind(e.Kind())
	}
	for _, kind := range manifest.PackKinds {
		if _, ok := r.Lock.Packs(kind)[key]; ok {
			return kind, true
		}
	}
	return "", false
}

// removePacks takes the packs out of a remove, leaving the
// ids the mod path handles.
func (r *Resolver) removePacks(ids []string) ([]string, error) {
	var mods []string
	for _, id := range ids {
		kind, isPack := r.packKind(id)
		if !isPack {
			mods = append(mods, id)
			continue
		}
		if _, listed := r.Manifest.Requires[id]; !listed {
			if from := r.Lock.Packs(kind)[id].Modpack; from != "" {
				return nil, providedBy(id, from, "remove the modpack or list it in shulker.json yourself")
			}
		}
		delete(r.Manifest.Requires, id)
		delete(r.Lock.Packs(kind), id)
	}
	return mods, nil
}

// reconcilePacks brings the lock's pack sections in line with shulker.json:
// an entry listed but not locked, or locked against different settings, is
// resolved again, and one the manifest no longer lists is dropped. Entries a
// locked modpack supplied are left alone, because applyLockedPacks owns them.
func (r *Resolver) reconcilePacks(ctx context.Context) error {
	for _, kind := range manifest.PackKinds {
		listed := r.Manifest.Packs(kind)
		section := r.Lock.Packs(kind)
		for _, key := range sortedKeys(section) {
			if _, still := listed[key]; !still && section[key].Modpack == "" {
				delete(section, key)
			}
		}
		for _, key := range sortedKeys(listed) {
			locked, ok := section[key]
			if ok {
				// A new name or side needs no new version, so it is taken without resolving again.
				locked.Filename = manifest.PackFilename(key, listed[key])
				locked.Side = packSide(kind, listed[key])
				locked.ResourcePack = listed[key].ResourcePack
				section[key] = locked
			}
			if ok && len(project.ZipEntryDifferences(r.Dir, key, listed[key], locked)) == 0 {
				r.checkPackFolder(key, kind, listed[key])
				continue
			}
			if err := r.relockPack(ctx, key, kind, listed[key]); err != nil {
				return err
			}
		}
	}
	return nil
}

// relockPack resolves one listed pack from its manifest
// entry, the way relock does for a mod.
func (r *Resolver) relockPack(ctx context.Context, key, kind string, entry manifest.Require) error {
	if entry.File != "" {
		return r.lockFilePack(key, kind, entry)
	}
	slug := key
	if entry.Project != "" {
		slug = entry.Project
	}
	p, proj, err := r.lookup(ctx, slug, entry.Provider, kind)
	if err != nil {
		return err
	}
	v, err := pickVersion(ctx, p, proj, r.queryFor(kind, p.Name()), entry.Pin, entry.Channel)
	if err != nil {
		return channelSetting(err, key)
	}
	return r.lockPackVersion(ctx, p, proj, v, key, kind, r.relistedChannel(key, entry, v))
}

// checkPackFilenames refuses two packs placed under one name, compared without
// case since macOS and Windows see one file there.
func (r *Resolver) checkPackFilenames() error {
	placed := map[string]string{}
	for _, kind := range manifest.PackKinds {
		section := r.Lock.Packs(kind)
		for _, key := range sortedKeys(section) {
			paths := []string{r.Lock.PackPath(kind, section[key], "client", "", r.Manifest.Integrations)}
			if section[key].ResourcePack {
				paths = append(paths, section[key].Path(manifest.TypeResourcePack))
			}
			for _, path := range paths {
				if other, ok := placed[strings.ToLower(path)]; ok {
					return out.Errorf("pack-filename-taken", "%s and %s are both placed as %s", other, key, path)
				}
				placed[strings.ToLower(path)] = key
			}
		}
	}
	return nil
}

// lockPack picks the pack's version, fetches it into the cache and records it in
// the lock under key, returning the channel it accepted: channel, widened for a pin.
func (r *Resolver) lockPack(ctx context.Context, p provider.Provider, proj *provider.Project, key, kind, pin, channel string) (string, error) {
	v, err := pickVersion(ctx, p, proj, r.queryFor(kind, p.Name()), pin, channel)
	if err != nil {
		return "", err
	}
	if pin != "" {
		channel = r.pinnedChannel(key, v, channel)
	}
	return channel, r.lockPackVersion(ctx, p, proj, v, key, kind, channel)
}

func (r *Resolver) lockPackVersion(ctx context.Context, p provider.Provider, proj *provider.Project, v *provider.Version, key, kind, channel string) error {
	r.fetching(proj.Slug, v.Number)
	got, err := r.obtainFrom(ctx, p, proj, v)
	if err != nil {
		return err
	}
	sha1 := v.File.Sha1
	if sha1 == "" {
		if sha1, err = fsutil.SHA1(got.path); err != nil {
			return err
		}
	}
	locked := lock.Pack{
		Provider:         p.Name(),
		Project:          proj.ID,
		Version:          v.ID,
		VersionNumber:    v.Number,
		Filename:         manifest.PackFilename(key, r.Manifest.Requires[key]),
		ProviderFilename: v.File.Filename,
		URL:              got.url,
		Page:             got.page,
		Sha512:           got.sha512,
		Sha1:             sha1,
		Size:             v.File.Size,
		Channel:          channelLabel(channel),
		Side:             packSide(kind, r.Manifest.Requires[key]),
		ResourcePack:     r.Manifest.Requires[key].ResourcePack,
	}
	if kind == manifest.TypeShader {
		locked.Loaders = v.Loaders
	}
	r.Lock.Packs(kind)[key] = locked
	return nil
}

// splitPackTargets takes the packs out of a command's
// arguments, so the mod path never sees a key it would call unknown.
func (r *Resolver) splitPackTargets(ids []string) (mods, packs []string, err error) {
	for _, id := range ids {
		kind, isPack := r.packKind(id)
		if !isPack {
			mods = append(mods, id)
			continue
		}
		if _, listed := r.Manifest.Requires[id]; !listed {
			if from := r.Lock.Packs(kind)[id].Modpack; from != "" {
				return nil, nil, providedBy(id, from, "update the modpack, or list it in shulker.json to resolve it here")
			}
		}
		packs = append(packs, id)
	}
	return mods, packs, nil
}

// updatePacks re-resolves listed packs to the newest
// version their channel allows. No ids means every one the manifest lists.
func (r *Resolver) updatePacks(ctx context.Context, ids []string) error {
	for _, kind := range manifest.PackKinds {
		listed := r.Manifest.Packs(kind)
		for _, key := range sortedKeys(listed) {
			if len(ids) > 0 && !slices.Contains(ids, key) {
				continue
			}
			if err := r.relockPack(ctx, key, kind, listed[key]); err != nil {
				return err
			}
		}
	}
	return nil
}

// outdatedPacks reports the listed packs a newer version is
// published for, without changing the lock.
func (r *Resolver) outdatedPacks(ctx context.Context, ids []string) ([]Outdated, error) {
	var res []Outdated
	for _, kind := range manifest.PackKinds {
		listed := r.Manifest.Packs(kind)
		section := r.Lock.Packs(kind)
		for _, key := range sortedKeys(listed) {
			locked, ok := section[key]
			if !ok || locked.File != "" || (len(ids) > 0 && !slices.Contains(ids, key)) {
				continue
			}
			p, err := r.provider(locked.Provider)
			if err != nil {
				return nil, err
			}
			newest, newer, err := newerThan(ctx, p, locked.Project, r.queryFor(kind, p.Name()), listed[key].Channel, locked.Version)
			if err != nil {
				return nil, err
			}
			if !newer {
				continue
			}
			res = append(res, Outdated{ID: key, Current: locked.VersionNumber, Latest: newest.Number, Pinned: listed[key].Pin != ""})
		}
	}
	return res, nil
}

// downloadable is one file the lock names, whichever section holds it.
type downloadable struct {
	id       string
	file     string
	modpack  string
	filename string
	provider string
	// host is the provider's title, for messages about its downloads.
	host   string
	sha512 string
	// sha1 is a pending mod's one hash, until its manual download fills in the rest.
	sha1 string
	url  *string
	page string
	size int64
	side string
}

// lockFiles is everything install has to put in the cache, mods first and then
// the zips, each section in key order.
func (r *Resolver) lockFiles() []downloadable {
	files := r.modFiles()
	for _, kind := range manifest.PackKinds {
		section := r.Lock.Packs(kind)
		for _, key := range sortedKeys(section) {
			p := section[key]
			side := cmp.Or(p.Side, "client")
			files = append(files, downloadable{id: key, file: p.File, modpack: p.Modpack, filename: p.ProviderFilename, provider: p.Provider, host: r.Providers.Title(p.Provider), sha512: p.Sha512, url: p.URL, page: packPage(p), size: p.Size, side: side})
		}
	}
	return files
}

func (r *Resolver) modFiles() []downloadable {
	var files []downloadable
	for _, id := range sortedKeys(r.Lock.Mods) {
		m := r.Lock.Mods[id]
		files = append(files, downloadable{id: id, file: m.File, modpack: m.Modpack, filename: m.Filename, provider: m.Provider, sha512: m.Sha512, sha1: m.Sha1, url: m.URL, page: r.pageFor(m), host: r.Providers.Title(m.Provider), size: m.Size, side: m.Side})
	}
	return files
}

func packPage(p lock.Pack) string {
	switch {
	case p.Page != "":
		return p.Page
	case p.URL != nil:
		return *p.URL
	}
	return ""
}

// addKind settles what is being added: what --type said, what the provider says
// the project is, and a mod when neither knows.
func addKind(asked string, proj *provider.Project, slug string) (string, error) {
	if asked == manifest.TypeDatapack && proj.Datapack {
		return asked, nil
	}
	if asked != "" && proj.Type != "" && asked != proj.Type {
		e := out.Errorf("type-mismatch", "%s is a %s, not a %s", slug, proj.Type, asked)
		e.Candidates, e.Given, e.Flag = []string{proj.Type}, asked, "--type"
		return "", e
	}
	switch {
	case asked != "":
		return asked, nil
	case proj.Type != "":
		return proj.Type, nil
	}
	return manifest.TypeMod, nil
}
