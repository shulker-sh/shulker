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

// packTags is how each provider files the pack kinds. Modrinth tags resource pack
// versions with the minecraft loader, datapacks with the datapack loader and
// shaders with the shader loader they target; CurseForge has no loader tag for
// resource packs or datapacks, and keeps shader loaders in gameVersions, where
// only Iris and OptiFine appear.
func packTags(providerName, kind string) []string {
	if kind == manifest.TypeShader {
		if providerName == "curseforge" {
			return []string{"iris", "optifine"}
		}
		return []string{"iris", "oculus", "canvas", "vanilla"}
	}
	if providerName == "curseforge" {
		return nil
	}
	if kind == manifest.TypeDatapack {
		return []string{provider.DatapackLoader}
	}
	return []string{"minecraft"}
}

func (r *Resolver) packSection(kind string) map[string]lock.Pack { return r.Lock.Packs(kind) }

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

// shaderLoaders are the shader mods a provider tagged a version for. Modrinth
// files them as loaders; CurseForge keeps them among the game versions, and tags
// OptiFine rather than Oculus, whose OptiFine-format packs Iris and Oculus both read.
func shaderLoaders(v *provider.Version) []string {
	tags := map[string]bool{}
	for _, tag := range slices.Concat(v.Loaders, v.GameVersions) {
		tags[strings.ToLower(tag)] = true
	}
	var loaders []string
	for _, name := range []string{"iris", "oculus", "canvas", "vanilla"} {
		if tags[name] || (tags["optifine"] && (name == "iris" || name == "oculus")) {
			loaders = append(loaders, name)
		}
	}
	return loaders
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
		listed.Pin = lockID(p.Name(), opts.Pin)
	}
	r.setSource(&listed, key, p, proj)
	locked := r.packSection(kind)[key]
	listed.Filename = r.Manifest.Requires[key].Filename
	if name := locked.ProviderFilename; listed.Filename == "" && strings.HasSuffix(name, ".zip") && name != key+".zip" {
		listed.Filename = name
	}
	locked.Filename = manifest.PackFilename(key, listed)
	if kind == manifest.TypeDatapack {
		listed.Side, listed.ResourcePack = opts.Side, opts.ResourcePack
		locked.Side, locked.ResourcePack = packSide(kind, listed), listed.ResourcePack
	}
	r.packSection(kind)[key] = locked
	r.Manifest.Requires[key] = listed
	return nil
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
		if _, ok := r.packSection(other)[key]; ok && other != kind {
			return manifest.KeyTaken(key, other, kind)
		}
	}
	return nil
}

func (r *Resolver) pickPack(ctx context.Context, p provider.Provider, proj *provider.Project, kind, pin, channel string) (*provider.Version, error) {
	if pin != "" {
		return pinnedVersion(ctx, p, proj, pin)
	}
	versions, err := p.Versions(ctx, proj.ID, r.Lock.Minecraft, packTags(p.Name(), kind))
	if err != nil {
		return nil, err
	}
	v, ok := provider.Newest(versions, channel, "")
	if !ok {
		e := out.Errorf("no-compatible-version", "%s has no %s version for Minecraft %s", proj.Slug, channelLabel(channel), r.Lock.Minecraft)
		e.Candidates, e.Pass = otherChannels(versions)
		e.Flag = "--channel"
		return nil, e
	}
	return &v, nil
}

// lockedPacks is every pack in the lock, by key. Keys are one namespace, so the
// sections never collide.
func (r *Resolver) lockedPacks() map[string]lock.Pack {
	all := map[string]lock.Pack{}
	for _, kind := range manifest.PackKinds {
		maps.Copy(all, r.packSection(kind))
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
		if _, ok := r.packSection(kind)[key]; ok {
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
			if from := r.packSection(kind)[id].Modpack; from != "" {
				return nil, providedBy(id, from, "remove the modpack or list it in shulker.json yourself")
			}
		}
		delete(r.Manifest.Requires, id)
		delete(r.packSection(kind), id)
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
		section := r.packSection(kind)
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
	if entry.Project != nil {
		slug = fmt.Sprint(entry.Project)
	}
	p, proj, err := r.lookup(ctx, slug, entry.Provider, kind)
	if err != nil {
		return err
	}
	pin := ""
	if entry.Pin != nil {
		pin = fmt.Sprint(entry.Pin)
	}
	channel, err := r.lockPack(ctx, p, proj, key, kind, pin, entry.Channel)
	if err != nil {
		return err
	}
	if channel != entry.Channel {
		r.listChannel(key, channel)
	}
	return nil
}

// checkPackFilenames refuses two packs placed under one name, compared without
// case since macOS and Windows see one file there.
func (r *Resolver) checkPackFilenames() error {
	placed := map[string]string{}
	for _, kind := range manifest.PackKinds {
		section := r.packSection(kind)
		for _, key := range sortedKeys(section) {
			paths := []string{r.Lock.PackPath(kind, section[key], "client", "")}
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
	v, err := r.pickPack(ctx, p, proj, kind, pin, channel)
	if err != nil {
		return "", err
	}
	if pin != "" {
		channel = r.pinnedChannel(key, v, channel)
	}
	return channel, r.lockPackVersion(ctx, p, proj, v, key, kind, channel)
}

func (r *Resolver) lockPackVersion(ctx context.Context, p provider.Provider, proj *provider.Project, v *provider.Version, key, kind, channel string) error {
	r.log("fetching %s %s", proj.Slug, v.Number)
	got, err := r.obtain(ctx, proj, v)
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
		Project:          lockID(p.Name(), proj.ID),
		Version:          lockID(p.Name(), v.ID),
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
		locked.Loaders = shaderLoaders(v)
	}
	r.packSection(kind)[key] = locked
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
			if from := r.packSection(kind)[id].Modpack; from != "" {
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
		section := r.packSection(kind)
		for _, key := range sortedKeys(listed) {
			locked, ok := section[key]
			if !ok || locked.File != "" || (len(ids) > 0 && !slices.Contains(ids, key)) {
				continue
			}
			p, err := r.provider(locked.Provider)
			if err != nil {
				return nil, err
			}
			versions, err := p.Versions(ctx, fmt.Sprint(locked.Project), r.Lock.Minecraft, packTags(p.Name(), kind))
			if err != nil {
				return nil, err
			}
			newest, ok := provider.Newest(versions, listed[key].Channel, "")
			if !ok || newest.ID == fmt.Sprint(locked.Version) {
				continue
			}
			res = append(res, Outdated{ID: key, Current: locked.VersionNumber, Latest: newest.Number, Pinned: listed[key].Pin != nil})
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
	sha512   string
	url      *string
	page     string
	size     int64
	side     string
}

// lockFiles is everything install has to put in the cache, mods first and then
// the zips, each section in key order.
func (r *Resolver) lockFiles() []downloadable {
	var files []downloadable
	for _, id := range sortedKeys(r.Lock.Mods) {
		m := r.Lock.Mods[id]
		files = append(files, downloadable{id: id, file: m.File, modpack: m.Modpack, filename: m.Filename, provider: m.Provider, sha512: m.Sha512, url: m.URL, page: pageFor(m), size: m.Size, side: m.Side})
	}
	for _, kind := range manifest.PackKinds {
		section := r.packSection(kind)
		for _, key := range sortedKeys(section) {
			p := section[key]
			side := cmp.Or(p.Side, "client")
			files = append(files, downloadable{id: key, file: p.File, modpack: p.Modpack, filename: p.ProviderFilename, provider: p.Provider, sha512: p.Sha512, url: p.URL, page: packPage(p), size: p.Size, side: side})
		}
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
