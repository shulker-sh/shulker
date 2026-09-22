package resolve

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
)

// Resource packs and shaders resolve like mods with the jar taken away: there is
// no metadata to read, no dependencies to follow, and nothing else can require
// them. Each is keyed by its requires key, which is also the name the build
// places it under, so a pack enabled in game survives its own updates.

// packTags is how each provider files the two kinds. Modrinth tags resource pack
// versions with the minecraft loader and shaders with the shader loader they
// target; CurseForge has no loader tag for resource packs, and keeps shader
// loaders in gameVersions, where only Iris and OptiFine appear.
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
	return []string{"minecraft"}
}

func (r *Resolver) packSection(kind string) map[string]lock.Pack {
	if kind == manifest.TypeShader {
		if r.Lock.Shaders == nil {
			r.Lock.Shaders = map[string]lock.Pack{}
		}
		return r.Lock.Shaders
	}
	if r.Lock.ResourcePacks == nil {
		r.Lock.ResourcePacks = map[string]lock.Pack{}
	}
	return r.Lock.ResourcePacks
}

// shaderLoader decides which shader mod will load a pack. One tagged only for
// vanilla ships core shaders, needs no shader mod at all, and is placed and
// enabled as a resource pack instead.
func (r *Resolver) shaderLoader(v *provider.Version) string {
	if slices.Contains(v.Loaders, "vanilla") && !slices.Contains(v.Loaders, "iris") && !slices.Contains(v.Loaders, "oculus") {
		return "vanilla"
	}
	for _, name := range []string{"iris", "oculus", "canvas"} {
		if _, installed := r.Lock.Mods[name]; installed {
			return name
		}
	}
	return "iris"
}

func (r *Resolver) addPack(ctx context.Context, p provider.Provider, proj *provider.Project, kind string, opts AddOptions) error {
	key := opts.As
	if key == "" {
		key = proj.Slug
	}
	if !manifest.IsValidKey(key) {
		return out.Errorf("usage", "%s can't be a requires key; pass `--as <key>` to give this %s one", proj.Slug, kind)
	}
	if err := r.packKeyFree(key, kind); err != nil {
		return err
	}
	v, err := r.pickPack(ctx, p, proj, kind, opts.Pin, opts.Channel)
	if err != nil {
		return err
	}
	r.log("fetching %s %s", proj.Slug, v.Number)
	got, err := r.obtain(ctx, proj, v)
	if err != nil {
		return err
	}
	entry := lock.Pack{
		Provider:      p.Name(),
		Project:       lockID(p.Name(), proj.ID),
		Version:       lockID(p.Name(), v.ID),
		VersionNumber: v.Number,
		Filename:      v.File.Filename,
		URL:           got.url,
		Page:          got.page,
		Sha512:        got.sha512,
		Size:          v.File.Size,
		Channel:       channelLabel(opts.Channel),
	}
	if kind == manifest.TypeShader {
		entry.Loader = r.shaderLoader(v)
	}
	r.packSection(kind)[key] = entry
	listed := manifest.Require{Type: kind}
	if opts.Channel != "" && opts.Channel != "release" {
		listed.Channel = opts.Channel
	}
	if opts.Pin != "" {
		listed.Pin = lockID(p.Name(), opts.Pin)
	}
	if proj.Slug != key || p.Name() != "modrinth" {
		listed.Project = lockID(p.Name(), proj.ID)
	}
	if p.Name() != r.Manifest.ProviderOrder()[0] {
		listed.Provider = p.Name()
	}
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
	other := manifest.TypeShader
	if kind == manifest.TypeShader {
		other = manifest.TypeResourcePack
	}
	if _, ok := r.packSection(other)[key]; ok {
		return manifest.KeyTaken(key, other, kind)
	}
	return nil
}

func (r *Resolver) pickPack(ctx context.Context, p provider.Provider, proj *provider.Project, kind, pin, channel string) (*provider.Version, error) {
	if pin != "" {
		v, err := p.Version(ctx, pin)
		if errors.Is(err, provider.ErrNotFound) {
			return nil, out.Errorf("version-not-found", "%s has no version %s for %s", p.Name(), pin, proj.Slug)
		}
		if err != nil {
			return nil, err
		}
		if v.ProjectID != proj.ID {
			return nil, out.Errorf("pin-mismatch", "version %s belongs to project %s, not %s", pin, v.ProjectID, proj.Slug)
		}
		return v, nil
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

// lockedPacks is every resource pack and shader in the lock, by key. Keys are
// one namespace, so the two sections never collide.
func (r *Resolver) lockedPacks() map[string]lock.Pack {
	all := make(map[string]lock.Pack, len(r.Lock.ResourcePacks)+len(r.Lock.Shaders))
	for key, p := range r.Lock.ResourcePacks {
		all[key] = p
	}
	for key, p := range r.Lock.Shaders {
		all[key] = p
	}
	return all
}

// packKind reports which section holds a key, when a resource pack or shader
// does. The manifest decides it where the key is listed, so a hand-written entry
// resolves before it has ever been locked.
func (r *Resolver) packKind(key string) (string, bool) {
	if e, listed := r.Manifest.Requires[key]; listed {
		switch e.Kind() {
		case manifest.TypeResourcePack, manifest.TypeShader:
			return e.Kind(), true
		}
		return "", false
	}
	if _, ok := r.Lock.ResourcePacks[key]; ok {
		return manifest.TypeResourcePack, true
	}
	if _, ok := r.Lock.Shaders[key]; ok {
		return manifest.TypeShader, true
	}
	return "", false
}

// removePacks takes the resource packs and shaders out of a remove, leaving the
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
				return nil, out.Errorf("modpack-provided", "%s is provided by modpack %s; remove the modpack or list it in shulker.json yourself", id, from)
			}
		}
		delete(r.Manifest.Requires, id)
		delete(r.packSection(kind), id)
	}
	return mods, nil
}

// reconcilePacks brings the lock's two zip sections in line with shulker.json:
// an entry listed but not locked, or locked against different settings, is
// resolved again, and one the manifest no longer lists is dropped. Entries a
// locked modpack supplied are left alone, because applyLockedPacks owns them.
func (r *Resolver) reconcilePacks(ctx context.Context) error {
	for _, kind := range []string{manifest.TypeResourcePack, manifest.TypeShader} {
		listed := r.Manifest.ResourcePacks()
		if kind == manifest.TypeShader {
			listed = r.Manifest.Shaders()
		}
		section := r.packSection(kind)
		for _, key := range sortedKeys(section) {
			if _, still := listed[key]; !still && section[key].Modpack == "" {
				delete(section, key)
			}
		}
		for _, key := range sortedKeys(listed) {
			locked, ok := section[key]
			if ok && len(project.ZipEntryDifferences(key, listed[key], locked)) == 0 {
				continue
			}
			if err := r.relockPack(ctx, key, kind, listed[key]); err != nil {
				return err
			}
		}
	}
	return nil
}

// relockPack resolves one listed resource pack or shader from its manifest
// entry, the way relock does for a mod.
func (r *Resolver) relockPack(ctx context.Context, key, kind string, entry manifest.Require) error {
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
	v, err := r.pickPack(ctx, p, proj, kind, pin, entry.Channel)
	if err != nil {
		return err
	}
	r.log("fetching %s %s", proj.Slug, v.Number)
	got, err := r.obtain(ctx, proj, v)
	if err != nil {
		return err
	}
	locked := lock.Pack{
		Provider:      p.Name(),
		Project:       lockID(p.Name(), proj.ID),
		Version:       lockID(p.Name(), v.ID),
		VersionNumber: v.Number,
		Filename:      v.File.Filename,
		URL:           got.url,
		Page:          got.page,
		Sha512:        got.sha512,
		Size:          v.File.Size,
		Channel:       channelLabel(entry.Channel),
	}
	if kind == manifest.TypeShader {
		locked.Loader = r.shaderLoader(v)
	}
	r.packSection(kind)[key] = locked
	return nil
}

// splitPackTargets takes the resource packs and shaders out of a command's
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
				return nil, nil, out.Errorf("modpack-provided", "%s is provided by modpack %s; update the modpack, or list it in shulker.json to resolve it here", id, from)
			}
		}
		packs = append(packs, id)
	}
	return mods, packs, nil
}

// updatePacks re-resolves listed resource packs and shaders to the newest
// version their channel allows. No ids means every one the manifest lists.
func (r *Resolver) updatePacks(ctx context.Context, ids []string) error {
	for _, kind := range []string{manifest.TypeResourcePack, manifest.TypeShader} {
		listed := r.Manifest.ResourcePacks()
		if kind == manifest.TypeShader {
			listed = r.Manifest.Shaders()
		}
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

// outdatedPacks reports the listed resource packs and shaders a newer version is
// published for, without changing the lock.
func (r *Resolver) outdatedPacks(ctx context.Context, ids []string) ([]Outdated, error) {
	var res []Outdated
	for _, kind := range []string{manifest.TypeResourcePack, manifest.TypeShader} {
		listed := r.Manifest.ResourcePacks()
		if kind == manifest.TypeShader {
			listed = r.Manifest.Shaders()
		}
		section := r.packSection(kind)
		for _, key := range sortedKeys(listed) {
			locked, ok := section[key]
			if !ok || (len(ids) > 0 && !slices.Contains(ids, key)) {
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
	filename string
	sha512   string
	url      *string
	page     string
	size     int64
}

// lockFiles is everything install has to put in the cache, mods first and then
// the zips, each section in key order.
func (r *Resolver) lockFiles() []downloadable {
	var files []downloadable
	for _, id := range sortedKeys(r.Lock.Mods) {
		m := r.Lock.Mods[id]
		files = append(files, downloadable{id: id, filename: m.Filename, sha512: m.Sha512, url: m.URL, page: pageFor(m), size: m.Size})
	}
	for _, section := range []map[string]lock.Pack{r.Lock.ResourcePacks, r.Lock.Shaders} {
		for _, key := range sortedKeys(section) {
			p := section[key]
			files = append(files, downloadable{id: key, filename: p.Filename, sha512: p.Sha512, url: p.URL, page: packPage(p), size: p.Size})
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
